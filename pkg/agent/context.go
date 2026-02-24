package agent

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/store"
)

type ContextBuilder struct {
	mu           sync.RWMutex
	staticHash   map[string]string
	cachedTokens map[string]int
	budget       int
}

type WorkspaceFile struct {
	Key     string
	Content string
}

func NewContextBuilder(budget int) *ContextBuilder {
	if budget <= 0 {
		budget = 128000
	}
	return &ContextBuilder{
		staticHash:   make(map[string]string),
		cachedTokens: make(map[string]int),
		budget:       budget,
	}
}

func (cb *ContextBuilder) Build(workspaceFiles []WorkspaceFile, history []store.Message, systemPrompt string) (string, []llm.ChatMessage) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	usedTokens := estimateTokens(systemPrompt)

	var wsContext string
	for _, wf := range workspaceFiles {
		hash := ContentHash(wf.Content)
		prevHash, known := cb.staticHash[wf.Key]

		if known && hash == prevHash {

			tokenCost := cb.cachedTokens[wf.Key]
			if usedTokens+tokenCost > cb.budget/2 {
				continue
			}
		}

		cb.staticHash[wf.Key] = hash
		tokens := estimateTokens(wf.Content)
		cb.cachedTokens[wf.Key] = tokens

		wsContext += "\n\n--- " + wf.Key + " ---\n" + wf.Content
		usedTokens += tokens
	}

	finalPrompt := systemPrompt
	if wsContext != "" {
		finalPrompt += "\n\n# Workspace Context" + wsContext
	}

	remaining := cb.budget - usedTokens

	remaining -= 4096

	messages := cb.selectHistory(history, remaining)

	slog.Debug("context build completed",
		slog.Int("workspace_files", len(workspaceFiles)),
		slog.Int("history_messages", len(history)),
		slog.Int("selected_messages", len(messages)),
		slog.Int("budget", cb.budget),
		slog.Int("remaining_budget", remaining),
	)

	return finalPrompt, messages
}

const imageTokenEstimate = 1600

const maxRecentImageMessages = 3

func (cb *ContextBuilder) selectHistory(history []store.Message, budget int) []llm.ChatMessage {
	if budget <= 0 || len(history) == 0 {
		slog.Debug("context selection skipped",
			slog.Int("budget", budget),
			slog.Int("history_len", len(history)),
		)
		return []llm.ChatMessage{}
	}

	type indexedMsg struct {
		idx    int
		msg    llm.ChatMessage
		tokens int
	}

	var selected []indexedMsg
	usedTokens := 0
	imageResultsSeen := 0

	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		tokens := estimateTokens(m.Content)

		chatMsg := messageToLLM(m)

		if len(chatMsg.ToolResults) > 0 && imageResultsSeen < maxRecentImageMessages {
			for j := range chatMsg.ToolResults {
				for range chatMsg.ToolResults[j].Images {
					tokens += imageTokenEstimate
				}
			}
			resolveImageData(chatMsg.ToolResults)
			imageResultsSeen++
		} else if len(chatMsg.ToolResults) > 0 {
			slog.Debug("stripping images from old tool result",
				slog.Int("message_index", i),
				slog.Int("images_seen", imageResultsSeen),
			)
			for j := range chatMsg.ToolResults {
				chatMsg.ToolResults[j].Images = nil
			}
		}

		if usedTokens+tokens > budget {
			slog.Debug("context budget exhausted",
				slog.Int("used_tokens", usedTokens),
				slog.Int("budget", budget),
				slog.Int("messages_selected", len(selected)),
				slog.Int("messages_remaining", i+1),
			)
			break
		}

		selected = append(selected, indexedMsg{idx: i, msg: chatMsg, tokens: tokens})
		usedTokens += tokens
	}

	result := make([]llm.ChatMessage, len(selected))
	for i, s := range selected {
		result[len(selected)-1-i] = s.msg
	}

	return sanitizeToolPairs(result)
}

func resolveImageData(results []llm.ToolResult) {
	for i := range results {
		for j := range results[i].Images {
			img := &results[i].Images[j]
			if img.Data() == nil && img.Path != "" {
				data, err := os.ReadFile(img.Path)
				if err == nil {
					img.SetData(data)
				}
			}
		}
	}
}

func sanitizeToolPairs(msgs []llm.ChatMessage) []llm.ChatMessage {
	result := make([]llm.ChatMessage, 0, len(msgs))
	i := 0
	for i < len(msgs) {
		if msgs[i].Role == llm.RoleAssistant && len(msgs[i].ToolCalls) > 0 {
			if i+1 < len(msgs) && len(msgs[i+1].ToolResults) > 0 {
				result = append(result, msgs[i], msgs[i+1])
				i += 2
				continue
			}
			i++
			continue
		}
		if len(msgs[i].ToolResults) > 0 {
			i++
			continue
		}
		result = append(result, msgs[i])
		i++
	}
	return result
}

func messageToLLM(m store.Message) llm.ChatMessage {
	switch m.ContentType {
	case store.ContentTypeToolCalls:
		var data struct {
			Text      string         `json:"text,omitempty"`
			ToolCalls []llm.ToolCall `json:"tool_calls"`
		}
		if err := json.Unmarshal([]byte(m.Content), &data); err == nil {
			return llm.ChatMessage{
				Role:      m.Role,
				Content:   data.Text,
				ToolCalls: data.ToolCalls,
			}
		}
	case store.ContentTypeToolResults:
		var results []llm.ToolResult
		if err := json.Unmarshal([]byte(m.Content), &results); err == nil {
			return llm.ChatMessage{
				Role:        m.Role,
				ToolResults: results,
			}
		}
	}

	return llm.ChatMessage{
		Role:    m.Role,
		Content: m.Content,
	}
}

func estimateTokens(s string) int {
	return (len(s) + 3) / 4
}
