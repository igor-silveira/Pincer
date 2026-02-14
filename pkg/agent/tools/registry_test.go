package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type dummyTool struct {
	name string
}

func (d *dummyTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{Name: d.name, Description: "test tool"}
}

func (d *dummyTool) Execute(_ context.Context, _ json.RawMessage, _ sandbox.Sandbox, _ sandbox.Policy) (string, error) {
	return "ok", nil
}

func TestNewRegistry_Empty(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("anything")
	if err == nil {
		t.Error("expected error for empty registry")
	}
	defs := r.Definitions()
	if len(defs) != 0 {
		t.Errorf("Definitions len = %d, want 0", len(defs))
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{name: "test"})

	tool, err := r.Get("test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if tool.Definition().Name != "test" {
		t.Errorf("Name = %q, want %q", tool.Definition().Name, "test")
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{name: "known"})
	_, err := r.Get("unknown")
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestRegistry_Definitions(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{name: "a"})
	r.Register(&dummyTool{name: "b"})
	r.Register(&dummyTool{name: "c"})

	defs := r.Definitions()
	if len(defs) != 3 {
		t.Errorf("Definitions len = %d, want 3", len(defs))
	}
}

func TestRegistry_RegisterOverwrite(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{name: "x"})
	r.Register(&dummyTool{name: "x"})

	defs := r.Definitions()
	if len(defs) != 1 {
		t.Errorf("Definitions len = %d, want 1 (overwritten)", len(defs))
	}
}

func TestDefaultRegistry_ContainsExpected(t *testing.T) {
	r := DefaultRegistry()
	expected := []string{"shell", "file_read", "file_write", "http_request", "browser"}
	for _, name := range expected {
		if _, err := r.Get(name); err != nil {
			t.Errorf("missing expected tool %q: %v", name, err)
		}
	}
}

func TestDefaultRegistry_Count(t *testing.T) {
	r := DefaultRegistry()
	defs := r.Definitions()
	if len(defs) != 5 {
		t.Errorf("DefaultRegistry has %d tools, want 5", len(defs))
	}
}
