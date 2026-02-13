package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type HTTPTool struct{}

type httpInput struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

func (t *HTTPTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "http_request",
		Description: "Make an HTTP request to a URL and return the response. Supports GET, POST, PUT, PATCH, DELETE methods.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"url": {
					"type": "string",
					"description": "The URL to send the request to"
				},
				"method": {
					"type": "string",
					"enum": ["GET", "POST", "PUT", "PATCH", "DELETE"],
					"description": "HTTP method (defaults to GET)"
				},
				"headers": {
					"type": "object",
					"description": "Optional HTTP headers as key-value pairs",
					"additionalProperties": { "type": "string" }
				},
				"body": {
					"type": "string",
					"description": "Optional request body"
				}
			},
			"required": ["url"]
		}`),
	}
}

func (t *HTTPTool) Execute(ctx context.Context, input json.RawMessage, sb sandbox.Sandbox, policy sandbox.Policy) (string, error) {
	var params httpInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("http_request: invalid input: %w", err)
	}

	if params.URL == "" {
		return "", fmt.Errorf("http_request: url is required")
	}

	if policy.NetworkAccess == sandbox.NetworkDeny {
		return "", fmt.Errorf("http_request: network access denied by sandbox policy")
	}

	method := params.Method
	if method == "" {
		method = "GET"
	}

	timeout := policy.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var bodyReader io.Reader
	if params.Body != "" {
		bodyReader = strings.NewReader(params.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, params.URL, bodyReader)
	if err != nil {
		return "", fmt.Errorf("http_request: creating request: %w", err)
	}

	for k, v := range params.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http_request: %w", err)
	}
	defer resp.Body.Close()

	maxOut := policy.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = 1024 * 1024
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxOut)))
	if err != nil {
		return "", fmt.Errorf("http_request: reading response: %w", err)
	}

	result := fmt.Sprintf("HTTP %d %s\n\n%s", resp.StatusCode, resp.Status, string(body))
	return result, nil
}
