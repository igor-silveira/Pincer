package retry

import "sort"

// TaskContext carries information about the current task for strategies to inspect.
type TaskContext struct {
	OriginalPrompt string
	SessionID      string
	Iteration      int
}

// ReframedTask is the alternative approach produced by a strategy.
type ReframedTask struct {
	Prompt        string
	EphemeralHint string // injected into system prompt as error-recovery context
}

// Strategy represents a single recovery approach the agent can try.
type Strategy interface {
	Name() string
	Priority() int
	Reframe(task TaskContext, priorErrors []error) (*ReframedTask, bool)
}

// Rotator cycles through strategies by priority, tracking which have been tried.
//
// Rotator is not safe for concurrent use. Callers must synchronise access
// externally if a Rotator is shared across goroutines.
type Rotator struct {
	strategies  []Strategy
	maxAttempts int
	tried       map[string]bool
	attempts    int
}

// NewRotator creates a Rotator that will try strategies in descending priority
// order, up to maxAttempts total attempts.
func NewRotator(strategies []Strategy, maxAttempts int) *Rotator {
	sorted := make([]Strategy, len(strategies))
	copy(sorted, strategies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority() > sorted[j].Priority()
	})
	return &Rotator{
		strategies:  sorted,
		maxAttempts: maxAttempts,
		tried:       make(map[string]bool),
	}
}

// Next returns the next applicable strategy along with its ReframedTask, or
// (nil, nil) if all strategies have been exhausted or maxAttempts has been
// reached. Returning the ReframedTask avoids a redundant second call to
// Reframe by the caller.
func (r *Rotator) Next(task TaskContext, errs []error) (Strategy, *ReframedTask) {
	if r.attempts >= r.maxAttempts {
		return nil, nil
	}
	for _, s := range r.strategies {
		if r.tried[s.Name()] {
			continue
		}
		rt, ok := s.Reframe(task, errs)
		if !ok {
			continue
		}
		r.tried[s.Name()] = true
		r.attempts++
		return s, rt
	}
	return nil, nil
}

// Attempts returns the number of strategies that have been dispensed so far.
func (r *Rotator) Attempts() int { return r.attempts }

// Reset clears all state so the rotator can be reused for a new task.
func (r *Rotator) Reset() {
	r.tried = make(map[string]bool)
	r.attempts = 0
}
