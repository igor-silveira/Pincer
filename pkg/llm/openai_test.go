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

func TestNewOpenAIProvider_NoKey(t *testing.T) {
	orig := os.Getenv("OPENAI_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	defer os.Setenv("OPENAI_API_KEY", orig)

	_, err := NewOpenAIProvider("", "")
	if err == nil {
		t.Error("expected error when no API key")
	}
}

func TestOpenAIChat_FullResponse(t *testing.T) {
	resp := openaiFullResponse{
		Choices: []openaiFullChoice{
			{Message: openaiFullMessage{Content: "Hello from GPT"}},
		},
		Usage: openaiUsage{PromptTokens: 8, CompletionTokens: 4},
	}
	respData, _ := json.Marshal(resp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(respData)
	}))
	defer srv.Close()

	p, err := NewOpenAIProvider("test-key", srv.URL)
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	events, err := p.Chat(context.Background(), ChatRequest{
		Model:    "gpt-4o",
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
			if ev.Usage.InputTokens != 8 {
				t.Errorf("InputTokens = %d, want 8", ev.Usage.InputTokens)
			}
			if ev.Usage.OutputTokens != 4 {
				t.Errorf("OutputTokens = %d, want 4", ev.Usage.OutputTokens)
			}
		case EventError:
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
	if !gotDone {
		t.Error("expected EventDone")
	}
	joined := strings.Join(tokens, "")
	if joined != "Hello from GPT" {
		t.Errorf("tokens = %q, want %q", joined, "Hello from GPT")
	}
}

func TestOpenAIChat_ToolCalls(t *testing.T) {
	resp := openaiFullResponse{
		Choices: []openaiFullChoice{
			{Message: openaiFullMessage{
				Content: "",
				ToolCalls: []openaiToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: openaiToolCallFunc{
							Name:      "shell",
							Arguments: `{"command":"ls"}`,
						},
					},
				},
			}},
		},
		Usage: openaiUsage{PromptTokens: 5, CompletionTokens: 3},
	}
	respData, _ := json.Marshal(resp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(respData)
	}))
	defer srv.Close()

	p, err := NewOpenAIProvider("test-key", srv.URL)
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	events, err := p.Chat(context.Background(), ChatRequest{
		Model:    "gpt-4o",
		Messages: []ChatMessage{{Role: RoleUser, Content: "run ls"}},
		Stream:   false,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var toolCalls int
	for ev := range events {
		if ev.Type == EventToolCall {
			toolCalls++
			if ev.ToolCall.Name != "shell" {
				t.Errorf("ToolCall.Name = %q, want %q", ev.ToolCall.Name, "shell")
			}
		}
	}
	if toolCalls != 1 {
		t.Errorf("tool calls = %d, want 1", toolCalls)
	}
}
