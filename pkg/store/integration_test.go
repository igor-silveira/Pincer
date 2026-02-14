package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/igorsilveira/pincer/pkg/audit"
	"github.com/igorsilveira/pincer/pkg/credentials"
	"github.com/igorsilveira/pincer/pkg/memory"
	"github.com/igorsilveira/pincer/pkg/store"
)

func testIntegrationStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "integration.db")
	s, err := store.New(dsn)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStoreToMemoryIntegration(t *testing.T) {
	s := testIntegrationStore(t)
	ctx := context.Background()

	mem := memory.New(s.DB(), nil)

	if err := mem.Set(ctx, "agent-1", "key", "value"); err != nil {
		t.Fatalf("memory.Set: %v", err)
	}

	e, err := mem.Get(ctx, "agent-1", "key")
	if err != nil {
		t.Fatalf("memory.Get: %v", err)
	}
	if e.Value != "value" {
		t.Errorf("Value = %q, want %q", e.Value, "value")
	}
}

func TestStoreToCredentialsIntegration(t *testing.T) {
	s := testIntegrationStore(t)
	ctx := context.Background()

	creds, err := credentials.New(s.DB(), "test-master-key")
	if err != nil {
		t.Fatalf("credentials.New: %v", err)
	}

	if err := creds.Set(ctx, "api-key", "sk-test"); err != nil {
		t.Fatalf("credentials.Set: %v", err)
	}

	val, err := creds.Get(ctx, "api-key")
	if err != nil {
		t.Fatalf("credentials.Get: %v", err)
	}
	if val != "sk-test" {
		t.Errorf("Get = %q, want %q", val, "sk-test")
	}
}

func TestStoreToAuditIntegration(t *testing.T) {
	s := testIntegrationStore(t)
	ctx := context.Background()

	logger, err := audit.New(s.DB())
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}

	if err := logger.Log(ctx, audit.EventToolExec, "s1", "a1", "user", "test"); err != nil {
		t.Fatalf("audit.Log: %v", err)
	}

	entries, err := logger.Query(ctx, audit.Filter{SessionID: "s1"})
	if err != nil {
		t.Fatalf("audit.Query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1", len(entries))
	}
}

func TestFullWiring(t *testing.T) {
	s := testIntegrationStore(t)
	ctx := context.Background()

	mem := memory.New(s.DB(), nil)
	creds, err := credentials.New(s.DB(), "master")
	if err != nil {
		t.Fatalf("credentials.New: %v", err)
	}
	auditLog, err := audit.New(s.DB())
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}

	sess := &store.Session{
		ID: "int-sess", AgentID: "a", Channel: "c", PeerID: "p",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := mem.Set(ctx, "a", "key", "val"); err != nil {
		t.Fatalf("memory.Set: %v", err)
	}

	if err := creds.Set(ctx, "secret", "password"); err != nil {
		t.Fatalf("credentials.Set: %v", err)
	}

	if err := auditLog.Log(ctx, audit.EventSessionNew, "int-sess", "a", "system", "created"); err != nil {
		t.Fatalf("audit.Log: %v", err)
	}

	if _, err := s.GetSession(ctx, "int-sess"); err != nil {
		t.Errorf("GetSession failed: %v", err)
	}
	if _, err := mem.Get(ctx, "a", "key"); err != nil {
		t.Errorf("memory.Get failed: %v", err)
	}
	if _, err := creds.Get(ctx, "secret"); err != nil {
		t.Errorf("credentials.Get failed: %v", err)
	}
	entries, _ := auditLog.Query(ctx, audit.Filter{SessionID: "int-sess"})
	if len(entries) != 1 {
		t.Errorf("audit entries = %d, want 1", len(entries))
	}
}

func TestWALModeEnabled(t *testing.T) {
	s := testIntegrationStore(t)

	sqlDB, err := s.DB().DB()
	if err != nil {
		t.Fatalf("getting sql.DB: %v", err)
	}

	var mode string
	if err := sqlDB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want %q", mode, "wal")
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	s := testIntegrationStore(t)

	sqlDB, err := s.DB().DB()
	if err != nil {
		t.Fatalf("getting sql.DB: %v", err)
	}

	var fk int
	if err := sqlDB.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestAutoMigrateIdempotentIntegration(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "idempotent.db")

	s1, err := store.New(dsn)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	s1.Close()

	s2, err := store.New(dsn)
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	s2.Close()
}
