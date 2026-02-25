package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/igorsilveira/pincer/pkg/audit"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type SubagentTool struct {
	RunSubturn func(ctx context.Context, prompt string, allowedTools []string) (string, error)
	AuditLog   *audit.ToolLogger
}

type subagentInput struct {
	Task         string   `json:"task"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

func (t *SubagentTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "subagent",
		Description: "Delegate a focused subtask to a subagent that runs its own agentic loop with tools. The subagent runs synchronously and returns its final text response. Use this to break complex tasks into smaller, focused operations.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"task": {
					"type": "string",
					"description": "The task description for the subagent to execute."
				},
				"allowed_tools": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Optional list of tool names the subagent can use. If omitted, all tools except subagent/spawn are available."
				}
			},
			"required": ["task"]
		}`),
	}
}

func (t *SubagentTool) Execute(ctx context.Context, input json.RawMessage, _ sandbox.Sandbox, _ sandbox.Policy) (string, error) {
	params, err := parseInput[subagentInput](input, "subagent")
	if err != nil {
		return "", err
	}

	if params.Task == "" {
		return "", fmt.Errorf("subagent: task is required")
	}

	sessionID := SessionIDFromContext(ctx)
	t.AuditLog.Log(ctx, "subagent_start", sessionID, fmt.Sprintf("task=%s", params.Task))

	result, err := t.RunSubturn(ctx, params.Task, params.AllowedTools)
	if err != nil {
		t.AuditLog.Log(ctx, "subagent_error", sessionID, fmt.Sprintf("error=%v", err))
		return "", fmt.Errorf("subagent: %w", err)
	}

	t.AuditLog.Log(ctx, "subagent_done", sessionID, fmt.Sprintf("result_len=%d", len(result)))

	return result, nil
}

