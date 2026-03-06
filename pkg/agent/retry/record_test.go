package retry

import (
	"testing"
	"time"
)

func TestAttemptRecord_String(t *testing.T) {
	r := AttemptRecord{
		Strategy: "rephrase",
		Attempt:  1,
		Error:    "tool failed",
		Duration: 500 * time.Millisecond,
	}

	s := r.String()
	if s == "" {
		t.Error("String() should not be empty")
	}
}

func TestAttemptLog_Summary(t *testing.T) {
	log := &AttemptLog{}
	log.Add(AttemptRecord{Strategy: "tool_swap", Attempt: 1, Error: "timeout"})
	log.Add(AttemptRecord{Strategy: "rephrase", Attempt: 2, Error: "still failed"})

	if log.Count() != 2 {
		t.Errorf("Count = %d, want 2", log.Count())
	}

	summary := log.Summary()
	if summary == "" {
		t.Error("Summary should not be empty")
	}
}
