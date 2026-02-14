package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorsilveira/pincer/pkg/agent"
	"github.com/igorsilveira/pincer/pkg/audit"
	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/store"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type ChannelRouter struct {
	runtime  *agent.Runtime
	adapters []channels.Adapter
	approver *agent.Approver
	logger   *slog.Logger
	store    *store.Store
	audit    *audit.Logger
}

func NewChannelRouter(runtime *agent.Runtime, adapters []channels.Adapter, approver *agent.Approver, logger *slog.Logger, db *store.Store, auditLog *audit.Logger) *ChannelRouter {
	return &ChannelRouter{
		runtime:  runtime,
		adapters: adapters,
		approver: approver,
		logger:   logger,
		store:    db,
		audit:    auditLog,
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

	if cr.store != nil {
		cr.ensureSession(ctx, msg)
	}

	events, err := cr.runtime.RunTurn(ctx, msg.SessionID, msg.Content)
	if err != nil {
		logger.Error("agent turn failed",
			slog.String("channel", msg.ChannelName),
			slog.String("err", err.Error()),
		)
		return
	}

	fullResponse := cr.consumeTurnEvents(ctx, logger, adapter, msg.SessionID, events,
		"Sorry, I encountered an error processing your message.")

	if fullResponse == "" {
		return
	}

	if err := adapter.Send(ctx, channels.OutboundMessage{
		SessionID: msg.SessionID,
		Content:   fullResponse,
	}); err != nil {
		logger.Error("failed to send response",
			slog.String("session_id", msg.SessionID),
			slog.String("err", err.Error()),
		)
	}
}

func (cr *ChannelRouter) consumeTurnEvents(ctx context.Context, logger *slog.Logger, adapter channels.Adapter, sessionID string, events <-chan agent.TurnEvent, errorMessage string) string {
	var fullResponse string
	for ev := range events {
		switch ev.Type {
		case agent.TurnApprovalNeeded:
			cr.sendApprovalRequest(ctx, adapter, sessionID, ev.ApprovalRequest)
		case agent.TurnDone:
			fullResponse = ev.Message
		case agent.TurnError:
			logger.Error("agent error during turn",
				slog.String("session_id", sessionID),
				slog.String("err", ev.Error.Error()),
			)
			fullResponse = errorMessage
		}
	}
	return fullResponse
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

func (cr *ChannelRouter) ensureSession(ctx context.Context, msg channels.InboundMessage) {
	sess, err := cr.store.GetSession(ctx, msg.SessionID)
	if err == nil {
		if sess.Channel != msg.ChannelName || sess.PeerID != msg.PeerID {
			if err := cr.store.UpdateSessionChannel(ctx, msg.SessionID, msg.ChannelName, msg.PeerID); err != nil {
				cr.logger.Warn("failed to update session channel",
					slog.String("session_id", msg.SessionID),
					slog.String("err", err.Error()),
				)
			}
		}
		return
	}

	now := time.Now().UTC()
	newSess := &store.Session{
		ID:        msg.SessionID,
		AgentID:   "default",
		Channel:   msg.ChannelName,
		PeerID:    msg.PeerID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := cr.store.CreateSession(ctx, newSess); err != nil {
		cr.logger.Debug("session already exists or create failed",
			slog.String("session_id", msg.SessionID),
			slog.String("err", err.Error()),
		)
	}
}

func (cr *ChannelRouter) adapterForSession(ctx context.Context, sessionID string) (channels.Adapter, error) {
	if cr.store == nil {
		return nil, fmt.Errorf("no store configured")
	}

	sess, err := cr.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("looking up session: %w", err)
	}

	for _, a := range cr.adapters {
		if a.Name() == sess.Channel {
			return a, nil
		}
	}

	return nil, fmt.Errorf("no adapter found for channel %q", sess.Channel)
}

func (cr *ChannelRouter) SendToSession(ctx context.Context, sessionID, content string) error {
	adapter, err := cr.adapterForSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("resolving adapter: %w", err)
	}

	if cr.store != nil {
		msg := &store.Message{
			ID:          uuid.NewString(),
			SessionID:   sessionID,
			Role:        "assistant",
			ContentType: store.ContentTypeText,
			Content:     content,
			CreatedAt:   time.Now().UTC(),
		}
		if err := cr.store.AppendMessage(ctx, msg); err != nil {
			cr.logger.Error("notify: failed to persist send message",
				slog.String("session_id", sessionID),
				slog.String("err", err.Error()),
			)
		}
	}

	cr.AuditLog(ctx, audit.EventNotifySend, sessionID, fmt.Sprintf("len=%d", len(content)))

	return adapter.Send(ctx, channels.OutboundMessage{
		SessionID: sessionID,
		Content:   content,
	})
}

func (cr *ChannelRouter) RunAndDeliver(ctx context.Context, sessionID, prompt string) {
	ctx = agent.WithAutoApprove(ctx)
	logger := telemetry.FromContext(ctx)

	adapter, err := cr.adapterForSession(ctx, sessionID)
	if err != nil {
		logger.Error("notify: cannot resolve adapter",
			slog.String("session_id", sessionID),
			slog.String("err", err.Error()),
		)
		return
	}

	turnID := uuid.NewString()
	logger.Info("notify: starting scheduled turn",
		slog.String("session_id", sessionID),
		slog.String("turn_id", turnID),
	)

	cr.AuditLog(ctx, audit.EventNotifyDeliver, sessionID, fmt.Sprintf("turn_id=%s prompt=%s", turnID, prompt))

	events, err := cr.runtime.RunTurn(ctx, sessionID, prompt)
	if err != nil {
		logger.Error("notify: agent turn failed",
			slog.String("session_id", sessionID),
			slog.String("err", err.Error()),
		)
		return
	}

	fullResponse := cr.consumeTurnEvents(ctx, logger, adapter, sessionID, events,
		"Sorry, I encountered an error processing a scheduled task.")

	if fullResponse == "" {
		return
	}

	if err := adapter.Send(ctx, channels.OutboundMessage{
		SessionID: sessionID,
		Content:   fullResponse,
	}); err != nil {
		logger.Error("failed to send response",
			slog.String("session_id", sessionID),
			slog.String("err", err.Error()),
		)
	}
}

func (cr *ChannelRouter) AuditLog(ctx context.Context, eventType, sessionID, detail string) {
	if cr.audit == nil {
		return
	}
	_ = cr.audit.Log(ctx, eventType, sessionID, "", "notify", detail)
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
