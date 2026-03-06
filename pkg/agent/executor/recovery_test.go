package executor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestClassifyError_Transient(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"deadline exceeded", context.DeadlineExceeded},
		{"canceled", context.Canceled},
		{"timeout string", fmt.Errorf("command timeout after 30s")},
		{"temporary string", fmt.Errorf("temporary failure")},
		{"connection refused", fmt.Errorf("dial tcp: connection refused")},
		{"wrapped deadline", fmt.Errorf("exec: %w", context.DeadlineExceeded)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyError(tt.err); got != Transient {
				t.Errorf("ClassifyError(%v) = %d, want Transient", tt.err, got)
			}
		})
	}
}

func TestClassifyError_Permanent(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"permission denied", fmt.Errorf("permission denied")},
		{"not found", fmt.Errorf("file not found")},
		{"generic", fmt.Errorf("something broke")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyError(tt.err); got != Permanent {
				t.Errorf("ClassifyError(%v) = %d, want Permanent", tt.err, got)
			}
		})
	}
}

func TestDefaultRecovery_TransientRetry(t *testing.T) {
	r := &DefaultRecovery{MaxRetries: 2}
	result := Result{Err: context.DeadlineExceeded}

	if got := r.Decide(result, 0); got != ActionRetry {
		t.Errorf("attempt 0: got %d, want ActionRetry", got)
	}
	if got := r.Decide(result, 1); got != ActionRetry {
		t.Errorf("attempt 1: got %d, want ActionRetry", got)
	}
	if got := r.Decide(result, 2); got != ActionReplan {
		t.Errorf("attempt 2 (exhausted): got %d, want ActionReplan", got)
	}
}

func TestDefaultRecovery_PermanentSkips(t *testing.T) {
	r := &DefaultRecovery{MaxRetries: 5}
	result := Result{Err: fmt.Errorf("permission denied")}

	if got := r.Decide(result, 0); got != ActionSkip {
		t.Errorf("permanent error: got %d, want ActionSkip", got)
	}
}

func TestDefaultRecovery_NilError(t *testing.T) {
	r := &DefaultRecovery{MaxRetries: 2}
	result := Result{Output: "ok"}

	if got := r.Decide(result, 0); got != ActionSkip {
		t.Errorf("nil error: got %d, want ActionSkip", got)
	}
}

func TestDefaultRecovery_Backoff(t *testing.T) {
	r := &DefaultRecovery{
		MaxRetries: 3,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   1 * time.Second,
	}

	if got := r.Backoff(0); got != 100*time.Millisecond {
		t.Errorf("backoff(0) = %v, want 100ms", got)
	}
	if got := r.Backoff(1); got != 200*time.Millisecond {
		t.Errorf("backoff(1) = %v, want 200ms", got)
	}
	if got := r.Backoff(2); got != 400*time.Millisecond {
		t.Errorf("backoff(2) = %v, want 400ms", got)
	}
	if got := r.Backoff(10); got != 1*time.Second {
		t.Errorf("backoff(10) = %v, want 1s (capped)", got)
	}
}

func TestErrorSummary_String(t *testing.T) {
	es := ErrorSummary{
		FailedTools: []FailedToolInfo{
			{Name: "shell", Error: "timeout after 30s", Retries: 2},
			{Name: "http_request", Error: "connection refused", Retries: 2},
		},
	}
	s := es.String()
	if s == "" {
		t.Fatal("ErrorSummary.String() returned empty")
	}
	for _, want := range []string{"shell", "timeout after 30s", "http_request", "connection refused", "different approach"} {
		if !strings.Contains(s, want) {
			t.Errorf("ErrorSummary.String() missing %q", want)
		}
	}
}
