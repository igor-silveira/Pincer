package filecache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tmpFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "fc-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestGet_CacheMiss(t *testing.T) {
	c := New()
	path := tmpFile(t, "hello")

	data, err := c.Get(path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("data = %q, want %q", data, "hello")
	}
	if c.Len() != 1 {
		t.Errorf("Len = %d, want 1", c.Len())
	}
}

func TestGet_CacheHit(t *testing.T) {
	c := New()
	path := tmpFile(t, "cached")

	if _, err := c.Get(path); err != nil {
		t.Fatal(err)
	}

	// Overwrite the file on disk.
	if err := os.WriteFile(path, []byte("updated"), 0600); err != nil {
		t.Fatal(err)
	}

	// Second call should return stale cached content.
	data, err := c.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cached" {
		t.Errorf("data = %q, want stale %q", data, "cached")
	}
}

func TestGet_ReturnsCopy(t *testing.T) {
	c := New()
	path := tmpFile(t, "immutable")

	data1, _ := c.Get(path)
	data1[0] = 'X'

	data2, _ := c.Get(path)
	if string(data2) != "immutable" {
		t.Errorf("cached data was mutated: %q", data2)
	}
}

func TestGet_RenewsTTL(t *testing.T) {
	c := New(WithTTL(80 * time.Millisecond))
	path := tmpFile(t, "alive")

	c.Get(path)

	// At 50ms, read again to renew TTL.
	time.Sleep(50 * time.Millisecond)
	c.Get(path)

	// At 100ms from start (50ms since last access), TTL (80ms) not exceeded.
	time.Sleep(50 * time.Millisecond)
	c.refreshAll()
	if c.Len() != 1 {
		t.Errorf("entry should survive — last access was 50ms ago, TTL is 80ms")
	}

	// Wait past TTL without reading.
	time.Sleep(90 * time.Millisecond)
	c.refreshAll()
	if c.Len() != 0 {
		t.Errorf("entry should be evicted — last access was >80ms ago")
	}
}

func TestRefreshAll_TTLEvictsUnusedEntry(t *testing.T) {
	c := New(WithTTL(20 * time.Millisecond))
	path := tmpFile(t, "forgotten")

	c.Get(path)
	time.Sleep(30 * time.Millisecond)

	c.refreshAll()
	if c.Len() != 0 {
		t.Errorf("Len = %d, want 0 — entry exceeded TTL without reads", c.Len())
	}
}

func TestRefreshAll_TTLKeepsActiveEntry(t *testing.T) {
	c := New(WithTTL(100 * time.Millisecond))
	path := tmpFile(t, "active")

	c.Get(path)
	time.Sleep(10 * time.Millisecond)

	c.refreshAll()
	if c.Len() != 1 {
		t.Errorf("Len = %d, want 1 — entry should survive within TTL", c.Len())
	}
}

func TestRefreshAll_DoesNotRenewTTL(t *testing.T) {
	c := New(WithTTL(60 * time.Millisecond))
	path := tmpFile(t, "v1")

	c.Get(path)

	// Update file on disk so refresh re-reads it.
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(path, []byte("v2"), 0600)

	// Refresh picks up the change but must NOT renew lastAccess.
	c.refreshAll()
	data, _ := c.Get(path) // this renews it
	if string(data) != "v2" {
		t.Errorf("data = %q, want %q", data, "v2")
	}

	// Now stop reading and let TTL expire.
	time.Sleep(70 * time.Millisecond)
	c.refreshAll()
	if c.Len() != 0 {
		t.Errorf("Len = %d, want 0 — TTL should not be renewed by background refresh alone", c.Len())
	}
}

func TestInvalidate(t *testing.T) {
	c := New()
	path := tmpFile(t, "v1")

	c.Get(path)
	if c.Len() != 1 {
		t.Fatal("expected 1 entry")
	}

	c.Invalidate(path)
	if c.Len() != 0 {
		t.Errorf("Len = %d after Invalidate, want 0", c.Len())
	}
}

func TestGet_LargeFileNotCached(t *testing.T) {
	c := New(WithMaxFileSize(10))
	path := tmpFile(t, "this is longer than 10 bytes")

	data, err := c.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "this is longer than 10 bytes" {
		t.Errorf("data = %q", data)
	}
	if c.Len() != 0 {
		t.Errorf("large file should not be cached, Len = %d", c.Len())
	}
}

func TestEviction_MaxEntries(t *testing.T) {
	c := New(WithMaxEntries(2))

	p1 := tmpFile(t, "one")
	p2 := tmpFile(t, "two")
	p3 := tmpFile(t, "three")

	c.Get(p1)
	time.Sleep(time.Millisecond)
	c.Get(p2)
	time.Sleep(time.Millisecond)
	c.Get(p3) // should evict p1 (least recently accessed)

	if c.Len() != 2 {
		t.Errorf("Len = %d, want 2", c.Len())
	}

	// p1 should have been evicted (oldest access).
	c.mu.RLock()
	_, hasP1 := c.entries[p1]
	c.mu.RUnlock()
	if hasP1 {
		t.Error("p1 should have been evicted")
	}
}

func TestRefreshAll_UpdatesChangedFile(t *testing.T) {
	c := New()
	path := tmpFile(t, "original")

	c.Get(path)

	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, []byte("refreshed"), 0600); err != nil {
		t.Fatal(err)
	}

	c.refreshAll()

	data, _ := c.Get(path)
	if string(data) != "refreshed" {
		t.Errorf("data = %q after refresh, want %q", data, "refreshed")
	}
}

func TestRefreshAll_EvictsDeletedFile(t *testing.T) {
	c := New()
	path := tmpFile(t, "temp")

	c.Get(path)
	os.Remove(path)
	c.refreshAll()

	if c.Len() != 0 {
		t.Errorf("Len = %d after refresh of deleted file, want 0", c.Len())
	}
}

func TestGet_NonexistentFile(t *testing.T) {
	c := New()
	_, err := c.Get(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
	if c.Len() != 0 {
		t.Error("should not cache failed reads")
	}
}

func TestStop_TerminatesRefreshLoop(t *testing.T) {
	c := New(WithRefreshInterval(10 * time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Start(ctx)
	c.Stop()
	c.Stop() // double-stop should not panic
}
