package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type SpawnTool struct {
	RunSpawn   func(ctx context.Context, sessionID, prompt string, allowedTools []string) string
	CheckSpawn func(spawnID string) (result string, done bool, err error)
	AuditLog   func(ctx context.Context, eventType, sessionID, detail string)
}

type spawnInput struct {
	Action       string   `json:"action"`
	Task         string   `json:"task,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	SpawnID      string   `json:"spawn_id,omitempty"`
}

func (t *SpawnTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "spawn",
		Description: "Start a background agent task or check its result. Actions: start (launch a background subtask, returns a spawn ID immediately), check (poll a spawn ID for its result).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["start", "check"],
					"description": "start: launch a background subtask. check: poll a spawn ID for its result. Default: start."
				},
				"task": {
					"type": "string",
					"description": "The task description for the background agent. Required for start."
				},
				"allowed_tools": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Optional list of tool names the spawned agent can use. If omitted, all tools except subagent/spawn are available. Only for start."
				},
				"spawn_id": {
					"type": "string",
					"description": "The spawn ID to check. Required for check."
				}
			}
		}`),
	}
}

func (t *SpawnTool) Execute(ctx context.Context, input json.RawMessage, _ sandbox.Sandbox, _ sandbox.Policy) (string, error) {
	params, err := parseInput[spawnInput](input, "spawn")
	if err != nil {
		return "", err
	}

	if params.Action == "" {
		params.Action = "start"
	}

	sessionID := SessionIDFromContext(ctx)

	switch params.Action {
	case "start":
		if params.Task == "" {
			return "", fmt.Errorf("spawn: task is required for start")
		}
		if sessionID == "" {
			return "", fmt.Errorf("spawn: no session in context")
		}

		spawnID := t.RunSpawn(ctx, sessionID, params.Task, params.AllowedTools)
		return fmt.Sprintf("spawned: %s", spawnID), nil

	case "check":
		if params.SpawnID == "" {
			return "", fmt.Errorf("spawn: spawn_id is required for check")
		}

		result, done, err := t.CheckSpawn(params.SpawnID)
		if err != nil {
			return "", fmt.Errorf("spawn: %w", err)
		}
		if !done {
			return "still running", nil
		}
		return result, nil

	default:
		return "", fmt.Errorf("spawn: unknown action %q", params.Action)
	}
}
