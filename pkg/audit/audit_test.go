package audit

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testLogger(t *testing.T) *Logger {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	l, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestLogAndQuery(t *testing.T) {
	l := testLogger(t)
	ctx := context.Background()

	err := l.Log(ctx, EventToolExec, "sess-1", "agent-1", "user", "ran shell command")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	entries, err := l.Query(ctx, Filter{EventType: EventToolExec})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1", len(entries))
	}
	if entries[0].Detail != "ran shell command" {
		t.Errorf("Detail = %q, want %q", entries[0].Detail, "ran shell command")
	}
}

func TestLogStructuredDetail(t *testing.T) {
	l := testLogger(t)
	ctx := context.Background()

	detail := map[string]string{"tool": "shell", "command": "ls -la"}
	l.Log(ctx, EventToolExec, "", "", "agent", detail)

	entries, _ := l.Query(ctx, Filter{Limit: 1})
	if len(entries) == 0 {
		t.Fatal("no entries")
	}
	if entries[0].Detail == "" {
		t.Error("detail is empty")
	}
}

func TestQueryFilters(t *testing.T) {
	l := testLogger(t)
	ctx := context.Background()

	l.Log(ctx, EventToolExec, "s1", "a1", "user", "exec 1")
	l.Log(ctx, EventMemorySet, "s1", "a1", "agent", "set key")
	l.Log(ctx, EventToolExec, "s2", "a2", "user", "exec 2")

	entries, _ := l.Query(ctx, Filter{EventType: EventToolExec})
	if len(entries) != 2 {
		t.Errorf("by event: len = %d, want 2", len(entries))
	}

	entries, _ = l.Query(ctx, Filter{SessionID: "s1"})
	if len(entries) != 2 {
		t.Errorf("by session: len = %d, want 2", len(entries))
	}

	entries, _ = l.Query(ctx, Filter{AgentID: "a2"})
	if len(entries) != 1 {
		t.Errorf("by agent: len = %d, want 1", len(entries))
	}

	entries, _ = l.Query(ctx, Filter{Limit: 1})
	if len(entries) != 1 {
		t.Errorf("by limit: len = %d, want 1", len(entries))
	}
}

func TestQueryTimeRange(t *testing.T) {
	l := testLogger(t)
	ctx := context.Background()

	before := time.Now().UTC().Add(-time.Second)
	l.Log(ctx, EventToolExec, "", "", "", "event")

	entries, _ := l.Query(ctx, Filter{Since: before})
	if len(entries) != 1 {
		t.Errorf("since: len = %d, want 1", len(entries))
	}

	entries, _ = l.Query(ctx, Filter{Until: before})
	if len(entries) != 0 {
		t.Errorf("before event: len = %d, want 0", len(entries))
	}
}
