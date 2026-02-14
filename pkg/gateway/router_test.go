package gateway

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/igorsilveira/pincer/pkg/agent"
	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/store"
)

func TestParseTextApproval(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOK    bool
		wantID    string
		wantAppr  bool
	}{
		{"approve valid", "approve abc-123", true, "abc-123", true},
		{"deny valid", "deny abc-123", true, "abc-123", false},
		{"approve uppercase", "Approve abc-123", true, "abc-123", true},
		{"deny uppercase", "Deny abc-123", true, "abc-123", false},
		{"approve with leading space", "  approve abc-123  ", true, "abc-123", true},
		{"approve empty id", "approve ", false, "", false},
		{"deny empty id", "deny ", false, "", false},
		{"random text", "hello world", false, "", false},
		{"empty string", "", false, "", false},
		{"approve no space", "approvefoo", false, "", false},
		{"partial approve", "app abc", false, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, ok := parseTextApproval(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("parseTextApproval(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if resp.RequestID != tt.wantID {
				t.Errorf("RequestID = %q, want %q", resp.RequestID, tt.wantID)
			}
			if resp.Approved != tt.wantAppr {
				t.Errorf("Approved = %v, want %v", resp.Approved, tt.wantAppr)
			}
		})
	}
}

type fakeAdapter struct {
	name    string
	inbound chan channels.InboundMessage
	sent    []channels.OutboundMessage
	mu      sync.Mutex
}

func newFakeAdapter(name string) *fakeAdapter {
	return &fakeAdapter{
		name:    name,
		inbound: make(chan channels.InboundMessage, 16),
	}
}

func (f *fakeAdapter) Name() string                                         { return f.name }
func (f *fakeAdapter) Start(ctx context.Context) error                      { return nil }
func (f *fakeAdapter) Stop(ctx context.Context) error                       { return nil }
func (f *fakeAdapter) Receive() <-chan channels.InboundMessage              { return f.inbound }
func (f *fakeAdapter) Capabilities() channels.ChannelCaps                   { return channels.ChannelCaps{} }

func (f *fakeAdapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeAdapter) getSent() []channels.OutboundMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]channels.OutboundMessage, len(f.sent))
	copy(result, f.sent)
	return result
}

type fakeApprovalAdapter struct {
	*fakeAdapter
	mu       sync.Mutex
	approval *channels.ApprovalRequest
}

func newFakeApprovalAdapter(name string) *fakeApprovalAdapter {
	return &fakeApprovalAdapter{
		fakeAdapter: newFakeAdapter(name),
	}
}

func (f *fakeApprovalAdapter) SendApprovalRequest(ctx context.Context, req channels.ApprovalRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approval = &req
	return nil
}

func (f *fakeApprovalAdapter) getApproval() *channels.ApprovalRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.approval
}

func TestInboundApprovalResponseRoutesToApprover(t *testing.T) {
	approver := agent.NewApprover(agent.ApprovalAsk, nil)
	adapter := newFakeAdapter("test")
	logger := slog.Default()

	router := NewChannelRouter(nil, []channels.Adapter{adapter}, approver, logger, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router.Start(ctx)

	doneCh := make(chan bool, 1)
	go func() {
		approved, err := approver.RequestApproval(ctx, agent.ApprovalRequest{
			ID:        "req-1",
			SessionID: "sess-1",
			ToolName:  "shell",
			Input:     "ls",
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}
		doneCh <- approved
	}()

	time.Sleep(50 * time.Millisecond)

	adapter.inbound <- channels.InboundMessage{
		ChannelName: "test",
		SessionID:   "sess-1",
		PeerID:      "user-1",
		ApprovalResponse: &channels.InboundApprovalResponse{
			RequestID: "req-1",
			Approved:  true,
		},
	}

	select {
	case approved := <-doneCh:
		if !approved {
			t.Error("expected approval to be true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval response")
	}
}

func TestTextApprovalRoutesToApprover(t *testing.T) {
	approver := agent.NewApprover(agent.ApprovalAsk, nil)
	adapter := newFakeAdapter("test")
	logger := slog.Default()

	router := NewChannelRouter(nil, []channels.Adapter{adapter}, approver, logger, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router.Start(ctx)

	doneCh := make(chan bool, 1)
	go func() {
		approved, err := approver.RequestApproval(ctx, agent.ApprovalRequest{
			ID:        "req-2",
			SessionID: "sess-1",
			ToolName:  "shell",
			Input:     "ls",
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}
		doneCh <- approved
	}()

	time.Sleep(50 * time.Millisecond)

	adapter.inbound <- channels.InboundMessage{
		ChannelName: "test",
		SessionID:   "sess-1",
		PeerID:      "user-1",
		Content:     "deny req-2",
	}

	select {
	case approved := <-doneCh:
		if approved {
			t.Error("expected approval to be false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval response")
	}
}

func TestSendApprovalRequestUsesApprovalSender(t *testing.T) {
	approver := agent.NewApprover(agent.ApprovalAsk, nil)
	adapter := newFakeApprovalAdapter("test")
	logger := slog.Default()

	router := NewChannelRouter(nil, []channels.Adapter{adapter}, approver, logger, nil, nil)

	ctx := context.Background()

	req := &agent.ApprovalRequest{
		ID:        "req-3",
		SessionID: "sess-1",
		ToolName:  "shell",
		Input:     `{"command":"rm -rf /"}`,
	}

	router.sendApprovalRequest(ctx, adapter, "sess-1", req)

	got := adapter.getApproval()
	if got == nil {
		t.Fatal("expected approval request to be sent via ApprovalSender")
	}
	if got.RequestID != "req-3" {
		t.Errorf("RequestID = %q, want %q", got.RequestID, "req-3")
	}
	if got.ToolName != "shell" {
		t.Errorf("ToolName = %q, want %q", got.ToolName, "shell")
	}

	sent := adapter.getSent()
	if len(sent) != 0 {
		t.Error("expected no text fallback when ApprovalSender is implemented")
	}
}

func TestSendApprovalRequestFallsBackToText(t *testing.T) {
	approver := agent.NewApprover(agent.ApprovalAsk, nil)
	adapter := newFakeAdapter("whatsapp")
	logger := slog.Default()

	router := NewChannelRouter(nil, []channels.Adapter{adapter}, approver, logger, nil, nil)

	ctx := context.Background()

	req := &agent.ApprovalRequest{
		ID:        "req-4",
		SessionID: "sess-1",
		ToolName:  "http",
		Input:     `{"url":"https://example.com"}`,
	}

	router.sendApprovalRequest(ctx, adapter, "sess-1", req)

	sent := adapter.getSent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 message sent, got %d", len(sent))
	}
	if sent[0].SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", sent[0].SessionID, "sess-1")
	}
	if !contains(sent[0].Content, "approve req-4") {
		t.Errorf("expected text to contain 'approve req-4', got %q", sent[0].Content)
	}
	if !contains(sent[0].Content, "deny req-4") {
		t.Errorf("expected text to contain 'deny req-4', got %q", sent[0].Content)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("creating test store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestEnsureSessionCreatesWithCorrectChannel(t *testing.T) {
	db := testStore(t)
	adapter := newFakeAdapter("telegram")
	logger := slog.Default()

	router := NewChannelRouter(nil, []channels.Adapter{adapter}, nil, logger, db, nil)

	ctx := context.Background()
	msg := channels.InboundMessage{
		ChannelName: "telegram",
		SessionID:   "sess-tg-1",
		PeerID:      "user-42",
	}

	router.ensureSession(ctx, msg)

	sess, err := db.GetSession(ctx, "sess-tg-1")
	if err != nil {
		t.Fatalf("expected session to exist: %v", err)
	}
	if sess.Channel != "telegram" {
		t.Errorf("Channel = %q, want %q", sess.Channel, "telegram")
	}
	if sess.PeerID != "user-42" {
		t.Errorf("PeerID = %q, want %q", sess.PeerID, "user-42")
	}

	router.ensureSession(ctx, msg)
	sess2, err := db.GetSession(ctx, "sess-tg-1")
	if err != nil {
		t.Fatalf("second lookup failed: %v", err)
	}
	if sess2.CreatedAt != sess.CreatedAt {
		t.Error("ensureSession should not recreate existing session")
	}
}

func TestEnsureSessionFixesStaleChannel(t *testing.T) {
	db := testStore(t)
	adapter := newFakeAdapter("telegram")
	logger := slog.Default()

	router := NewChannelRouter(nil, []channels.Adapter{adapter}, nil, logger, db, nil)

	ctx := context.Background()

	now := time.Now().UTC()
	if err := db.CreateSession(ctx, &store.Session{
		ID:        "sess-tg-stale",
		AgentID:   "default",
		Channel:   "webchat",
		PeerID:    "anonymous",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("creating stale session: %v", err)
	}

	msg := channels.InboundMessage{
		ChannelName: "telegram",
		SessionID:   "sess-tg-stale",
		PeerID:      "user-42",
	}
	router.ensureSession(ctx, msg)

	sess, err := db.GetSession(ctx, "sess-tg-stale")
	if err != nil {
		t.Fatalf("expected session to exist: %v", err)
	}
	if sess.Channel != "telegram" {
		t.Errorf("Channel = %q, want %q", sess.Channel, "telegram")
	}
	if sess.PeerID != "user-42" {
		t.Errorf("PeerID = %q, want %q", sess.PeerID, "user-42")
	}
}

func TestSendToSessionRoutesToCorrectAdapter(t *testing.T) {
	db := testStore(t)
	tgAdapter := newFakeAdapter("telegram")
	dcAdapter := newFakeAdapter("discord")
	logger := slog.Default()

	router := NewChannelRouter(nil, []channels.Adapter{tgAdapter, dcAdapter}, nil, logger, db, nil)

	ctx := context.Background()

	now := time.Now().UTC()
	if err := db.CreateSession(ctx, &store.Session{
		ID:        "sess-dc-1",
		AgentID:   "default",
		Channel:   "discord",
		PeerID:    "user-99",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("creating session: %v", err)
	}

	if err := router.SendToSession(ctx, "sess-dc-1", "hello discord"); err != nil {
		t.Fatalf("SendToSession failed: %v", err)
	}

	dcSent := dcAdapter.getSent()
	if len(dcSent) != 1 {
		t.Fatalf("expected 1 message to discord, got %d", len(dcSent))
	}
	if dcSent[0].Content != "hello discord" {
		t.Errorf("Content = %q, want %q", dcSent[0].Content, "hello discord")
	}
	if dcSent[0].SessionID != "sess-dc-1" {
		t.Errorf("SessionID = %q, want %q", dcSent[0].SessionID, "sess-dc-1")
	}

	tgSent := tgAdapter.getSent()
	if len(tgSent) != 0 {
		t.Errorf("expected 0 messages to telegram, got %d", len(tgSent))
	}
}

func TestSendToSessionErrorsOnUnknownChannel(t *testing.T) {
	db := testStore(t)
	adapter := newFakeAdapter("telegram")
	logger := slog.Default()

	router := NewChannelRouter(nil, []channels.Adapter{adapter}, nil, logger, db, nil)

	ctx := context.Background()

	now := time.Now().UTC()
	if err := db.CreateSession(ctx, &store.Session{
		ID:        "sess-slack-1",
		AgentID:   "default",
		Channel:   "slack",
		PeerID:    "user-1",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("creating session: %v", err)
	}

	err := router.SendToSession(ctx, "sess-slack-1", "hi")
	if err == nil {
		t.Fatal("expected error when no adapter matches channel")
	}
	if !contains(err.Error(), "no adapter found") {
		t.Errorf("unexpected error: %v", err)
	}
}
