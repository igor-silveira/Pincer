package llm

import (
	"context"
	"encoding/base64"
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

func TestConvertToAnthropicMessage_ToolResultNoImages(t *testing.T) {
	msg := ChatMessage{
		Role: RoleUser,
		ToolResults: []ToolResult{
			{ToolCallID: "tc1", Content: "output text"},
		},
	}

	got := convertToAnthropicMessage(msg)
	blocks, ok := got.Content.([]anthropicContentBlock)
	if !ok {
		t.Fatalf("Content type = %T, want []anthropicContentBlock", got.Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks len = %d, want 1", len(blocks))
	}
	if blocks[0].Type != "tool_result" {
		t.Errorf("Type = %q, want %q", blocks[0].Type, "tool_result")
	}
	if blocks[0].Content != "output text" {
		t.Errorf("Content = %v, want %q", blocks[0].Content, "output text")
	}
}

func TestConvertToAnthropicMessage_ToolResultWithImages(t *testing.T) {
	imgData := []byte("fake png data")
	img := ImageContent{MediaType: "image/png", Path: "/tmp/test.png"}
	img.SetData(imgData)

	msg := ChatMessage{
		Role: RoleUser,
		ToolResults: []ToolResult{
			{
				ToolCallID: "tc1",
				Content:    "screenshot taken",
				Images:     []ImageContent{img},
			},
		},
	}

	got := convertToAnthropicMessage(msg)
	blocks, ok := got.Content.([]anthropicContentBlock)
	if !ok {
		t.Fatalf("Content type = %T, want []anthropicContentBlock", got.Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks len = %d, want 1", len(blocks))
	}

	innerBlocks, ok := blocks[0].Content.([]anthropicInlineBlock)
	if !ok {
		t.Fatalf("inner Content type = %T, want []anthropicInlineBlock", blocks[0].Content)
	}
	if len(innerBlocks) != 2 {
		t.Fatalf("inner blocks len = %d, want 2 (text + image)", len(innerBlocks))
	}

	if innerBlocks[0].Type != "text" {
		t.Errorf("inner[0].Type = %q, want %q", innerBlocks[0].Type, "text")
	}
	if innerBlocks[0].Text != "screenshot taken" {
		t.Errorf("inner[0].Text = %q, want %q", innerBlocks[0].Text, "screenshot taken")
	}

	if innerBlocks[1].Type != "image" {
		t.Errorf("inner[1].Type = %q, want %q", innerBlocks[1].Type, "image")
	}
	if innerBlocks[1].Source == nil {
		t.Fatal("inner[1].Source should not be nil")
	}
	if innerBlocks[1].Source.Type != "base64" {
		t.Errorf("Source.Type = %q, want %q", innerBlocks[1].Source.Type, "base64")
	}
	if innerBlocks[1].Source.MediaType != "image/png" {
		t.Errorf("Source.MediaType = %q, want %q", innerBlocks[1].Source.MediaType, "image/png")
	}

	expectedB64 := base64.StdEncoding.EncodeToString(imgData)
	if innerBlocks[1].Source.Data != expectedB64 {
		t.Errorf("Source.Data = %q, want %q", innerBlocks[1].Source.Data, expectedB64)
	}
}

func TestConvertToAnthropicMessage_ToolResultImageNoData(t *testing.T) {
	img := ImageContent{MediaType: "image/png", Path: "/tmp/test.png"}

	msg := ChatMessage{
		Role: RoleUser,
		ToolResults: []ToolResult{
			{
				ToolCallID: "tc1",
				Content:    "output",
				Images:     []ImageContent{img},
			},
		},
	}

	got := convertToAnthropicMessage(msg)
	blocks := got.Content.([]anthropicContentBlock)
	innerBlocks := blocks[0].Content.([]anthropicInlineBlock)

	if len(innerBlocks) != 1 {
		t.Fatalf("inner blocks len = %d, want 1 (only text, image skipped without data)", len(innerBlocks))
	}
	if innerBlocks[0].Type != "text" {
		t.Errorf("inner[0].Type = %q, want %q", innerBlocks[0].Type, "text")
	}
}

func TestConvertToAnthropicMessage_ToolResultSerializesJSON(t *testing.T) {
	imgData := []byte("png")
	img := ImageContent{MediaType: "image/png"}
	img.SetData(imgData)

	msg := ChatMessage{
		Role: RoleUser,
		ToolResults: []ToolResult{
			{ToolCallID: "tc1", Content: "text", Images: []ImageContent{img}},
		},
	}

	apiMsg := convertToAnthropicMessage(msg)

	data, err := json.Marshal(apiMsg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if !strings.Contains(string(data), `"type":"image"`) {
		t.Error("serialized JSON should contain image block")
	}
	if !strings.Contains(string(data), `"type":"base64"`) {
		t.Error("serialized JSON should contain base64 source type")
	}
}
