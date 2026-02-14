package agent

import (
	"context"
	"testing"
)

func TestContentHash_Deterministic(t *testing.T) {
	h1 := ContentHash("test input")
	h2 := ContentHash("test input")
	if h1 != h2 {
		t.Errorf("same input produced different hashes: %q vs %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64 (SHA-256 hex)", len(h1))
	}
}

func TestContentHash_DifferentInputs(t *testing.T) {
	h1 := ContentHash("input a")
	h2 := ContentHash("input b")
	if h1 == h2 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestWithAutoApprove(t *testing.T) {
	ctx := WithAutoApprove(context.Background())
	if !autoApproveFromContext(ctx) {
		t.Error("WithAutoApprove should make autoApproveFromContext return true")
	}
}

func TestAutoApproveFromContext_Default(t *testing.T) {
	if autoApproveFromContext(context.Background()) {
		t.Error("bare context should return false")
	}
}

func TestAutoApproveFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKeyAutoApprove, "not-a-bool")
	if autoApproveFromContext(ctx) {
		t.Error("non-bool value should return false")
	}
}
