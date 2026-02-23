package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/igorsilveira/pincer/pkg/agent/tools"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
	"github.com/igorsilveira/pincer/pkg/store"
)

func newSubturnRuntime(t *testing.T, provider llm.Provider) *Runtime {
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

	return NewRuntime(RuntimeConfig{
		Provider:     provider,
		Store:        s,
		Registry:     reg,
		Sandbox:      sb,
		Model:        "fake-1",
		SystemPrompt: "test system prompt",
	})
}

func TestRunSubturn_SimpleTextResponse(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "subagent says hi"},
			{Type: llm.EventDone, Usage: &llm.Usage{InputTokens: 5, OutputTokens: 3}},
		},
	}

	rt := newSubturnRuntime(t, fp)
	ctx := tools.WithSessionInfo(context.Background(), "sess-sub", "agent-1")

	result, err := rt.RunSubturn(ctx, "say hi", nil)
	if err != nil {
		t.Fatalf("RunSubturn: %v", err)
	}
	if result != "subagent says hi" {
		t.Errorf("result = %q, want %q", result, "subagent says hi")
	}
}

func TestRunSubturn_ToolCallLoop(t *testing.T) {
	fp := &fakeProviderMulti{
		responses: [][]llm.ChatEvent{
			toolCallEvents("tc-sub", "shell", json.RawMessage(`{"command":"echo hi"}`)),
			{
				{Type: llm.EventToken, Token: "done with tools"},
				{Type: llm.EventDone, Usage: &llm.Usage{}},
			},
		},
	}

	rt := newSubturnRuntime(t, fp)
	ctx := tools.WithSessionInfo(context.Background(), "sess-sub-tool", "agent-1")

	result, err := rt.RunSubturn(ctx, "run a command", nil)
	if err != nil {
		t.Fatalf("RunSubturn: %v", err)
	}
	if result != "done with tools" {
		t.Errorf("result = %q, want %q", result, "done with tools")
	}
	if fp.calls != 2 {
		t.Errorf("provider calls = %d, want 2", fp.calls)
	}
}

func TestRunSubturn_DepthLimitExceeded(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "should not run"},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
	}

	rt := newSubturnRuntime(t, fp)
	ctx := tools.WithSessionInfo(context.Background(), "sess-deep", "agent-1")
	ctx = tools.WithSubagentDepth(ctx, maxSubagentDepth)

	_, err := rt.RunSubturn(ctx, "too deep", nil)
	if err == nil {
		t.Fatal("expected error for depth limit")
	}
	if !strings.Contains(err.Error(), "depth limit") {
		t.Errorf("error = %q, want depth limit message", err.Error())
	}
}

func TestRunSubturn_DepthIncremented(t *testing.T) {
	var capturedDepth int
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "ok"},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
	}

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	depthCapture := &depthCaptureTool{capture: func(ctx context.Context) {
		capturedDepth = tools.SubagentDepthFromContext(ctx)
	}}

	reg := tools.NewRegistry()
	reg.Register(depthCapture)

	fpMulti := &fakeProviderMulti{
		responses: [][]llm.ChatEvent{
			toolCallEvents("tc-depth", "depth_capture", json.RawMessage(`{}`)),
			{
				{Type: llm.EventToken, Token: "ok"},
				{Type: llm.EventDone, Usage: &llm.Usage{}},
			},
		},
	}

	rt := NewRuntime(RuntimeConfig{
		Provider:     fpMulti,
		Store:        s,
		Registry:     reg,
		Sandbox:      &fakeSandboxAgent{result: &sandbox.Result{ExitCode: 0}},
		Model:        "fake-1",
		SystemPrompt: "test",
	})

	ctx := tools.WithSessionInfo(context.Background(), "sess-inc", "agent-1")
	ctx = tools.WithSubagentDepth(ctx, 1)

	_, err = rt.RunSubturn(ctx, "check depth", nil)
	if err != nil {
		t.Fatalf("RunSubturn: %v", err)
	}
	if capturedDepth != 2 {
		t.Errorf("depth = %d, want 2", capturedDepth)
	}

	_ = fp
}

func TestRunSubturn_StripsSubagentAndSpawn(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "ok"},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
	}

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})
	reg.Register(&dummyRegisteredTool{name: "subagent"})
	reg.Register(&dummyRegisteredTool{name: "spawn"})

	rt := NewRuntime(RuntimeConfig{
		Provider:     fp,
		Store:        s,
		Registry:     reg,
		Sandbox:      &fakeSandboxAgent{result: &sandbox.Result{ExitCode: 0}},
		Model:        "fake-1",
		SystemPrompt: "test",
	})

	ctx := tools.WithSessionInfo(context.Background(), "sess-strip", "agent-1")
	_, err = rt.RunSubturn(ctx, "test", nil)
	if err != nil {
		t.Fatalf("RunSubturn: %v", err)
	}

	req := fp.gotReq
	if req == nil {
		t.Fatal("no request captured")
	}
	for _, td := range req.Tools {
		if td.Name == "subagent" || td.Name == "spawn" {
			t.Errorf("subagent registry should not contain %q", td.Name)
		}
	}
}

func TestRunSubturn_ProviderError(t *testing.T) {
	fp := &fakeProvider{
		err: errForTest,
	}

	rt := newSubturnRuntime(t, fp)
	ctx := tools.WithSessionInfo(context.Background(), "sess-perr", "agent-1")

	_, err := rt.RunSubturn(ctx, "fail please", nil)
	if err == nil {
		t.Fatal("expected error from provider")
	}
}

var errForTest = &testError{msg: "provider down"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

type depthCaptureTool struct {
	capture func(ctx context.Context)
}

func (d *depthCaptureTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: "depth_capture", Description: "captures depth"}
}

func (d *depthCaptureTool) Execute(ctx context.Context, _ json.RawMessage, _ sandbox.Sandbox, _ sandbox.Policy) (string, error) {
	if d.capture != nil {
		d.capture(ctx)
	}
	return "captured", nil
}

type dummyRegisteredTool struct {
	name string
}

func (d *dummyRegisteredTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: d.name, Description: "dummy"}
}

func (d *dummyRegisteredTool) Execute(_ context.Context, _ json.RawMessage, _ sandbox.Sandbox, _ sandbox.Policy) (string, error) {
	return "ok", nil
}
