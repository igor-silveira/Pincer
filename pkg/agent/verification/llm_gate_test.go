package verification

import (
	"context"
	"testing"
)

type stubLLM struct{ response string }

func (s *stubLLM) Check(ctx context.Context, prompt string) (string, error) {
	return s.response, nil
}

func TestLLMSelfCheckGate_Confirmed(t *testing.T) {
	g := &LLMSelfCheckGate{LLM: &stubLLM{response: "PASS: The task was completed successfully."}}
	result := g.Verify(context.Background(), TaskResult{FinalMessage: "done"})
	if result.Status != Confirmed {
		t.Errorf("status = %v, want Confirmed", result.Status)
	}
}

func TestLLMSelfCheckGate_Failed(t *testing.T) {
	g := &LLMSelfCheckGate{LLM: &stubLLM{response: "FAIL: The output does not match the request."}}
	result := g.Verify(context.Background(), TaskResult{FinalMessage: "done"})
	if result.Status != Failed {
		t.Errorf("status = %v, want Failed", result.Status)
	}
}

func TestLLMSelfCheckGate_Uncertain(t *testing.T) {
	g := &LLMSelfCheckGate{LLM: &stubLLM{response: "UNCERTAIN: I'm not sure if this is correct."}}
	result := g.Verify(context.Background(), TaskResult{FinalMessage: "done"})
	if result.Status != Uncertain {
		t.Errorf("status = %v, want Uncertain", result.Status)
	}
}

func TestLLMSelfCheckGate_AlwaysApplies(t *testing.T) {
	g := &LLMSelfCheckGate{}
	if !g.AppliesTo(TaskResult{}) {
		t.Error("LLMSelfCheckGate should always apply")
	}
}
