package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type NotifyTool struct {
	RunAndDeliver func(ctx context.Context, sessionID, prompt string)
	Send          func(ctx context.Context, sessionID, content string) error
	AuditLog      func(ctx context.Context, eventType, sessionID, detail string)
}

type notifyInput struct {
	Action  string `json:"action"`
	Delay   string `json:"delay,omitempty"`
	Message string `json:"message"`
}

func (t *NotifyTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "notify",
		Description: "Proactively message the user. Actions: schedule (run a full agent turn after a delay and deliver the result), send (immediately send a message to the current session).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["schedule", "send"],
					"description": "schedule: start a delayed agent turn that delivers its response to the user. send: immediately send a text message to the current session."
				},
				"delay": {
					"type": "string",
					"description": "Duration to wait before running the scheduled turn (e.g. '5m', '1h30m'). Required for schedule."
				},
				"message": {
					"type": "string",
					"description": "For schedule: the prompt that will be used as the user message when the timer fires. For send: the text to deliver immediately."
				}
			},
			"required": ["action", "message"]
		}`),
	}
}

func (t *NotifyTool) Execute(ctx context.Context, input json.RawMessage, _ sandbox.Sandbox, _ sandbox.Policy) (string, error) {
	var params notifyInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("notify: invalid input: %w", err)
	}

	sessionID := SessionIDFromContext(ctx)
	if sessionID == "" {
		return "", fmt.Errorf("notify: no session in context")
	}

	switch params.Action {
	case "schedule":
		if params.Delay == "" {
			return "", fmt.Errorf("notify: delay is required for schedule")
		}
		if params.Message == "" {
			return "", fmt.Errorf("notify: message is required for schedule")
		}
		delay, err := time.ParseDuration(params.Delay)
		if err != nil {
			return "", fmt.Errorf("notify: invalid delay %q: %w", params.Delay, err)
		}
		if delay <= 0 {
			return "", fmt.Errorf("notify: delay must be positive")
		}

		sid := sessionID
		prompt := params.Message
		time.AfterFunc(delay, func() {
			t.RunAndDeliver(context.Background(), sid, prompt)
		})

		t.auditLog(ctx, "notify_schedule", sid, fmt.Sprintf("delay=%s prompt=%s", delay, params.Message))

		return fmt.Sprintf("scheduled: will run in %s", delay), nil

	case "send":
		if params.Message == "" {
			return "", fmt.Errorf("notify: message is required for send")
		}
		if err := t.Send(ctx, sessionID, params.Message); err != nil {
			return "", fmt.Errorf("notify: send failed: %w", err)
		}
		return "message sent", nil

	default:
		return "", fmt.Errorf("notify: unknown action %q", params.Action)
	}
}

func (t *NotifyTool) auditLog(ctx context.Context, eventType, sessionID, detail string) {
	if t.AuditLog != nil {
		t.AuditLog(ctx, eventType, sessionID, detail)
	}
}
