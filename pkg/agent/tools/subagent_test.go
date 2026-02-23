package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/igorsilveira/pincer/pkg/sandbox"
)

func subagentCtx() context.Context {
	return WithSessionInfo(context.Background(), "sess-sub", "agent-1")
}

func TestSubagentTool_BasicExecution(t *testing.T) {
	var gotPrompt string
	var gotTools []string
	tool := &SubagentTool{
		RunSubturn: func(_ context.Context, prompt string, allowedTools []string) (string, error) {
			gotPrompt = prompt
			gotTools = allowedTools
			return "subagent result", nil
		},
	}

	input, _ := json.Marshal(subagentInput{Task: "do something"})
	output, err := tool.Execute(subagentCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output != "subagent result" {
		t.Errorf("output = %q, want %q", output, "subagent result")
	}
	if gotPrompt != "do something" {
		t.Errorf("prompt = %q, want %q", gotPrompt, "do something")
	}
	if gotTools != nil {
		t.Errorf("tools = %v, want nil", gotTools)
	}
}

func TestSubagentTool_EmptyTask(t *testing.T) {
	tool := &SubagentTool{
		RunSubturn: func(_ context.Context, _ string, _ []string) (string, error) {
			return "", nil
		},
	}

	input, _ := json.Marshal(subagentInput{Task: ""})
	_, err := tool.Execute(subagentCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for empty task")
	}
}

func TestSubagentTool_AllowedToolsForwarded(t *testing.T) {
	var gotTools []string
	tool := &SubagentTool{
		RunSubturn: func(_ context.Context, _ string, allowedTools []string) (string, error) {
			gotTools = allowedTools
			return "ok", nil
		},
	}

	input, _ := json.Marshal(subagentInput{Task: "test", AllowedTools: []string{"shell", "http_request"}})
	_, err := tool.Execute(subagentCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(gotTools) != 2 || gotTools[0] != "shell" || gotTools[1] != "http_request" {
		t.Errorf("tools = %v, want [shell http_request]", gotTools)
	}
}

func TestSubagentTool_CallbackError(t *testing.T) {
	tool := &SubagentTool{
		RunSubturn: func(_ context.Context, _ string, _ []string) (string, error) {
			return "", fmt.Errorf("depth limit exceeded")
		},
	}

	input, _ := json.Marshal(subagentInput{Task: "deep task"})
	_, err := tool.Execute(subagentCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Fatal("expected error from callback")
	}
	if !strings.Contains(err.Error(), "depth limit exceeded") {
		t.Errorf("error = %q, should wrap callback error", err.Error())
	}
}

func TestSubagentTool_AuditLogging(t *testing.T) {
	var events []string
	tool := &SubagentTool{
		RunSubturn: func(_ context.Context, _ string, _ []string) (string, error) {
			return "done", nil
		},
		AuditLog: func(_ context.Context, eventType, _, _ string) {
			events = append(events, eventType)
		},
	}

	input, _ := json.Marshal(subagentInput{Task: "audit me"})
	_, err := tool.Execute(subagentCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("audit events = %d, want 2", len(events))
	}
	if events[0] != "subagent_start" {
		t.Errorf("events[0] = %q, want %q", events[0], "subagent_start")
	}
	if events[1] != "subagent_done" {
		t.Errorf("events[1] = %q, want %q", events[1], "subagent_done")
	}
}

func TestSubagentTool_AuditLoggingOnError(t *testing.T) {
	var events []string
	tool := &SubagentTool{
		RunSubturn: func(_ context.Context, _ string, _ []string) (string, error) {
			return "", fmt.Errorf("boom")
		},
		AuditLog: func(_ context.Context, eventType, _, _ string) {
			events = append(events, eventType)
		},
	}

	input, _ := json.Marshal(subagentInput{Task: "fail"})
	_, _ = tool.Execute(subagentCtx(), input, nil, sandbox.Policy{})
	if len(events) != 2 {
		t.Fatalf("audit events = %d, want 2", len(events))
	}
	if events[1] != "subagent_error" {
		t.Errorf("events[1] = %q, want %q", events[1], "subagent_error")
	}
}
