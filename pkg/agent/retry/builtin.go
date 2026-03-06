package retry

import (
	"fmt"
	"strings"
)

// formatErrors joins error messages with "; ".
func formatErrors(errs []error) string {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}

// Rephrase is a fallback strategy (priority 1) that always applies.
// It returns the original prompt with a hint containing prior errors,
// asking the agent to rephrase its approach.
type Rephrase struct{}

func (r Rephrase) Name() string   { return "rephrase" }
func (r Rephrase) Priority() int  { return 1 }

func (r Rephrase) Reframe(task TaskContext, priorErrors []error) (*ReframedTask, bool) {
	return &ReframedTask{
		Prompt:        task.OriginalPrompt,
		EphemeralHint: fmt.Sprintf("Previous attempt failed: %s. Rephrase your approach and try a different strategy.", formatErrors(priorErrors)),
	}, true
}

// Decompose is a mid-priority strategy (priority 5) that applies when the
// original prompt exceeds MinPromptLen, suggesting the agent break the task
// into smaller steps.
type Decompose struct {
	MinPromptLen int
}

func (d Decompose) Name() string   { return "decompose" }
func (d Decompose) Priority() int  { return 5 }

func (d Decompose) Reframe(task TaskContext, priorErrors []error) (*ReframedTask, bool) {
	if len(task.OriginalPrompt) <= d.MinPromptLen {
		return nil, false
	}
	return &ReframedTask{
		Prompt:        task.OriginalPrompt,
		EphemeralHint: fmt.Sprintf("Previous attempt failed: %s. The task is complex — try breaking it into smaller, sequential steps.", formatErrors(priorErrors)),
	}, true
}

// ToolSwap is the highest-priority strategy (priority 10) that applies when an
// error message mentions a tool name that has a known alternative.
type ToolSwap struct {
	Alternatives map[string]string
}

func (ts ToolSwap) Name() string   { return "tool_swap" }
func (ts ToolSwap) Priority() int  { return 10 }

func (ts ToolSwap) Reframe(task TaskContext, priorErrors []error) (*ReframedTask, bool) {
	combined := formatErrors(priorErrors)
	for tool, alt := range ts.Alternatives {
		if strings.Contains(combined, tool) {
			return &ReframedTask{
				Prompt:        task.OriginalPrompt,
				EphemeralHint: fmt.Sprintf("The tool %q failed. Try using %q instead.", tool, alt),
			}, true
		}
	}
	return nil, false
}
