package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

const maxSubagentDepth = 3

const (
	llmMaxRetries     = 3
	llmBaseRetryDelay = 5 * time.Second
	llmMaxRetryDelay  = 60 * time.Second
)

func (r *Runtime) chatWithRetry(ctx context.Context, logger *slog.Logger, req llm.ChatRequest, notify func(string)) (<-chan llm.ChatEvent, error) {
	var lastErr error
	for attempt := 0; attempt <= llmMaxRetries; attempt++ {
		events, err := r.provider.Chat(ctx, req)
		if err == nil {
			return events, nil
		}

		retryAfter, retryable := llm.IsRetryable(err)
		if !retryable {
			return nil, err
		}

		lastErr = err
		delay := retryAfter
		if delay == 0 {
			delay = llmBaseRetryDelay * (1 << attempt)
			if delay > llmMaxRetryDelay {
				delay = llmMaxRetryDelay
			}
		}

		if attempt >= llmMaxRetries {
			break
		}

		logger.Warn("llm request failed, retrying",
			slog.Int("attempt", attempt+1),
			slog.Int("max_retries", llmMaxRetries),
			slog.Duration("delay", delay),
			slog.String("err", err.Error()),
		)

		if notify != nil {
			notify(fmt.Sprintf("Rate limited, retrying in %s (%d/%d)...", delay.Round(time.Second), attempt+1, llmMaxRetries))
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil, lastErr
}

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
	TurnProgress
)

type Runtime struct {
	provider         llm.Provider
	store            *store.Store
	registry         *tools.Registry
	sandbox          sandbox.Sandbox
	approver         *Approver
	model            string
	maxTokens        int
	maxOutputTokens  int
	maxToolIter      int
	systemPrompt     string
	memory           *memory.Store
	audit            *audit.Logger
	defaultPolicy    sandbox.Policy
	ctxBuilder       *ContextBuilder
	memoryMu         sync.Mutex
	memoryHashes     map[string]map[string]string
}

type RuntimeConfig struct {
	Provider         llm.Provider
	Store            *store.Store
	Registry         *tools.Registry
	Sandbox          sandbox.Sandbox
	Approver         *Approver
	Model            string
	MaxTokens        int
	MaxOutputTokens  int
	MaxToolIterations int
	SystemPrompt     string
	Memory           *memory.Store
	Audit            *audit.Logger
	DefaultPolicy    sandbox.Policy
}

func NewRuntime(cfg RuntimeConfig) *Runtime {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 128000
	}
	if cfg.MaxOutputTokens <= 0 {
		cfg.MaxOutputTokens = 4096
	}
	if cfg.MaxToolIterations <= 0 {
		cfg.MaxToolIterations = 25
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
		maxToolIter:     cfg.MaxToolIterations,
		systemPrompt:    cfg.SystemPrompt,
		memory:          cfg.Memory,
		audit:           cfg.Audit,
		defaultPolicy:   cfg.DefaultPolicy,
		ctxBuilder:      NewContextBuilder(cfg.MaxTokens),
		memoryHashes:    make(map[string]map[string]string),
	}
}

const defaultSystemPrompt = `You are Pincer, a helpful AI assistant. Be concise and accurate.
You have access to tools for executing shell commands, reading/writing files, making HTTP requests, and browsing the web.

When a task involves viewing, interacting with, or extracting information from a web page, prefer the browser tool over http_request. The browser tool renders pages like a real browser (JavaScript, screenshots, clicking, typing) while http_request only fetches raw HTML. Use http_request only for simple API calls or downloading raw content.

When a task requires multiple steps, complete them all in a single turn by chaining tool calls.
Do not stop to ask for confirmation between steps. If a tool call fails, try an alternative approach before giving up.
Briefly summarize what you accomplished at the end.`

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

	for iteration := 0; iteration < r.maxToolIter; iteration++ {

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

		llmStart := time.Now()
		events, err := r.chatWithRetry(ctx, logger, llm.ChatRequest{
			Model:     r.model,
			System:    systemPrompt,
			Messages:  chatMessages,
			MaxTokens: r.maxOutputTokens,
			Stream:    true,
			Tools:     toolDefs,
		}, func(msg string) {
			out <- TurnEvent{Type: TurnProgress, Message: msg}
		})
		if err != nil {
			telemetry.Metrics.ErrorsTotal.WithLabelValues("llm").Inc()
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
				out <- TurnEvent{Type: TurnToolCall, ToolCall: ev.ToolCall, Message: fmt.Sprintf("Calling %s...", ev.ToolCall.Name)}

			case llm.EventDone:
				usage = ev.Usage

			case llm.EventError:
				telemetry.Metrics.ErrorsTotal.WithLabelValues("llm").Inc()
				logger.Error("llm stream error", slog.String("err", ev.Error.Error()))
				out <- TurnEvent{Type: TurnError, Error: ev.Error}
				return
			}
		}

		llmElapsed := time.Since(llmStart)
		telemetry.Metrics.LLMRequestsTotal.WithLabelValues("default", r.model).Inc()
		telemetry.Metrics.LLMLatency.WithLabelValues("default", r.model).Observe(llmElapsed.Seconds())
		if usage != nil {
			telemetry.Metrics.TokensUsed.WithLabelValues("input", r.model).Add(float64(usage.InputTokens))
			telemetry.Metrics.TokensUsed.WithLabelValues("output", r.model).Add(float64(usage.OutputTokens))
		}
		logger.Info("llm turn completed",
			slog.Duration("duration", llmElapsed),
			slog.Int("tool_calls", len(toolCalls)),
			slog.Int("text_len", len(textContent)),
		)

		if len(toolCalls) == 0 {
			r.persistAssistantMessage(ctx, logger, sessionID, string(textContent), usage)
			out <- TurnEvent{Type: TurnDone, Message: string(textContent), Usage: usage}
			return
		}

		r.persistToolCallMessage(ctx, logger, sessionID, string(textContent), toolCalls, usage)

		var toolResults []llm.ToolResult
		var toolNames []string
		for _, tc := range toolCalls {
			result := r.executeTool(ctx, logger, sessionID, tc, out)
			toolResults = append(toolResults, result)
			toolNames = append(toolNames, tc.Name)
			msg := fmt.Sprintf("%s completed", tc.Name)
			if result.IsError {
				msg = fmt.Sprintf("%s failed: %s", tc.Name, truncate(result.Content, 200))
			}
			out <- TurnEvent{Type: TurnToolResult, Message: msg}
		}

		r.persistToolResultMessage(ctx, logger, sessionID, toolResults)

		out <- TurnEvent{
			Type:    TurnProgress,
			Message: fmt.Sprintf("Step %d: executed %s", iteration+1, strings.Join(toolNames, ", ")),
		}

		logger.Debug("tool iteration complete",
			slog.Int("iteration", iteration+1),
			slog.Int("tool_calls", len(toolCalls)),
		)
	}

	logger.Warn("max tool iterations reached", slog.Int("max", r.maxToolIter))
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

	start := time.Now()
	output, err := tool.Execute(ctx, tc.Input, r.sandbox, policy)
	elapsed := time.Since(start)

	telemetry.Metrics.ToolDuration.WithLabelValues(tc.Name).Observe(elapsed.Seconds())

	if err != nil {
		telemetry.Metrics.ToolExecutions.WithLabelValues(tc.Name, "error").Inc()
		telemetry.Metrics.ErrorsTotal.WithLabelValues("tool").Inc()
		logger.Error("tool execution failed",
			slog.String("tool", tc.Name),
			slog.Duration("duration", elapsed),
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

	telemetry.Metrics.ToolExecutions.WithLabelValues(tc.Name, "ok").Inc()
	logger.Info("tool execution completed",
		slog.String("tool", tc.Name),
		slog.Duration("duration", elapsed),
		slog.Int("output_len", len(output)),
	)

	r.auditLog(ctx, audit.EventToolExec, sessionID, tc.Name, "ok")

	result := llm.ToolResult{
		ToolCallID: tc.ID,
		Content:    output,
	}
	if ip, ok := tool.(tools.ImageProducer); ok {
		result.Images = ip.ConsumeImages(ctx)
	}
	return result
}

func (r *Runtime) persistMessage(ctx context.Context, logger *slog.Logger, sessionID, role, contentType, content string, usage *llm.Usage) {
	tokenCount := 0
	if usage != nil {
		tokenCount = usage.OutputTokens
	}

	msg := &store.Message{
		ID:          uuid.NewString(),
		SessionID:   sessionID,
		Role:        role,
		ContentType: contentType,
		Content:     content,
		TokenCount:  tokenCount,
		CreatedAt:   time.Now().UTC(),
	}

	if err := r.store.AppendMessage(ctx, msg); err != nil {
		logger.Error("failed to persist message",
			slog.String("content_type", contentType),
			slog.String("err", err.Error()),
		)
	}
}

func (r *Runtime) persistAssistantMessage(ctx context.Context, logger *slog.Logger, sessionID, content string, usage *llm.Usage) {
	r.persistMessage(ctx, logger, sessionID, llm.RoleAssistant, store.ContentTypeText, content, usage)

	if err := r.store.TouchSession(ctx, sessionID); err != nil {
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

	r.persistMessage(ctx, logger, sessionID, llm.RoleAssistant, store.ContentTypeToolCalls, string(data), usage)
}

func (r *Runtime) persistToolResultMessage(ctx context.Context, logger *slog.Logger, sessionID string, results []llm.ToolResult) {
	data, _ := json.Marshal(results)

	r.persistMessage(ctx, logger, sessionID, llm.RoleUser, store.ContentTypeToolResults, string(data), nil)
}

func (r *Runtime) buildContext(history []store.Message) []llm.ChatMessage {
	msgs := make([]llm.ChatMessage, 0, len(history))
	for _, m := range history {
		msgs = append(msgs, messageToLLM(m))
	}
	return sanitizeToolPairs(msgs)
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
	telemetry.Metrics.ActiveSessions.Inc()
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

func (r *Runtime) RunSubturn(ctx context.Context, prompt string, allowedTools []string) (string, error) {
	depth := tools.SubagentDepthFromContext(ctx)
	if depth >= maxSubagentDepth {
		return "", fmt.Errorf("subagent depth limit exceeded (max %d)", maxSubagentDepth)
	}

	ctx = tools.WithSubagentDepth(ctx, depth+1)
	ctx = WithAutoApprove(ctx)

	registry := r.registry
	if len(allowedTools) > 0 {
		registry = registry.Filter(allowedTools)
	}
	registry = registry.Without([]string{"subagent", "spawn"})

	sessionID := tools.SessionIDFromContext(ctx)
	agentID := tools.AgentIDFromContext(ctx)
	systemPrompt := r.buildSubagentContext(ctx, agentID, sessionID)

	logger := telemetry.FromContext(ctx)

	messages := make([]llm.ChatMessage, 0, 1)
	messages = append(messages, llm.ChatMessage{
		Role:    llm.RoleUser,
		Content: prompt,
	})

	var toolDefs []llm.ToolDefinition
	if registry != nil && r.provider.SupportsToolUse() {
		toolDefs = registry.Definitions()
	}

	for iteration := 0; iteration < r.maxToolIter; iteration++ {
		events, err := r.chatWithRetry(ctx, logger, llm.ChatRequest{
			Model:     r.model,
			System:    systemPrompt,
			Messages:  messages,
			MaxTokens: r.maxOutputTokens,
			Stream:    false,
			Tools:     toolDefs,
		}, nil)
		if err != nil {
			return "", fmt.Errorf("calling LLM: %w", err)
		}

		var textContent []byte
		var toolCalls []llm.ToolCall

		for ev := range events {
			switch ev.Type {
			case llm.EventToken:
				textContent = append(textContent, ev.Token...)
			case llm.EventToolCall:
				toolCalls = append(toolCalls, *ev.ToolCall)
			case llm.EventError:
				return "", fmt.Errorf("LLM error: %w", ev.Error)
			}
		}

		if len(toolCalls) == 0 {
			return string(textContent), nil
		}

		assistantMsg := llm.ChatMessage{
			Role:      llm.RoleAssistant,
			Content:   string(textContent),
			ToolCalls: toolCalls,
		}
		messages = append(messages, assistantMsg)

		var toolResults []llm.ToolResult
		for _, tc := range toolCalls {
			result := executeSubagentTool(ctx, logger, sessionID, tc, registry, r.sandbox, r.defaultPolicy)
			toolResults = append(toolResults, result)
		}

		messages = append(messages, llm.ChatMessage{
			Role:        llm.RoleUser,
			ToolResults: toolResults,
		})
	}

	return "(max tool iterations reached)", nil
}

func (r *Runtime) buildSubagentContext(ctx context.Context, agentID, sessionID string) string {
	systemPrompt := r.systemPrompt

	if r.memory != nil {
		memCtx, _, err := r.memory.BuildContext(ctx, agentID, make(map[string]string))
		if err == nil && memCtx != "" {
			systemPrompt += "\n\n" + memCtx
		}
	}

	return systemPrompt
}

func executeSubagentTool(ctx context.Context, logger *slog.Logger, sessionID string, tc llm.ToolCall, registry *tools.Registry, sb sandbox.Sandbox, defaultPolicy sandbox.Policy) llm.ToolResult {
	if registry == nil {
		return llm.ToolResult{
			ToolCallID: tc.ID,
			Content:    "no tools available",
			IsError:    true,
		}
	}

	tool, err := registry.Get(tc.Name)
	if err != nil {
		return llm.ToolResult{
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("unknown tool: %s", tc.Name),
			IsError:    true,
		}
	}

	policy := defaultPolicy
	if policy.Timeout == 0 {
		policy = sandbox.DefaultPolicy()
	}
	policy.RequireApproval = false

	start := time.Now()
	output, err := tool.Execute(ctx, tc.Input, sb, policy)
	elapsed := time.Since(start)

	telemetry.Metrics.ToolDuration.WithLabelValues(tc.Name).Observe(elapsed.Seconds())

	if err != nil {
		telemetry.Metrics.ToolExecutions.WithLabelValues(tc.Name, "error").Inc()
		telemetry.Metrics.ErrorsTotal.WithLabelValues("tool").Inc()
		logger.Error("subagent tool execution failed",
			slog.String("tool", tc.Name),
			slog.Duration("duration", elapsed),
			slog.String("err", err.Error()),
		)
		return llm.ToolResult{
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("error: %v", err),
			IsError:    true,
		}
	}

	telemetry.Metrics.ToolExecutions.WithLabelValues(tc.Name, "ok").Inc()
	logger.Info("subagent tool execution completed",
		slog.String("tool", tc.Name),
		slog.Duration("duration", elapsed),
		slog.Int("output_len", len(output)),
	)

	result := llm.ToolResult{
		ToolCallID: tc.ID,
		Content:    output,
	}
	if ip, ok := tool.(tools.ImageProducer); ok {
		result.Images = ip.ConsumeImages(ctx)
	}
	return result
}

func ContentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
