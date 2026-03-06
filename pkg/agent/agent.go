package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/igorsilveira/pincer/pkg/agent/executor"
	"github.com/igorsilveira/pincer/pkg/agent/tools"
	"github.com/igorsilveira/pincer/pkg/audit"
	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/memory"
	"github.com/igorsilveira/pincer/pkg/sandbox"
	"github.com/igorsilveira/pincer/pkg/store"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)


func (r *Runtime) chatWithRetry(ctx context.Context, logger *slog.Logger, req llm.ChatRequest, notify func(string)) (<-chan llm.ChatEvent, error) {
	var lastErr error
	for attempt := 0; attempt <= config.LLMMaxRetries; attempt++ {
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
			delay = config.LLMBaseRetryDelay * (1 << attempt)
			if delay > config.LLMMaxRetryDelay {
				delay = config.LLMMaxRetryDelay
			}
		}

		if attempt >= config.LLMMaxRetries {
			break
		}

		logger.Warn("llm request failed, retrying",
			slog.Int("attempt", attempt+1),
			slog.Int("max_retries", config.LLMMaxRetries),
			slog.Duration("delay", delay),
			slog.String("err", err.Error()),
		)

		if notify != nil {
			notify(fmt.Sprintf("Rate limited, retrying in %s (%d/%d)...", delay.Round(time.Second), attempt+1, config.LLMMaxRetries))
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
	TurnToolStart
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
	sessionMu        sync.Mutex
	sessionLocks     map[string]*sync.Mutex
	executor         *executor.Executor
	recovery         executor.RecoveryStrategy
	toolTimeout      time.Duration
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
	Executor         *executor.Executor
	Recovery         executor.RecoveryStrategy
	ToolTimeout      time.Duration
}

func NewRuntime(cfg RuntimeConfig) *Runtime {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = config.DefaultMaxContextTokens
	}
	if cfg.MaxOutputTokens <= 0 {
		cfg.MaxOutputTokens = config.DefaultMaxOutputTokens
	}
	if cfg.MaxToolIterations <= 0 {
		cfg.MaxToolIterations = config.DefaultMaxToolIterations
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = defaultSystemPrompt
	}
	exec := cfg.Executor
	if exec == nil {
		exec = executor.New(config.DefaultToolConcurrency)
	}
	recov := cfg.Recovery
	if recov == nil {
		recov = &executor.DefaultRecovery{
			MaxRetries: config.DefaultRecoveryRetries,
			BaseDelay:  config.RecoveryBaseDelay,
			MaxDelay:   config.RecoveryMaxDelay,
		}
	}
	toolTimeout := cfg.ToolTimeout
	if toolTimeout == 0 {
		toolTimeout = config.DefaultToolTimeout
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
		ctxBuilder:      NewContextBuilder(cfg.MaxTokens, cfg.MaxOutputTokens),
		memoryHashes:    make(map[string]map[string]string),
		sessionLocks:    make(map[string]*sync.Mutex),
		executor:        exec,
		recovery:        recov,
		toolTimeout:     toolTimeout,
	}
}

const defaultSystemPrompt = `You are a helpful AI assistant.

# Operational Guidelines

## Task Execution
- Complete multi-step tasks in a single turn by chaining tool calls. Do not stop to ask for confirmation between steps.
- If a tool call fails, try an alternative approach before giving up.

## Tool Selection
- For web pages: prefer browser over http_request. The browser renders JavaScript, captures screenshots, and supports interaction. Use http_request only for API calls or raw content downloads.
- For persistent information: use memory to store facts, user preferences, and project context that should survive across sessions.
- For secrets and API keys: use credential to store and retrieve encrypted secrets. Never store secrets in memory.
- For complex tasks: use subagent to delegate focused subtasks synchronously. Use spawn for independent background tasks.

## Error Recovery
- When you see [System: ...] messages, the system encountered an error on your behalf. Reduce complexity: use fewer parallel tool calls, produce shorter responses, or break the task into smaller steps. Do not repeat the exact same approach.
- If a tool times out, retry with a simpler approach or different parameters.

## Context Awareness
- Long conversations are automatically summarized. Key information may be in a [Session Summary] at the start of your history.
- Store important facts in memory early to avoid losing them during summarization.`

func (r *Runtime) sessionLock(sessionID string) *sync.Mutex {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	mu, ok := r.sessionLocks[sessionID]
	if !ok {
		mu = &sync.Mutex{}
		r.sessionLocks[sessionID] = mu
	}
	return mu
}

func (r *Runtime) RunTurn(ctx context.Context, sessionID, userMessage string) (<-chan TurnEvent, error) {
	logger := telemetry.FromContext(ctx)

	session, err := r.getOrCreateSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("resolving session: %w", err)
	}

	mu := r.sessionLock(session.ID)
	mu.Lock()

	userMsg := &store.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      llm.RoleUser,
		Content:   userMessage,
		CreatedAt: time.Now().UTC(),
	}
	if err := r.store.AppendMessage(ctx, userMsg); err != nil {
		mu.Unlock()
		return nil, fmt.Errorf("persisting user message: %w", err)
	}

	logger.Debug("running agent turn",
		slog.String("session_id", session.ID),
	)

	out := make(chan TurnEvent, config.TurnEventBufferSize)
	go func() {
		defer mu.Unlock()
		r.runAgenticLoop(ctx, session.ID, out)
	}()

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

	var toolDefs []llm.ToolDefinition
	if r.registry != nil && r.provider.SupportsToolUse() {
		toolDefs = r.registry.Definitions()
	}

	var llmErrors int
	var ephemeralContext string
	for iteration := 0; iteration < r.maxToolIter; iteration++ {

		history, err := r.store.RecentMessages(ctx, sessionID, config.RecentMessagesLimit)
		if err != nil {
			out <- TurnEvent{Type: TurnError, Error: fmt.Errorf("loading history: %w", err)}
			return
		}

		systemPrompt, chatMessages := r.buildSmartContext(ctx, sess.AgentID, sessionID, history)

		if ephemeralContext != "" {
			systemPrompt += "\n\n# Error Recovery Context\n" + ephemeralContext
			ephemeralContext = ""
		}

		if len(chatMessages) == 0 {
			out <- TurnEvent{Type: TurnError, Error: fmt.Errorf("no messages after context building")}
			return
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
			llmErrors++
			if llmErrors > config.LLMMaxErrors {
				out <- TurnEvent{Type: TurnError, Error: fmt.Errorf("calling LLM: %w", err)}
				return
			}
			logger.Warn("llm call failed, will retry with ephemeral error context",
				slog.Int("llm_errors", llmErrors),
				slog.String("err", err.Error()),
			)
			ephemeralContext = fmt.Sprintf("Previous LLM call failed: %s. To recover: (1) reduce parallel tool calls, (2) produce a shorter response, (3) break the task into a smaller step. Do not repeat the exact same approach.", truncate(err.Error(), config.ErrorTruncateLen))
			out <- TurnEvent{Type: TurnProgress, Message: fmt.Sprintf("LLM error, retrying (%d/%d)...", llmErrors, config.LLMMaxErrors)}
			continue
		}

		var textContent []byte
		var toolCalls []llm.ToolCall
		var usage *llm.Usage
		var streamErr error

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
				streamErr = ev.Error
			}
		}

		if streamErr != nil {
			telemetry.Metrics.ErrorsTotal.WithLabelValues("llm").Inc()
			llmErrors++
			if llmErrors > config.LLMMaxErrors {
				logger.Error("llm stream error, max retries exceeded", slog.String("err", streamErr.Error()))
				out <- TurnEvent{Type: TurnError, Error: streamErr}
				return
			}
			logger.Warn("llm stream error, will retry with ephemeral error context",
				slog.Int("llm_errors", llmErrors),
				slog.String("err", streamErr.Error()),
			)
			ephemeralContext = fmt.Sprintf("Previous response stream was interrupted: %s. Your output was lost. Retry with a shorter response or fewer tool calls. If this recurs, complete the task in smaller increments.", truncate(streamErr.Error(), config.ErrorTruncateLen))
			out <- TurnEvent{Type: TurnProgress, Message: "Stream interrupted, retrying..."}
			continue
		}

		llmErrors = 0

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
			if err := r.persistAssistantMessage(ctx, sessionID, string(textContent), usage); err != nil {
				out <- TurnEvent{Type: TurnError, Error: fmt.Errorf("persisting assistant message: %w", err)}
				return
			}
			out <- TurnEvent{Type: TurnDone, Message: string(textContent), Usage: usage}
			return
		}

		if err := r.persistToolCallMessage(ctx, sessionID, string(textContent), toolCalls, usage); err != nil {
			out <- TurnEvent{Type: TurnError, Error: fmt.Errorf("persisting tool calls: %w", err)}
			return
		}

		toolResults := make([]llm.ToolResult, len(toolCalls))
		toolNames := make([]string, len(toolCalls))
		tasks := make([]executor.Task, len(toolCalls))
		for i, tc := range toolCalls {
			toolNames[i] = tc.Name
			tc := tc
			idx := i
			tasks[i] = executor.Task{
				ID: tc.ID,
				Fn: func(ctx context.Context) (string, error) {
					result := r.executeTool(ctx, logger, sessionID, tc, out)
					toolResults[idx] = result
					if result.IsError {
						if result.ErrorKind() == llm.ToolErrorPermanent {
							return result.Content, &executor.PermanentError{Msg: result.Content}
						}
						return result.Content, fmt.Errorf("%s", result.Content)
					}
					return result.Content, nil
				},
				Timeout: r.toolTimeout,
				OnStart: func() {
					out <- TurnEvent{Type: TurnToolStart, Message: fmt.Sprintf("Running %s...", tc.Name), ToolCall: &tc}
				},
				OnDone: func(res executor.Result) {
					msg := fmt.Sprintf("%s completed", tc.Name)
					if res.Err != nil {
						msg = fmt.Sprintf("%s failed: %s", tc.Name, truncate(res.Err.Error(), config.ToolResultTruncateLen))
					}
					out <- TurnEvent{Type: TurnToolResult, Message: msg}
				},
			}
		}

		batch := r.executor.RunBatch(ctx, tasks)

		var replanSummary executor.ErrorSummary
		for i, res := range batch.Results {
			if res.Err == nil {
				continue
			}
			attempts := 0
			for {
				action := r.recovery.Decide(res, attempts)
				if action == executor.ActionRetry {
					delay := r.recovery.Backoff(attempts)
					logger.Info("retrying tool with backoff",
						slog.String("tool", toolCalls[i].Name),
						slog.Duration("delay", delay),
						slog.Int("attempt", attempts+1),
					)
					out <- TurnEvent{Type: TurnToolStart, Message: fmt.Sprintf("Retrying %s...", toolCalls[i].Name), ToolCall: &toolCalls[i]}
					select {
					case <-ctx.Done():
						res = executor.Result{ID: res.ID, Err: ctx.Err()}
					case <-time.After(delay):
						retryBatch := r.executor.RunBatch(ctx, []executor.Task{tasks[i]})
						res = retryBatch.Results[0]
					}
					attempts++
					if res.Err == nil {
						break
					}
					continue
				}
				if action == executor.ActionReplan {
					replanSummary.FailedTools = append(replanSummary.FailedTools, executor.FailedToolInfo{
						Name:    toolCalls[i].Name,
						Error:   truncate(res.Err.Error(), config.ErrorTruncateLen),
						Retries: attempts,
					})
				}
				break
			}
		}

		if len(replanSummary.FailedTools) > 0 {
			ephemeralContext = replanSummary.String()
		}

		if err := r.persistToolResultMessage(ctx, sessionID, toolResults); err != nil {
			out <- TurnEvent{Type: TurnError, Error: fmt.Errorf("persisting tool results: %w", err)}
			return
		}

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

	policy := r.defaultPolicy
	if policy.Timeout == 0 {
		policy = sandbox.DefaultPolicy()
	}
	if (r.approver != nil && r.approver.mode == ApprovalAuto) || autoApproveFromContext(ctx) {
		policy.RequireApproval = false
	}

	result := runTool(ctx, logger, tc, r.registry, r.sandbox, policy)

	if result.IsError {
		r.auditLog(ctx, audit.EventToolExec, sessionID, tc.Name, result.Content)
	} else {
		r.auditLog(ctx, audit.EventToolExec, sessionID, tc.Name, "ok")
	}

	return result
}

func (r *Runtime) persistMessage(ctx context.Context, sessionID, role, contentType, content string, usage *llm.Usage) error {
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

	return r.store.AppendMessage(ctx, msg)
}

func (r *Runtime) persistAssistantMessage(ctx context.Context, sessionID, content string, usage *llm.Usage) error {
	if err := r.persistMessage(ctx, sessionID, llm.RoleAssistant, store.ContentTypeText, content, usage); err != nil {
		return err
	}
	return r.store.TouchSession(ctx, sessionID)
}

func (r *Runtime) persistToolCallMessage(ctx context.Context, sessionID, textContent string, toolCalls []llm.ToolCall, usage *llm.Usage) error {
	data, err := json.Marshal(struct {
		Text      string         `json:"text,omitempty"`
		ToolCalls []llm.ToolCall `json:"tool_calls"`
	}{
		Text:      textContent,
		ToolCalls: toolCalls,
	})
	if err != nil {
		return fmt.Errorf("marshaling tool calls: %w", err)
	}

	return r.persistMessage(ctx, sessionID, llm.RoleAssistant, store.ContentTypeToolCalls, string(data), usage)
}

func (r *Runtime) persistToolResultMessage(ctx context.Context, sessionID string, results []llm.ToolResult) error {
	data, err := json.Marshal(results)
	if err != nil {
		return fmt.Errorf("marshaling tool results: %w", err)
	}

	return r.persistMessage(ctx, sessionID, llm.RoleUser, store.ContentTypeToolResults, string(data), nil)
}

func (r *Runtime) buildContext(history []store.Message) []llm.ChatMessage {
	msgs := make([]llm.ChatMessage, 0, len(history))
	for _, m := range history {
		msgs = append(msgs, messageToLLM(m))
	}
	return sanitizeToolPairs(msgs)
}

func (r *Runtime) getOrCreateSession(ctx context.Context, sessionID string) (*store.Session, error) {
	sess, created, err := r.store.GetOrCreateSession(ctx, sessionID, "webchat", "anonymous")
	if err != nil {
		return nil, err
	}
	if created {
		telemetry.Metrics.ActiveSessions.Inc()
		r.auditLog(ctx, audit.EventSessionNew, sessionID, "system",
			fmt.Sprintf("channel=%s peer=%s", sess.Channel, sess.PeerID))
	}
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

		memCtx, newHashes, err := r.memory.BuildContext(ctx, agentID, lastHashes)
		if err == nil {
			r.memoryHashes[sessionID] = newHashes
			if memCtx != "" {
				wsFiles = append(wsFiles, WorkspaceFile{Key: "memory", Content: memCtx})
			}
		}
		r.memoryMu.Unlock()
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
	if depth >= config.MaxSubagentDepth {
		return "", fmt.Errorf("subagent depth limit exceeded (max %d)", config.MaxSubagentDepth)
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

	var llmErrors int
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
			llmErrors++
			if llmErrors > config.LLMMaxErrors {
				return "", fmt.Errorf("calling LLM: %w", err)
			}
			logger.Warn("subagent llm call failed, injecting error context",
				slog.Int("llm_errors", llmErrors),
				slog.String("err", err.Error()),
			)
			messages = append(messages, llm.ChatMessage{
				Role:    llm.RoleUser,
				Content: fmt.Sprintf("[System: LLM call failed: %s. Reduce response length or tool call count and try a different approach.]", truncate(err.Error(), config.ErrorTruncateLen)),
			})
			continue
		}

		var textContent []byte
		var toolCalls []llm.ToolCall
		var streamErr error

		for ev := range events {
			switch ev.Type {
			case llm.EventToken:
				textContent = append(textContent, ev.Token...)
			case llm.EventToolCall:
				toolCalls = append(toolCalls, *ev.ToolCall)
			case llm.EventError:
				streamErr = ev.Error
			}
		}

		if streamErr != nil {
			llmErrors++
			if llmErrors > config.LLMMaxErrors {
				return "", fmt.Errorf("LLM error: %w", streamErr)
			}
			logger.Warn("subagent stream error, injecting error context",
				slog.Int("llm_errors", llmErrors),
				slog.String("err", streamErr.Error()),
			)
			messages = append(messages, llm.ChatMessage{
				Role:    llm.RoleUser,
				Content: fmt.Sprintf("[System: Stream interrupted: %s. Previous output was lost. Retry with a shorter response or break into smaller steps.]", truncate(streamErr.Error(), config.ErrorTruncateLen)),
			})
			continue
		}

		llmErrors = 0

		if len(toolCalls) == 0 {
			return string(textContent), nil
		}

		assistantMsg := llm.ChatMessage{
			Role:      llm.RoleAssistant,
			Content:   string(textContent),
			ToolCalls: toolCalls,
		}
		messages = append(messages, assistantMsg)

		toolResults := make([]llm.ToolResult, len(toolCalls))
		subTasks := make([]executor.Task, len(toolCalls))
		for i, tc := range toolCalls {
			tc := tc
			idx := i
			subTasks[i] = executor.Task{
				ID: tc.ID,
				Fn: func(ctx context.Context) (string, error) {
					policy := r.defaultPolicy
					if policy.Timeout == 0 {
						policy = sandbox.DefaultPolicy()
					}
					policy.RequireApproval = false
					result := runTool(ctx, logger, tc, registry, r.sandbox, policy)
					toolResults[idx] = result
					if result.IsError {
						if result.ErrorKind() == llm.ToolErrorPermanent {
							return result.Content, &executor.PermanentError{Msg: result.Content}
						}
						return result.Content, fmt.Errorf("%s", result.Content)
					}
					return result.Content, nil
				},
				Timeout: r.toolTimeout,
			}
		}
		r.executor.RunBatch(ctx, subTasks)

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

func runTool(ctx context.Context, logger *slog.Logger, tc llm.ToolCall, registry *tools.Registry, sb sandbox.Sandbox, policy sandbox.Policy) llm.ToolResult {
	if registry == nil {
		result := llm.ToolResult{
			ToolCallID: tc.ID,
			Content:    "no tools available",
			IsError:    true,
		}
		result.SetErrorKind(llm.ToolErrorPermanent)
		return result
	}

	tool, err := registry.Get(tc.Name)
	if err != nil {
		result := llm.ToolResult{
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("unknown tool: %s", tc.Name),
			IsError:    true,
		}
		result.SetErrorKind(llm.ToolErrorPermanent)
		return result
	}

	start := time.Now()
	output, err := tool.Execute(ctx, tc.Input, sb, policy)
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
		result := llm.ToolResult{
			ToolCallID: tc.ID,
			Content:    fmt.Sprintf("error: %v", err),
			IsError:    true,
		}
		kind := executor.ClassifyError(err)
		if kind == executor.Transient {
			result.SetErrorKind(llm.ToolErrorTransient)
		} else {
			result.SetErrorKind(llm.ToolErrorPermanent)
		}
		return result
	}

	telemetry.Metrics.ToolExecutions.WithLabelValues(tc.Name, "ok").Inc()
	logger.Info("tool execution completed",
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
