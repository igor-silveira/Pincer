package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

type EventType int

const (
	EventToken EventType = iota
	EventToolCall
	EventDone
	EventError
)

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type ImageContent struct {
	MediaType string `json:"media_type"`
	Path      string `json:"path,omitempty"`
	data      []byte
}

func (ic *ImageContent) Data() []byte     { return ic.data }
func (ic *ImageContent) SetData(b []byte) { ic.data = b }

type ToolErrorKind int

const (
	ToolErrorPermanent ToolErrorKind = iota
	ToolErrorTransient
)

type ToolResult struct {
	ToolCallID string         `json:"tool_call_id"`
	Content    string         `json:"content"`
	IsError    bool           `json:"is_error,omitempty"`
	Images     []ImageContent `json:"images,omitempty"`
	errorKind  ToolErrorKind
}

func (tr *ToolResult) ErrorKind() ToolErrorKind     { return tr.errorKind }
func (tr *ToolResult) SetErrorKind(k ToolErrorKind) { tr.errorKind = k }

type ChatMessage struct {
	Role        string       `json:"role"`
	Content     string       `json:"content,omitempty"`
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
	ToolResults []ToolResult `json:"tool_results,omitempty"`
}

type ToolChoiceType string

const (
	ToolChoiceAuto ToolChoiceType = "auto"
	ToolChoiceAny  ToolChoiceType = "any"
	ToolChoiceNone ToolChoiceType = "none"
	ToolChoiceTool ToolChoiceType = "tool"
)

type ToolChoice struct {
	Type                   ToolChoiceType `json:"type"`
	Name                   string         `json:"name,omitempty"`
	DisableParallelToolUse *bool          `json:"disable_parallel_tool_use,omitempty"`
}

type ChatRequest struct {
	Model       string           `json:"model"`
	Messages    []ChatMessage    `json:"messages"`
	System      string           `json:"system,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	Stream      bool             `json:"stream"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	ToolChoice  *ToolChoice      `json:"tool_choice,omitempty"`
}

type ChatEvent struct {
	Type     EventType
	Token    string
	ToolCall *ToolCall
	Error    error
	Usage    *Usage
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type ModelInfo struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	MaxContextTokens int    `json:"max_context_tokens"`
}

type Provider interface {
	Name() string

	Chat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)

	Models() []ModelInfo

	SupportsStreaming() bool

	SupportsToolUse() bool
}

type APIError struct {
	Provider   string
	StatusCode int
	Body       string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: API returned %d: %s", e.Provider, e.StatusCode, e.Body)
}

func (e *APIError) IsRetryable() bool {
	switch e.StatusCode {
	case 429, 503, 529:
		return true
	default:
		return false
	}
}

func IsRetryable(err error) (time.Duration, bool) {
	if apiErr, ok := errors.AsType[*APIError](err); ok && apiErr.IsRetryable() {
		return apiErr.RetryAfter, true
	}
	return 0, false
}
