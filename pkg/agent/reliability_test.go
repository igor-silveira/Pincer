// pkg/agent/reliability_test.go
package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/igorsilveira/pincer/pkg/agent/retry"
	"github.com/igorsilveira/pincer/pkg/agent/tools"
	"github.com/igorsilveira/pincer/pkg/agent/verification"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
	"github.com/igorsilveira/pincer/pkg/store"
)

func TestReliability_FullRoundTrip(t *testing.T) {
	// Scenario: tool call → tool completes → text response → verification passes
	fp := &fakeProviderMulti{
		responses: [][]llm.ChatEvent{
			// 1. First: calls shell tool
			toolCallEvents("tc-1", "shell", json.RawMessage(`{"command":"echo hi"}`)),
			// 2. After tool execution: returns clean text
			{
				{Type: llm.EventToken, Token: "completed successfully"},
				{Type: llm.EventDone, Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5}},
			},
		},
	}

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})

	sb := &fakeSandboxAgent{
		result: &sandbox.Result{Stdout: "hi", ExitCode: 0},
	}

	confirmGate := &stubVerifyGate{result: verification.Result{
		Status: verification.Confirmed, Evidence: "ok",
	}}

	rt := NewRuntime(RuntimeConfig{
		Provider:     fp,
		Store:        s,
		Registry:     reg,
		Sandbox:      sb,
		Approver:     NewApprover(ApprovalAuto, nil),
		Model:        "test",
		SystemPrompt: "test",
		RetryStrategies: []retry.Strategy{
			&retry.Rephrase{},
		},
		VerificationRunner: verification.NewRunner(
			[]verification.Gate{confirmGate},
			0.8, 2,
		),
	})

	ctx := context.Background()
	events, err := rt.RunTurn(ctx, "reliability-test", "do the thing")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	var done bool
	var finalMsg string
	for ev := range events {
		if ev.Type == TurnDone {
			done = true
			finalMsg = ev.Message
		}
	}
	if !done {
		t.Error("expected TurnDone event")
	}
	if finalMsg != "completed successfully" {
		t.Errorf("final message = %q, want %q", finalMsg, "completed successfully")
	}
}

func TestReliability_VerificationRetry(t *testing.T) {
	// Scenario: first response fails verification, second passes
	fp := &fakeProviderMulti{
		responses: [][]llm.ChatEvent{
			{
				{Type: llm.EventToken, Token: "bad answer"},
				{Type: llm.EventDone, Usage: &llm.Usage{InputTokens: 5, OutputTokens: 3}},
			},
			{
				{Type: llm.EventToken, Token: "good answer"},
				{Type: llm.EventDone, Usage: &llm.Usage{InputTokens: 5, OutputTokens: 3}},
			},
		},
	}

	s, err := store.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	reg := tools.NewRegistry()
	reg.Register(&tools.ShellTool{})

	failOnce := &failOnceGate{failCount: 1}

	rt := NewRuntime(RuntimeConfig{
		Provider:     fp,
		Store:        s,
		Registry:     reg,
		Sandbox:      &fakeSandboxAgent{result: &sandbox.Result{ExitCode: 0}},
		Approver:     NewApprover(ApprovalAuto, nil),
		Model:        "test",
		SystemPrompt: "test",
		VerificationRunner: verification.NewRunner(
			[]verification.Gate{failOnce},
			0.8, 2,
		),
	})

	ctx := context.Background()
	events, err := rt.RunTurn(ctx, "verify-retry-test", "answer me")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	var finalMsg string
	for ev := range events {
		if ev.Type == TurnDone {
			finalMsg = ev.Message
		}
	}

	if finalMsg != "good answer" {
		t.Errorf("final = %q, want %q", finalMsg, "good answer")
	}
	if failOnce.verifyCalls != 2 {
		t.Errorf("verify calls = %d, want 2", failOnce.verifyCalls)
	}
}

type stubVerifyGate struct {
	result verification.Result
}

func (g *stubVerifyGate) Name() string                                        { return "stub" }
func (g *stubVerifyGate) AppliesTo(_ verification.TaskResult) bool            { return true }
func (g *stubVerifyGate) Verify(_ context.Context, _ verification.TaskResult) verification.Result {
	return g.result
}

type failOnceGate struct {
	failCount   int
	verifyCalls int
}

func (g *failOnceGate) Name() string                             { return "fail_once" }
func (g *failOnceGate) AppliesTo(_ verification.TaskResult) bool { return true }
func (g *failOnceGate) Verify(_ context.Context, _ verification.TaskResult) verification.Result {
	g.verifyCalls++
	if g.verifyCalls <= g.failCount {
		return verification.Result{
			Status: verification.Failed,
			Reason: "verification failed, try again",
		}
	}
	return verification.Result{Status: verification.Confirmed, Evidence: "ok now"}
}
