package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/store"
)

func seedMessages(t *testing.T, s *store.Store, sessionID string, count int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < count; i++ {
		role := llm.RoleUser
		if i%2 == 1 {
			role = llm.RoleAssistant
		}
		msg := &store.Message{
			ID:          uuid.NewString(),
			SessionID:   sessionID,
			Role:        role,
			ContentType: store.ContentTypeText,
			Content:     fmt.Sprintf("message %d", i),
			CreatedAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		if err := s.AppendMessage(ctx, msg); err != nil {
			t.Fatalf("seeding message %d: %v", i, err)
		}
	}
}

func TestCompactSession_BelowThreshold(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "summary"},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
	}
	rt, s := newTestRuntime(t, fp)

	sessionID := "compact-below"
	ctx := context.Background()
	now := time.Now().UTC()
	_ = s.CreateSession(ctx, &store.Session{
		ID: sessionID, AgentID: "default", Channel: "test", PeerID: "test",
		CreatedAt: now, UpdatedAt: now,
	})
	seedMessages(t, s, sessionID, 20)

	err := rt.CompactSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CompactSession: %v", err)
	}

	count, _ := s.MessageCount(ctx, sessionID)
	if count != 20 {
		t.Errorf("message count = %d, want 20 (no compaction)", count)
	}
}

func TestCompactSession_AboveThreshold(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "This is the session summary."},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
	}
	rt, s := newTestRuntime(t, fp)

	sessionID := "compact-above"
	ctx := context.Background()
	now := time.Now().UTC()
	_ = s.CreateSession(ctx, &store.Session{
		ID: sessionID, AgentID: "default", Channel: "test", PeerID: "test",
		CreatedAt: now, UpdatedAt: now,
	})
	seedMessages(t, s, sessionID, 45)

	err := rt.CompactSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CompactSession: %v", err)
	}

	count, _ := s.MessageCount(ctx, sessionID)
	if count > 15 {
		t.Errorf("message count = %d, want <=15 (10 recent + 1 summary + margin)", count)
	}
}

func TestCompactSession_KeepsRecentMessages(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "summary text"},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
	}
	rt, s := newTestRuntime(t, fp)

	sessionID := "compact-recent"
	ctx := context.Background()
	now := time.Now().UTC()
	_ = s.CreateSession(ctx, &store.Session{
		ID: sessionID, AgentID: "default", Channel: "test", PeerID: "test",
		CreatedAt: now, UpdatedAt: now,
	})
	seedMessages(t, s, sessionID, 45)

	err := rt.CompactSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CompactSession: %v", err)
	}

	msgs, _ := s.RecentMessages(ctx, sessionID, 50)
	var lastMsgFound bool
	for _, m := range msgs {
		if m.Content == "message 44" {
			lastMsgFound = true
		}
	}
	if !lastMsgFound {
		t.Error("most recent message (message 44) should be kept")
	}
}

func TestCompactSession_ProviderError(t *testing.T) {
	fp := &fakeProvider{
		err: fmt.Errorf("LLM unavailable"),
	}
	rt, s := newTestRuntime(t, fp)

	sessionID := "compact-err"
	ctx := context.Background()
	now := time.Now().UTC()
	_ = s.CreateSession(ctx, &store.Session{
		ID: sessionID, AgentID: "default", Channel: "test", PeerID: "test",
		CreatedAt: now, UpdatedAt: now,
	})
	seedMessages(t, s, sessionID, 45)

	err := rt.CompactSession(ctx, sessionID)
	if err == nil {
		t.Error("expected error when provider fails")
	}
}

func TestCompactSession_SummaryContent(t *testing.T) {
	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "bullet points here"},
			{Type: llm.EventDone, Usage: &llm.Usage{}},
		},
	}
	rt, s := newTestRuntime(t, fp)

	sessionID := "compact-summary"
	ctx := context.Background()
	now := time.Now().UTC()
	_ = s.CreateSession(ctx, &store.Session{
		ID: sessionID, AgentID: "default", Channel: "test", PeerID: "test",
		CreatedAt: now, UpdatedAt: now,
	})
	seedMessages(t, s, sessionID, 45)

	err := rt.CompactSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CompactSession: %v", err)
	}

	msgs, _ := s.RecentMessages(ctx, sessionID, 50)
	var foundSummary bool
	for _, m := range msgs {
		if m.Role == llm.RoleAssistant && m.ContentType == store.ContentTypeText {
			if len(m.Content) >= 16 && m.Content[:16] == "[Session Summary" {
				foundSummary = true
			}
		}
	}
	if !foundSummary {
		t.Error("expected a summary message with [Session Summary] prefix")
	}
}

func TestMessageIDs(t *testing.T) {
	msgs := []store.Message{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
	}
	ids := messageIDs(msgs)
	if len(ids) != 3 {
		t.Fatalf("len = %d, want 3", len(ids))
	}
	if ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Errorf("ids = %v, want [a b c]", ids)
	}
}
