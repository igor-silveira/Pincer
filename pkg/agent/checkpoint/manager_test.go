package checkpoint

import (
	"testing"
)

func TestShouldCheckpoint_TokenThreshold(t *testing.T) {
	m := &Manager{TokenThreshold: 10000}
	state := IterationState{TokensConsumed: 15000, LastCheckpointTokens: 0}
	if !m.ShouldCheckpoint(state) {
		t.Error("should checkpoint when tokens exceed threshold")
	}
}

func TestShouldCheckpoint_BelowThreshold(t *testing.T) {
	m := &Manager{TokenThreshold: 10000}
	state := IterationState{TokensConsumed: 5000, LastCheckpointTokens: 0}
	if m.ShouldCheckpoint(state) {
		t.Error("should not checkpoint below threshold")
	}
}

func TestShouldCheckpoint_SideEffectBoundary(t *testing.T) {
	m := &Manager{TokenThreshold: 100000}
	state := IterationState{TokensConsumed: 100, HasSideEffects: true}
	if !m.ShouldCheckpoint(state) {
		t.Error("should checkpoint before side-effect step")
	}
}

func TestShouldCheckpoint_ExplicitRequest(t *testing.T) {
	m := &Manager{TokenThreshold: 100000}
	state := IterationState{ExplicitCheckpoint: true}
	if !m.ShouldCheckpoint(state) {
		t.Error("should checkpoint on explicit request")
	}
}

func TestBuildSnapshot(t *testing.T) {
	m := &Manager{}
	snap := m.BuildSnapshot(5, []string{"output1", "output2"}, "context so far")
	if snap.StepIndex != 5 {
		t.Errorf("StepIndex = %d, want 5", snap.StepIndex)
	}
	if snap.ContextSummary != "context so far" {
		t.Errorf("ContextSummary = %q", snap.ContextSummary)
	}
}

func TestRollbackTarget(t *testing.T) {
	step, err := RollbackTarget(5, []int{1, 2, 3, 4})
	if err != nil {
		t.Fatalf("RollbackTarget: %v", err)
	}
	if step != 4 {
		t.Errorf("step = %d, want 4", step)
	}
}

func TestRollbackTarget_NoneAvailable(t *testing.T) {
	_, err := RollbackTarget(1, []int{2, 3})
	if err == nil {
		t.Error("expected error when no earlier checkpoint exists")
	}
}
