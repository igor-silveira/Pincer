package verification

import (
	"context"
	"testing"
)

func TestRunner_AllConfirmed(t *testing.T) {
	gates := []Gate{
		&stubGate{applies: true, result: Result{Status: Confirmed, Evidence: "looks good"}},
		&stubGate{applies: true, result: Result{Status: Confirmed, Evidence: "also good"}},
	}
	r := NewRunner(gates, 0.8, 2)
	result := r.Run(context.Background(), TaskResult{})
	if result.Status != Confirmed {
		t.Errorf("status = %v, want Confirmed", result.Status)
	}
}

func TestRunner_AnyFailed(t *testing.T) {
	gates := []Gate{
		&stubGate{applies: true, result: Result{Status: Confirmed, Evidence: "ok"}},
		&stubGate{applies: true, result: Result{Status: Failed, Reason: "tests fail"}},
	}
	r := NewRunner(gates, 0.8, 2)
	result := r.Run(context.Background(), TaskResult{})
	if result.Status != Failed {
		t.Errorf("status = %v, want Failed", result.Status)
	}
	if result.Reason != "tests fail" {
		t.Errorf("reason = %q, want %q", result.Reason, "tests fail")
	}
}

func TestRunner_UncertainBelowThreshold(t *testing.T) {
	gates := []Gate{
		&stubGate{applies: true, result: Result{Status: Uncertain, Confidence: 0.5, Evidence: "not sure"}},
	}
	r := NewRunner(gates, 0.8, 2)
	result := r.Run(context.Background(), TaskResult{})
	if result.Status != Uncertain {
		t.Errorf("status = %v, want Uncertain", result.Status)
	}
}

func TestRunner_UncertainAboveThreshold(t *testing.T) {
	gates := []Gate{
		&stubGate{applies: true, result: Result{Status: Uncertain, Confidence: 0.9, Evidence: "pretty sure"}},
	}
	r := NewRunner(gates, 0.8, 2)
	result := r.Run(context.Background(), TaskResult{})
	if result.Status != Confirmed {
		t.Errorf("status = %v, want Confirmed (confidence above threshold)", result.Status)
	}
}

func TestRunner_SkipsInapplicable(t *testing.T) {
	gates := []Gate{
		&stubGate{applies: false, result: Result{Status: Failed, Reason: "should not run"}},
		&stubGate{applies: true, result: Result{Status: Confirmed, Evidence: "ok"}},
	}
	r := NewRunner(gates, 0.8, 2)
	result := r.Run(context.Background(), TaskResult{})
	if result.Status != Confirmed {
		t.Errorf("status = %v, want Confirmed", result.Status)
	}
}

func TestRunner_NoApplicableGates(t *testing.T) {
	gates := []Gate{
		&stubGate{applies: false, result: Result{Status: Failed}},
	}
	r := NewRunner(gates, 0.8, 2)
	result := r.Run(context.Background(), TaskResult{})
	if result.Status != Confirmed {
		t.Error("no applicable gates should default to Confirmed")
	}
}

type stubGate struct {
	applies bool
	result  Result
}

func (g *stubGate) Name() string                                     { return "stub" }
func (g *stubGate) AppliesTo(tr TaskResult) bool                     { return g.applies }
func (g *stubGate) Verify(ctx context.Context, tr TaskResult) Result { return g.result }
