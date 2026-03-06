package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/igorsilveira/pincer/pkg/agent/tools"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
	"github.com/igorsilveira/pincer/pkg/store"
)

func TestRunTurn_ParallelToolExecution(t *testing.T) {
	fp := &fakeProviderMulti{
		responses: [][]llm.ChatEvent{
			{
				{Type: llm.EventToolCall, ToolCall: &llm.ToolCall{ID: "tc-1", Name: "shell", Input: json.RawMessage(`{"command":"echo 1"}`)}},
				{Type: llm.EventToolCall, ToolCall: &llm.ToolCall{ID: "tc-2", Name: "shell", Input: json.RawMessage(`{"command":"echo 2"}`)}},
				{Type: llm.EventToolCall, ToolCall: &llm.ToolCall{ID: "tc-3", Name: "shell", Input: json.RawMessage(`{"command":"echo 3"}`)}},
				{Type: llm.EventDone, Usage: &llm.Usage{}},
			},
			{
				{Type: llm.EventToken, Token: "all done"},
				{Type: llm.EventDone, Usage: &llm.Usage{}},
			},
		},
	}

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	sb := &concurrencySandbox{
		concurrent:    &concurrent,
		maxConcurrent: &maxConcurrent,
		delay:         50 * time.Millisecond,
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
		Sandbox:      sb,
		Approver:     NewApprover(ApprovalAuto, nil),
		Model:        "fake-1",
		SystemPrompt: "test",
	})

	ch, err := rt.RunTurn(context.Background(), "sess-parallel", "run three commands")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	events := collectTurnEvents(ch)

	if mc := maxConcurrent.Load(); mc < 2 {
		t.Errorf("max concurrent tool executions = %d, want >= 2 (tools should run in parallel)", mc)
	}

	var toolResults int
	for _, e := range events {
		if e.Type == TurnToolResult {
			toolResults++
		}
	}
	if toolResults != 3 {
		t.Errorf("expected 3 TurnToolResult events, got %d", toolResults)
	}
}

func TestRunTurn_ParallelToolResultOrder(t *testing.T) {
	fp := &fakeProviderMulti{
		responses: [][]llm.ChatEvent{
			{
				{Type: llm.EventToolCall, ToolCall: &llm.ToolCall{ID: "tc-a", Name: "shell", Input: json.RawMessage(`{"command":"echo a"}`)}},
				{Type: llm.EventToolCall, ToolCall: &llm.ToolCall{ID: "tc-b", Name: "shell", Input: json.RawMessage(`{"command":"echo b"}`)}},
				{Type: llm.EventDone, Usage: &llm.Usage{}},
			},
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

	rt := NewRuntime(RuntimeConfig{
		Provider:     fp,
		Store:        s,
		Registry:     reg,
		Sandbox:      &fakeSandboxAgent{result: &sandbox.Result{Stdout: "output", ExitCode: 0}},
		Approver:     NewApprover(ApprovalAuto, nil),
		Model:        "fake-1",
		SystemPrompt: "test",
	})

	ch, err := rt.RunTurn(context.Background(), "sess-order", "run two")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	collectTurnEvents(ch)

	msgs, err := s.RecentMessages(context.Background(), "sess-order", 50)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}

	for _, m := range msgs {
		if m.ContentType != store.ContentTypeToolResults {
			continue
		}
		var results []llm.ToolResult
		if err := json.Unmarshal([]byte(m.Content), &results); err != nil {
			t.Fatalf("unmarshal tool results: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 tool results, got %d", len(results))
		}
		if results[0].ToolCallID != "tc-a" {
			t.Errorf("first result tool_call_id = %q, want %q", results[0].ToolCallID, "tc-a")
		}
		if results[1].ToolCallID != "tc-b" {
			t.Errorf("second result tool_call_id = %q, want %q", results[1].ToolCallID, "tc-b")
		}
		return
	}
	t.Error("tool results message not found")
}

func TestRunTurn_TurnToolStartEvents(t *testing.T) {
	fp := &fakeProviderMulti{
		responses: [][]llm.ChatEvent{
			{
				{Type: llm.EventToolCall, ToolCall: &llm.ToolCall{ID: "tc-1", Name: "shell", Input: json.RawMessage(`{"command":"echo hi"}`)}},
				{Type: llm.EventDone, Usage: &llm.Usage{}},
			},
			{
				{Type: llm.EventToken, Token: "done"},
				{Type: llm.EventDone, Usage: &llm.Usage{}},
			},
		},
	}
	rt, _ := newTestRuntime(t, fp)

	ch, err := rt.RunTurn(context.Background(), "sess-start", "run")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	events := collectTurnEvents(ch)
	var toolStarts int
	for _, e := range events {
		if e.Type == TurnToolStart {
			toolStarts++
			if e.ToolCall == nil {
				t.Error("TurnToolStart event should include ToolCall reference")
			}
			if !strings.Contains(e.Message, "shell") {
				t.Errorf("TurnToolStart message should mention tool name, got %q", e.Message)
			}
		}
	}
	if toolStarts < 1 {
		t.Errorf("expected at least 1 TurnToolStart event, got %d", toolStarts)
	}
}

func TestRunTurn_EphemeralErrorNotPersisted(t *testing.T) {
	fp := &fakeProviderMulti{
		responses: [][]llm.ChatEvent{},
	}

	callCount := 0
	provider := &errorThenSuccessProvider{
		errorCount: 1,
		err:        fmt.Errorf("API overloaded"),
		successEvents: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "recovered"},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
		calls: &callCount,
	}
	_ = fp

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})

	rt := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Store:        s,
		Registry:     reg,
		Sandbox:      &fakeSandboxAgent{result: &sandbox.Result{Stdout: "ok"}},
		Approver:     NewApprover(ApprovalAuto, nil),
		Model:        "fake-1",
		SystemPrompt: "test",
	})

	ch, err := rt.RunTurn(context.Background(), "sess-ephemeral", "test")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	collectTurnEvents(ch)

	msgs, err := s.RecentMessages(context.Background(), "sess-ephemeral", 50)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}

	for _, m := range msgs {
		if strings.Contains(m.Content, "[System:") || strings.Contains(m.Content, "Error Recovery") {
			t.Errorf("found persisted error message in DB: %q", m.Content)
		}
	}

	if provider.gotSystemPrompts == nil || len(provider.gotSystemPrompts) < 2 {
		t.Fatal("expected at least 2 LLM calls (1 failed, 1 success)")
	}
	retryPrompt := provider.gotSystemPrompts[1]
	if !strings.Contains(retryPrompt, "Error Recovery Context") {
		t.Error("retry LLM call should have ephemeral error context in system prompt")
	}
}

func TestRunTurn_StreamErrorEphemeral(t *testing.T) {
	provider := &streamErrorThenSuccessProvider{
		errorCount: 1,
		streamErr:  fmt.Errorf("connection reset"),
		successEvents: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "ok"},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
	}

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	rt := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Store:        s,
		Registry:     tools.NewRegistry(),
		Sandbox:      &fakeSandboxAgent{result: &sandbox.Result{Stdout: "ok"}},
		Approver:     NewApprover(ApprovalAuto, nil),
		Model:        "fake-1",
		SystemPrompt: "test",
	})

	ch, err := rt.RunTurn(context.Background(), "sess-stream-eph", "test")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	collectTurnEvents(ch)

	msgs, err := s.RecentMessages(context.Background(), "sess-stream-eph", 50)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}

	for _, m := range msgs {
		if strings.Contains(m.Content, "[System:") {
			t.Errorf("found persisted stream error message in DB: %q", m.Content)
		}
	}
}

type concurrencySandbox struct {
	concurrent    *atomic.Int32
	maxConcurrent *atomic.Int32
	delay         time.Duration
}

func (cs *concurrencySandbox) Exec(_ context.Context, _ sandbox.Command, _ sandbox.Policy) (*sandbox.Result, error) {
	cur := cs.concurrent.Add(1)
	for {
		old := cs.maxConcurrent.Load()
		if cur <= old {
			break
		}
		if cs.maxConcurrent.CompareAndSwap(old, cur) {
			break
		}
	}
	time.Sleep(cs.delay)
	cs.concurrent.Add(-1)
	return &sandbox.Result{Stdout: "ok", ExitCode: 0}, nil
}

type errorThenSuccessProvider struct {
	errorCount      int
	err             error
	successEvents   []llm.ChatEvent
	calls           *int
	gotSystemPrompts []string
}

func (p *errorThenSuccessProvider) Name() string            { return "error-then-success" }
func (p *errorThenSuccessProvider) SupportsStreaming() bool { return true }
func (p *errorThenSuccessProvider) SupportsToolUse() bool   { return true }
func (p *errorThenSuccessProvider) Models() []llm.ModelInfo {
	return []llm.ModelInfo{{ID: "fake-1", Name: "Fake", MaxContextTokens: 128000}}
}

func (p *errorThenSuccessProvider) Chat(_ context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	p.gotSystemPrompts = append(p.gotSystemPrompts, req.System)
	*p.calls++
	if *p.calls <= p.errorCount {
		return nil, p.err
	}
	ch := make(chan llm.ChatEvent, len(p.successEvents)+1)
	for _, e := range p.successEvents {
		ch <- e
	}
	close(ch)
	return ch, nil
}

type streamErrorThenSuccessProvider struct {
	errorCount    int
	streamErr     error
	successEvents []llm.ChatEvent
	calls         int
	gotSystemPrompts []string
}

func (p *streamErrorThenSuccessProvider) Name() string            { return "stream-error-then-success" }
func (p *streamErrorThenSuccessProvider) SupportsStreaming() bool { return true }
func (p *streamErrorThenSuccessProvider) SupportsToolUse() bool   { return true }
func (p *streamErrorThenSuccessProvider) Models() []llm.ModelInfo {
	return []llm.ModelInfo{{ID: "fake-1", Name: "Fake", MaxContextTokens: 128000}}
}

func (p *streamErrorThenSuccessProvider) Chat(_ context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	p.gotSystemPrompts = append(p.gotSystemPrompts, req.System)
	p.calls++
	if p.calls <= p.errorCount {
		ch := make(chan llm.ChatEvent, 2)
		ch <- llm.ChatEvent{Type: llm.EventToken, Token: "partial"}
		ch <- llm.ChatEvent{Type: llm.EventError, Error: p.streamErr}
		close(ch)
		return ch, nil
	}
	ch := make(chan llm.ChatEvent, len(p.successEvents)+1)
	for _, e := range p.successEvents {
		ch <- e
	}
	close(ch)
	return ch, nil
}
