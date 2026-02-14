package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/igorsilveira/pincer/pkg/agent"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/store"
)

type fakeProvider struct {
	events []llm.ChatEvent
}

func (f *fakeProvider) Name() string            { return "fake" }
func (f *fakeProvider) SupportsStreaming() bool  { return true }
func (f *fakeProvider) SupportsToolUse() bool    { return false }
func (f *fakeProvider) Models() []llm.ModelInfo  { return nil }

func (f *fakeProvider) Chat(_ context.Context, _ llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	ch := make(chan llm.ChatEvent, len(f.events)+1)
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func testRuntime(t *testing.T) *agent.Runtime {
	t.Helper()
	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	fp := &fakeProvider{
		events: []llm.ChatEvent{
			{Type: llm.EventToken, Token: "hello from agent"},
			{Type: llm.EventDone, Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5}},
		},
	}

	return agent.NewRuntime(agent.RuntimeConfig{
		Provider:     fp,
		Store:        s,
		Approver:     agent.NewApprover(agent.ApprovalAuto, nil),
		Model:        "fake-1",
		SystemPrompt: "test",
	})
}

func testHandler(t *testing.T) *Handler {
	t.Helper()
	return NewHandler(HandlerConfig{
		Card: &AgentCard{
			Name:        "TestAgent",
			Description: "A test agent",
			URL:         "http://localhost:18789",
			Version:     "1.0.0",
			Capabilities: Capabilities{Streaming: true},
			Skills: []Skill{{ID: "shell", Name: "shell", Description: "Execute commands"}},
		},
		Runtime:   testRuntime(t),
		AuthToken: "",
	})
}

func testHandlerWithAuth(t *testing.T) *Handler {
	t.Helper()
	return NewHandler(HandlerConfig{
		Card: &AgentCard{
			Name:        "TestAgent",
			Description: "A test agent",
			URL:         "http://localhost:18789",
			Version:     "1.0.0",
		},
		Runtime:   testRuntime(t),
		AuthToken: "secret-token",
	})
}

func TestAgentCard(t *testing.T) {
	h := testHandler(t)
	req := httptest.NewRequest("GET", "/.well-known/agentcard", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var card AgentCard
	if err := json.NewDecoder(w.Body).Decode(&card); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if card.Name != "TestAgent" {
		t.Errorf("Name = %q, want %q", card.Name, "TestAgent")
	}
	if card.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", card.Version, "1.0.0")
	}
	if len(card.Skills) != 1 {
		t.Errorf("Skills len = %d, want 1", len(card.Skills))
	}
}

func TestAgentCardIsPublic(t *testing.T) {
	h := testHandlerWithAuth(t)
	req := httptest.NewRequest("GET", "/.well-known/agentcard", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("agent card should be public, got status %d", w.Code)
	}
}

func TestSendMessageCreatesTask(t *testing.T) {
	h := testHandler(t)

	msg := Message{Role: "user", Parts: []Part{{Type: "text", Text: "hello"}}}
	body, _ := json.Marshal(msg)
	req := httptest.NewRequest("POST", "/a2a/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var task Task
	if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if task.ID == "" {
		t.Error("expected task to have an ID")
	}
	if task.State != TaskStateCompleted {
		t.Errorf("State = %q, want %q", task.State, TaskStateCompleted)
	}
}

func TestGetTask(t *testing.T) {
	h := testHandler(t)

	msg := Message{Role: "user", Parts: []Part{{Type: "text", Text: "hello"}}}
	body, _ := json.Marshal(msg)
	createReq := httptest.NewRequest("POST", "/a2a/messages", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	h.ServeHTTP(createW, createReq)

	var created Task
	_ = json.NewDecoder(createW.Body).Decode(&created)

	getReq := httptest.NewRequest("GET", "/a2a/tasks/"+created.ID, nil)
	getW := httptest.NewRecorder()
	h.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", getW.Code, http.StatusOK)
	}

	var got Task
	_ = json.NewDecoder(getW.Body).Decode(&got)
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	h := testHandler(t)
	req := httptest.NewRequest("GET", "/a2a/tasks/nonexistent", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestListTasks(t *testing.T) {
	h := testHandler(t)

	msg := Message{Role: "user", Parts: []Part{{Type: "text", Text: "hello"}}}
	body, _ := json.Marshal(msg)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/a2a/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
	}

	listReq := httptest.NewRequest("GET", "/a2a/tasks", nil)
	listW := httptest.NewRecorder()
	h.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", listW.Code, http.StatusOK)
	}

	var tasks []*Task
	_ = json.NewDecoder(listW.Body).Decode(&tasks)
	if len(tasks) != 3 {
		t.Errorf("tasks len = %d, want 3", len(tasks))
	}
}

func TestCancelTask(t *testing.T) {
	h := testHandler(t)

	msg := Message{Role: "user", Parts: []Part{{Type: "text", Text: "hello"}}}
	body, _ := json.Marshal(msg)
	createReq := httptest.NewRequest("POST", "/a2a/messages", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	h.ServeHTTP(createW, createReq)

	var created Task
	_ = json.NewDecoder(createW.Body).Decode(&created)

	cancelReq := httptest.NewRequest("POST", "/a2a/tasks/"+created.ID+":cancel", nil)
	cancelW := httptest.NewRecorder()
	h.ServeHTTP(cancelW, cancelReq)

	if cancelW.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", cancelW.Code, http.StatusOK)
	}

	var canceled Task
	_ = json.NewDecoder(cancelW.Body).Decode(&canceled)
	if canceled.State != TaskStateCanceled {
		t.Errorf("State = %q, want %q", canceled.State, TaskStateCanceled)
	}
}

func TestCancelTaskNotFound(t *testing.T) {
	h := testHandler(t)
	req := httptest.NewRequest("POST", "/a2a/tasks/nonexistent:cancel", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestJSONRPCSendMessage(t *testing.T) {
	h := testHandler(t)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tasks/send",
		Params:  json.RawMessage(`{"message":{"role":"user","parts":[{"type":"text","text":"hi"}]}}`),
	}
	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp JSONRPCResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %q, want %q", resp.JSONRPC, "2.0")
	}
}

func TestJSONRPCGetTask(t *testing.T) {
	h := testHandler(t)

	msg := Message{Role: "user", Parts: []Part{{Type: "text", Text: "hi"}}}
	body, _ := json.Marshal(msg)
	createReq := httptest.NewRequest("POST", "/a2a/messages", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	h.ServeHTTP(createW, createReq)
	var created Task
	_ = json.NewDecoder(createW.Body).Decode(&created)

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tasks/get",
		Params:  json.RawMessage(`{"id":"` + created.ID + `"}`),
	}
	rpcBody, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(rpcBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp JSONRPCResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestJSONRPCUnknownMethod(t *testing.T) {
	h := testHandler(t)

	rpcReq := JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "unknown"}
	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp JSONRPCResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != ErrCodeNotFound {
		t.Errorf("Code = %d, want %d", resp.Error.Code, ErrCodeNotFound)
	}
}

func TestJSONRPCInvalidVersion(t *testing.T) {
	h := testHandler(t)

	rpcReq := JSONRPCRequest{JSONRPC: "1.0", ID: 1, Method: "tasks/get"}
	body, _ := json.Marshal(rpcReq)
	req := httptest.NewRequest("POST", "/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var resp JSONRPCResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected error for invalid version")
	}
	if resp.Error.Code != ErrCodeInvalidReq {
		t.Errorf("Code = %d, want %d", resp.Error.Code, ErrCodeInvalidReq)
	}
}

func TestAuthMiddleware(t *testing.T) {
	h := testHandlerWithAuth(t)

	req := httptest.NewRequest("GET", "/a2a/tasks", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (no auth)", w.Code, http.StatusUnauthorized)
	}

	req2 := httptest.NewRequest("GET", "/a2a/tasks", nil)
	req2.Header.Set("Authorization", "Bearer wrong-token")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	if w2.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (wrong token)", w2.Code, http.StatusUnauthorized)
	}

	req3 := httptest.NewRequest("GET", "/a2a/tasks", nil)
	req3.Header.Set("Authorization", "Bearer secret-token")
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (valid token)", w3.Code, http.StatusOK)
	}
}

func TestSendMessageStream(t *testing.T) {
	h := testHandler(t)

	msg := Message{Role: "user", Parts: []Part{{Type: "text", Text: "hello"}}}
	body, _ := json.Marshal(msg)
	req := httptest.NewRequest("POST", "/a2a/messages:stream", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	respBody, _ := io.ReadAll(w.Body)
	sseData := string(respBody)

	if !strings.Contains(sseData, "event: status") {
		t.Error("expected SSE status events")
	}
	if !strings.Contains(sseData, "event: token") {
		t.Error("expected SSE token events")
	}
}

func TestSendMessageEmptyMessage(t *testing.T) {
	h := testHandler(t)

	msg := Message{Role: "user", Parts: []Part{}}
	body, _ := json.Marshal(msg)
	req := httptest.NewRequest("POST", "/a2a/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
