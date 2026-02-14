package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/igorsilveira/pincer/pkg/agent/tools"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
	"github.com/igorsilveira/pincer/pkg/store"
)

func newTestRuntime(t *testing.T, provider llm.Provider) (*Runtime, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})

	sb := &fakeSandboxAgent{
		result: &sandbox.Result{Stdout: "tool output", ExitCode: 0},
	}

	rt := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Store:        s,
		Registry:     reg,
		Sandbox:      sb,
		Approver:     NewApprover(ApprovalAuto, nil),
		Model:        "fake-1",
		SystemPrompt: "test system prompt",
	})
	return rt, s
}

func collectTurnEvents(ch <-chan TurnEvent) []TurnEvent {
	var events []TurnEvent
	for e := range ch {
		events = append(events, e)
	}
	return events
}

func TestRunTurn_SimpleTextResponse(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "hello "},
			{Type: llm.EventToken, Token: "world"},
			{Type: llm.EventDone, Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5}},
		},
	}
	rt, _ := newTestRuntime(t, fp)

	ch, err := rt.RunTurn(context.Background(), "sess-1", "hi")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	events := collectTurnEvents(ch)
	var tokens, dones int
	for _, e := range events {
		switch e.Type {
		case TurnToken:
			tokens++
		case TurnDone:
			dones++
		}
	}
	if tokens < 2 {
		t.Errorf("expected at least 2 token events, got %d", tokens)
	}
	if dones != 1 {
		t.Errorf("expected 1 done event, got %d", dones)
	}
}

func TestRunTurn_CreatesSession(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "ok"},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
	}
	rt, s := newTestRuntime(t, fp)

	ch, err := rt.RunTurn(context.Background(), "new-sess", "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	collectTurnEvents(ch)

	sess, err := s.GetSession(context.Background(), "new-sess")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.ID != "new-sess" {
		t.Errorf("session ID = %q, want %q", sess.ID, "new-sess")
	}
}

func TestRunTurn_PersistsUserMessage(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "response"},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
	}
	rt, s := newTestRuntime(t, fp)

	ch, err := rt.RunTurn(context.Background(), "sess-msg", "user input")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	collectTurnEvents(ch)

	msgs, err := s.RecentMessages(context.Background(), "sess-msg", 50)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}

	var foundUser bool
	for _, m := range msgs {
		if m.Role == llm.RoleUser && m.Content == "user input" {
			foundUser = true
		}
	}
	if !foundUser {
		t.Error("user message not found in store")
	}
}

func TestRunTurn_PersistsAssistantMessage(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "answer"},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
	}
	rt, s := newTestRuntime(t, fp)

	ch, err := rt.RunTurn(context.Background(), "sess-asst", "q")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	collectTurnEvents(ch)

	time.Sleep(50 * time.Millisecond)

	msgs, err := s.RecentMessages(context.Background(), "sess-asst", 50)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}

	var foundAssistant bool
	for _, m := range msgs {
		if m.Role == llm.RoleAssistant && m.ContentType == store.ContentTypeText {
			foundAssistant = true
		}
	}
	if !foundAssistant {
		t.Error("assistant message not found in store")
	}
}

func TestRunTurn_ProviderError(t *testing.T) {
	fp := &fakeProvider{
		err: fmt.Errorf("API down"),
	}
	rt, _ := newTestRuntime(t, fp)

	ch, err := rt.RunTurn(context.Background(), "sess-err", "hello")
	if err != nil {
		t.Fatalf("RunTurn itself should not error (error comes via channel): %v", err)
	}

	events := collectTurnEvents(ch)
	var foundError bool
	for _, e := range events {
		if e.Type == TurnError {
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected TurnError event from provider failure")
	}
}

func TestRunTurn_StreamError(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "partial"},
			{Type: llm.EventError, Error: fmt.Errorf("stream broke")},
		},
	}
	rt, _ := newTestRuntime(t, fp)

	ch, err := rt.RunTurn(context.Background(), "sess-stream-err", "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	events := collectTurnEvents(ch)
	var foundError bool
	for _, e := range events {
		if e.Type == TurnError {
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected TurnError event from stream error")
	}
}

func TestRunTurn_ToolCallFlow(t *testing.T) {
	fp := &fakeProviderMulti{
		responses: [][]llm.ChatEvent{
			toolCallEvents("tc-1", "shell", json.RawMessage(`{"command":"echo hi"}`)),
			{
				{Type: llm.EventToken, Token: "done"},
				{Type: llm.EventDone, Usage: &llm.Usage{}},
			},
		},
	}

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})

	sb := &fakeSandboxAgent{
		result: &sandbox.Result{Stdout: "hi", ExitCode: 0},
	}

	rt := NewRuntime(RuntimeConfig{
		Provider:     fp,
		Store:        s,
		Registry:     reg,
		Sandbox:      sb,
		Approver:     NewApprover(ApprovalAuto, nil),
		Model:        "fake-1",
		SystemPrompt: "test",
	})

	ch, err := rt.RunTurn(context.Background(), "sess-tool", "run echo")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	events := collectTurnEvents(ch)
	var toolCalls, toolResults, dones int
	for _, e := range events {
		switch e.Type {
		case TurnToolCall:
			toolCalls++
		case TurnToolResult:
			toolResults++
		case TurnDone:
			dones++
		}
	}
	if toolCalls < 1 {
		t.Errorf("expected at least 1 TurnToolCall, got %d", toolCalls)
	}
	if toolResults < 1 {
		t.Errorf("expected at least 1 TurnToolResult, got %d", toolResults)
	}
	if dones != 1 {
		t.Errorf("expected 1 TurnDone, got %d", dones)
	}
}

func TestRunTurn_ToolCallDenied(t *testing.T) {
	fp := &fakeProviderMulti{
		responses: [][]llm.ChatEvent{
			toolCallEvents("tc-deny", "shell", json.RawMessage(`{"command":"rm -rf /"}`)),
			{
				{Type: llm.EventToken, Token: "ok"},
				{Type: llm.EventDone, Usage: &llm.Usage{}},
			},
		},
	}

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})

	rt := NewRuntime(RuntimeConfig{
		Provider:     fp,
		Store:        s,
		Registry:     reg,
		Sandbox:      &fakeSandboxAgent{result: &sandbox.Result{Stdout: "ok"}},
		Approver:     NewApprover(ApprovalDeny, nil),
		Model:        "fake-1",
		SystemPrompt: "test",
	})

	ch, err := rt.RunTurn(context.Background(), "sess-deny", "delete everything")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	events := collectTurnEvents(ch)
	var hasToolResult bool
	for _, e := range events {
		if e.Type == TurnToolResult {
			hasToolResult = true
		}
	}
	if hasToolResult {
		t.Error("denied tool call should not produce TurnToolResult")
	}
}

func TestRunTurn_AutoApproveContext(t *testing.T) {
	fp := &fakeProviderMulti{
		responses: [][]llm.ChatEvent{
			toolCallEvents("tc-auto", "shell", json.RawMessage(`{"command":"ls"}`)),
			{
				{Type: llm.EventToken, Token: "listed"},
				{Type: llm.EventDone, Usage: &llm.Usage{}},
			},
		},
	}

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})

	rt := NewRuntime(RuntimeConfig{
		Provider:     fp,
		Store:        s,
		Registry:     reg,
		Sandbox:      &fakeSandboxAgent{result: &sandbox.Result{Stdout: "files", ExitCode: 0}},
		Approver:     NewApprover(ApprovalDeny, nil),
		Model:        "fake-1",
		SystemPrompt: "test",
	})

	ctx := WithAutoApprove(context.Background())
	ch, err := rt.RunTurn(ctx, "sess-auto", "list files")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	events := collectTurnEvents(ch)
	var hasToolResult bool
	for _, e := range events {
		if e.Type == TurnToolResult {
			hasToolResult = true
		}
	}
	if !hasToolResult {
		t.Error("WithAutoApprove should bypass deny mode")
	}
}

func TestRunTurn_MaxIterations(t *testing.T) {
	alwaysToolCall := []llm.ChatEvent{
		{
			Type:     llm.EventToolCall,
			ToolCall: &llm.ToolCall{ID: "tc", Name: "shell", Input: json.RawMessage(`{"command":"echo loop"}`)},
		},
		{Type: llm.EventDone, Usage: &llm.Usage{}},
	}

	responses := make([][]llm.ChatEvent, 15)
	for i := range responses {
		responses[i] = alwaysToolCall
	}

	fp := &fakeProviderMulti{responses: responses}

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})

	rt := NewRuntime(RuntimeConfig{
		Provider:     fp,
		Store:        s,
		Registry:     reg,
		Sandbox:      &fakeSandboxAgent{result: &sandbox.Result{Stdout: "loop", ExitCode: 0}},
		Approver:     NewApprover(ApprovalAuto, nil),
		Model:        "fake-1",
		SystemPrompt: "test",
	})

	ch, err := rt.RunTurn(context.Background(), "sess-max", "loop forever")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	events := collectTurnEvents(ch)
	var doneEvents []TurnEvent
	for _, e := range events {
		if e.Type == TurnDone {
			doneEvents = append(doneEvents, e)
		}
	}
	if len(doneEvents) != 1 {
		t.Fatalf("expected 1 TurnDone, got %d", len(doneEvents))
	}
	if fp.calls > maxToolIterations+1 {
		t.Errorf("provider called %d times, want <= %d", fp.calls, maxToolIterations+1)
	}
}
