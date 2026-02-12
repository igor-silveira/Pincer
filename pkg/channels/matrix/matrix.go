package matrix

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/telemetry"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type Adapter struct {
	client     *mautrix.Client
	homeserver string
	userID     string
	token      string
	inbound    chan channels.InboundMessage
	sessions   map[id.RoomID]string
	mu         sync.RWMutex
}

type Config struct {
	Homeserver  string
	UserID      string
	AccessToken string
}

func New(cfg Config) (*Adapter, error) {
	if cfg.Homeserver == "" {
		cfg.Homeserver = os.Getenv("MATRIX_HOMESERVER")
	}
	if cfg.UserID == "" {
		cfg.UserID = os.Getenv("MATRIX_USER_ID")
	}
	if cfg.AccessToken == "" {
		cfg.AccessToken = os.Getenv("MATRIX_ACCESS_TOKEN")
	}

	if cfg.Homeserver == "" || cfg.UserID == "" || cfg.AccessToken == "" {
		return nil, fmt.Errorf("matrix: homeserver, user_id, and access_token are required")
	}

	return &Adapter{
		homeserver: cfg.Homeserver,
		userID:     cfg.UserID,
		token:      cfg.AccessToken,
		inbound:    make(chan channels.InboundMessage, 256),
		sessions:   make(map[id.RoomID]string),
	}, nil
}

func (a *Adapter) Name() string { return "matrix" }

func (a *Adapter) Start(ctx context.Context) error {
	logger := telemetry.FromContext(ctx)

	client, err := mautrix.NewClient(a.homeserver, id.UserID(a.userID), a.token)
	if err != nil {
		return fmt.Errorf("matrix: creating client: %w", err)
	}
	a.client = client

	syncer := client.Syncer.(*mautrix.DefaultSyncer)
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		a.handleMessage(evt)
	})

	logger.Info("matrix adapter started", "homeserver", a.homeserver)

	go func() {
		if err := client.SyncWithContext(ctx); err != nil {
			logger.Error("matrix sync error", "err", err.Error())
		}
	}()

	return nil
}

func (a *Adapter) Stop(_ context.Context) error {
	if a.client != nil {
		a.client.StopSync()
	}
	return nil
}

func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	a.mu.RLock()
	var roomID id.RoomID
	for rid, sid := range a.sessions {
		if sid == msg.SessionID {
			roomID = rid
			break
		}
	}
	a.mu.RUnlock()

	if roomID == "" {
		return fmt.Errorf("matrix: no room for session %s", msg.SessionID)
	}

	_, err := a.client.SendText(ctx, roomID, msg.Content)
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

func (a *Adapter) handleMessage(evt *event.Event) {

	if evt.Sender.String() == a.userID {
		return
	}

	content := evt.Content.AsMessage()
	if content == nil || content.Body == "" {
		return
	}

	sessionID := a.getOrCreateSession(evt.RoomID)

	a.inbound <- channels.InboundMessage{
		ChannelName: "matrix",
		SessionID:   sessionID,
		PeerID:      evt.Sender.String(),
		Content:     content.Body,
	}
}

func (a *Adapter) getOrCreateSession(roomID id.RoomID) string {
	a.mu.RLock()
	sid, ok := a.sessions[roomID]
	a.mu.RUnlock()

	if ok {
		return sid
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if sid, ok := a.sessions[roomID]; ok {
		return sid
	}

	sid = fmt.Sprintf("mx-%s", roomID)
	a.sessions[roomID] = sid
	return sid
}
