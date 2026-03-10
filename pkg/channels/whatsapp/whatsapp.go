package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/igorsilveira/pincer/pkg/channels"
	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Adapter struct {
	client    *whatsmeow.Client
	dbPath    string
	allowList map[string]struct{}
	inbound   chan channels.InboundMessage
	sessions  *channels.SessionMap[types.JID]
}

func New(dbPath string, allowList []string) (*Adapter, error) {
	if dbPath == "" {
		dbPath = os.Getenv("WHATSAPP_DB_PATH")
	}
	if dbPath == "" {
		dbPath = "whatsapp.db"
	}
	al := make(map[string]struct{}, len(allowList))
	for _, v := range allowList {
		al[v] = struct{}{}
	}
	return &Adapter{
		dbPath:    dbPath,
		allowList: al,
		inbound:   make(chan channels.InboundMessage, 256),
		sessions:  channels.NewSessionMap[types.JID]("wa", func(k types.JID) string { return k.User }),
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

	slog.Info("whatsapp adapter started", slog.Int("allow_list_size", len(a.allowList)))

	if a.client.Store.ID == nil {

		qrChan, err := a.client.GetQRChannel(ctx)
		if err != nil {
			return fmt.Errorf("whatsapp: getting QR channel: %w", err)
		}
		err = a.client.Connect()
		if err != nil {
			return fmt.Errorf("whatsapp: connecting: %w", err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				qr, err := qrcode.New(evt.Code, qrcode.Medium)
				if err != nil {
					fmt.Printf("WhatsApp QR code: %s\n", evt.Code)
					continue
				}
				fmt.Println("Scan this QR code with WhatsApp:")
				fmt.Println(qr.ToSmallString(false))
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
	jid, ok := a.sessions.Reverse(msg.SessionID)
	if !ok {
		return fmt.Errorf("whatsapp: no chat for session %s", msg.SessionID)
	}

	formatted := markdownToWhatsApp(msg.Content)
	chunks := channels.SplitMessage(formatted, 65536)
	for _, chunk := range chunks {
		if _, err := a.client.SendMessage(ctx, jid, &waE2E.Message{
			Conversation: new(chunk),
		}); err != nil {
			return err
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

func (a *Adapter) SendTyping(ctx context.Context, sessionID string) error {
	jid, ok := a.sessions.Reverse(sessionID)
	if !ok {
		return fmt.Errorf("whatsapp: no chat for session %s", sessionID)
	}
	return a.client.SendChatPresence(ctx, jid, types.ChatPresenceComposing, types.ChatPresenceMediaText)
}

func (a *Adapter) handleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		if v.Message == nil {
			return
		}

		if len(a.allowList) > 0 {
			senderUser := v.Info.Sender.User
			if _, ok := a.allowList[senderUser]; !ok {
				slog.Info("whatsapp message blocked by allow_list",
					slog.String("sender_user", senderUser),
					slog.String("sender_full", v.Info.Sender.String()),
					slog.String("chat", v.Info.Chat.String()),
				)
				return
			}
		}

		text := v.Message.GetConversation()
		if text == "" && v.Message.GetExtendedTextMessage() != nil {
			text = v.Message.GetExtendedTextMessage().GetText()
		}
		if text == "" {
			return
		}

		sender := v.Info.Sender.ToNonAD()
		sessionID := a.sessions.GetOrCreate(sender)

		a.inbound <- channels.InboundMessage{
			ChannelName: "whatsapp",
			SessionID:   sessionID,
			PeerID:      sender.String(),
			Content:     text,
		}
	}
}
