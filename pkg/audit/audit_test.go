package audit

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testLogger(t *testing.T) *Logger {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})

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
	if err := l.Log(ctx, EventToolExec, "", "", "agent", detail); err != nil {
		t.Fatalf("Log: %v", err)
	}

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

	if err := l.Log(ctx, EventToolExec, "s1", "a1", "user", "exec 1"); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := l.Log(ctx, EventMemorySet, "s1", "a1", "agent", "set key"); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := l.Log(ctx, EventToolExec, "s2", "a2", "user", "exec 2"); err != nil {
		t.Fatalf("Log: %v", err)
	}

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
	if err := l.Log(ctx, EventToolExec, "", "", "", "event"); err != nil {
		t.Fatalf("Log: %v", err)
	}

	entries, _ := l.Query(ctx, Filter{Since: before})
	if len(entries) != 1 {
		t.Errorf("since: len = %d, want 1", len(entries))
	}

	entries, _ = l.Query(ctx, Filter{Until: before})
	if len(entries) != 0 {
		t.Errorf("before event: len = %d, want 0", len(entries))
	}
}

func TestQueryOrdering(t *testing.T) {
	l := testLogger(t)
	ctx := context.Background()

	if err := l.Log(ctx, EventToolExec, "s1", "a1", "user", "first"); err != nil {
		t.Fatalf("Log: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := l.Log(ctx, EventToolExec, "s1", "a1", "user", "second"); err != nil {
		t.Fatalf("Log: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := l.Log(ctx, EventToolExec, "s1", "a1", "user", "third"); err != nil {
		t.Fatalf("Log: %v", err)
	}

	entries, err := l.Query(ctx, Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("len = %d, want 3", len(entries))
	}
	if entries[0].Detail != "third" {
		t.Errorf("entries[0].Detail = %q, want %q (DESC order)", entries[0].Detail, "third")
	}
	if entries[2].Detail != "first" {
		t.Errorf("entries[2].Detail = %q, want %q", entries[2].Detail, "first")
	}
}

func TestQueryCombinedFilters(t *testing.T) {
	l := testLogger(t)
	ctx := context.Background()

	if err := l.Log(ctx, EventToolExec, "s1", "a1", "user", "match"); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := l.Log(ctx, EventMemorySet, "s1", "a1", "agent", "wrong type"); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if err := l.Log(ctx, EventToolExec, "s2", "a2", "user", "wrong session"); err != nil {
		t.Fatalf("Log: %v", err)
	}

	entries, err := l.Query(ctx, Filter{EventType: EventToolExec, SessionID: "s1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1", len(entries))
	}
	if entries[0].Detail != "match" {
		t.Errorf("Detail = %q, want %q", entries[0].Detail, "match")
	}
}

func TestAutoMigrateIdempotent(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})

	if _, err := New(db); err != nil {
		t.Fatalf("first New: %v", err)
	}
	if _, err := New(db); err != nil {
		t.Fatalf("second New: %v", err)
	}
}

func TestQueryNoLimit(t *testing.T) {
	l := testLogger(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := l.Log(ctx, EventToolExec, "", "", "", fmt.Sprintf("event-%d", i)); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	entries, err := l.Query(ctx, Filter{Limit: 0})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("len = %d, want 5", len(entries))
	}
}
