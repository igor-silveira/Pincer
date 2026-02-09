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

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

type GeminiProvider struct {
	apiKey     string
	httpClient *http.Client
}

func NewGeminiProvider(apiKey string) (*GeminiProvider, error) {
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("gemini: API key not set (provide it or set GEMINI_API_KEY)")
	}
	return &GeminiProvider{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}, nil
}

func (g *GeminiProvider) Name() string            { return "gemini" }
func (g *GeminiProvider) SupportsStreaming() bool { return true }
func (g *GeminiProvider) SupportsToolUse() bool   { return true }

func (g *GeminiProvider) Models() []ModelInfo {
	return []ModelInfo{
		{ID: "gemini-2.0-flash", Name: "Gemini 2.0 Flash", MaxContextTokens: 1048576},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", MaxContextTokens: 1048576},
	}
}

type geminiRequest struct {
	Contents          []geminiContent  `json:"contents"`
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	Tools             []geminiToolDecl `json:"tools,omitempty"`
	GenerationConfig  *geminiGenConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string              `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type geminiFuncResponse struct {
	Name     string         `json:"name"`
	Response geminiRespBody `json:"response"`
}

type geminiRespBody struct {
	Content string `json:"content"`
}

type geminiToolDecl struct {
	FunctionDeclarations []geminiFunc `json:"functionDeclarations"`
}

type geminiFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type geminiGenConfig struct {
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
}

func (g *GeminiProvider) Chat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	model := req.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}

	apiReq := geminiRequest{}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	apiReq.GenerationConfig = &geminiGenConfig{
		MaxOutputTokens: maxTokens,
		Temperature:     req.Temperature,
	}

	if req.System != "" {
		apiReq.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	if len(req.Tools) > 0 {
		var funcs []geminiFunc
		for _, t := range req.Tools {
			funcs = append(funcs, geminiFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			})
		}
		apiReq.Tools = []geminiToolDecl{{FunctionDeclarations: funcs}}
	}

	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			continue
		}
		apiReq.Contents = append(apiReq.Contents, convertToGeminiContent(m))
	}

	action := "generateContent"
	if req.Stream {
		action = "streamGenerateContent?alt=sse"
	}
	url := fmt.Sprintf("%s/models/%s:%s&key=%s", geminiBaseURL, model, action, g.apiKey)
	if !req.Stream {
		url = fmt.Sprintf("%s/models/%s:%s?key=%s", geminiBaseURL, model, action, g.apiKey)
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini: API returned %d: %s", resp.StatusCode, string(errBody))
	}

	ch := make(chan ChatEvent, 64)

	if req.Stream {
		go g.readStream(ctx, resp.Body, ch)
	} else {
		go g.readFull(resp.Body, ch)
	}

	return ch, nil
}

func convertToGeminiContent(m ChatMessage) geminiContent {
	role := "user"
	if m.Role == RoleAssistant {
		role = "model"
	}

	if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
		var parts []geminiPart
		if m.Content != "" {
			parts = append(parts, geminiPart{Text: m.Content})
		}
		for _, tc := range m.ToolCalls {
			parts = append(parts, geminiPart{
				FunctionCall: &geminiFunctionCall{Name: tc.Name, Args: tc.Input},
			})
		}
		return geminiContent{Role: role, Parts: parts}
	}

	if m.Role == RoleUser && len(m.ToolResults) > 0 {
		var parts []geminiPart
		for _, tr := range m.ToolResults {
			parts = append(parts, geminiPart{
				FunctionResponse: &geminiFuncResponse{
					Name:     tr.ToolCallID,
					Response: geminiRespBody{Content: tr.Content},
				},
			})
		}
		return geminiContent{Role: role, Parts: parts}
	}

	return geminiContent{Role: role, Parts: []geminiPart{{Text: m.Content}}}
}

type geminiStreamResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *geminiUsage      `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
}

func (g *GeminiProvider) readStream(ctx context.Context, body io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

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

		var resp geminiStreamResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			continue
		}

		if resp.UsageMetadata != nil {
			usage.InputTokens = resp.UsageMetadata.PromptTokenCount
			usage.OutputTokens = resp.UsageMetadata.CandidatesTokenCount
		}

		for _, c := range resp.Candidates {
			for _, p := range c.Content.Parts {
				if p.Text != "" {
					ch <- ChatEvent{Type: EventToken, Token: p.Text}
				}
				if p.FunctionCall != nil {
					ch <- ChatEvent{
						Type: EventToolCall,
						ToolCall: &ToolCall{
							ID:    p.FunctionCall.Name,
							Name:  p.FunctionCall.Name,
							Input: p.FunctionCall.Args,
						},
					}
				}
			}
		}
	}

	ch <- ChatEvent{Type: EventDone, Usage: &usage}
}

func (g *GeminiProvider) readFull(body io.ReadCloser, ch chan<- ChatEvent) {
	defer close(ch)
	defer body.Close()

	var resp geminiStreamResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		ch <- ChatEvent{Type: EventError, Error: fmt.Errorf("gemini: decoding response: %w", err)}
		return
	}

	for _, c := range resp.Candidates {
		for _, p := range c.Content.Parts {
			if p.Text != "" {
				ch <- ChatEvent{Type: EventToken, Token: p.Text}
			}
			if p.FunctionCall != nil {
				ch <- ChatEvent{
					Type: EventToolCall,
					ToolCall: &ToolCall{
						ID:    p.FunctionCall.Name,
						Name:  p.FunctionCall.Name,
						Input: p.FunctionCall.Args,
					},
				}
			}
		}
	}

	var usage Usage
	if resp.UsageMetadata != nil {
		usage.InputTokens = resp.UsageMetadata.PromptTokenCount
		usage.OutputTokens = resp.UsageMetadata.CandidatesTokenCount
	}
	ch <- ChatEvent{Type: EventDone, Usage: &usage}
}
