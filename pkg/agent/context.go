package agent

import (
	"encoding/json"
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

	return finalPrompt, messages
}

func (cb *ContextBuilder) selectHistory(history []store.Message, budget int) []llm.ChatMessage {
	if budget <= 0 || len(history) == 0 {
		return nil
	}

	type indexedMsg struct {
		idx    int
		msg    llm.ChatMessage
		tokens int
	}

	var selected []indexedMsg
	usedTokens := 0

	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		tokens := estimateTokens(m.Content)

		if usedTokens+tokens > budget {
			break
		}

		chatMsg := messageToLLM(m)
		selected = append(selected, indexedMsg{idx: i, msg: chatMsg, tokens: tokens})
		usedTokens += tokens
	}

	result := make([]llm.ChatMessage, len(selected))
	for i, s := range selected {
		result[len(selected)-1-i] = s.msg
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
