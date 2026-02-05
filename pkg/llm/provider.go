package llm

import "context"

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

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	System      string        `json:"system,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	Stream      bool          `json:"stream"`
}

type ChatEvent struct {
	Type  EventType
	Token string
	Error error
	Usage *Usage
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
}
