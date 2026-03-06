package checkpoint

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// IterationState captures the current state of the agentic loop for checkpoint decisions.
type IterationState struct {
	TokensConsumed       int
	LastCheckpointTokens int
	HasSideEffects       bool
	ExplicitCheckpoint   bool
	Iteration            int
}

// Snapshot is the data to persist at a checkpoint.
type Snapshot struct {
	ID             string
	SessionID      string
	StepIndex      int
	ToolOutputs    string // JSON array of outputs
	ContextSummary string
}

// Manager decides when to create checkpoints and builds snapshot data.
type Manager struct {
	TokenThreshold int // checkpoint after this many tokens since last checkpoint
}

func (m *Manager) ShouldCheckpoint(state IterationState) bool {
	if state.ExplicitCheckpoint {
		return true
	}
	if state.HasSideEffects {
		return true
	}
	tokensSinceLast := state.TokensConsumed - state.LastCheckpointTokens
	if m.TokenThreshold > 0 && tokensSinceLast >= m.TokenThreshold {
		return true
	}
	return false
}

func (m *Manager) BuildSnapshot(stepIndex int, toolOutputs []string, contextSummary string) Snapshot {
	outputsJSON, _ := json.Marshal(toolOutputs)
	return Snapshot{
		ID:             uuid.NewString(),
		StepIndex:      stepIndex,
		ToolOutputs:    string(outputsJSON),
		ContextSummary: contextSummary,
	}
}

// RollbackTarget determines which checkpoint to roll back to.
func RollbackTarget(currentStep int, available []int) (int, error) {
	best := -1
	for _, step := range available {
		if step < currentStep && step > best {
			best = step
		}
	}
	if best == -1 {
		return 0, fmt.Errorf("no checkpoint available before step %d", currentStep)
	}
	return best, nil
}
