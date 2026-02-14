package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	anthropicDefaultBaseURL = "https://api.anthropic.com"
	anthropicMessagesPath   = "/v1/messages"
	anthropicAPIVersion     = "2023-06-01"
)

type AnthropicProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewAnthropicProvider(apiKey, baseURL string) (*AnthropicProvider, error) {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if baseURL == "" {
		baseURL = anthropicDefaultBaseURL
	}
	if apiKey == "" && baseURL == anthropicDefaultBaseURL {
		return nil, fmt.Errorf("anthropic: API key not set (provide it or set ANTHROPIC_API_KEY)")
	}
	return &AnthropicProvider{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}, nil
}

func (a *AnthropicProvider) Name() string            { return "anthropic" }
func (a *AnthropicProvider) SupportsStreaming() bool { return true }
func (a *AnthropicProvider) SupportsToolUse() bool   { return true }

func (a *AnthropicProvider) Models() []ModelInfo {
	return []ModelInfo{
		{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", MaxContextTokens: 200000},
		{ID: "claude-haiku-3-5-20241022", Name: "Claude 3.5 Haiku", MaxContextTokens: 200000},
		{ID: "claude-opus-4-20250514", Name: "Claude Opus 4", MaxContextTokens: 200000},
	}
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Stream    bool               `json:"stream"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

func (a *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	model := req.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	apiReq := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    req.System,
		Messages:  make([]anthropicMessage, 0, len(req.Messages)),
		Stream:    req.Stream,
	}

	for _, t := range req.Tools {
		apiReq.Tools = append(apiReq.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			continue
		}
		apiReq.Messages = append(apiReq.Messages, convertToAnthropicMessage(m))
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+anthropicMessagesPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Api-Key", a.apiKey)
	httpReq.Header.Set("Anthropic-Version", anthropicAPIVersion)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic: API returned %d: %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan ChatEvent, 64)

	if req.Stream {
		go a.readStream(ctx, resp.Body, ch)
	} else {
		go a.readFull(resp.Body, ch)
	}

	return ch, nil
}

func convertToAnthropicMessage(m ChatMessage) anthropicMessage {

	if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
		var blocks []anthropicContentBlock
		if m.Content != "" {
			blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Content})
		}
		for _, tc := range m.ToolCalls {
			blocks = append(blocks, anthropicContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			})
		}
		return anthropicMessage{Role: m.Role, Content: blocks}
	}

	if m.Role == RoleUser && len(m.ToolResults) > 0 {
		var blocks []anthropicContentBlock
		for _, tr := range m.ToolResults {
			blocks = append(blocks, anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: tr.ToolCallID,
				Content:   tr.Content,
				IsError:   tr.IsError,
			})
		}
		return anthropicMessage{Role: m.Role, Content: blocks}
	}

	return anthropicMessage{Role: m.Role, Content: m.Content}
}

type sseEvent struct {
	Type         string     `json:"type"`
	Index        int        `json:"index"`
	ContentBlock *sseBlock  `json:"content_block,omitempty"`
	Delta        sseDelta   `json:"delta,omitempty"`
	Message      sseMessage `json:"message,omitempty"`
	Usage        sseUsage   `json:"usage,omitempty"`
}

type sseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type sseDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

type sseMessage struct {
	Usage sseUsage `json:"usage,omitempty"`
}

type sseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type streamToolState struct {
	id    string
	name  string
	input strings.Builder
}

func (a *AnthropicProvider) readStream(ctx context.Context, body io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)

	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var usage Usage

	toolStates := make(map[int]*streamToolState)

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

		var event sseEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
				toolStates[event.Index] = &streamToolState{
					id:   event.ContentBlock.ID,
					name: event.ContentBlock.Name,
				}
			}

		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				ch <- ChatEvent{Type: EventToken, Token: event.Delta.Text}
			case "input_json_delta":
				if ts, ok := toolStates[event.Index]; ok {
					ts.input.WriteString(event.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			if ts, ok := toolStates[event.Index]; ok {
				inputJSON := json.RawMessage(ts.input.String())
				if len(inputJSON) == 0 {
					inputJSON = json.RawMessage("{}")
				}
				ch <- ChatEvent{
					Type: EventToolCall,
					ToolCall: &ToolCall{
						ID:    ts.id,
						Name:  ts.name,
						Input: inputJSON,
					},
				}
				delete(toolStates, event.Index)
			}

		case "message_start":
			if event.Message.Usage.InputTokens > 0 {
				usage.InputTokens = event.Message.Usage.InputTokens
			}

		case "message_delta":
			if event.Usage.OutputTokens > 0 {
				usage.OutputTokens = event.Usage.OutputTokens
			}

		case "message_stop":
			ch <- ChatEvent{Type: EventDone, Usage: &usage}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- ChatEvent{Type: EventError, Error: err}
	}
}

type anthropicFullResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Usage   sseUsage                `json:"usage"`
}

func (a *AnthropicProvider) readFull(body io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer body.Close()

	var resp anthropicFullResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		ch <- ChatEvent{Type: EventError, Error: fmt.Errorf("anthropic: decoding response: %w", err)}
		return
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			ch <- ChatEvent{Type: EventToken, Token: block.Text}
		case "tool_use":
			input := block.Input
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			ch <- ChatEvent{
				Type: EventToolCall,
				ToolCall: &ToolCall{
					ID:    block.ID,
					Name:  block.Name,
					Input: input,
				},
			}
		}
	}

	ch <- ChatEvent{
		Type: EventDone,
		Usage: &Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		},
	}
}
