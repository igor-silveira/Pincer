package discord

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type Adapter struct {
	token    string
	session  *discordgo.Session
	inbound  chan channels.InboundMessage
	sessions *channels.SessionMap[string]
	done     chan struct{}
}

func New(token string) (*Adapter, error) {
	if token == "" {
		token = os.Getenv("DISCORD_BOT_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("discord: bot token not set")
	}
	return &Adapter{
		token:    token,
		inbound:  make(chan channels.InboundMessage, 256),
		sessions: channels.NewSessionMap[string]("dc", func(k string) string { return k }),
		done:     make(chan struct{}),
	}, nil
}

func (a *Adapter) Name() string { return "discord" }

func (a *Adapter) Start(ctx context.Context) error {
	logger := telemetry.FromContext(ctx)

	dg, err := discordgo.New("Bot " + a.token)
	if err != nil {
		return fmt.Errorf("discord: creating session: %w", err)
	}

	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentMessageContent
	dg.AddHandler(a.handleMessage)
	dg.AddHandler(a.handleInteraction)

	if err := dg.Open(); err != nil {
		return fmt.Errorf("discord: opening connection: %w", err)
	}

	a.session = dg
	logger.Info("discord adapter started")

	go func() {
		select {
		case <-ctx.Done():
			dg.Close()
		case <-a.done:
		}
	}()

	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	close(a.done)
	if a.session != nil {
		return a.session.Close()
	}
	return nil
}

func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.session == nil {
		return fmt.Errorf("discord: not connected")
	}

	channelID, ok := a.sessions.Reverse(msg.SessionID)
	if !ok {
		return fmt.Errorf("discord: no channel for session %s", msg.SessionID)
	}

	content := msg.Content
	for len(content) > 0 {
		chunk := content
		if len(chunk) > 2000 {
			chunk = content[:2000]
			content = content[2000:]
		} else {
			content = ""
		}
		if _, err := a.session.ChannelMessageSend(channelID, chunk); err != nil {
			return fmt.Errorf("discord: sending message: %w", err)
		}
	}

	return nil
}

func (a *Adapter) Receive() <-chan channels.InboundMessage {
	return a.inbound
}

func (a *Adapter) Capabilities() channels.ChannelCaps {
	return channels.ChannelCaps{
		SupportsStreaming: false,
		SupportsMedia:     true,
		SupportsReactions: true,
	}
}

func (a *Adapter) SendApprovalRequest(ctx context.Context, req channels.ApprovalRequest) error {
	if a.session == nil {
		return fmt.Errorf("discord: not connected")
	}

	channelID, ok := a.sessions.Reverse(req.SessionID)
	if !ok {
		return fmt.Errorf("discord: no channel for session %s", req.SessionID)
	}

	content := fmt.Sprintf("**Tool approval needed: %s**\nInput: `%s`", req.ToolName, req.Input)

	_, err := a.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: content,
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "Approve",
						Style:    discordgo.SuccessButton,
						CustomID: "approval:approve:" + req.RequestID,
					},
					discordgo.Button{
						Label:    "Deny",
						Style:    discordgo.DangerButton,
						CustomID: "approval:deny:" + req.RequestID,
					},
				},
			},
		},
	})
	return err
}

func (a *Adapter) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	customID := i.MessageComponentData().CustomID
	parts := strings.SplitN(customID, ":", 3)
	if len(parts) != 3 || parts[0] != "approval" {
		return
	}

	action := parts[1]
	requestID := parts[2]
	approved := action == "approve"

	status := "Approved"
	if !approved {
		status = "Denied"
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    i.Message.Content + "\n\n**" + status + "**",
			Components: []discordgo.MessageComponent{},
		},
	})

	sessionID, _ := a.sessions.Lookup(i.ChannelID)
	peerID := ""
	if i.Member != nil && i.Member.User != nil {
		peerID = i.Member.User.ID
	} else if i.User != nil {
		peerID = i.User.ID
	}

	a.inbound <- channels.InboundMessage{
		ChannelName: "discord",
		SessionID:   sessionID,
		PeerID:      peerID,
		ApprovalResponse: &channels.InboundApprovalResponse{
			RequestID: requestID,
			Approved:  approved,
		},
	}
}

func (a *Adapter) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {

	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "" {
		return
	}

	sessionID := a.sessions.GetOrCreate(m.ChannelID)

	slog.Debug("discord message received",
		slog.String("channel_id", m.ChannelID),
		slog.String("author", m.Author.Username),
	)

	a.inbound <- channels.InboundMessage{
		ChannelName: "discord",
		SessionID:   sessionID,
		PeerID:      m.Author.ID,
		Content:     m.Content,
	}
}

