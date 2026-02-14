package llm

import (
	"encoding/json"
	"testing"
)

func TestConvertToAnthropicMessage_Text(t *testing.T) {
	m := ChatMessage{Role: RoleUser, Content: "hello"}
	got := convertToAnthropicMessage(m)

	if got.Role != "user" {
		t.Errorf("Role = %q, want %q", got.Role, "user")
	}
	content, ok := got.Content.(string)
	if !ok {
		t.Fatalf("Content type = %T, want string", got.Content)
	}
	if content != "hello" {
		t.Errorf("Content = %q, want %q", content, "hello")
	}
}

func TestConvertToAnthropicMessage_ToolCalls(t *testing.T) {
	m := ChatMessage{
		Role:    RoleAssistant,
		Content: "thinking",
		ToolCalls: []ToolCall{
			{ID: "tc1", Name: "shell", Input: json.RawMessage(`{"command":"ls"}`)},
		},
	}
	got := convertToAnthropicMessage(m)

	blocks, ok := got.Content.([]anthropicContentBlock)
	if !ok {
		t.Fatalf("Content type = %T, want []anthropicContentBlock", got.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks len = %d, want 2 (text + tool_use)", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "thinking" {
		t.Errorf("blocks[0] = %+v, want text block", blocks[0])
	}
	if blocks[1].Type != "tool_use" || blocks[1].Name != "shell" {
		t.Errorf("blocks[1] = %+v, want tool_use block", blocks[1])
	}
}

func TestConvertToAnthropicMessage_ToolResults(t *testing.T) {
	m := ChatMessage{
		Role: RoleUser,
		ToolResults: []ToolResult{
			{ToolCallID: "tc1", Content: "output", IsError: false},
		},
	}
	got := convertToAnthropicMessage(m)

	blocks, ok := got.Content.([]anthropicContentBlock)
	if !ok {
		t.Fatalf("Content type = %T, want []anthropicContentBlock", got.Content)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks len = %d, want 1", len(blocks))
	}
	if blocks[0].Type != "tool_result" {
		t.Errorf("blocks[0].Type = %q, want %q", blocks[0].Type, "tool_result")
	}
	if blocks[0].ToolUseID != "tc1" {
		t.Errorf("blocks[0].ToolUseID = %q, want %q", blocks[0].ToolUseID, "tc1")
	}
}

func TestConvertToOpenAIMessages_Text(t *testing.T) {
	m := ChatMessage{Role: RoleUser, Content: "hello"}
	got := convertToOpenAIMessages(m)

	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Role != "user" || got[0].Content != "hello" {
		t.Errorf("got %+v, want user/hello", got[0])
	}
}

func TestConvertToOpenAIMessages_ToolCalls(t *testing.T) {
	m := ChatMessage{
		Role:    RoleAssistant,
		Content: "let me check",
		ToolCalls: []ToolCall{
			{ID: "tc1", Name: "shell", Input: json.RawMessage(`{"command":"ls"}`)},
		},
	}
	got := convertToOpenAIMessages(m)

	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if len(got[0].ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(got[0].ToolCalls))
	}
	if got[0].ToolCalls[0].Function.Name != "shell" {
		t.Errorf("Function.Name = %q, want %q", got[0].ToolCalls[0].Function.Name, "shell")
	}
}

func TestConvertToOpenAIMessages_ToolResults(t *testing.T) {
	m := ChatMessage{
		Role: RoleUser,
		ToolResults: []ToolResult{
			{ToolCallID: "tc1", Content: "output1"},
			{ToolCallID: "tc2", Content: "output2"},
		},
	}
	got := convertToOpenAIMessages(m)

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (one per result)", len(got))
	}
	if got[0].Role != "tool" || got[0].ToolCallID != "tc1" {
		t.Errorf("got[0] = %+v, want tool/tc1", got[0])
	}
	if got[1].Role != "tool" || got[1].ToolCallID != "tc2" {
		t.Errorf("got[1] = %+v, want tool/tc2", got[1])
	}
}

func TestConvertToOpenAIMessages_ResultExpansion(t *testing.T) {
	m := ChatMessage{
		Role: RoleUser,
		ToolResults: []ToolResult{
			{ToolCallID: "a", Content: "1"},
			{ToolCallID: "b", Content: "2"},
			{ToolCallID: "c", Content: "3"},
		},
	}
	got := convertToOpenAIMessages(m)
	if len(got) != 3 {
		t.Errorf("len = %d, want 3 (one message per result)", len(got))
	}
}

func TestConvertToGeminiContent_UserText(t *testing.T) {
	m := ChatMessage{Role: RoleUser, Content: "hello"}
	got := convertToGeminiContent(m)

	if got.Role != "user" {
		t.Errorf("Role = %q, want %q", got.Role, "user")
	}
	if len(got.Parts) != 1 || got.Parts[0].Text != "hello" {
		t.Errorf("Parts = %+v, want single text part", got.Parts)
	}
}

func TestConvertToGeminiContent_AssistantText(t *testing.T) {
	m := ChatMessage{Role: RoleAssistant, Content: "response"}
	got := convertToGeminiContent(m)

	if got.Role != "model" {
		t.Errorf("Role = %q, want %q (assistant mapped to model)", got.Role, "model")
	}
}

func TestConvertToGeminiContent_ToolCalls(t *testing.T) {
	m := ChatMessage{
		Role: RoleAssistant,
		ToolCalls: []ToolCall{
			{ID: "tc1", Name: "shell", Input: json.RawMessage(`{"command":"ls"}`)},
		},
	}
	got := convertToGeminiContent(m)

	if len(got.Parts) != 1 {
		t.Fatalf("Parts len = %d, want 1", len(got.Parts))
	}
	if got.Parts[0].FunctionCall == nil {
		t.Fatal("expected FunctionCall part")
	}
	if got.Parts[0].FunctionCall.Name != "shell" {
		t.Errorf("FunctionCall.Name = %q, want %q", got.Parts[0].FunctionCall.Name, "shell")
	}
}

func TestConvertToGeminiContent_ToolResults(t *testing.T) {
	m := ChatMessage{
		Role: RoleUser,
		ToolResults: []ToolResult{
			{ToolCallID: "shell", Content: "file.txt"},
		},
	}
	got := convertToGeminiContent(m)

	if len(got.Parts) != 1 {
		t.Fatalf("Parts len = %d, want 1", len(got.Parts))
	}
	if got.Parts[0].FunctionResponse == nil {
		t.Fatal("expected FunctionResponse part")
	}
	if got.Parts[0].FunctionResponse.Response.Content != "file.txt" {
		t.Errorf("Response.Content = %q, want %q", got.Parts[0].FunctionResponse.Response.Content, "file.txt")
	}
}

func TestConvertToGeminiContent_RoleMapping(t *testing.T) {
	tests := []struct {
		role string
		want string
	}{
		{RoleUser, "user"},
		{RoleAssistant, "model"},
	}
	for _, tt := range tests {
		got := convertToGeminiContent(ChatMessage{Role: tt.role, Content: "x"})
		if got.Role != tt.want {
			t.Errorf("role %q mapped to %q, want %q", tt.role, got.Role, tt.want)
		}
	}
}
