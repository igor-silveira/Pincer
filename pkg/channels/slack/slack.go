package slack

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/telemetry"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

type Adapter struct {
	botToken string
	appToken string
	client   *slackapi.Client
	socket   *socketmode.Client
	inbound  chan channels.InboundMessage
	sessions map[string]string
	mu       sync.RWMutex
	done     chan struct{}
}

func New(botToken, appToken string) (*Adapter, error) {
	if botToken == "" {
		botToken = os.Getenv("SLACK_BOT_TOKEN")
	}
	if appToken == "" {
		appToken = os.Getenv("SLACK_APP_TOKEN")
	}
	if botToken == "" || appToken == "" {
		return nil, fmt.Errorf("slack: bot token and app token required (set SLACK_BOT_TOKEN and SLACK_APP_TOKEN)")
	}
	return &Adapter{
		botToken: botToken,
		appToken: appToken,
		inbound:  make(chan channels.InboundMessage, 256),
		sessions: make(map[string]string),
		done:     make(chan struct{}),
	}, nil
}

func (a *Adapter) Name() string { return "slack" }

func (a *Adapter) Start(ctx context.Context) error {
	logger := telemetry.FromContext(ctx)

	a.client = slackapi.New(
		a.botToken,
		slackapi.OptionAppLevelToken(a.appToken),
	)

	a.socket = socketmode.New(a.client)

	go a.listenEvents(ctx)
	go a.socket.Run()

	logger.Info("slack adapter started")
	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	close(a.done)
	return nil
}

func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.client == nil {
		return fmt.Errorf("slack: not connected")
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
		return fmt.Errorf("slack: no channel for session %s", msg.SessionID)
	}

	_, _, err := a.client.PostMessageContext(ctx, channelID,
		slackapi.MsgOptionText(msg.Content, false),
	)
	return err
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
	if a.client == nil {
		return fmt.Errorf("slack: not connected")
	}

	a.mu.RLock()
	var channelID string
	for cid, sid := range a.sessions {
		if sid == req.SessionID {
			channelID = cid
			break
		}
	}
	a.mu.RUnlock()

	if channelID == "" {
		return fmt.Errorf("slack: no channel for session %s", req.SessionID)
	}

	headerText := slackapi.NewTextBlockObject(slackapi.MarkdownType,
		fmt.Sprintf("*Tool approval needed: %s*\nInput: `%s`", req.ToolName, req.Input), false, false)
	headerSection := slackapi.NewSectionBlock(headerText, nil, nil)

	approveBtn := slackapi.NewButtonBlockElement("approval:approve:"+req.RequestID, "approve",
		slackapi.NewTextBlockObject(slackapi.PlainTextType, "Approve", false, false))
	approveBtn.Style = slackapi.StylePrimary

	denyBtn := slackapi.NewButtonBlockElement("approval:deny:"+req.RequestID, "deny",
		slackapi.NewTextBlockObject(slackapi.PlainTextType, "Deny", false, false))
	denyBtn.Style = slackapi.StyleDanger

	actions := slackapi.NewActionBlock("approval_actions", approveBtn, denyBtn)

	_, _, err := a.client.PostMessageContext(ctx, channelID,
		slackapi.MsgOptionBlocks(headerSection, actions),
	)
	return err
}

func (a *Adapter) listenEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.done:
			return
		case evt, ok := <-a.socket.Events:
			if !ok {
				return
			}
			a.handleEvent(evt)
		}
	}
}

func (a *Adapter) handleEvent(evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		data, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		a.socket.Ack(*evt.Request)
		a.handleEventsAPI(data)

	case socketmode.EventTypeInteractive:
		callback, ok := evt.Data.(slackapi.InteractionCallback)
		if !ok {
			return
		}
		a.socket.Ack(*evt.Request)
		a.handleInteraction(callback)
	}
}

func (a *Adapter) handleInteraction(callback slackapi.InteractionCallback) {
	if len(callback.ActionCallback.BlockActions) == 0 {
		return
	}

	action := callback.ActionCallback.BlockActions[0]
	parts := strings.SplitN(action.ActionID, ":", 3)
	if len(parts) != 3 || parts[0] != "approval" {
		return
	}

	act := parts[1]
	requestID := parts[2]
	approved := act == "approve"

	channelID := callback.Channel.ID
	sessionID := a.sessionForChannel(channelID)

	a.inbound <- channels.InboundMessage{
		ChannelName: "slack",
		SessionID:   sessionID,
		PeerID:      callback.User.ID,
		ApprovalResponse: &channels.InboundApprovalResponse{
			RequestID: requestID,
			Approved:  approved,
		},
	}
}

func (a *Adapter) handleEventsAPI(event slackevents.EventsAPIEvent) {
	switch event.Type {
	case slackevents.CallbackEvent:
		inner := event.InnerEvent
		switch ev := inner.Data.(type) {
		case *slackevents.MessageEvent:
			if ev.SubType != "" || ev.Text == "" {
				return
			}

			sessionID := a.getOrCreateSession(ev.Channel)

			slog.Debug("slack message received",
				slog.String("channel", ev.Channel),
				slog.String("user", ev.User),
			)

			a.inbound <- channels.InboundMessage{
				ChannelName: "slack",
				SessionID:   sessionID,
				PeerID:      ev.User,
				Content:     ev.Text,
			}
		}
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

	sid = fmt.Sprintf("sl-%s", channelID)
	a.sessions[channelID] = sid
	return sid
}

func (a *Adapter) sessionForChannel(channelID string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.sessions[channelID]
}
