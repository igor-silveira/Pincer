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
	anthropicAPIURL     = "https://api.anthropic.com/v1/messages"
	anthropicAPIVersion = "2023-06-01"
)

type AnthropicProvider struct {
	apiKey     string
	httpClient *http.Client
}

func NewAnthropicProvider(apiKey string) (*AnthropicProvider, error) {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic: API key not set (provide it or set ANTHROPIC_API_KEY)")
	}
	return &AnthropicProvider{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}, nil
}

func (a *AnthropicProvider) Name() string { return "anthropic" }

func (a *AnthropicProvider) SupportsStreaming() bool { return true }

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
	Stream    bool               `json:"stream"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
		Stream:    req.Stream,
		Messages:  make([]anthropicMessage, 0, len(req.Messages)),
	}

	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			continue
		}
		apiReq.Messages = append(apiReq.Messages, anthropicMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(body))
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

func (a *AnthropicProvider) readStream(ctx context.Context, body io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
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

		var event sseEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_delta":
			if event.Delta.Type == "text_delta" {
				ch <- ChatEvent{Type: EventToken, Token: event.Delta.Text}
			}
		case "message_delta":
			if event.Usage.OutputTokens > 0 {
				usage.OutputTokens = event.Usage.OutputTokens
			}
		case "message_start":
			if event.Message.Usage.InputTokens > 0 {
				usage.InputTokens = event.Message.Usage.InputTokens
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

func (a *AnthropicProvider) readFull(body io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer body.Close()

	var resp anthropicFullResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		ch <- ChatEvent{Type: EventError, Error: fmt.Errorf("anthropic: decoding response: %w", err)}
		return
	}

	for _, block := range resp.Content {
		if block.Type == "text" {
			ch <- ChatEvent{Type: EventToken, Token: block.Text}
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

type sseEvent struct {
	Type    string   `json:"type"`
	Delta   sseDelta `json:"delta,omitempty"`
	Message struct {
		Usage sseUsage `json:"usage,omitempty"`
	} `json:"message,omitempty"`
	Usage sseUsage `json:"usage,omitempty"`
}

type sseDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type sseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicFullResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage sseUsage `json:"usage"`
}
