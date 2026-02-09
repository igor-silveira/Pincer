package llm

import (
	"context"
	"fmt"
	"os"
)

const ollamaDefaultURL = "http://localhost:11434/v1/chat/completions"

type OllamaProvider struct {
	inner *OpenAIProvider
	model string
}

func NewOllamaProvider(baseURL, model string) (*OllamaProvider, error) {
	if baseURL == "" {
		baseURL = os.Getenv("OLLAMA_BASE_URL")
	}
	if baseURL == "" {
		baseURL = ollamaDefaultURL
	}
	if model == "" {
		model = "llama3"
	}

	inner, err := NewOpenAIProvider("ollama", baseURL)
	if err != nil {
		return nil, fmt.Errorf("ollama: creating provider: %w", err)
	}

	return &OllamaProvider{inner: inner, model: model}, nil
}

func (o *OllamaProvider) Name() string            { return "ollama" }
func (o *OllamaProvider) SupportsStreaming() bool { return true }
func (o *OllamaProvider) SupportsToolUse() bool   { return true }

func (o *OllamaProvider) Models() []ModelInfo {
	return []ModelInfo{
		{ID: o.model, Name: o.model, MaxContextTokens: 8192},
	}
}

func (o *OllamaProvider) Chat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	if req.Model == "" {
		req.Model = o.model
	}
	return o.inner.Chat(ctx, req)
}
