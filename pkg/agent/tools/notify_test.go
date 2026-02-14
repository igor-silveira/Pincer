package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/igorsilveira/pincer/pkg/sandbox"
)

func notifyCtx() context.Context {
	return WithSessionInfo(context.Background(), "sess-notify", "agent-1")
}

func TestNotifyTool_SendSuccess(t *testing.T) {
	var sentSession, sentContent string
	tool := &NotifyTool{
		Send: func(_ context.Context, sessionID, content string) error {
			sentSession = sessionID
			sentContent = content
			return nil
		},
	}
	input, _ := json.Marshal(notifyInput{Action: "send", Message: "hello user"})

	output, err := tool.Execute(notifyCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output != "message sent" {
		t.Errorf("output = %q, want %q", output, "message sent")
	}
	if sentSession != "sess-notify" {
		t.Errorf("session = %q, want %q", sentSession, "sess-notify")
	}
	if sentContent != "hello user" {
		t.Errorf("content = %q, want %q", sentContent, "hello user")
	}
}

func TestNotifyTool_SendEmptyMessage(t *testing.T) {
	tool := &NotifyTool{
		Send: func(_ context.Context, _, _ string) error { return nil },
	}
	input, _ := json.Marshal(notifyInput{Action: "send", Message: ""})

	_, err := tool.Execute(notifyCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for empty message")
	}
}

func TestNotifyTool_SendError(t *testing.T) {
	tool := &NotifyTool{
		Send: func(_ context.Context, _, _ string) error {
			return fmt.Errorf("channel offline")
		},
	}
	input, _ := json.Marshal(notifyInput{Action: "send", Message: "test"})

	_, err := tool.Execute(notifyCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Fatal("expected error from Send callback")
	}
	if !strings.Contains(err.Error(), "channel offline") {
		t.Errorf("error = %q, should wrap callback error", err.Error())
	}
}

func TestNotifyTool_SendNoSession(t *testing.T) {
	tool := &NotifyTool{
		Send: func(_ context.Context, _, _ string) error { return nil },
	}
	input, _ := json.Marshal(notifyInput{Action: "send", Message: "test"})

	_, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for missing session")
	}
	if !strings.Contains(err.Error(), "no session") {
		t.Errorf("error = %q, want 'no session' message", err.Error())
	}
}

func TestNotifyTool_ScheduleSuccess(t *testing.T) {
	tool := &NotifyTool{
		RunAndDeliver: func(_ context.Context, _, _ string) {},
	}
	input, _ := json.Marshal(notifyInput{Action: "schedule", Delay: "5m", Message: "remind me"})

	output, err := tool.Execute(notifyCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "scheduled") {
		t.Errorf("output = %q, want 'scheduled' message", output)
	}
}

func TestNotifyTool_ScheduleInvalidDelay(t *testing.T) {
	tool := &NotifyTool{
		RunAndDeliver: func(_ context.Context, _, _ string) {},
	}
	input, _ := json.Marshal(notifyInput{Action: "schedule", Delay: "notaduration", Message: "test"})

	_, err := tool.Execute(notifyCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for invalid delay")
	}
}

func TestNotifyTool_ScheduleNegativeDelay(t *testing.T) {
	tool := &NotifyTool{
		RunAndDeliver: func(_ context.Context, _, _ string) {},
	}
	input, _ := json.Marshal(notifyInput{Action: "schedule", Delay: "-5m", Message: "test"})

	_, err := tool.Execute(notifyCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for negative delay")
	}
}

func TestNotifyTool_UnknownAction(t *testing.T) {
	tool := &NotifyTool{}
	input, _ := json.Marshal(notifyInput{Action: "broadcast", Message: "test"})

	_, err := tool.Execute(notifyCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}
