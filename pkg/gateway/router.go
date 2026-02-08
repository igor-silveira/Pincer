package gateway

import (
	"context"
	"log/slog"

	"github.com/igorsilveira/pincer/pkg/agent"
	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type ChannelRouter struct {
	runtime  *agent.Runtime
	adapters []channels.Adapter
	logger   *slog.Logger
}

func NewChannelRouter(runtime *agent.Runtime, adapters []channels.Adapter, logger *slog.Logger) *ChannelRouter {
	return &ChannelRouter{
		runtime:  runtime,
		adapters: adapters,
		logger:   logger,
	}
}

func (cr *ChannelRouter) Start(ctx context.Context) {
	for _, adapter := range cr.adapters {
		go cr.listenAdapter(ctx, adapter)
	}
}

func (cr *ChannelRouter) listenAdapter(ctx context.Context, adapter channels.Adapter) {
	ch := adapter.Receive()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			go cr.handleMessage(ctx, adapter, msg)
		}
	}
}

func (cr *ChannelRouter) handleMessage(ctx context.Context, adapter channels.Adapter, msg channels.InboundMessage) {
	logger := telemetry.FromContext(ctx)

	logger.Info("routing message",
		slog.String("channel", msg.ChannelName),
		slog.String("session_id", msg.SessionID),
		slog.String("peer_id", msg.PeerID),
	)

	events, err := cr.runtime.RunTurn(ctx, msg.SessionID, msg.Content)
	if err != nil {
		logger.Error("agent turn failed",
			slog.String("channel", msg.ChannelName),
			slog.String("err", err.Error()),
		)
		return
	}

	var fullResponse string
	for ev := range events {
		switch ev.Type {
		case agent.TurnDone:
			fullResponse = ev.Message
		case agent.TurnError:
			logger.Error("agent error during turn",
				slog.String("channel", msg.ChannelName),
				slog.String("err", ev.Error.Error()),
			)
			fullResponse = "Sorry, I encountered an error processing your message."
		}
	}

	if fullResponse == "" {
		return
	}

	if err := adapter.Send(ctx, channels.OutboundMessage{
		SessionID: msg.SessionID,
		Content:   fullResponse,
	}); err != nil {
		logger.Error("failed to send response",
			slog.String("channel", msg.ChannelName),
			slog.String("err", err.Error()),
		)
	}
}
