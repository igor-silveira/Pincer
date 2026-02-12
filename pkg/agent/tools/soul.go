package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
	"github.com/igorsilveira/pincer/pkg/soul"
)

type SoulTool struct {
	Soul *soul.Soul
}

type soulInput struct {
	Section string `json:"section,omitempty"`
}

func (t *SoulTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "soul",
		Description: "Introspect your own identity, values, tone, boundaries, and expertise. Use this to recall who you are and how you should behave.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"section": {
					"type": "string",
					"enum": ["identity", "values", "tone", "boundaries", "expertise", "all"],
					"description": "Which section of the soul to retrieve (defaults to all)"
				}
			}
		}`),
	}
}

func (t *SoulTool) Execute(ctx context.Context, input json.RawMessage, _ sandbox.Sandbox, _ sandbox.Policy) (string, error) {
	var params soulInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("soul: invalid input: %w", err)
	}

	if params.Section == "" || params.Section == "all" {
		return t.Soul.Render(), nil
	}

	return t.Soul.Section(params.Section), nil
}
