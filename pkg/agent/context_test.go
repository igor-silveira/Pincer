package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/store"
)

func TestEstimateTokens_Empty(t *testing.T) {
	if got := estimateTokens(""); got != 0 {
		t.Errorf("estimateTokens(\"\") = %d, want 0", got)
	}
}

func TestEstimateTokens_Short(t *testing.T) {
	if got := estimateTokens("a"); got != 1 {
		t.Errorf("estimateTokens(\"a\") = %d, want 1", got)
	}
}

func TestEstimateTokens_FourChars(t *testing.T) {
	if got := estimateTokens("abcd"); got != 1 {
		t.Errorf("estimateTokens(\"abcd\") = %d, want 1", got)
	}
}

func TestEstimateTokens_FiveChars(t *testing.T) {
	if got := estimateTokens("abcde"); got != 2 {
		t.Errorf("estimateTokens(\"abcde\") = %d, want 2", got)
	}
}

func TestMessageToLLM_TextContent(t *testing.T) {
	m := store.Message{
		Role:        llm.RoleUser,
		ContentType: store.ContentTypeText,
		Content:     "hello world",
	}
	got := messageToLLM(m)
	if got.Role != llm.RoleUser {
		t.Errorf("Role = %q, want %q", got.Role, llm.RoleUser)
	}
	if got.Content != "hello world" {
		t.Errorf("Content = %q, want %q", got.Content, "hello world")
	}
	if len(got.ToolCalls) != 0 {
		t.Errorf("ToolCalls should be empty, got %d", len(got.ToolCalls))
	}
}

func TestMessageToLLM_ToolCalls(t *testing.T) {
	data, _ := json.Marshal(struct {
		Text      string         `json:"text,omitempty"`
		ToolCalls []llm.ToolCall `json:"tool_calls"`
	}{
		Text: "thinking",
		ToolCalls: []llm.ToolCall{
			{ID: "tc1", Name: "shell", Input: json.RawMessage(`{"command":"ls"}`)},
		},
	})
	m := store.Message{
		Role:        llm.RoleAssistant,
		ContentType: store.ContentTypeToolCalls,
		Content:     string(data),
	}
	got := messageToLLM(m)
	if got.Content != "thinking" {
		t.Errorf("Content = %q, want %q", got.Content, "thinking")
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Name != "shell" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", got.ToolCalls[0].Name, "shell")
	}
}

func TestMessageToLLM_ToolResults(t *testing.T) {
	results := []llm.ToolResult{
		{ToolCallID: "tc1", Content: "output", IsError: false},
	}
	data, _ := json.Marshal(results)
	m := store.Message{
		Role:        llm.RoleUser,
		ContentType: store.ContentTypeToolResults,
		Content:     string(data),
	}
	got := messageToLLM(m)
	if len(got.ToolResults) != 1 {
		t.Fatalf("ToolResults len = %d, want 1", len(got.ToolResults))
	}
	if got.ToolResults[0].Content != "output" {
		t.Errorf("ToolResults[0].Content = %q, want %q", got.ToolResults[0].Content, "output")
	}
}

func TestMessageToLLM_InvalidJSON(t *testing.T) {
	m := store.Message{
		Role:        llm.RoleAssistant,
		ContentType: store.ContentTypeToolCalls,
		Content:     "not json",
	}
	got := messageToLLM(m)
	if got.Content != "not json" {
		t.Errorf("Content = %q, want %q (raw fallback)", got.Content, "not json")
	}
	if len(got.ToolCalls) != 0 {
		t.Errorf("ToolCalls should be empty on invalid JSON")
	}
}

func TestSanitizeToolPairs_Empty(t *testing.T) {
	got := sanitizeToolPairs(nil)
	if got == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestSanitizeToolPairs_TextOnly(t *testing.T) {
	msgs := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "hi"},
		{Role: llm.RoleAssistant, Content: "hello"},
	}
	got := sanitizeToolPairs(msgs)
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestSanitizeToolPairs_ValidPair(t *testing.T) {
	msgs := []llm.ChatMessage{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "shell"}}},
		{Role: llm.RoleUser, ToolResults: []llm.ToolResult{{ToolCallID: "1", Content: "ok"}}},
	}
	got := sanitizeToolPairs(msgs)
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (pair preserved)", len(got))
	}
}

func TestSanitizeToolPairs_OrphanedToolCall(t *testing.T) {
	msgs := []llm.ChatMessage{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "shell"}}},
		{Role: llm.RoleUser, Content: "hi"},
	}
	got := sanitizeToolPairs(msgs)
	if len(got) != 1 {
		t.Errorf("len = %d, want 1 (orphan stripped)", len(got))
	}
	if got[0].Content != "hi" {
		t.Errorf("remaining msg Content = %q, want %q", got[0].Content, "hi")
	}
}

func TestSanitizeToolPairs_OrphanedToolResult(t *testing.T) {
	msgs := []llm.ChatMessage{
		{Role: llm.RoleUser, ToolResults: []llm.ToolResult{{ToolCallID: "1", Content: "ok"}}},
	}
	got := sanitizeToolPairs(msgs)
	if len(got) != 0 {
		t.Errorf("len = %d, want 0 (orphan result stripped)", len(got))
	}
}

func TestSanitizeToolPairs_MixedSequence(t *testing.T) {
	msgs := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "start"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "shell"}}},
		{Role: llm.RoleUser, ToolResults: []llm.ToolResult{{ToolCallID: "1", Content: "ok"}}},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "2", Name: "http"}}},
		{Role: llm.RoleUser, Content: "end"},
	}
	got := sanitizeToolPairs(msgs)
	if len(got) != 4 {
		t.Errorf("len = %d, want 4 (text + pair + text, orphan stripped)", len(got))
	}
}

func TestSelectHistory_Empty(t *testing.T) {
	cb := NewContextBuilder(100000, 4096)
	got := cb.selectHistory(nil, 1000)
	if got == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestSelectHistory_BudgetZero(t *testing.T) {
	cb := NewContextBuilder(100000, 4096)
	history := []store.Message{
		{Role: llm.RoleUser, Content: "hi"},
	}
	got := cb.selectHistory(history, 0)
	if got == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestSelectHistory_FitsAll(t *testing.T) {
	cb := NewContextBuilder(100000, 4096)
	history := []store.Message{
		{Role: llm.RoleUser, Content: "a"},
		{Role: llm.RoleAssistant, Content: "b"},
		{Role: llm.RoleUser, Content: "c"},
	}
	got := cb.selectHistory(history, 100000)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Content != "a" || got[1].Content != "b" || got[2].Content != "c" {
		t.Errorf("order not preserved: %v", got)
	}
}

func TestSelectHistory_BudgetExhausted(t *testing.T) {
	cb := NewContextBuilder(100000, 4096)
	history := []store.Message{
		{Role: llm.RoleUser, Content: "aaaa"},
		{Role: llm.RoleAssistant, Content: "bbbb"},
		{Role: llm.RoleUser, Content: "cc"},
	}
	got := cb.selectHistory(history, 1)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (only last fits)", len(got))
	}
	if got[0].Content != "cc" {
		t.Errorf("Content = %q, want %q (most recent)", got[0].Content, "cc")
	}
}

func TestContextBuilder_Build_EmptyInputs(t *testing.T) {
	cb := NewContextBuilder(100000, 4096)
	prompt, msgs := cb.Build(nil, nil, "system prompt")
	if prompt != "system prompt" {
		t.Errorf("prompt = %q, want %q", prompt, "system prompt")
	}
	if len(msgs) != 0 {
		t.Errorf("msgs len = %d, want 0", len(msgs))
	}
}

func TestContextBuilder_Build_WithHistory(t *testing.T) {
	cb := NewContextBuilder(100000, 4096)
	history := []store.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "hi"},
	}
	_, msgs := cb.Build(nil, history, "sys")
	if len(msgs) != 2 {
		t.Fatalf("msgs len = %d, want 2", len(msgs))
	}
}

func TestContextBuilder_Build_HashCaching(t *testing.T) {
	cb := NewContextBuilder(100000, 4096)
	wsFiles := []WorkspaceFile{
		{Key: "test", Content: "same content"},
	}
	p1, _ := cb.Build(wsFiles, nil, "sys")
	p2, _ := cb.Build(wsFiles, nil, "sys")

	if p1 != p2 {
		t.Errorf("same workspace files should produce same prompt on second call")
	}
}

func TestResolveImageData_LoadsFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	if err := os.WriteFile(path, []byte("fake png"), 0600); err != nil {
		t.Fatal(err)
	}

	cb := NewContextBuilder(100000, 4096)
	img := llm.ImageContent{MediaType: "image/png", Path: path}
	results := []llm.ToolResult{
		{ToolCallID: "tc1", Content: "output", Images: []llm.ImageContent{img}},
	}

	cb.resolveImageData(results)

	if results[0].Images[0].Data() == nil {
		t.Fatal("expected image data to be loaded")
	}
	if string(results[0].Images[0].Data()) != "fake png" {
		t.Errorf("data = %q, want %q", string(results[0].Images[0].Data()), "fake png")
	}
}

func TestResolveImageData_CachesAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	if err := os.WriteFile(path, []byte("fake png"), 0600); err != nil {
		t.Fatal(err)
	}

	cb := NewContextBuilder(100000, 4096)

	// First call loads from disk
	results1 := []llm.ToolResult{
		{ToolCallID: "tc1", Content: "output", Images: []llm.ImageContent{
			{MediaType: "image/png", Path: path},
		}},
	}
	cb.resolveImageData(results1)

	if results1[0].Images[0].Data() == nil {
		t.Fatal("expected image data to be loaded")
	}

	// Delete the file — second call should use cache
	_ = os.Remove(path)

	results2 := []llm.ToolResult{
		{ToolCallID: "tc2", Content: "output", Images: []llm.ImageContent{
			{MediaType: "image/png", Path: path},
		}},
	}
	cb.resolveImageData(results2)

	if results2[0].Images[0].Data() == nil {
		t.Fatal("expected image data from cache after file deleted")
	}
	if string(results2[0].Images[0].Data()) != "fake png" {
		t.Errorf("cached data = %q, want %q", string(results2[0].Images[0].Data()), "fake png")
	}
}

func TestResolveImageData_MissingFile(t *testing.T) {
	cb := NewContextBuilder(100000, 4096)
	img := llm.ImageContent{MediaType: "image/png", Path: "/nonexistent/path.png"}
	results := []llm.ToolResult{
		{ToolCallID: "tc1", Content: "output", Images: []llm.ImageContent{img}},
	}

	cb.resolveImageData(results)

	if results[0].Images[0].Data() != nil {
		t.Error("expected nil data for missing file")
	}
}

func TestResolveImageData_AlreadyHasData(t *testing.T) {
	cb := NewContextBuilder(100000, 4096)
	img := llm.ImageContent{MediaType: "image/png", Path: "/some/path.png"}
	img.SetData([]byte("existing"))
	results := []llm.ToolResult{
		{ToolCallID: "tc1", Content: "output", Images: []llm.ImageContent{img}},
	}

	cb.resolveImageData(results)

	if string(results[0].Images[0].Data()) != "existing" {
		t.Errorf("data should not be overwritten, got %q", string(results[0].Images[0].Data()))
	}
}

func TestResolveImageData_EmptyImages(t *testing.T) {
	cb := NewContextBuilder(100000, 4096)
	results := []llm.ToolResult{
		{ToolCallID: "tc1", Content: "output"},
	}
	cb.resolveImageData(results)
}

func TestRebuildMessages_UsesCachedBudget(t *testing.T) {
	cb := NewContextBuilder(100000, 4096)

	wsFiles := []WorkspaceFile{
		{Key: "test", Content: "workspace content here"},
	}
	history := []store.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "hi"},
	}

	_, msgsFromBuild := cb.Build(wsFiles, history, "system prompt")
	msgsFromRebuild := cb.RebuildMessages(history)

	if len(msgsFromBuild) != len(msgsFromRebuild) {
		t.Errorf("RebuildMessages returned %d messages, Build returned %d", len(msgsFromRebuild), len(msgsFromBuild))
	}
}

func TestSelectHistory_ImageTokenBudget(t *testing.T) {
	img := llm.ImageContent{MediaType: "image/png", Path: "/nonexistent.png"}
	toolResults := []llm.ToolResult{
		{ToolCallID: "tc1", Content: "ok", Images: []llm.ImageContent{img}},
	}
	trData, _ := json.Marshal(toolResults)

	history := []store.Message{
		{Role: llm.RoleUser, Content: "hi", ContentType: store.ContentTypeText},
		{Role: llm.RoleAssistant, Content: "let me check", ContentType: store.ContentTypeText},
		{Role: llm.RoleAssistant, ContentType: store.ContentTypeToolCalls, Content: `{"tool_calls":[{"id":"tc1","name":"browser","input":{}}]}`},
		{Role: llm.RoleUser, ContentType: store.ContentTypeToolResults, Content: string(trData)},
	}

	cb := NewContextBuilder(100000, 4096)

	msgs := cb.selectHistory(history, 100000)
	if len(msgs) == 0 {
		t.Fatal("expected some messages in history")
	}
}

func TestSelectHistory_StripsImagesFromOldMessages(t *testing.T) {
	img := llm.ImageContent{MediaType: "image/png", Path: "/nonexistent.png"}
	makeToolResultMsg := func(id string) store.Message {
		results := []llm.ToolResult{
			{ToolCallID: id, Content: "ok", Images: []llm.ImageContent{img}},
		}
		data, _ := json.Marshal(results)
		return store.Message{
			Role:        llm.RoleUser,
			ContentType: store.ContentTypeToolResults,
			Content:     string(data),
		}
	}

	makeToolCallMsg := func(id string) store.Message {
		data, _ := json.Marshal(struct {
			ToolCalls []llm.ToolCall `json:"tool_calls"`
		}{
			ToolCalls: []llm.ToolCall{{ID: id, Name: "browser", Input: json.RawMessage(`{}`)}},
		})
		return store.Message{
			Role:        llm.RoleAssistant,
			ContentType: store.ContentTypeToolCalls,
			Content:     string(data),
		}
	}

	var history []store.Message
	for i := 0; i < 5; i++ {
		id := "tc" + string(rune('0'+i))
		history = append(history, makeToolCallMsg(id), makeToolResultMsg(id))
	}

	cb := NewContextBuilder(100000, 4096)
	msgs := cb.selectHistory(history, 100000)

	resultMsgCount := 0
	for _, m := range msgs {
		if len(m.ToolResults) > 0 {
			resultMsgCount++
		}
	}

	imagesKept := 0
	for _, m := range msgs {
		for _, tr := range m.ToolResults {
			if len(tr.Images) > 0 {
				imagesKept++
			}
		}
	}

	if imagesKept > config.MaxRecentImageMessages {
		t.Errorf("kept %d image-bearing results, want at most %d", imagesKept, config.MaxRecentImageMessages)
	}
}
