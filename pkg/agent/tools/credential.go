package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorsilveira/pincer/pkg/credentials"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type CredentialTool struct {
	Credentials *credentials.Store
}

type credentialInput struct {
	Action string `json:"action"`
	Name   string `json:"name,omitempty"`
	Value  string `json:"value,omitempty"`
}

func (t *CredentialTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "credential",
		Description: "Encrypted credential store for managing secrets and API keys. Actions: get, set, delete, list.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["get", "set", "delete", "list"],
					"description": "The action to perform"
				},
				"name": {
					"type": "string",
					"description": "The credential name (required for get, set, delete)"
				},
				"value": {
					"type": "string",
					"description": "The credential value (required for set)"
				}
			},
			"required": ["action"]
		}`),
	}
}

func (t *CredentialTool) Execute(ctx context.Context, input json.RawMessage, _ sandbox.Sandbox, _ sandbox.Policy) (string, error) {
	params, err := parseInput[credentialInput](input, "credential")
	if err != nil {
		return "", err
	}

	switch params.Action {
	case "get":
		if params.Name == "" {
			return "", fmt.Errorf("credential: name is required for get")
		}
		val, err := t.Credentials.Get(ctx, params.Name)
		if err != nil {
			return "", err
		}
		return val, nil

	case "set":
		if params.Name == "" {
			return "", fmt.Errorf("credential: name is required for set")
		}
		if params.Value == "" {
			return "", fmt.Errorf("credential: value is required for set")
		}
		if err := t.Credentials.Set(ctx, params.Name, params.Value); err != nil {
			return "", err
		}
		return fmt.Sprintf("credential %q stored", params.Name), nil

	case "delete":
		if params.Name == "" {
			return "", fmt.Errorf("credential: name is required for delete")
		}
		if err := t.Credentials.Delete(ctx, params.Name); err != nil {
			return "", err
		}
		return fmt.Sprintf("credential %q deleted", params.Name), nil

	case "list":
		names, err := t.Credentials.List(ctx)
		if err != nil {
			return "", err
		}
		if len(names) == 0 {
			return "no credentials stored", nil
		}
		return strings.Join(names, "\n"), nil

	default:
		return "", fmt.Errorf("credential: unknown action %q", params.Action)
	}
}
