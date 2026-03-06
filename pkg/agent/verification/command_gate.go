package verification

import (
	"context"
	"fmt"
	"strings"
)

// CommandRunner abstracts the execution of a shell command.
type CommandRunner interface {
	Run(ctx context.Context, cmd string) (string, error)
}

// CommandOutputGate runs a verification command and checks whether its output
// contains the expected string.
type CommandOutputGate struct {
	ToolNames      []string
	VerifyCommand  string
	ExpectedOutput string
	Runner         CommandRunner
}

func (g *CommandOutputGate) Name() string { return "command_output" }

func (g *CommandOutputGate) AppliesTo(tr TaskResult) bool {
	for _, used := range tr.ToolsUsed {
		for _, target := range g.ToolNames {
			if used == target {
				return true
			}
		}
	}
	return false
}

func (g *CommandOutputGate) Verify(ctx context.Context, tr TaskResult) Result {
	if g.Runner == nil {
		return Result{Status: Uncertain, Evidence: "no command runner configured"}
	}
	output, err := g.Runner.Run(ctx, g.VerifyCommand)
	if err != nil {
		return Result{Status: Failed, Reason: fmt.Sprintf("verification command failed: %v", err)}
	}
	output = strings.TrimSpace(output)
	expected := strings.TrimSpace(g.ExpectedOutput)
	if strings.Contains(output, expected) {
		return Result{Status: Confirmed, Evidence: fmt.Sprintf("output contains %q", expected)}
	}
	return Result{
		Status:     Failed,
		Reason:     fmt.Sprintf("expected output containing %q, got %q", expected, output),
		Suggestion: "Re-run the task or check the command output manually",
	}
}
