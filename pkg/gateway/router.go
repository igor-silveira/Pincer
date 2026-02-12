package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/igorsilveira/pincer/pkg/agent"
	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type ChannelRouter struct {
	runtime  *agent.Runtime
	adapters []channels.Adapter
	approver *agent.Approver
	logger   *slog.Logger
}

func NewChannelRouter(runtime *agent.Runtime, adapters []channels.Adapter, approver *agent.Approver, logger *slog.Logger) *ChannelRouter {
	return &ChannelRouter{
		runtime:  runtime,
		adapters: adapters,
		approver: approver,
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
			if msg.ApprovalResponse != nil {
				cr.approver.Respond(agent.ApprovalResponse{
					RequestID: msg.ApprovalResponse.RequestID,
					Approved:  msg.ApprovalResponse.Approved,
				})
				continue
			}
			if resp, ok := parseTextApproval(msg.Content); ok {
				cr.approver.Respond(agent.ApprovalResponse{
					RequestID: resp.RequestID,
					Approved:  resp.Approved,
				})
				continue
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
		case agent.TurnApprovalNeeded:
			cr.sendApprovalRequest(ctx, adapter, msg.SessionID, ev.ApprovalRequest)
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

func (cr *ChannelRouter) sendApprovalRequest(ctx context.Context, adapter channels.Adapter, sessionID string, req *agent.ApprovalRequest) {
	if req == nil {
		return
	}

	channelReq := channels.ApprovalRequest{
		RequestID: req.ID,
		SessionID: sessionID,
		ToolName:  req.ToolName,
		Input:     req.Input,
	}

	if sender, ok := adapter.(channels.ApprovalSender); ok {
		if err := sender.SendApprovalRequest(ctx, channelReq); err != nil {
			cr.logger.Error("failed to send approval request via adapter",
				slog.String("channel", adapter.Name()),
				slog.String("err", err.Error()),
			)
		}
		return
	}

	text := fmt.Sprintf("Tool approval needed: %s\nInput: %s\n\nReply with:\n  approve %s\n  deny %s", req.ToolName, req.Input, req.ID, req.ID)
	if err := adapter.Send(ctx, channels.OutboundMessage{
		SessionID: sessionID,
		Content:   text,
	}); err != nil {
		cr.logger.Error("failed to send text approval request",
			slog.String("channel", adapter.Name()),
			slog.String("err", err.Error()),
		)
	}
}

func parseTextApproval(text string) (channels.InboundApprovalResponse, bool) {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)

	if strings.HasPrefix(lower, "approve ") {
		id := strings.TrimSpace(text[len("approve "):])
		if id == "" {
			return channels.InboundApprovalResponse{}, false
		}
		return channels.InboundApprovalResponse{RequestID: id, Approved: true}, true
	}

	if strings.HasPrefix(lower, "deny ") {
		id := strings.TrimSpace(text[len("deny "):])
		if id == "" {
			return channels.InboundApprovalResponse{}, false
		}
		return channels.InboundApprovalResponse{RequestID: id, Approved: false}, true
	}

	return channels.InboundApprovalResponse{}, false
}
