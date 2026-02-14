package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNewAnthropicProvider_NoKey(t *testing.T) {
	orig := os.Getenv("ANTHROPIC_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	defer os.Setenv("ANTHROPIC_API_KEY", orig)

	_, err := NewAnthropicProvider("", "")
	if err == nil {
		t.Error("expected error when no API key and default base URL")
	}
}

func TestNewAnthropicProvider_CustomBase(t *testing.T) {
	p, err := NewAnthropicProvider("", "http://localhost:9999")
	if err != nil {
		t.Fatalf("unexpected error with custom base URL: %v", err)
	}
	if p.baseURL != "http://localhost:9999" {
		t.Errorf("baseURL = %q, want %q", p.baseURL, "http://localhost:9999")
	}
}

func TestAnthropicChat_FullResponse(t *testing.T) {
	resp := anthropicFullResponse{
		Content: []anthropicContentBlock{
			{Type: "text", Text: "Hello from Claude"},
		},
		Usage: sseUsage{InputTokens: 10, OutputTokens: 5},
	}
	respData, _ := json.Marshal(resp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respData)
	}))
	defer srv.Close()

	p, err := NewAnthropicProvider("test-key", srv.URL)
	if err != nil {
		t.Fatalf("NewAnthropicProvider: %v", err)
	}

	events, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []ChatMessage{{Role: RoleUser, Content: "hello"}},
		Stream:   false,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var tokens []string
	var gotDone bool
	for ev := range events {
		switch ev.Type {
		case EventToken:
			tokens = append(tokens, ev.Token)
		case EventDone:
			gotDone = true
			if ev.Usage.InputTokens != 10 {
				t.Errorf("InputTokens = %d, want 10", ev.Usage.InputTokens)
			}
		case EventError:
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
	if !gotDone {
		t.Error("expected EventDone")
	}
	joined := strings.Join(tokens, "")
	if joined != "Hello from Claude" {
		t.Errorf("tokens = %q, want %q", joined, "Hello from Claude")
	}
}

func TestAnthropicChat_StreamResponse(t *testing.T) {
	sseData := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":15,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","usage":{"output_tokens":3}}`,
		`data: {"type":"message_stop"}`,
	}, "\n") + "\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseData))
	}))
	defer srv.Close()

	p, err := NewAnthropicProvider("test-key", srv.URL)
	if err != nil {
		t.Fatalf("NewAnthropicProvider: %v", err)
	}

	events, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var tokens []string
	var gotDone bool
	for ev := range events {
		switch ev.Type {
		case EventToken:
			tokens = append(tokens, ev.Token)
		case EventDone:
			gotDone = true
		case EventError:
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
	if !gotDone {
		t.Error("expected EventDone")
	}
	joined := strings.Join(tokens, "")
	if joined != "Hi there" {
		t.Errorf("tokens = %q, want %q", joined, "Hi there")
	}
}
