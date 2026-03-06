package verification

import (
	"context"
	"testing"
)

type stubCommandRunner struct {
	output string
	err    error
}

func (r *stubCommandRunner) Run(ctx context.Context, cmd string) (string, error) {
	return r.output, r.err
}

func TestCommandOutputGate_AppliesWhenToolsUsed(t *testing.T) {
	g := &CommandOutputGate{ToolNames: []string{"shell"}}
	tr := TaskResult{ToolsUsed: []string{"shell", "file_read"}}
	if !g.AppliesTo(tr) {
		t.Error("should apply when shell was used")
	}
}

func TestCommandOutputGate_SkipsWhenNoMatchingTools(t *testing.T) {
	g := &CommandOutputGate{ToolNames: []string{"shell"}}
	tr := TaskResult{ToolsUsed: []string{"file_read"}}
	if g.AppliesTo(tr) {
		t.Error("should not apply when shell was not used")
	}
}

func TestCommandOutputGate_Confirmed(t *testing.T) {
	g := &CommandOutputGate{
		ToolNames:      []string{"shell"},
		VerifyCommand:  "echo ok",
		ExpectedOutput: "ok",
		Runner:         &stubCommandRunner{output: "ok"},
	}
	result := g.Verify(context.Background(), TaskResult{ToolsUsed: []string{"shell"}})
	if result.Status != Confirmed {
		t.Errorf("status = %v, want Confirmed", result.Status)
	}
}

func TestCommandOutputGate_Failed(t *testing.T) {
	g := &CommandOutputGate{
		ToolNames:      []string{"shell"},
		VerifyCommand:  "echo ok",
		ExpectedOutput: "ok",
		Runner:         &stubCommandRunner{output: "error"},
	}
	result := g.Verify(context.Background(), TaskResult{ToolsUsed: []string{"shell"}})
	if result.Status != Failed {
		t.Errorf("status = %v, want Failed", result.Status)
	}
}
