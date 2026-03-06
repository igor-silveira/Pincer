package retry

import (
	"errors"
	"strings"
	"testing"
)

func TestRephrase_AlwaysApplies(t *testing.T) {
	r := Rephrase{}
	task := TaskContext{OriginalPrompt: "do something"}
	errs := []error{errors.New("connection timeout")}

	rt, ok := r.Reframe(task, errs)
	if !ok {
		t.Fatal("expected Rephrase to always apply")
	}
	if rt.Prompt != task.OriginalPrompt {
		t.Fatalf("expected prompt %q, got %q", task.OriginalPrompt, rt.Prompt)
	}
	if !strings.Contains(rt.EphemeralHint, "connection timeout") {
		t.Fatalf("expected hint to contain error message, got %q", rt.EphemeralHint)
	}
}

func TestRephrase_Priority(t *testing.T) {
	r := Rephrase{}
	if r.Priority() != 1 {
		t.Fatalf("expected priority 1, got %d", r.Priority())
	}
}

func TestDecompose_AppliesWhenPromptIsLong(t *testing.T) {
	d := Decompose{MinPromptLen: 50}
	task := TaskContext{OriginalPrompt: strings.Repeat("a", 100)}
	errs := []error{errors.New("failed")}

	rt, ok := d.Reframe(task, errs)
	if !ok {
		t.Fatal("expected Decompose to apply for long prompt")
	}
	if rt.Prompt != task.OriginalPrompt {
		t.Fatalf("expected prompt %q, got %q", task.OriginalPrompt, rt.Prompt)
	}
	if rt.EphemeralHint == "" {
		t.Fatal("expected non-empty hint")
	}
}

func TestDecompose_SkipsShortPrompt(t *testing.T) {
	d := Decompose{MinPromptLen: 50}
	task := TaskContext{OriginalPrompt: "short"}
	errs := []error{errors.New("failed")}

	_, ok := d.Reframe(task, errs)
	if ok {
		t.Fatal("expected Decompose to skip short prompt")
	}
}

func TestToolSwap_AppliesWhenToolError(t *testing.T) {
	ts := ToolSwap{
		Alternatives: map[string]string{
			"http_request": "browser",
		},
	}
	task := TaskContext{OriginalPrompt: "fetch data"}
	errs := []error{errors.New("http_request failed with 500")}

	rt, ok := ts.Reframe(task, errs)
	if !ok {
		t.Fatal("expected ToolSwap to apply when error mentions known tool")
	}
	if !strings.Contains(rt.EphemeralHint, "browser") {
		t.Fatalf("expected hint to suggest alternative tool 'browser', got %q", rt.EphemeralHint)
	}
}

func TestToolSwap_SkipsUnknownTool(t *testing.T) {
	ts := ToolSwap{
		Alternatives: map[string]string{
			"http_request": "browser",
		},
	}
	task := TaskContext{OriginalPrompt: "run command"}
	errs := []error{errors.New("shell command failed")}

	_, ok := ts.Reframe(task, errs)
	if ok {
		t.Fatal("expected ToolSwap to skip when no known tool in error")
	}
}
