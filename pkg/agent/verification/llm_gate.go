package verification

import (
	"context"
	"strings"
)

// LLMChecker abstracts an LLM call used for self-check verification.
type LLMChecker interface {
	Check(ctx context.Context, prompt string) (string, error)
}

// LLMSelfCheckGate asks an LLM whether the task result satisfies the original
// request and interprets the response as PASS, FAIL, or UNCERTAIN.
type LLMSelfCheckGate struct {
	LLM LLMChecker
}

func (g *LLMSelfCheckGate) Name() string                { return "llm_self_check" }
func (g *LLMSelfCheckGate) AppliesTo(_ TaskResult) bool { return true }

func (g *LLMSelfCheckGate) Verify(ctx context.Context, tr TaskResult) Result {
	if g.LLM == nil {
		return Result{Status: Uncertain, Confidence: 0, Evidence: "no LLM configured for verification"}
	}
	prompt := "Does this result satisfy the original request? Reply with exactly one of: PASS, FAIL, or UNCERTAIN, followed by a colon and a brief explanation.\n\nResult:\n" + tr.FinalMessage
	response, err := g.LLM.Check(ctx, prompt)
	if err != nil {
		return Result{Status: Uncertain, Confidence: 0, Evidence: "LLM check failed: " + err.Error()}
	}
	response = strings.TrimSpace(response)
	upper := strings.ToUpper(response)
	switch {
	case strings.HasPrefix(upper, "PASS"):
		return Result{Status: Confirmed, Evidence: response}
	case strings.HasPrefix(upper, "FAIL"):
		return Result{Status: Failed, Reason: response}
	default:
		return Result{Status: Uncertain, Confidence: 0.5, Evidence: response}
	}
}
