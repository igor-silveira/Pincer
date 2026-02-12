package credentials

import (
	"context"
	"path/filepath"
	"testing"

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

	if err := db.AutoMigrate(&Credential{}); err != nil {
		t.Fatal(err)
	}

	return db
}

func TestNewRequiresKey(t *testing.T) {
	_, err := New(testDB(t), "")
	if err == nil {
		t.Fatal("expected error for empty master key")
	}
}

func TestSetAndGet(t *testing.T) {
	s, err := New(testDB(t), "test-master-key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	if err := s.Set(ctx, "api-key", "sk-123456"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, err := s.Get(ctx, "api-key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "sk-123456" {
		t.Errorf("Get = %q, want %q", val, "sk-123456")
	}
}

func TestGetNotFound(t *testing.T) {
	s, _ := New(testDB(t), "test-key")
	_, err := s.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
}

func TestUpsert(t *testing.T) {
	s, _ := New(testDB(t), "test-key")
	ctx := context.Background()

	s.Set(ctx, "token", "old-value")
	s.Set(ctx, "token", "new-value")

	val, _ := s.Get(ctx, "token")
	if val != "new-value" {
		t.Errorf("Get after upsert = %q, want %q", val, "new-value")
	}
}

func TestDelete(t *testing.T) {
	s, _ := New(testDB(t), "test-key")
	ctx := context.Background()

	s.Set(ctx, "temp", "value")
	if err := s.Delete(ctx, "temp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := s.Get(ctx, "temp")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteNotFound(t *testing.T) {
	s, _ := New(testDB(t), "test-key")
	err := s.Delete(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent delete")
	}
}

func TestList(t *testing.T) {
	s, _ := New(testDB(t), "test-key")
	ctx := context.Background()

	s.Set(ctx, "b-key", "val")
	s.Set(ctx, "a-key", "val")

	names, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("len = %d, want 2", len(names))
	}

	if names[0] != "a-key" {
		t.Errorf("first = %q, want %q", names[0], "a-key")
	}
}

func TestWrongKeyCannotDecrypt(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	s1, _ := New(db, "key-one")
	s1.Set(ctx, "secret", "plaintext")

	s2, _ := New(db, "key-two")
	_, err := s2.Get(ctx, "secret")
	if err == nil {
		t.Fatal("expected decryption error with wrong key")
	}
}
