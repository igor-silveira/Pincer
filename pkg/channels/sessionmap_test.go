package channels

import (
	"fmt"
	"sync"
	"testing"
)

func TestSessionMapGetOrCreate(t *testing.T) {
	sm := NewSessionMap[string]("test", func(k string) string { return k })

	sid1 := sm.GetOrCreate("chan-1")
	sid2 := sm.GetOrCreate("chan-1")
	sid3 := sm.GetOrCreate("chan-2")

	if sid1 != sid2 {
		t.Errorf("same channel should return same session: got %s and %s", sid1, sid2)
	}
	if sid1 == sid3 {
		t.Error("different channels should return different sessions")
	}
	if sid1 != "test-chan-1" {
		t.Errorf("unexpected session ID: got %s, want test-chan-1", sid1)
	}
}

func TestSessionMapLookup(t *testing.T) {
	sm := NewSessionMap[int64]("tg", func(k int64) string { return fmt.Sprintf("%d", k) })

	if _, ok := sm.Lookup(123); ok {
		t.Error("lookup on empty map should return false")
	}

	sm.GetOrCreate(123)

	sid, ok := sm.Lookup(123)
	if !ok {
		t.Error("lookup should find existing channel")
	}
	if sid != "tg-123" {
		t.Errorf("unexpected session ID: got %s", sid)
	}
}

func TestSessionMapReverse(t *testing.T) {
	sm := NewSessionMap[string]("dc", func(k string) string { return k })

	sm.GetOrCreate("channel-abc")

	cid, ok := sm.Reverse("dc-channel-abc")
	if !ok {
		t.Error("reverse lookup should find existing session")
	}
	if cid != "channel-abc" {
		t.Errorf("unexpected channel ID: got %s", cid)
	}

	if _, ok := sm.Reverse("nonexistent"); ok {
		t.Error("reverse lookup should return false for unknown session")
	}
}

func TestSessionMapConcurrency(t *testing.T) {
	sm := NewSessionMap[int64]("tg", func(k int64) string { return fmt.Sprintf("%d", k) })

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			sm.GetOrCreate(id % 10)
		}(int64(i))
	}
	wg.Wait()

	for i := int64(0); i < 10; i++ {
		sid, ok := sm.Lookup(i)
		if !ok {
			t.Errorf("channel %d should exist", i)
		}
		if _, ok := sm.Reverse(sid); !ok {
			t.Errorf("reverse lookup should work for session %s", sid)
		}
	}
}
