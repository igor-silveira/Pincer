package discord

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type Adapter struct {
	token    string
	session  *discordgo.Session
	inbound  chan channels.InboundMessage
	sessions map[string]string
	mu       sync.RWMutex
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
		sessions: make(map[string]string),
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

	a.mu.RLock()
	var channelID string
	for cid, sid := range a.sessions {
		if sid == msg.SessionID {
			channelID = cid
			break
		}
	}
	a.mu.RUnlock()

	if channelID == "" {
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

func (a *Adapter) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {

	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "" {
		return
	}

	sessionID := a.getOrCreateSession(m.ChannelID)

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

func (a *Adapter) getOrCreateSession(channelID string) string {
	a.mu.RLock()
	sid, ok := a.sessions[channelID]
	a.mu.RUnlock()

	if ok {
		return sid
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if sid, ok := a.sessions[channelID]; ok {
		return sid
	}

	sid = fmt.Sprintf("dc-%s", channelID)
	a.sessions[channelID] = sid
	return sid
}
