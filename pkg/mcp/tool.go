package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type MCPTool struct {
	serverName string
	toolName   string
	desc       string
	schema     json.RawMessage
	session    func() (*mcpsdk.ClientSession, error)
}

func NewMCPTool(serverName string, tool *mcpsdk.Tool, sessionFn func() (*mcpsdk.ClientSession, error)) *MCPTool {
	schema, _ := json.Marshal(tool.InputSchema)
	if len(schema) == 0 || string(schema) == "null" {
		schema = json.RawMessage(`{"type":"object"}`)
	}
	return &MCPTool{
		serverName: serverName,
		toolName:   tool.Name,
		desc:       tool.Description,
		schema:     schema,
		session:    sessionFn,
	}
}

func (t *MCPTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        ToolName(t.serverName, t.toolName),
		Description: fmt.Sprintf("[MCP:%s] %s", t.serverName, t.desc),
		InputSchema: t.schema,
	}
}

func (t *MCPTool) Execute(ctx context.Context, input json.RawMessage, _ sandbox.Sandbox, _ sandbox.Policy) (string, error) {
	var args map[string]any
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("mcp tool %s: invalid input: %w", t.toolName, err)
		}
	}
	if args == nil {
		args = map[string]any{}
	}

	sess, err := t.session()
	if err != nil {
		return "", fmt.Errorf("mcp tool %s: %w", t.toolName, err)
	}

	result, err := sess.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      t.toolName,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("mcp tool %s: call failed: %w", t.toolName, err)
	}

	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}

	text := strings.Join(parts, "\n")
	if result.IsError {
		return "", fmt.Errorf("mcp tool %s: %s", t.toolName, text)
	}
	return text, nil
}

func ToolName(serverName, toolName string) string {
	return fmt.Sprintf("mcp_%s__%s", serverName, toolName)
}
