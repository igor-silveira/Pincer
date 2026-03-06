package retry

import (
	"context"
	"testing"
)

func TestEmitSpan_DoesNotPanic(t *testing.T) {
	ctx := context.Background()
	rec := AttemptRecord{
		Strategy: "rephrase",
		Attempt:  1,
		Error:    "some error",
	}
	// Should not panic even without a configured tracer
	EmitRetrySpan(ctx, rec)
}
