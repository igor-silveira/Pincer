package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const openaiAPIURL = "https://api.openai.com/v1/chat/completions"

type OpenAIProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewOpenAIProvider(apiKey, baseURL string) (*OpenAIProvider, error) {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("openai: API key not set (provide it or set OPENAI_API_KEY)")
	}
	if baseURL == "" {
		baseURL = openaiAPIURL
	}
	return &OpenAIProvider{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}, nil
}

func (o *OpenAIProvider) Name() string            { return "openai" }
func (o *OpenAIProvider) SupportsStreaming() bool { return true }
func (o *OpenAIProvider) SupportsToolUse() bool   { return true }

func (o *OpenAIProvider) Models() []ModelInfo {
	return []ModelInfo{
		{ID: "gpt-4o", Name: "GPT-4o", MaxContextTokens: 128000},
		{ID: "gpt-4o-mini", Name: "GPT-4o Mini", MaxContextTokens: 128000},
		{ID: "o3-mini", Name: "o3-mini", MaxContextTokens: 200000},
	}
}

type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	Tools       []openaiTool    `json:"tools,omitempty"`
	Stream      bool            `json:"stream"`
}

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openaiToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openaiToolCallFunc `json:"function"`
}

type openaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (o *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	model := req.Model
	if model == "" {
		model = "gpt-4o"
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	apiReq := openaiRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		Stream:      req.Stream,
	}

	for _, t := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	for _, m := range req.Messages {
		apiReq.Messages = append(apiReq.Messages, convertToOpenAIMessages(m)...)
	}

	if req.System != "" {
		apiReq.Messages = append(
			[]openaiMessage{{Role: "system", Content: req.System}},
			apiReq.Messages...,
		)
	}

	resp, err := doLLMRequest(ctx, o.httpClient, "openai", o.baseURL, map[string]string{
		"Authorization": "Bearer " + o.apiKey,
	}, apiReq)
	if err != nil {
		return nil, err
	}

	return dispatchResponse(resp, req.Stream,
		func(body io.ReadCloser, ch chan<- ChatEvent) { o.readStream(ctx, body, ch) },
		func(body io.ReadCloser, ch chan<- ChatEvent) { o.readFull(body, ch) },
	), nil
}

func convertToOpenAIMessages(m ChatMessage) []openaiMessage {

	if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
		var calls []openaiToolCall
		for _, tc := range m.ToolCalls {
			calls = append(calls, openaiToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: openaiToolCallFunc{
					Name:      tc.Name,
					Arguments: string(tc.Input),
				},
			})
		}
		return []openaiMessage{{
			Role:      "assistant",
			Content:   m.Content,
			ToolCalls: calls,
		}}
	}

	if m.Role == RoleUser && len(m.ToolResults) > 0 {
		var msgs []openaiMessage
		for _, tr := range m.ToolResults {
			msgs = append(msgs, openaiMessage{
				Role:       "tool",
				Content:    tr.Content,
				ToolCallID: tr.ToolCallID,
			})
		}
		return msgs
	}

	return []openaiMessage{{Role: m.Role, Content: m.Content}}
}

type openaiStreamChunk struct {
	Choices []openaiStreamChoice `json:"choices"`
	Usage   *openaiUsage         `json:"usage,omitempty"`
}

type openaiStreamChoice struct {
	Delta        openaiStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type openaiStreamDelta struct {
	Content   string           `json:"content,omitempty"`
	ToolCalls []openaiStreamTC `json:"tool_calls,omitempty"`
}

type openaiStreamTC struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openaiStreamTCFunc `json:"function,omitempty"`
}

type openaiStreamTCFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (o *OpenAIProvider) readStream(ctx context.Context, body io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	type tcState struct {
		id   string
		name string
		args strings.Builder
	}
	toolStates := make(map[int]*tcState)
	var usage Usage

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- ChatEvent{Type: EventError, Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Usage != nil {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				ch <- ChatEvent{Type: EventToken, Token: choice.Delta.Content}
			}

			for _, tc := range choice.Delta.ToolCalls {
				state, ok := toolStates[tc.Index]
				if !ok {
					state = &tcState{id: tc.ID, name: tc.Function.Name}
					toolStates[tc.Index] = state
				}
				if tc.Function.Arguments != "" {
					state.args.WriteString(tc.Function.Arguments)
				}
			}

			if choice.FinishReason != nil {

				for _, state := range toolStates {
					args := json.RawMessage(state.args.String())
					if len(args) == 0 {
						args = json.RawMessage("{}")
					}
					ch <- ChatEvent{
						Type: EventToolCall,
						ToolCall: &ToolCall{
							ID:    state.id,
							Name:  state.name,
							Input: args,
						},
					}
				}
			}
		}
	}

	ch <- ChatEvent{Type: EventDone, Usage: &usage}
}

type openaiFullResponse struct {
	Choices []openaiFullChoice `json:"choices"`
	Usage   openaiUsage        `json:"usage"`
}

type openaiFullChoice struct {
	Message openaiFullMessage `json:"message"`
}

type openaiFullMessage struct {
	Content   string           `json:"content"`
	ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
}

func (o *OpenAIProvider) readFull(body io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer body.Close()

	var resp openaiFullResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		ch <- ChatEvent{Type: EventError, Error: fmt.Errorf("openai: decoding response: %w", err)}
		return
	}

	for _, choice := range resp.Choices {
		if choice.Message.Content != "" {
			ch <- ChatEvent{Type: EventToken, Token: choice.Message.Content}
		}
		for _, tc := range choice.Message.ToolCalls {
			args := json.RawMessage(tc.Function.Arguments)
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			ch <- ChatEvent{
				Type: EventToolCall,
				ToolCall: &ToolCall{
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: args,
				},
			}
		}
	}

	ch <- ChatEvent{
		Type: EventDone,
		Usage: &Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
}
