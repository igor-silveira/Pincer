package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/igorsilveira/pincer/pkg/agent"
	"github.com/igorsilveira/pincer/pkg/audit"
)

type Handler struct {
	router    chi.Router
	card      *AgentCard
	store     *TaskStore
	runtime   *agent.Runtime
	auditLog  *audit.Logger
	logger    *slog.Logger
	authToken string
}

type HandlerConfig struct {
	Card      *AgentCard
	Runtime   *agent.Runtime
	AuditLog  *audit.Logger
	Logger    *slog.Logger
	AuthToken string
}

func NewHandler(cfg HandlerConfig) *Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	h := &Handler{
		card:      cfg.Card,
		store:     NewTaskStore(),
		runtime:   cfg.Runtime,
		auditLog:  cfg.AuditLog,
		logger:    cfg.Logger,
		authToken: cfg.AuthToken,
	}
	h.buildRouter()
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (h *Handler) buildRouter() {
	r := chi.NewRouter()
	r.Get("/.well-known/agentcard", h.handleAgentCard)

	r.Group(func(r chi.Router) {
		if h.authToken != "" {
			r.Use(h.authMiddleware)
		}
		r.Post("/a2a", h.handleJSONRPC)
		r.Post("/a2a/messages", h.handleSendMessage)
		r.Post("/a2a/messages:stream", h.handleSendMessageStream)
		r.Get("/a2a/tasks/{id}", h.handleGetTask)
		r.Get("/a2a/tasks", h.handleListTasks)
		r.Post("/a2a/tasks/{id}:cancel", h.handleCancelTask)
	})
	h.router = r
}

func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agentcard" {
			next.ServeHTTP(w, r)
			return
		}
		header := r.Header.Get("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		if token == "" || token == header || token != h.authToken {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.card)
}

func (h *Handler) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, NewJSONRPCError(nil, ErrCodeParse, "parse error"))
		return
	}
	if req.JSONRPC != "2.0" {
		writeJSON(w, http.StatusOK, NewJSONRPCError(req.ID, ErrCodeInvalidReq, "invalid jsonrpc version"))
		return
	}

	switch req.Method {
	case "tasks/send":
		h.rpcSendMessage(w, r, req)
	case "tasks/get":
		h.rpcGetTask(w, req)
	case "tasks/cancel":
		h.rpcCancelTask(w, r, req)
	default:
		writeJSON(w, http.StatusOK, NewJSONRPCError(req.ID, ErrCodeNotFound, fmt.Sprintf("method %q not found", req.Method)))
	}
}

func (h *Handler) rpcSendMessage(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	var params struct {
		TaskID  string  `json:"id"`
		Message Message `json:"message"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeJSON(w, http.StatusOK, NewJSONRPCError(req.ID, ErrCodeParse, "invalid params"))
		return
	}

	task, response, err := h.processMessage(r.Context(), params.TaskID, params.Message)
	if err != nil {
		writeJSON(w, http.StatusOK, NewJSONRPCError(req.ID, ErrCodeInternal, err.Error()))
		return
	}
	_ = response
	writeJSON(w, http.StatusOK, NewJSONRPCResponse(req.ID, task))
}

func (h *Handler) rpcGetTask(w http.ResponseWriter, req JSONRPCRequest) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeJSON(w, http.StatusOK, NewJSONRPCError(req.ID, ErrCodeParse, "invalid params"))
		return
	}

	task, err := h.store.Get(params.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, NewJSONRPCError(req.ID, ErrCodeTaskNotFound, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, NewJSONRPCResponse(req.ID, task))
}

func (h *Handler) rpcCancelTask(w http.ResponseWriter, r *http.Request, req JSONRPCRequest) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeJSON(w, http.StatusOK, NewJSONRPCError(req.ID, ErrCodeParse, "invalid params"))
		return
	}

	task, err := h.store.Get(params.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, NewJSONRPCError(req.ID, ErrCodeTaskNotFound, err.Error()))
		return
	}
	if err := h.store.Update(params.ID, TaskStateCanceled); err != nil {
		writeJSON(w, http.StatusOK, NewJSONRPCError(req.ID, ErrCodeInternal, err.Error()))
		return
	}
	h.auditLogEvent(r.Context(), audit.EventA2ATaskCancel, task.ID)
	task.State = TaskStateCanceled
	writeJSON(w, http.StatusOK, NewJSONRPCResponse(req.ID, task))
}

func (h *Handler) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var msg Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	taskID := r.URL.Query().Get("taskId")
	task, response, err := h.processMessage(r.Context(), taskID, msg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	_ = h.store.AppendMessage(task.ID, Message{
		Role:  "assistant",
		Parts: []Part{{Type: "text", Text: response}},
	})

	task, _ = h.store.Get(task.ID)
	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) handleSendMessageStream(w http.ResponseWriter, r *http.Request) {
	var msg Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	taskID := r.URL.Query().Get("taskId")

	text := extractText(msg)
	if text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty message"})
		return
	}

	task := h.getOrCreateTask(r.Context(), taskID, msg)
	_ = h.store.Update(task.ID, TaskStateWorking)

	events, err := h.runtime.RunTurn(r.Context(), task.ID, text)
	if err != nil {
		_ = h.store.Update(task.ID, TaskStateFailed)
		h.auditLogEvent(r.Context(), audit.EventA2ATaskFail, task.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	flusher, canFlush := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	writeSSE(w, flusher, canFlush, "status", TaskStatus{ID: task.ID, State: TaskStateWorking})

	var fullResponse string
	for ev := range events {
		switch ev.Type {
		case agent.TurnToken:
			writeSSE(w, flusher, canFlush, "token", map[string]string{"text": ev.Token})
		case agent.TurnDone:
			fullResponse = ev.Message
		case agent.TurnError:
			_ = h.store.Update(task.ID, TaskStateFailed)
			h.auditLogEvent(r.Context(), audit.EventA2ATaskFail, task.ID)
			writeSSE(w, flusher, canFlush, "status", TaskStatus{ID: task.ID, State: TaskStateFailed})
			return
		}
	}

	_ = h.store.Update(task.ID, TaskStateCompleted)
	_ = h.store.AppendMessage(task.ID, Message{
		Role:  "assistant",
		Parts: []Part{{Type: "text", Text: fullResponse}},
	})
	h.auditLogEvent(r.Context(), audit.EventA2ATaskDone, task.ID)
	writeSSE(w, flusher, canFlush, "status", TaskStatus{ID: task.ID, State: TaskStateCompleted})
}

func (h *Handler) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := h.store.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := h.store.List()
	writeJSON(w, http.StatusOK, tasks)
}

func (h *Handler) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := h.store.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err := h.store.Update(id, TaskStateCanceled); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.auditLogEvent(r.Context(), audit.EventA2ATaskCancel, task.ID)
	task.State = TaskStateCanceled
	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) processMessage(ctx context.Context, taskID string, msg Message) (*Task, string, error) {
	text := extractText(msg)
	if text == "" {
		return nil, "", fmt.Errorf("empty message")
	}

	task := h.getOrCreateTask(ctx, taskID, msg)
	_ = h.store.Update(task.ID, TaskStateWorking)

	events, err := h.runtime.RunTurn(ctx, task.ID, text)
	if err != nil {
		_ = h.store.Update(task.ID, TaskStateFailed)
		h.auditLogEvent(ctx, audit.EventA2ATaskFail, task.ID)
		return task, "", fmt.Errorf("agent turn: %w", err)
	}

	var fullResponse string
	for ev := range events {
		switch ev.Type {
		case agent.TurnDone:
			fullResponse = ev.Message
		case agent.TurnError:
			_ = h.store.Update(task.ID, TaskStateFailed)
			h.auditLogEvent(ctx, audit.EventA2ATaskFail, task.ID)
			return task, "", fmt.Errorf("agent error: %w", ev.Error)
		}
	}

	_ = h.store.Update(task.ID, TaskStateCompleted)
	h.auditLogEvent(ctx, audit.EventA2ATaskDone, task.ID)
	return task, fullResponse, nil
}

func (h *Handler) getOrCreateTask(ctx context.Context, taskID string, msg Message) *Task {
	if taskID != "" {
		if task, err := h.store.Get(taskID); err == nil {
			_ = h.store.AppendMessage(taskID, msg)
			return task
		}
	}

	id := taskID
	if id == "" {
		id = uuid.NewString()
	}
	task := &Task{
		ID:       id,
		State:    TaskStateSubmitted,
		Messages: []Message{msg},
	}
	h.store.Create(task)
	h.auditLogEvent(ctx, audit.EventA2ATaskNew, id)
	return task
}

func (h *Handler) auditLogEvent(ctx context.Context, eventType, taskID string) {
	if h.auditLog == nil {
		return
	}
	_ = h.auditLog.Log(ctx, eventType, taskID, "", "a2a", fmt.Sprintf("task_id=%s", taskID))
}

func extractText(msg Message) string {
	var parts []string
	for _, p := range msg.Parts {
		if p.Type == "text" && p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, canFlush bool, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	if canFlush {
		flusher.Flush()
	}
}
