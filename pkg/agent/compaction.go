package agent

import (
	"context"
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

const compactionPrompt = `Summarize the following conversation history into a concise summary.
Focus on: key facts discussed, decisions made, user preferences revealed, and any pending tasks.
Be specific and factual. Use bullet points. Keep it under 500 words.

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
		slog.Int("message_count", count),
	)

	messages, err := r.store.RecentMessages(ctx, sessionID, count)
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
		if m.ContentType != store.ContentTypeText {

			conv.WriteString(fmt.Sprintf("[%s: %s interaction]\n", m.Role, m.ContentType))
			continue
		}
		conv.WriteString(fmt.Sprintf("%s: %s\n\n", m.Role, m.Content))
	}

	prompt := fmt.Sprintf(compactionPrompt, conv.String())

	events, err := r.provider.Chat(ctx, llm.ChatRequest{
		Model:     r.model,
		System:    "You are a conversation summarizer. Be concise and factual.",
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
