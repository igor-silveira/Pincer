package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/store"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)

const (
	compactionThreshold = 40
	keepRecentMessages  = 10
	summaryMaxTokens    = 1024
)

const compactionPrompt = `Summarize this conversation history for use as context in future turns.

Preserve:
- Key facts, decisions, and conclusions reached
- User preferences, corrections, and explicit requests
- Outcomes of tool calls (what was created, modified, found, or failed)
- Names, paths, URLs, and identifiers that were referenced
- Any pending or incomplete tasks

Omit:
- Intermediate debugging steps that led nowhere
- Raw tool output already summarized in conversation
- Redundant clarifications already resolved

Use bullet points. Be specific and factual. Keep under 500 words.

Conversation:
%s`

func (r *Runtime) CompactSession(ctx context.Context, sessionID string) error {
	logger := telemetry.FromContext(ctx)

	count, err := r.store.MessageCount(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("counting messages: %w", err)
	}

	if count <= compactionThreshold {
		return nil
	}

	logger.Info("compacting session",
		slog.String("session_id", sessionID),
		slog.Int64("message_count", count),
	)

	messages, err := r.store.RecentMessages(ctx, sessionID, int(count))
	if err != nil {
		return fmt.Errorf("loading messages: %w", err)
	}

	splitIdx := len(messages) - keepRecentMessages
	if splitIdx <= 0 {
		return nil
	}

	oldMessages := messages[:splitIdx]
	recentMessages := messages[splitIdx:]

	var conv strings.Builder
	for _, m := range oldMessages {
		if m.ContentType == store.ContentTypeText && strings.HasPrefix(m.Content, "[Session Summary]") {
			fmt.Fprintf(&conv, "[Previous Summary]: %s\n\n", m.Content[len("[Session Summary]\n"):])
			continue
		}
		if m.ContentType == store.ContentTypeToolCalls {
			var data struct {
				Text      string         `json:"text,omitempty"`
				ToolCalls []llm.ToolCall `json:"tool_calls"`
			}
			if err := json.Unmarshal([]byte(m.Content), &data); err == nil {
				var names []string
				for _, tc := range data.ToolCalls {
					names = append(names, tc.Name)
				}
				if data.Text != "" {
					fmt.Fprintf(&conv, "%s: %s [called: %s]\n", m.Role, truncate(data.Text, 200), strings.Join(names, ", "))
				} else {
					fmt.Fprintf(&conv, "%s: [called: %s]\n", m.Role, strings.Join(names, ", "))
				}
			} else {
				fmt.Fprintf(&conv, "[%s: tool interaction]\n", m.Role)
			}
			continue
		}
		if m.ContentType == store.ContentTypeToolResults {
			var results []llm.ToolResult
			if err := json.Unmarshal([]byte(m.Content), &results); err == nil {
				for _, r := range results {
					status := "ok"
					if r.IsError {
						status = "error"
					}
					fmt.Fprintf(&conv, "[tool result (%s): %s]\n", status, truncate(r.Content, 150))
				}
			} else {
				fmt.Fprintf(&conv, "[%s: tool results]\n", m.Role)
			}
			continue
		}
		if m.ContentType != store.ContentTypeText {
			fmt.Fprintf(&conv, "[%s: %s interaction]\n", m.Role, m.ContentType)
			continue
		}
		fmt.Fprintf(&conv, "%s: %s\n\n", m.Role, m.Content)
	}

	prompt := fmt.Sprintf(compactionPrompt, conv.String())

	events, err := r.provider.Chat(ctx, llm.ChatRequest{
		Model:     r.model,
		System:    "You are a conversation summarizer for an AI assistant. Produce a structured, factual summary that preserves actionable context. Never fabricate information not present in the conversation.",
		Messages:  []llm.ChatMessage{{Role: llm.RoleUser, Content: prompt}},
		MaxTokens: summaryMaxTokens,
		Stream:    false,
	})
	if err != nil {
		return fmt.Errorf("calling LLM for summary: %w", err)
	}

	var summary strings.Builder
	for ev := range events {
		if ev.Type == llm.EventToken {
			summary.WriteString(ev.Token)
		}
		if ev.Type == llm.EventError {
			return fmt.Errorf("LLM summary error: %w", ev.Error)
		}
	}

	if err := r.store.DeleteMessages(ctx, messageIDs(oldMessages)); err != nil {
		return fmt.Errorf("deleting old messages: %w", err)
	}

	summaryMsg := &store.Message{
		ID:          uuid.NewString(),
		SessionID:   sessionID,
		Role:        llm.RoleAssistant,
		ContentType: store.ContentTypeText,
		Content:     "[Session Summary]\n" + summary.String(),
		CreatedAt:   recentMessages[0].CreatedAt.Add(-time.Second),
	}

	if err := r.store.AppendMessage(ctx, summaryMsg); err != nil {
		return fmt.Errorf("inserting summary: %w", err)
	}

	logger.Info("session compacted",
		slog.String("session_id", sessionID),
		slog.Int("removed", len(oldMessages)),
		slog.Int("kept", len(recentMessages)),
	)

	return nil
}

func messageIDs(msgs []store.Message) []string {
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	return ids
}
