package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type ShellTool struct{}

type shellInput struct {
	Command string `json:"command"`
	WorkDir string `json:"work_dir,omitempty"`
}

func (t *ShellTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "shell",
		Description: "Execute a shell command and return its output. Use this for running programs, scripts, system commands, and CLI tools.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"command": {
					"type": "string",
					"description": "The shell command to execute"
				},
				"work_dir": {
					"type": "string",
					"description": "Optional working directory for the command"
				}
			},
			"required": ["command"]
		}`),
	}
}

func (t *ShellTool) Execute(ctx context.Context, input json.RawMessage, sb sandbox.Sandbox, policy sandbox.Policy) (string, error) {
	var params shellInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("shell: invalid input: %w", err)
	}

	if params.Command == "" {
		return "", fmt.Errorf("shell: command is required")
	}

	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		shell = "cmd"
	}

	cmd := sandbox.Command{
		Name:    "shell",
		Program: shell,
		Args:    []string{"-c", params.Command},
		WorkDir: params.WorkDir,
	}

	result, err := sb.Exec(ctx, cmd, policy)
	if err != nil {
		return "", fmt.Errorf("shell: execution failed: %w", err)
	}

	output := result.Stdout
	if result.Stderr != "" {
		output += "\nSTDERR:\n" + result.Stderr
	}
	if result.ExitCode != 0 {
		output += fmt.Sprintf("\n(exit code: %d)", result.ExitCode)
	}
	if result.Error != "" {
		output += "\nError: " + result.Error
	}

	return output, nil
}
