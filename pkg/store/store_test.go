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
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

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
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	for i := 0; i < 5; i++ {
		if err := s.AppendMessage(ctx, &Message{
			ID:         fmt.Sprintf("msg-%d", i),
			SessionID:  "sess-3",
			Role:       "user",
			Content:    fmt.Sprintf("message %d", i),
			TokenCount: 10,
			CreatedAt:  time.Now().UTC().Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
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
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.AppendMessage(ctx, &Message{
		ID: "m1", SessionID: "sess-4", Role: "user", Content: "hi",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := s.AppendMessage(ctx, &Message{
		ID: "m2", SessionID: "sess-4", Role: "assistant", Content: "hello",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

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
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.AppendMessage(ctx, &Message{
		ID: "d1", SessionID: "sess-5", Role: "user", Content: "delete me",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := s.AppendMessage(ctx, &Message{
		ID: "d2", SessionID: "sess-5", Role: "user", Content: "keep me",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

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
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.AppendMessage(ctx, &Message{
		ID: "t1", SessionID: "sess-6", Role: "user", Content: "test",
		TokenCount: 50, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := s.AppendMessage(ctx, &Message{
		ID: "t2", SessionID: "sess-6", Role: "assistant", Content: "response",
		TokenCount: 150, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

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
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

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

func TestCheckpoint_CreateAndGet(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	cp := &Checkpoint{
		ID:             "cp-1",
		SessionID:      "sess-cp-1",
		StepIndex:      1,
		StateSnapshot:  `{"state":"snapshot"}`,
		ToolOutputs:    `{"tool":"output"}`,
		ContextSummary: "summary of context",
	}
	if err := s.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	if cp.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should have been set")
	}

	got, err := s.LatestCheckpoint(ctx, "sess-cp-1")
	if err != nil {
		t.Fatalf("LatestCheckpoint: %v", err)
	}
	if got.ID != "cp-1" {
		t.Errorf("ID = %q, want %q", got.ID, "cp-1")
	}
	if got.SessionID != "sess-cp-1" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "sess-cp-1")
	}
	if got.StepIndex != 1 {
		t.Errorf("StepIndex = %d, want 1", got.StepIndex)
	}
	if got.StateSnapshot != `{"state":"snapshot"}` {
		t.Errorf("StateSnapshot = %q, want %q", got.StateSnapshot, `{"state":"snapshot"}`)
	}
	if got.ToolOutputs != `{"tool":"output"}` {
		t.Errorf("ToolOutputs = %q, want %q", got.ToolOutputs, `{"tool":"output"}`)
	}
	if got.ContextSummary != "summary of context" {
		t.Errorf("ContextSummary = %q, want %q", got.ContextSummary, "summary of context")
	}
}

func TestCheckpoint_LatestByStepIndex(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		cp := &Checkpoint{
			ID:             fmt.Sprintf("cp-lat-%d", i),
			SessionID:      "sess-lat",
			StepIndex:      i,
			StateSnapshot:  fmt.Sprintf("state-%d", i),
			ToolOutputs:    "{}",
			ContextSummary: "ctx",
		}
		if err := s.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatalf("SaveCheckpoint step %d: %v", i, err)
		}
	}

	got, err := s.LatestCheckpoint(ctx, "sess-lat")
	if err != nil {
		t.Fatalf("LatestCheckpoint: %v", err)
	}
	if got.StepIndex != 5 {
		t.Errorf("StepIndex = %d, want 5", got.StepIndex)
	}
	if got.StateSnapshot != "state-5" {
		t.Errorf("StateSnapshot = %q, want %q", got.StateSnapshot, "state-5")
	}
}

func TestCheckpoint_GetByStep(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		cp := &Checkpoint{
			ID:             fmt.Sprintf("cp-step-%d", i),
			SessionID:      "sess-step",
			StepIndex:      i,
			StateSnapshot:  fmt.Sprintf("state-%d", i),
			ToolOutputs:    "{}",
			ContextSummary: "ctx",
		}
		if err := s.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatalf("SaveCheckpoint step %d: %v", i, err)
		}
	}

	got, err := s.CheckpointAtStep(ctx, "sess-step", 2)
	if err != nil {
		t.Fatalf("CheckpointAtStep: %v", err)
	}
	if got.StepIndex != 2 {
		t.Errorf("StepIndex = %d, want 2", got.StepIndex)
	}
	if got.StateSnapshot != "state-2" {
		t.Errorf("StateSnapshot = %q, want %q", got.StateSnapshot, "state-2")
	}
}

func TestCheckpoint_DeleteOlderThan(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	cp := &Checkpoint{
		ID:             "cp-del-1",
		SessionID:      "sess-del",
		StepIndex:      1,
		StateSnapshot:  "state",
		ToolOutputs:    "{}",
		ContextSummary: "ctx",
		CreatedAt:      time.Now().UTC(),
	}
	if err := s.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	deleted, err := s.DeleteCheckpointsOlderThan(ctx, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("DeleteCheckpointsOlderThan: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	_, err = s.LatestCheckpoint(ctx, "sess-del")
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestCheckpoint_NotFound(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	_, err := s.LatestCheckpoint(ctx, "nonexistent-session")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("expected gorm.ErrRecordNotFound, got: %v", err)
	}

	_, err = s.CheckpointAtStep(ctx, "nonexistent-session", 1)
	if err == nil {
		t.Fatal("expected error for nonexistent checkpoint")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("expected gorm.ErrRecordNotFound, got: %v", err)
	}
}

func TestAppendMessageDefaultContentType(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	sess := &Session{
		ID: "ct-sess", AgentID: "a", Channel: "c", PeerID: "p",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.AppendMessage(ctx, &Message{
		ID: "ct-msg", SessionID: "ct-sess", Role: "user", Content: "hello",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	msgs, _ := s.RecentMessages(ctx, "ct-sess", 1)
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].ContentType != ContentTypeText {
		t.Errorf("ContentType = %q, want %q", msgs[0].ContentType, ContentTypeText)
	}
}
