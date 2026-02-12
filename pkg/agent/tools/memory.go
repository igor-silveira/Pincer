package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/memory"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type MemoryTool struct {
	Memory *memory.Store
}

type memoryInput struct {
	Action string `json:"action"`
	Key    string `json:"key,omitempty"`
	Value  string `json:"value,omitempty"`
	Query  string `json:"query,omitempty"`
}

func (t *MemoryTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "memory",
		Description: "Persistent key-value memory for storing and retrieving information across sessions. Actions: get, set, delete, list, search.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["get", "set", "delete", "list", "search"],
					"description": "The action to perform"
				},
				"key": {
					"type": "string",
					"description": "The memory key (required for get, set, delete)"
				},
				"value": {
					"type": "string",
					"description": "The value to store (required for set)"
				},
				"query": {
					"type": "string",
					"description": "Search query (required for search)"
				}
			},
			"required": ["action"]
		}`),
	}
}

func (t *MemoryTool) Execute(ctx context.Context, input json.RawMessage, _ sandbox.Sandbox, _ sandbox.Policy) (string, error) {
	var params memoryInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("memory: invalid input: %w", err)
	}

	agentID := AgentIDFromContext(ctx)

	switch params.Action {
	case "get":
		if params.Key == "" {
			return "", fmt.Errorf("memory: key is required for get")
		}
		entry, err := t.Memory.Get(ctx, agentID, params.Key)
		if err != nil {
			return "", err
		}
		return entry.Value, nil

	case "set":
		if params.Key == "" {
			return "", fmt.Errorf("memory: key is required for set")
		}
		if params.Value == "" {
			return "", fmt.Errorf("memory: value is required for set")
		}
		if err := t.Memory.Set(ctx, agentID, params.Key, params.Value); err != nil {
			return "", err
		}
		return fmt.Sprintf("stored %q", params.Key), nil

	case "delete":
		if params.Key == "" {
			return "", fmt.Errorf("memory: key is required for delete")
		}
		if err := t.Memory.Delete(ctx, agentID, params.Key); err != nil {
			return "", err
		}
		return fmt.Sprintf("deleted %q", params.Key), nil

	case "list":
		entries, err := t.Memory.List(ctx, agentID)
		if err != nil {
			return "", err
		}
		if len(entries) == 0 {
			return "no memory entries", nil
		}
		var sb strings.Builder
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("[%s]: %s\n", e.Key, e.Value))
		}
		return sb.String(), nil

	case "search":
		if params.Query == "" {
			return "", fmt.Errorf("memory: query is required for search")
		}
		entries, err := t.Memory.Search(ctx, agentID, params.Query)
		if err != nil {
			return "", err
		}
		if len(entries) == 0 {
			return "no matching entries", nil
		}
		var sb strings.Builder
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("[%s]: %s\n", e.Key, e.Value))
		}
		return sb.String(), nil

	default:
		return "", fmt.Errorf("memory: unknown action %q", params.Action)
	}
}
