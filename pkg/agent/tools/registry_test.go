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

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{name: "a"})
	r.Register(&dummyTool{name: "b"})

	r.Unregister("a")
	if _, err := r.Get("a"); err == nil {
		t.Error("expected error after unregister")
	}
	if _, err := r.Get("b"); err != nil {
		t.Errorf("tool b should still exist: %v", err)
	}
	if len(r.Definitions()) != 1 {
		t.Errorf("Definitions len = %d, want 1", len(r.Definitions()))
	}
}

func TestRegistry_UnregisterNonExistent(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{name: "a"})
	r.Unregister("nonexistent")
	if len(r.Definitions()) != 1 {
		t.Errorf("Definitions len = %d, want 1", len(r.Definitions()))
	}
}

func TestDefaultRegistry_ContainsExpected(t *testing.T) {
	r := DefaultRegistry(nil)
	expected := []string{"shell", "file_read", "file_write", "http_request"}
	for _, name := range expected {
		if _, err := r.Get(name); err != nil {
			t.Errorf("missing expected tool %q: %v", name, err)
		}
	}
}

func TestDefaultRegistry_Count(t *testing.T) {
	r := DefaultRegistry(nil)
	defs := r.Definitions()
	if len(defs) != 4 {
		t.Errorf("DefaultRegistry has %d tools, want 4", len(defs))
	}
}

func TestRegistry_Filter_Subset(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{name: "a"})
	r.Register(&dummyTool{name: "b"})
	r.Register(&dummyTool{name: "c"})

	filtered := r.Filter([]string{"a", "c"})
	defs := filtered.Definitions()
	if len(defs) != 2 {
		t.Errorf("Filter len = %d, want 2", len(defs))
	}
	if _, err := filtered.Get("a"); err != nil {
		t.Errorf("Filter should include 'a': %v", err)
	}
	if _, err := filtered.Get("c"); err != nil {
		t.Errorf("Filter should include 'c': %v", err)
	}
	if _, err := filtered.Get("b"); err == nil {
		t.Error("Filter should exclude 'b'")
	}
}

func TestRegistry_Filter_Empty(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{name: "a"})

	filtered := r.Filter(nil)
	if filtered != r {
		t.Error("Filter(nil) should return original registry")
	}
	filtered = r.Filter([]string{})
	if filtered != r {
		t.Error("Filter([]) should return original registry")
	}
}

func TestRegistry_Filter_UnknownNames(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{name: "a"})

	filtered := r.Filter([]string{"a", "nonexistent"})
	if len(filtered.Definitions()) != 1 {
		t.Errorf("Filter should have 1 tool, got %d", len(filtered.Definitions()))
	}
}

func TestRegistry_Without_Excludes(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{name: "a"})
	r.Register(&dummyTool{name: "b"})
	r.Register(&dummyTool{name: "c"})

	without := r.Without([]string{"b"})
	defs := without.Definitions()
	if len(defs) != 2 {
		t.Errorf("Without len = %d, want 2", len(defs))
	}
	if _, err := without.Get("b"); err == nil {
		t.Error("Without should exclude 'b'")
	}
	if _, err := without.Get("a"); err != nil {
		t.Errorf("Without should keep 'a': %v", err)
	}
}

func TestRegistry_Without_Empty(t *testing.T) {
	r := NewRegistry()
	r.Register(&dummyTool{name: "a"})

	without := r.Without(nil)
	if without != r {
		t.Error("Without(nil) should return original registry")
	}
	without = r.Without([]string{})
	if without != r {
		t.Error("Without([]) should return original registry")
	}
}
