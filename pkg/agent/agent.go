package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/igorsilveira/pincer/pkg/agent/tools"
	"github.com/igorsilveira/pincer/pkg/audit"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/memory"
	"github.com/igorsilveira/pincer/pkg/sandbox"
	"github.com/igorsilveira/pincer/pkg/store"
	"github.com/igorsilveira/pincer/pkg/telemetry"
	"gorm.io/gorm"
)

const maxToolIterations = 10

type agentCtxKey int

const ctxKeyAutoApprove agentCtxKey = iota

func WithAutoApprove(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyAutoApprove, true)
}

func autoApproveFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(ctxKeyAutoApprove).(bool)
	return v
}

type TurnEvent struct {
	Type            TurnEventType
	Token           string
	Error           error
	Usage           *llm.Usage
	Message         string
	ToolCall        *llm.ToolCall
	ApprovalRequest *ApprovalRequest
}

type TurnEventType int

const (
	TurnToken TurnEventType = iota
	TurnDone
	TurnError
	TurnToolCall
	TurnToolResult
	TurnApprovalNeeded
)

type Runtime struct {
	provider      llm.Provider
	store         *store.Store
	registry      *tools.Registry
	sandbox       sandbox.Sandbox
	approver      *Approver
	model           string
	maxTokens       int
	maxOutputTokens int
	systemPrompt    string
	memory        *memory.Store
	audit         *audit.Logger
	defaultPolicy sandbox.Policy
	ctxBuilder    *ContextBuilder
	memoryMu      sync.Mutex
	memoryHashes  map[string]map[string]string
}

type RuntimeConfig struct {
	Provider      llm.Provider
	Store         *store.Store
	Registry      *tools.Registry
	Sandbox       sandbox.Sandbox
	Approver      *Approver
	Model           string
	MaxTokens       int
	MaxOutputTokens int
	SystemPrompt    string
	Memory        *memory.Store
	Audit         *audit.Logger
	DefaultPolicy sandbox.Policy
}

func NewRuntime(cfg RuntimeConfig) *Runtime {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 128000
	}
	if cfg.MaxOutputTokens <= 0 {
		cfg.MaxOutputTokens = 4096
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = defaultSystemPrompt
	}
	return &Runtime{
		provider:        cfg.Provider,
		store:           cfg.Store,
		registry:        cfg.Registry,
		sandbox:         cfg.Sandbox,
		approver:        cfg.Approver,
		model:           cfg.Model,
		maxTokens:       cfg.MaxTokens,
		maxOutputTokens: cfg.MaxOutputTokens,
		systemPrompt:    cfg.SystemPrompt,
		memory:        cfg.Memory,
		audit:         cfg.Audit,
		defaultPolicy: cfg.DefaultPolicy,
		ctxBuilder:    NewContextBuilder(cfg.MaxTokens),
		memoryHashes:  make(map[string]map[string]string),
	}
}

const defaultSystemPrompt = `You are Pincer, a helpful AI assistant. Be concise and accurate.
You have access to tools for executing shell commands, reading/writing files, and making HTTP requests.
Use tools when needed to accomplish tasks. Always explain what you're doing.`

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

	logger.Debug("running agent turn",
		slog.String("session_id", session.ID),
	)

	out := make(chan TurnEvent, 64)
	go r.runAgenticLoop(ctx, session.ID, out)

	return out, nil
}

func (r *Runtime) runAgenticLoop(ctx context.Context, sessionID string, out chan<- TurnEvent) {
	defer close(out)
	logger := telemetry.FromContext(ctx)

	sess, err := r.store.GetSession(ctx, sessionID)
	if err != nil {
		out <- TurnEvent{Type: TurnError, Error: fmt.Errorf("loading session: %w", err)}
		return
	}
	ctx = tools.WithSessionInfo(ctx, sessionID, sess.AgentID)

	if err := r.CompactSession(ctx, sessionID); err != nil {
		logger.Warn("session compaction failed", slog.String("err", err.Error()))
	}

	for iteration := 0; iteration < maxToolIterations; iteration++ {

		history, err := r.store.RecentMessages(ctx, sessionID, 50)
		if err != nil {
			out <- TurnEvent{Type: TurnError, Error: fmt.Errorf("loading history: %w", err)}
			return
		}

		systemPrompt, chatMessages := r.buildSmartContext(ctx, sess.AgentID, sessionID, history)

		if len(chatMessages) == 0 {
			out <- TurnEvent{Type: TurnError, Error: fmt.Errorf("no messages after context building")}
			return
		}

		var toolDefs []llm.ToolDefinition
		if r.registry != nil && r.provider.SupportsToolUse() {
			toolDefs = r.registry.Definitions()
		}

		events, err := r.provider.Chat(ctx, llm.ChatRequest{
			Model:     r.model,
			System:    systemPrompt,
			Messages:  chatMessages,
			MaxTokens: r.maxOutputTokens,
			Stream:    true,
			Tools:     toolDefs,
		})
		if err != nil {
			out <- TurnEvent{Type: TurnError, Error: fmt.Errorf("calling LLM: %w", err)}
			return
		}

		var textContent []byte
		var toolCalls []llm.ToolCall
		var usage *llm.Usage

		for ev := range events {
			switch ev.Type {
			case llm.EventToken:
				textContent = append(textContent, ev.Token...)
				out <- TurnEvent{Type: TurnToken, Token: ev.Token}

			case llm.EventToolCall:
				toolCalls = append(toolCalls, *ev.ToolCall)
				out <- TurnEvent{Type: TurnToolCall, ToolCall: ev.ToolCall}

			case llm.EventDone:
				usage = ev.Usage

			case llm.EventError:
				logger.Error("llm stream error", slog.String("err", ev.Error.Error()))
				out <- TurnEvent{Type: TurnError, Error: ev.Error}
				return
			}
		}

		if len(toolCalls) == 0 {
			r.persistAssistantMessage(ctx, logger, sessionID, string(textContent), usage)
			out <- TurnEvent{Type: TurnDone, Message: string(textContent), Usage: usage}
			return
		}

		r.persistToolCallMessage(ctx, logger, sessionID, string(textContent), toolCalls, usage)

		var toolResults []llm.ToolResult
		for _, tc := range toolCalls {
			result := r.executeTool(ctx, logger, sessionID, tc, out)
			toolResults = append(toolResults, result)
		}

		r.persistToolResultMessage(ctx, logger, sessionID, toolResults)

		logger.Debug("tool iteration complete",
			slog.Int("iteration", iteration+1),
			slog.Int("tool_calls", len(toolCalls)),
		)
	}

	logger.Warn("max tool iterations reached", slog.Int("max", maxToolIterations))
	out <- TurnEvent{Type: TurnDone, Message: "(max tool iterations reached)"}
}

func (r *Runtime) executeTool(ctx context.Context, logger *slog.Logger, sessionID string, tc llm.ToolCall, out chan<- TurnEvent) llm.ToolResult {
	logger.Info("executing tool",
		slog.String("tool", tc.Name),
		slog.String("id", tc.ID),
	)

	if r.approver != nil && !autoApproveFromContext(ctx) {
		req := ApprovalRequest{
			ID:        uuid.NewString(),
			SessionID: sessionID,
			ToolName:  tc.Name,
			Input:     string(tc.Input),
		}

		if r.approver.mode == ApprovalAsk {
			out <- TurnEvent{
				Type:            TurnApprovalNeeded,
				ApprovalRequest: &req,
			}
		}

		approved, err := r.approver.RequestApproval(ctx, req)
		if err != nil || !approved {
			reason := "tool call denied by user"
			if err != nil {
				reason = fmt.Sprintf("approval error: %v", err)
			}
			r.auditLog(ctx, audit.EventToolDeny, sessionID, tc.Name, reason)
			return llm.ToolResult{
				ToolCallID: tc.ID,
				Content:    reason,
				IsError:    true,
			}
		}
		r.auditLog(ctx, audit.EventToolApprove, sessionID, tc.Name, "")
	}

	if r.registry == nil {
		return llm.ToolResult{
			ToolCallID: tc.ID,
			Content:    "no tools available",
			IsError:    true,
		}
	}

	tool, err := r.registry.Get(tc.Name)
	if err != nil {
		return llm.ToolResult{
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("unknown tool: %s", tc.Name),
			IsError:    true,
		}
	}

	policy := r.defaultPolicy
	if policy.Timeout == 0 {
		policy = sandbox.DefaultPolicy()
	}
	if (r.approver != nil && r.approver.mode == ApprovalAuto) || autoApproveFromContext(ctx) {
		policy.RequireApproval = false
	}

	output, err := tool.Execute(ctx, tc.Input, r.sandbox, policy)
	if err != nil {
		logger.Error("tool execution failed",
			slog.String("tool", tc.Name),
			slog.String("err", err.Error()),
		)
		r.auditLog(ctx, audit.EventToolExec, sessionID, tc.Name,
			fmt.Sprintf("error: %v", err))
		return llm.ToolResult{
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("error: %v", err),
			IsError:    true,
		}
	}

	r.auditLog(ctx, audit.EventToolExec, sessionID, tc.Name, "ok")
	out <- TurnEvent{Type: TurnToolResult}

	return llm.ToolResult{
		ToolCallID: tc.ID,
		Content:    output,
	}
}

func (r *Runtime) persistAssistantMessage(ctx context.Context, logger *slog.Logger, sessionID, content string, usage *llm.Usage) {
	tokenCount := 0
	if usage != nil {
		tokenCount = usage.OutputTokens
	}

	msg := &store.Message{
		ID:          uuid.NewString(),
		SessionID:   sessionID,
		Role:        llm.RoleAssistant,
		ContentType: store.ContentTypeText,
		Content:     content,
		TokenCount:  tokenCount,
		CreatedAt:   time.Now().UTC(),
	}

	if err := r.store.AppendMessage(context.Background(), msg); err != nil {
		logger.Error("failed to persist assistant message", slog.String("err", err.Error()))
	}

	if err := r.store.TouchSession(context.Background(), sessionID); err != nil {
		logger.Error("failed to touch session", slog.String("err", err.Error()))
	}
}

func (r *Runtime) persistToolCallMessage(ctx context.Context, logger *slog.Logger, sessionID, textContent string, toolCalls []llm.ToolCall, usage *llm.Usage) {
	data, _ := json.Marshal(struct {
		Text      string         `json:"text,omitempty"`
		ToolCalls []llm.ToolCall `json:"tool_calls"`
	}{
		Text:      textContent,
		ToolCalls: toolCalls,
	})

	tokenCount := 0
	if usage != nil {
		tokenCount = usage.OutputTokens
	}

	msg := &store.Message{
		ID:          uuid.NewString(),
		SessionID:   sessionID,
		Role:        llm.RoleAssistant,
		ContentType: store.ContentTypeToolCalls,
		Content:     string(data),
		TokenCount:  tokenCount,
		CreatedAt:   time.Now().UTC(),
	}

	if err := r.store.AppendMessage(context.Background(), msg); err != nil {
		logger.Error("failed to persist tool call message", slog.String("err", err.Error()))
	}
}

func (r *Runtime) persistToolResultMessage(ctx context.Context, logger *slog.Logger, sessionID string, results []llm.ToolResult) {
	data, _ := json.Marshal(results)

	msg := &store.Message{
		ID:          uuid.NewString(),
		SessionID:   sessionID,
		Role:        llm.RoleUser,
		ContentType: store.ContentTypeToolResults,
		Content:     string(data),
		CreatedAt:   time.Now().UTC(),
	}

	if err := r.store.AppendMessage(context.Background(), msg); err != nil {
		logger.Error("failed to persist tool result message", slog.String("err", err.Error()))
	}
}

func (r *Runtime) buildContext(history []store.Message) []llm.ChatMessage {
	msgs := make([]llm.ChatMessage, 0, len(history))
	for _, m := range history {
		msgs = append(msgs, r.messageToChat(m))
	}
	return sanitizeToolPairs(msgs)
}

func (r *Runtime) messageToChat(m store.Message) llm.ChatMessage {
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

func (r *Runtime) getOrCreateSession(ctx context.Context, sessionID string) (*store.Session, error) {
	sess, err := r.store.GetSession(ctx, sessionID)
	if err == nil {
		return sess, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
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
	r.auditLog(ctx, audit.EventSessionNew, sessionID, "system",
		fmt.Sprintf("channel=%s peer=%s", sess.Channel, sess.PeerID))
	return sess, nil
}

func (r *Runtime) buildSmartContext(ctx context.Context, agentID, sessionID string, history []store.Message) (string, []llm.ChatMessage) {
	if r.ctxBuilder == nil {
		return r.systemPrompt, r.buildContext(history)
	}

	var wsFiles []WorkspaceFile

	if r.memory != nil {
		r.memoryMu.Lock()
		lastHashes := r.memoryHashes[sessionID]
		if lastHashes == nil {
			lastHashes = make(map[string]string)
		}
		r.memoryMu.Unlock()

		memCtx, newHashes, err := r.memory.BuildContext(ctx, agentID, lastHashes)
		if err == nil {
			r.memoryMu.Lock()
			r.memoryHashes[sessionID] = newHashes
			r.memoryMu.Unlock()

			if memCtx != "" {
				wsFiles = append(wsFiles, WorkspaceFile{Key: "memory", Content: memCtx})
			}
		}
	}

	return r.ctxBuilder.Build(wsFiles, history, r.systemPrompt)
}

func (r *Runtime) auditLog(ctx context.Context, eventType, sessionID, actor, detail string) {
	if r.audit == nil {
		return
	}
	_ = r.audit.Log(ctx, eventType, sessionID, tools.AgentIDFromContext(ctx), actor, detail)
}

func ContentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}
