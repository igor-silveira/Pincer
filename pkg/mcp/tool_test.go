package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

func TestToolName(t *testing.T) {
	tests := []struct {
		server string
		tool   string
		want   string
	}{
		{"github", "search", "mcp_github__search"},
		{"fs", "read_file", "mcp_fs__read_file"},
	}
	for _, tt := range tests {
		got := ToolName(tt.server, tt.tool)
		if got != tt.want {
			t.Errorf("ToolName(%q, %q) = %q, want %q", tt.server, tt.tool, got, tt.want)
		}
	}
}

func TestMCPTool_Definition(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
	}
	tool := &mcpsdk.Tool{
		Name:        "search",
		Description: "Search repos",
		InputSchema: schema,
	}

	mcpTool := NewMCPTool("github", tool, nil)
	def := mcpTool.Definition()

	if def.Name != "mcp_github__search" {
		t.Errorf("Name = %q, want %q", def.Name, "mcp_github__search")
	}
	if !strings.Contains(def.Description, "[MCP:github]") {
		t.Errorf("Description should contain server prefix, got %q", def.Description)
	}
	if !strings.Contains(def.Description, "Search repos") {
		t.Errorf("Description should contain original desc, got %q", def.Description)
	}
	if len(def.InputSchema) == 0 {
		t.Error("InputSchema should not be empty")
	}
	var parsed map[string]any
	if err := json.Unmarshal(def.InputSchema, &parsed); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("InputSchema type = %v, want object", parsed["type"])
	}
}

func TestMCPTool_DefinitionNilSchema(t *testing.T) {
	tool := &mcpsdk.Tool{
		Name:        "ping",
		Description: "Ping server",
		InputSchema: nil,
	}

	mcpTool := NewMCPTool("test", tool, nil)
	def := mcpTool.Definition()

	var parsed map[string]any
	if err := json.Unmarshal(def.InputSchema, &parsed); err != nil {
		t.Fatalf("InputSchema should be valid JSON: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("nil schema should default to {type:object}, got %v", parsed)
	}
}

func TestMCPTool_ExecuteNoSession(t *testing.T) {
	tool := &mcpsdk.Tool{
		Name:        "test",
		Description: "test tool",
		InputSchema: map[string]any{"type": "object"},
	}

	sessionErr := func() (*mcpsdk.ClientSession, error) {
		return nil, errNotConnected
	}

	mcpTool := NewMCPTool("srv", tool, sessionErr)
	_, err := mcpTool.Execute(t.Context(), json.RawMessage(`{}`), nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error when session unavailable")
	}
}

func TestMCPTool_ExecuteInvalidInput(t *testing.T) {
	tool := &mcpsdk.Tool{
		Name:        "test",
		Description: "test tool",
		InputSchema: map[string]any{"type": "object"},
	}

	mcpTool := NewMCPTool("srv", tool, nil)
	_, err := mcpTool.Execute(t.Context(), json.RawMessage(`{invalid`), nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for invalid JSON input")
	}
}

var errNotConnected = fmt.Errorf("not connected")
