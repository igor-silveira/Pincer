package agent

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/store"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type TurnEvent struct {
	Type    TurnEventType
	Token   string
	Error   error
	Usage   *llm.Usage
	Message string
}

type TurnEventType int

const (
	TurnToken TurnEventType = iota
	TurnDone
	TurnError
)

type Runtime struct {
	provider     llm.Provider
	store        *store.Store
	model        string
	maxTokens    int
	systemPrompt string
	contextCache map[string]string
}

type RuntimeConfig struct {
	Provider     llm.Provider
	Store        *store.Store
	Model        string
	MaxTokens    int
	SystemPrompt string
}

func NewRuntime(cfg RuntimeConfig) *Runtime {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = defaultSystemPrompt
	}
	return &Runtime{
		provider:     cfg.Provider,
		store:        cfg.Store,
		model:        cfg.Model,
		maxTokens:    cfg.MaxTokens,
		systemPrompt: cfg.SystemPrompt,
		contextCache: make(map[string]string),
	}
}

const defaultSystemPrompt = "You are Pincer, a helpful AI assistant. Be concise and accurate."

func (r *Runtime) RunTurn(ctx context.Context, sessionID, userMessage string) (<-chan TurnEvent, error) {
	logger := telemetry.FromContext(ctx)

	session, err := r.getOrCreateSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("resolving session: %w", err)
	}

	userMsg := &store.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      llm.RoleUser,
		Content:   userMessage,
		CreatedAt: time.Now().UTC(),
	}
	if err := r.store.AppendMessage(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("persisting user message: %w", err)
	}

	history, err := r.store.RecentMessages(ctx, session.ID, 50)
	if err != nil {
		return nil, fmt.Errorf("loading history: %w", err)
	}

	chatMessages := r.buildContext(history)

	logger.Debug("running agent turn",
		slog.String("session_id", session.ID),
		slog.Int("history_len", len(chatMessages)),
	)

	events, err := r.provider.Chat(ctx, llm.ChatRequest{
		Model:     r.model,
		System:    r.systemPrompt,
		Messages:  chatMessages,
		MaxTokens: r.maxTokens,
		Stream:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("calling LLM: %w", err)
	}

	out := make(chan TurnEvent, 64)
	go r.processTurn(ctx, session.ID, events, out)

	return out, nil
}

func (r *Runtime) processTurn(ctx context.Context, sessionID string, events <-chan llm.ChatEvent, out chan<- TurnEvent) {
	defer close(out)
	logger := telemetry.FromContext(ctx)

	var full []byte
	var usage *llm.Usage

	for ev := range events {
		switch ev.Type {
		case llm.EventToken:
			full = append(full, ev.Token...)
			out <- TurnEvent{Type: TurnToken, Token: ev.Token}

		case llm.EventDone:
			usage = ev.Usage

		case llm.EventError:
			logger.Error("llm stream error", slog.String("err", ev.Error.Error()))
			out <- TurnEvent{Type: TurnError, Error: ev.Error}
			return
		}
	}

	tokenCount := 0
	if usage != nil {
		tokenCount = usage.OutputTokens
	}

	assistantMsg := &store.Message{
		ID:         uuid.NewString(),
		SessionID:  sessionID,
		Role:       llm.RoleAssistant,
		Content:    string(full),
		TokenCount: tokenCount,
		CreatedAt:  time.Now().UTC(),
	}

	if err := r.store.AppendMessage(context.Background(), assistantMsg); err != nil {
		logger.Error("failed to persist assistant message", slog.String("err", err.Error()))
	}

	if err := r.store.TouchSession(context.Background(), sessionID); err != nil {
		logger.Error("failed to touch session", slog.String("err", err.Error()))
	}

	out <- TurnEvent{
		Type:    TurnDone,
		Message: string(full),
		Usage:   usage,
	}
}

func (r *Runtime) buildContext(history []store.Message) []llm.ChatMessage {
	msgs := make([]llm.ChatMessage, 0, len(history))
	for _, m := range history {
		msgs = append(msgs, llm.ChatMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return msgs
}

func (r *Runtime) getOrCreateSession(ctx context.Context, sessionID string) (*store.Session, error) {
	sess, err := r.store.GetSession(ctx, sessionID)
	if err == nil {
		return sess, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	now := time.Now().UTC()
	sess = &store.Session{
		ID:        sessionID,
		AgentID:   "default",
		Channel:   "webchat",
		PeerID:    "anonymous",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := r.store.CreateSession(ctx, sess); err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}
	return sess, nil
}

func ContentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}
