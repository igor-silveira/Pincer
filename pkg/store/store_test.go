package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db")
	s, err := New(dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNewStore(t *testing.T) {
	s := testStore(t)
	if s == nil {
		t.Fatal("store is nil")
	}
}

func TestCreateAndGetSession(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sess := &Session{
		ID:        "sess-1",
		AgentID:   "agent-1",
		Channel:   "test",
		PeerID:    "peer-1",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := s.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", got.AgentID, "agent-1")
	}
}

func TestFindSession(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sess := &Session{
		ID:        "sess-2",
		AgentID:   "agent-1",
		Channel:   "telegram",
		PeerID:    "user-42",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	s.CreateSession(ctx, sess)

	found, err := s.FindSession(ctx, "agent-1", "telegram", "user-42")
	if err != nil {
		t.Fatalf("FindSession: %v", err)
	}
	if found.ID != "sess-2" {
		t.Errorf("ID = %q, want %q", found.ID, "sess-2")
	}
}

func TestAppendAndRecentMessages(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sess := &Session{
		ID: "sess-3", AgentID: "a", Channel: "c", PeerID: "p",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	s.CreateSession(ctx, sess)

	for i := 0; i < 5; i++ {
		s.AppendMessage(ctx, &Message{
			ID:         fmt.Sprintf("msg-%d", i),
			SessionID:  "sess-3",
			Role:       "user",
			Content:    fmt.Sprintf("message %d", i),
			TokenCount: 10,
			CreatedAt:  time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
	}

	msgs, err := s.RecentMessages(ctx, "sess-3", 3)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}

	if msgs[0].Content != "message 2" {
		t.Errorf("first message = %q, want %q", msgs[0].Content, "message 2")
	}
}

func TestMessageCount(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sess := &Session{
		ID: "sess-4", AgentID: "a", Channel: "c", PeerID: "p",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	s.CreateSession(ctx, sess)

	s.AppendMessage(ctx, &Message{
		ID: "m1", SessionID: "sess-4", Role: "user", Content: "hi",
		CreatedAt: time.Now().UTC(),
	})
	s.AppendMessage(ctx, &Message{
		ID: "m2", SessionID: "sess-4", Role: "assistant", Content: "hello",
		CreatedAt: time.Now().UTC(),
	})

	count, err := s.MessageCount(ctx, "sess-4")
	if err != nil {
		t.Fatalf("MessageCount: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestDeleteMessages(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sess := &Session{
		ID: "sess-5", AgentID: "a", Channel: "c", PeerID: "p",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	s.CreateSession(ctx, sess)

	s.AppendMessage(ctx, &Message{
		ID: "d1", SessionID: "sess-5", Role: "user", Content: "delete me",
		CreatedAt: time.Now().UTC(),
	})
	s.AppendMessage(ctx, &Message{
		ID: "d2", SessionID: "sess-5", Role: "user", Content: "keep me",
		CreatedAt: time.Now().UTC(),
	})

	if err := s.DeleteMessages(ctx, []string{"d1"}); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	count, _ := s.MessageCount(ctx, "sess-5")
	if count != 1 {
		t.Errorf("count after delete = %d, want 1", count)
	}
}

func TestSessionTokenUsage(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sess := &Session{
		ID: "sess-6", AgentID: "a", Channel: "c", PeerID: "p",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	s.CreateSession(ctx, sess)

	s.AppendMessage(ctx, &Message{
		ID: "t1", SessionID: "sess-6", Role: "user", Content: "test",
		TokenCount: 50, CreatedAt: time.Now().UTC(),
	})
	s.AppendMessage(ctx, &Message{
		ID: "t2", SessionID: "sess-6", Role: "assistant", Content: "response",
		TokenCount: 150, CreatedAt: time.Now().UTC(),
	})

	total, err := s.SessionTokenUsage(ctx, "sess-6")
	if err != nil {
		t.Fatalf("SessionTokenUsage: %v", err)
	}
	if total != 200 {
		t.Errorf("total = %d, want 200", total)
	}
}

func TestDBAccessor(t *testing.T) {
	s := testStore(t)
	if s.DB() == nil {
		t.Fatal("DB() returned nil")
	}
}

func TestGetSessionNotFound(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	_, err := s.GetSession(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("expected gorm.ErrRecordNotFound, got: %v", err)
	}
}

func TestFindSessionNotFound(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	_, err := s.FindSession(ctx, "x", "y", "z")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("expected gorm.ErrRecordNotFound, got: %v", err)
	}
}

func TestTouchSession(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	original := time.Now().UTC().Add(-time.Minute)
	sess := &Session{
		ID: "touch-1", AgentID: "a", Channel: "c", PeerID: "p",
		CreatedAt: original, UpdatedAt: original,
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.TouchSession(ctx, "touch-1"); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	got, err := s.GetSession(ctx, "touch-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !got.UpdatedAt.After(original) {
		t.Errorf("UpdatedAt not advanced: got %v, original %v", got.UpdatedAt, original)
	}
}

func TestCreateDuplicateSession(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sess := &Session{
		ID: "dup-1", AgentID: "a", Channel: "c", PeerID: "p",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("first create: %v", err)
	}

	dup := &Session{
		ID: "dup-1", AgentID: "b", Channel: "d", PeerID: "q",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.CreateSession(ctx, dup); err == nil {
		t.Fatal("expected error for duplicate primary key")
	}
}

func TestDeleteMessagesEmpty(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.DeleteMessages(ctx, nil); err != nil {
		t.Errorf("DeleteMessages(nil): %v", err)
	}
	if err := s.DeleteMessages(ctx, []string{}); err != nil {
		t.Errorf("DeleteMessages(empty): %v", err)
	}
}

func TestDeleteMessagesNonExistent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.DeleteMessages(ctx, []string{"nonexistent-id"}); err != nil {
		t.Errorf("DeleteMessages(nonexistent): %v", err)
	}
}

func TestRecentMessagesEmpty(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sess := &Session{
		ID: "empty-msgs", AgentID: "a", Channel: "c", PeerID: "p",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	s.CreateSession(ctx, sess)

	msgs, err := s.RecentMessages(ctx, "empty-msgs", 10)
	if err != nil {
		t.Fatalf("RecentMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("len = %d, want 0", len(msgs))
	}
}

func TestSessionTokenUsageEmpty(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	total, err := s.SessionTokenUsage(ctx, "nonexistent-session")
	if err != nil {
		t.Fatalf("SessionTokenUsage: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
}

func TestAppendMessageDefaultContentType(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sess := &Session{
		ID: "ct-sess", AgentID: "a", Channel: "c", PeerID: "p",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	s.CreateSession(ctx, sess)

	s.AppendMessage(ctx, &Message{
		ID: "ct-msg", SessionID: "ct-sess", Role: "user", Content: "hello",
		CreatedAt: time.Now().UTC(),
	})

	msgs, _ := s.RecentMessages(ctx, "ct-sess", 1)
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].ContentType != ContentTypeText {
		t.Errorf("ContentType = %q, want %q", msgs[0].ContentType, ContentTypeText)
	}
}
