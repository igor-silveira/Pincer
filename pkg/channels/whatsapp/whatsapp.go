package whatsapp

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/igorsilveira/pincer/pkg/channels"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Adapter struct {
	client   *whatsmeow.Client
	dbPath   string
	inbound  chan channels.InboundMessage
	sessions map[types.JID]string
	mu       sync.RWMutex
}

func New(dbPath string) (*Adapter, error) {
	if dbPath == "" {
		dbPath = os.Getenv("WHATSAPP_DB_PATH")
	}
	if dbPath == "" {
		dbPath = "whatsapp.db"
	}
	return &Adapter{
		dbPath:   dbPath,
		inbound:  make(chan channels.InboundMessage, 256),
		sessions: make(map[types.JID]string),
	}, nil
}

func (a *Adapter) Name() string { return "whatsapp" }

func (a *Adapter) Start(ctx context.Context) error {
	container, err := sqlstore.New(ctx, "sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", a.dbPath), waLog.Noop)
	if err != nil {
		return fmt.Errorf("whatsapp: opening session store: %w", err)
	}

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp: getting device: %w", err)
	}

	a.client = whatsmeow.NewClient(device, waLog.Noop)
	a.client.AddEventHandler(a.handleEvent)

	if a.client.Store.ID == nil {

		qrChan, _ := a.client.GetQRChannel(ctx)
		err = a.client.Connect()
		if err != nil {
			return fmt.Errorf("whatsapp: connecting: %w", err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Printf("WhatsApp QR code: %s\n", evt.Code)
			}
		}
	} else {
		err = a.client.Connect()
		if err != nil {
			return fmt.Errorf("whatsapp: connecting: %w", err)
		}
	}

	return nil
}

func (a *Adapter) Stop(_ context.Context) error {
	if a.client != nil {
		a.client.Disconnect()
	}
	return nil
}

func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	a.mu.RLock()
	var jid types.JID
	for j, sid := range a.sessions {
		if sid == msg.SessionID {
			jid = j
			break
		}
	}
	a.mu.RUnlock()

	if jid.IsEmpty() {
		return fmt.Errorf("whatsapp: no chat for session %s", msg.SessionID)
	}

	text := msg.Content
	_, err := a.client.SendMessage(ctx, jid, &waE2E.Message{
		Conversation: &text,
	})
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

func (a *Adapter) handleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		if v.Message == nil {
			return
		}

		text := v.Message.GetConversation()
		if text == "" && v.Message.GetExtendedTextMessage() != nil {
			text = v.Message.GetExtendedTextMessage().GetText()
		}
		if text == "" {
			return
		}

		sessionID := a.getOrCreateSession(v.Info.Sender)

		a.inbound <- channels.InboundMessage{
			ChannelName: "whatsapp",
			SessionID:   sessionID,
			PeerID:      v.Info.Sender.String(),
			Content:     text,
		}
	}
}

func (a *Adapter) getOrCreateSession(jid types.JID) string {
	a.mu.RLock()
	sid, ok := a.sessions[jid]
	a.mu.RUnlock()

	if ok {
		return sid
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if sid, ok := a.sessions[jid]; ok {
		return sid
	}

	sid = fmt.Sprintf("wa-%s", jid.User)
	a.sessions[jid] = sid
	return sid
}
