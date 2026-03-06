// Package verification provides interfaces and orchestration for verifying
// whether a completed task actually succeeded.
package verification

import "context"

// Status represents the outcome of a verification gate.
type Status int

const (
	Confirmed Status = iota
	Failed
	Uncertain
)

// Result is what a verification gate returns.
type Result struct {
	Status     Status
	Evidence   string
	Reason     string
	Confidence float64
	Suggestion string
}

// TaskResult represents the completed task output to be verified.
type TaskResult struct {
	SessionID    string
	FinalMessage string
	ToolsUsed    []string
	FilesWritten []string
}

// Gate verifies whether a completed task actually succeeded.
type Gate interface {
	Name() string
	AppliesTo(tr TaskResult) bool
	Verify(ctx context.Context, tr TaskResult) Result
}

// Runner orchestrates multiple gates and produces a final verdict.
type Runner struct {
	gates               []Gate
	confidenceThreshold float64
	maxAttempts         int
}

// NewRunner creates a Runner with the given gates, confidence threshold, and
// maximum retry attempts.
func NewRunner(gates []Gate, confidenceThreshold float64, maxAttempts int) *Runner {
	return &Runner{
		gates:               gates,
		confidenceThreshold: confidenceThreshold,
		maxAttempts:         maxAttempts,
	}
}

// Run executes all applicable gates against the task result and returns a
// combined verdict. A single Failed gate short-circuits the run. Uncertain
// results whose confidence meets the threshold are treated as Confirmed.
func (r *Runner) Run(ctx context.Context, tr TaskResult) Result {
	var applicable []Gate
	for _, g := range r.gates {
		if g.AppliesTo(tr) {
			applicable = append(applicable, g)
		}
	}

	if len(applicable) == 0 {
		return Result{Status: Confirmed, Evidence: "no applicable verification gates"}
	}

	for _, g := range applicable {
		result := g.Verify(ctx, tr)
		switch result.Status {
		case Failed:
			return result
		case Uncertain:
			if result.Confidence >= r.confidenceThreshold {
				continue // treat as confirmed
			}
			return result
		case Confirmed:
			continue
		}
	}

	return Result{Status: Confirmed, Evidence: "all gates passed"}
}
