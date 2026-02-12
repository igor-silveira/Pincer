package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testDB(t *testing.T) *gorm.DB {
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

	if err := db.AutoMigrate(&Entry{}); err != nil {
		t.Fatal(err)
	}

	return db
}

func TestSetAndGet(t *testing.T) {
	s := New(testDB(t), nil)
	ctx := context.Background()

	if err := s.Set(ctx, "agent-1", "name", "Pincer"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	e, err := s.Get(ctx, "agent-1", "name")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if e.Value != "Pincer" {
		t.Errorf("Value = %q, want %q", e.Value, "Pincer")
	}
}

func TestImmutableKey(t *testing.T) {
	s := New(testDB(t), []string{"identity"})
	ctx := context.Background()

	if err := s.Set(ctx, "agent-1", "identity", "I am Pincer"); err != nil {
		t.Fatalf("first Set: %v", err)
	}

	err := s.Set(ctx, "agent-1", "identity", "I am evil")
	if err == nil {
		t.Fatal("expected error on immutable key overwrite")
	}

	err = s.Delete(ctx, "agent-1", "identity")
	if err == nil {
		t.Fatal("expected error on immutable key delete")
	}
}

func TestList(t *testing.T) {
	s := New(testDB(t), nil)
	ctx := context.Background()

	s.Set(ctx, "agent-1", "a", "1")
	s.Set(ctx, "agent-1", "b", "2")
	s.Set(ctx, "agent-2", "c", "3")

	entries, err := s.List(ctx, "agent-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("len = %d, want 2", len(entries))
	}
}

func TestSearch(t *testing.T) {
	s := New(testDB(t), nil)
	ctx := context.Background()

	s.Set(ctx, "agent-1", "greeting", "Hello world")
	s.Set(ctx, "agent-1", "farewell", "Goodbye")

	results, err := s.Search(ctx, "agent-1", "world")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len = %d, want 1", len(results))
	}
}

func TestDiff(t *testing.T) {
	s := New(testDB(t), nil)
	ctx := context.Background()

	before := time.Now().UTC().Add(-time.Second)
	s.Set(ctx, "agent-1", "new-key", "new-value")

	entries, err := s.Diff(ctx, "agent-1", before)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("len = %d, want 1", len(entries))
	}
}

func TestBuildContext(t *testing.T) {
	s := New(testDB(t), nil)
	ctx := context.Background()

	s.Set(ctx, "agent-1", "identity", "I am Pincer")
	s.Set(ctx, "agent-1", "role", "Assistant")

	text, hashes, err := s.BuildContext(ctx, "agent-1", nil)
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	if text == "" {
		t.Error("expected non-empty context")
	}
	if len(hashes) != 2 {
		t.Errorf("len(hashes) = %d, want 2", len(hashes))
	}

	text2, _, err := s.BuildContext(ctx, "agent-1", hashes)
	if err != nil {
		t.Fatalf("BuildContext (2): %v", err)
	}
	if text2 != "" {
		t.Errorf("expected empty context for unchanged data, got %q", text2)
	}
}

func TestDelete(t *testing.T) {
	s := New(testDB(t), nil)
	ctx := context.Background()

	s.Set(ctx, "agent-1", "temp", "value")
	if err := s.Delete(ctx, "agent-1", "temp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := s.Get(ctx, "agent-1", "temp")
	if err == nil {
		t.Error("expected error after delete")
	}
}
