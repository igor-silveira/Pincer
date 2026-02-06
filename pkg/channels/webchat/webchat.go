package webchat

import (
	"context"
	"log/slog"
	"sync"

	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type Adapter struct {
	inbound chan channels.InboundMessage
	clients map[string]*Client
	mu      sync.RWMutex
	done    chan struct{}
}

type Client struct {
	SessionID string
	Send      chan string
}

func New() *Adapter {
	return &Adapter{
		inbound: make(chan channels.InboundMessage, 256),
		clients: make(map[string]*Client),
		done:    make(chan struct{}),
	}
}

func (a *Adapter) Name() string { return "webchat" }

func (a *Adapter) Start(ctx context.Context) error {
	logger := telemetry.FromContext(ctx)
	logger.Info("webchat adapter started")
	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	close(a.done)
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, c := range a.clients {
		close(c.Send)
		delete(a.clients, id)
	}
	return nil
}

func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	a.mu.RLock()
	client, ok := a.clients[msg.SessionID]
	a.mu.RUnlock()

	if !ok {
		slog.Warn("webchat: no client for session", slog.String("session_id", msg.SessionID))
		return nil
	}

	select {
	case client.Send <- msg.Content:
	default:
		slog.Warn("webchat: client send buffer full", slog.String("session_id", msg.SessionID))
	}
	return nil
}

func (a *Adapter) Receive() <-chan channels.InboundMessage {
	return a.inbound
}

func (a *Adapter) Capabilities() channels.ChannelCaps {
	return channels.ChannelCaps{
		SupportsStreaming: true,
		SupportsMedia:     false,
		SupportsReactions: false,
	}
}

func (a *Adapter) RegisterClient(sessionID string) *Client {
	client := &Client{
		SessionID: sessionID,
		Send:      make(chan string, 64),
	}
	a.mu.Lock()
	a.clients[sessionID] = client
	a.mu.Unlock()
	return client
}

func (a *Adapter) UnregisterClient(sessionID string) {
	a.mu.Lock()
	if c, ok := a.clients[sessionID]; ok {
		close(c.Send)
		delete(a.clients, sessionID)
	}
	a.mu.Unlock()
}

func (a *Adapter) PushInbound(msg channels.InboundMessage) {
	select {
	case a.inbound <- msg:
	default:
		slog.Warn("webchat: inbound buffer full, dropping message")
	}
}
