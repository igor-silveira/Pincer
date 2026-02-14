package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/igorsilveira/pincer/pkg/memory"
	"github.com/igorsilveira/pincer/pkg/sandbox"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestMemoryStore(t *testing.T) *memory.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	if err := db.AutoMigrate(&memory.Entry{}); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	return memory.New(db, nil)
}

func memoryCtx() context.Context {
	return WithSessionInfo(context.Background(), "sess-1", "agent-1")
}

func TestMemoryTool_Set(t *testing.T) {
	tool := &MemoryTool{Memory: newTestMemoryStore(t)}
	input, _ := json.Marshal(memoryInput{Action: "set", Key: "name", Value: "pincer"})

	output, err := tool.Execute(memoryCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "name") {
		t.Errorf("output = %q, want key name mentioned", output)
	}
}

func TestMemoryTool_Get(t *testing.T) {
	store := newTestMemoryStore(t)
	ctx := memoryCtx()
	_ = store.Set(ctx, AgentIDFromContext(ctx), "color", "blue")

	tool := &MemoryTool{Memory: store}
	input, _ := json.Marshal(memoryInput{Action: "get", Key: "color"})

	output, err := tool.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output != "blue" {
		t.Errorf("output = %q, want %q", output, "blue")
	}
}

func TestMemoryTool_GetNotFound(t *testing.T) {
	tool := &MemoryTool{Memory: newTestMemoryStore(t)}
	input, _ := json.Marshal(memoryInput{Action: "get", Key: "missing"})

	_, err := tool.Execute(memoryCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestMemoryTool_Delete(t *testing.T) {
	store := newTestMemoryStore(t)
	ctx := memoryCtx()
	_ = store.Set(ctx, AgentIDFromContext(ctx), "temp", "val")

	tool := &MemoryTool{Memory: store}
	input, _ := json.Marshal(memoryInput{Action: "delete", Key: "temp"})

	output, err := tool.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "temp") {
		t.Errorf("output = %q, want key mentioned", output)
	}
}

func TestMemoryTool_DeleteNotFound(t *testing.T) {
	tool := &MemoryTool{Memory: newTestMemoryStore(t)}
	input, _ := json.Marshal(memoryInput{Action: "delete", Key: "nope"})

	_, err := tool.Execute(memoryCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for deleting missing key")
	}
}

func TestMemoryTool_ListEmpty(t *testing.T) {
	tool := &MemoryTool{Memory: newTestMemoryStore(t)}
	input, _ := json.Marshal(memoryInput{Action: "list"})

	output, err := tool.Execute(memoryCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output != "no memory entries" {
		t.Errorf("output = %q, want %q", output, "no memory entries")
	}
}

func TestMemoryTool_ListWithEntries(t *testing.T) {
	store := newTestMemoryStore(t)
	ctx := memoryCtx()
	agentID := AgentIDFromContext(ctx)
	_ = store.Set(ctx, agentID, "a", "1")
	_ = store.Set(ctx, agentID, "b", "2")

	tool := &MemoryTool{Memory: store}
	input, _ := json.Marshal(memoryInput{Action: "list"})

	output, err := tool.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "[a]: 1") || !strings.Contains(output, "[b]: 2") {
		t.Errorf("output = %q, want entries listed", output)
	}
}

func TestMemoryTool_SearchFound(t *testing.T) {
	store := newTestMemoryStore(t)
	ctx := memoryCtx()
	agentID := AgentIDFromContext(ctx)
	_ = store.Set(ctx, agentID, "project", "pincer")
	_ = store.Set(ctx, agentID, "language", "go")

	tool := &MemoryTool{Memory: store}
	input, _ := json.Marshal(memoryInput{Action: "search", Query: "pincer"})

	output, err := tool.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "pincer") {
		t.Errorf("output = %q, want match for 'pincer'", output)
	}
}

func TestMemoryTool_SearchNotFound(t *testing.T) {
	tool := &MemoryTool{Memory: newTestMemoryStore(t)}
	input, _ := json.Marshal(memoryInput{Action: "search", Query: "xyz"})

	output, err := tool.Execute(memoryCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output != "no matching entries" {
		t.Errorf("output = %q, want %q", output, "no matching entries")
	}
}

func TestMemoryTool_UnknownAction(t *testing.T) {
	tool := &MemoryTool{Memory: newTestMemoryStore(t)}
	input, _ := json.Marshal(memoryInput{Action: "purge"})

	_, err := tool.Execute(memoryCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}
