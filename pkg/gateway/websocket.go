package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"github.com/igorsilveira/pincer/pkg/agent"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type wsIncoming struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	Content   string `json:"content"`
	RequestID string `json:"request_id,omitempty"`
	Approved  *bool  `json:"approved,omitempty"`
}

type wsOutgoing struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	Content   string `json:"content,omitempty"`
	Error     string `json:"error,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	ToolInput string `json:"tool_input,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func (g *Gateway) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		g.logger.Error("websocket accept failed", slog.String("err", err.Error()))
		return
	}
	defer func() { _ = conn.CloseNow() }()

	telemetry.Metrics.ActiveConnections.Inc()
	defer telemetry.Metrics.ActiveConnections.Dec()

	sessionID := uuid.NewString()
	client := g.chat.RegisterClient(sessionID)
	defer g.chat.UnregisterClient(sessionID)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	g.logger.Info("webchat client connected", slog.String("session_id", sessionID))

	var writeMu sync.Mutex
	wsWrite := func(msg wsOutgoing) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = wsjson.Write(ctx, conn, msg)
	}

	wsWrite(wsOutgoing{
		Type:      "session",
		SessionID: sessionID,
	})

	go func() {
		for {
			select {
			case msg, ok := <-client.Send:
				if !ok {
					return
				}
				wsWrite(wsOutgoing{
					Type:      "message",
					SessionID: sessionID,
					Content:   msg,
				})
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
				websocket.CloseStatus(err) == websocket.StatusGoingAway {
				g.logger.Info("webchat client disconnected", slog.String("session_id", sessionID))
			} else {
				g.logger.Error("websocket read error", slog.String("err", err.Error()))
			}
			return
		}

		var incoming wsIncoming
		if err := json.Unmarshal(data, &incoming); err != nil {
			wsWrite(wsOutgoing{
				Type:  "error",
				Error: "invalid message format",
			})
			continue
		}

		if incoming.Type == "approval_response" && incoming.RequestID != "" && incoming.Approved != nil {
			if g.approver != nil {
				g.approver.Respond(agent.ApprovalResponse{
					RequestID: incoming.RequestID,
					Approved:  *incoming.Approved,
				})
			}
			continue
		}

		if incoming.Content == "" {
			continue
		}

		events, err := g.runtime.RunTurn(ctx, sessionID, incoming.Content)
		if err != nil {
			g.logger.Error("agent turn failed", slog.String("err", err.Error()))
			wsWrite(wsOutgoing{
				Type:  "error",
				Error: "failed to process message",
			})
			continue
		}

		go func() {
			for ev := range events {
				switch ev.Type {
				case agent.TurnToken:
					wsWrite(wsOutgoing{
						Type:      "token",
						SessionID: sessionID,
						Content:   ev.Token,
					})
				case agent.TurnToolCall:
					wsWrite(wsOutgoing{
						Type:      "tool_call",
						SessionID: sessionID,
						Content:   ev.Message,
						ToolName:  ev.ToolCall.Name,
						ToolInput: string(ev.ToolCall.Input),
					})
				case agent.TurnToolResult:
					wsWrite(wsOutgoing{
						Type:      "tool_result",
						SessionID: sessionID,
						Content:   ev.Message,
					})
				case agent.TurnApprovalNeeded:
					wsWrite(wsOutgoing{
						Type:      "approval_request",
						SessionID: sessionID,
						RequestID: ev.ApprovalRequest.ID,
						ToolName:  ev.ApprovalRequest.ToolName,
						ToolInput: ev.ApprovalRequest.Input,
					})
				case agent.TurnProgress:
					wsWrite(wsOutgoing{
						Type:      "progress",
						SessionID: sessionID,
						Content:   ev.Message,
					})
				case agent.TurnDone:
					wsWrite(wsOutgoing{
						Type:      "done",
						SessionID: sessionID,
						Content:   ev.Message,
					})
				case agent.TurnError:
					wsWrite(wsOutgoing{
						Type:  "error",
						Error: ev.Error.Error(),
					})
				}
			}
		}()
	}
}
