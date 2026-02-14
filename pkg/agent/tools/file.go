package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type FileReadTool struct{}

type fileReadInput struct {
	Path string `json:"path"`
}

func (t *FileReadTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "file_read",
		Description: "Read the contents of a file at the given path. Returns the file content as text.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "Absolute or relative path to the file to read"
				}
			},
			"required": ["path"]
		}`),
	}
}

func (t *FileReadTool) Execute(ctx context.Context, input json.RawMessage, sb sandbox.Sandbox, policy sandbox.Policy) (string, error) {
	params, err := parseInput[fileReadInput](input, "file_read")
	if err != nil {
		return "", err
	}

	if params.Path == "" {
		return "", fmt.Errorf("file_read: path is required")
	}

	path := params.Path
	if !filepath.IsAbs(path) {
		path = filepath.Clean(path)
	}

	if err := sandbox.CheckPathAllowed(path, policy.AllowedPaths); err != nil {
		return "", fmt.Errorf("file_read: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("file_read: %w", err)
	}

	maxOut := policy.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = 1024 * 1024
	}
	content := string(data)
	if len(content) > maxOut {
		content = content[:maxOut] + "\n... (file truncated)"
	}

	return content, nil
}

type FileWriteTool struct{}

type fileWriteInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Append  bool   `json:"append,omitempty"`
}

func (t *FileWriteTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "file_write",
		Description: "Write content to a file at the given path. Creates the file and parent directories if they don't exist. Set append=true to append instead of overwrite.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {
					"type": "string",
					"description": "Absolute or relative path to the file to write"
				},
				"content": {
					"type": "string",
					"description": "The content to write to the file"
				},
				"append": {
					"type": "boolean",
					"description": "If true, append to the file instead of overwriting"
				}
			},
			"required": ["path", "content"]
		}`),
	}
}

func (t *FileWriteTool) Execute(ctx context.Context, input json.RawMessage, sb sandbox.Sandbox, policy sandbox.Policy) (string, error) {
	params, err := parseInput[fileWriteInput](input, "file_write")
	if err != nil {
		return "", err
	}

	if params.Path == "" {
		return "", fmt.Errorf("file_write: path is required")
	}

	path := params.Path
	if !filepath.IsAbs(path) {
		path = filepath.Clean(path)
	}

	if err := sandbox.CheckPathAllowed(path, policy.AllowedPaths); err != nil {
		return "", fmt.Errorf("file_write: %w", err)
	}
	if err := sandbox.CheckPathWritable(path, policy.ReadOnlyPaths); err != nil {
		return "", fmt.Errorf("file_write: %w", err)
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("file_write: creating directory: %w", err)
		}
	}

	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if params.Append {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}

	f, err := os.OpenFile(path, flag, 0644)
	if err != nil {
		return "", fmt.Errorf("file_write: %w", err)
	}
	defer f.Close()

	n, err := f.WriteString(params.Content)
	if err != nil {
		return "", fmt.Errorf("file_write: %w", err)
	}

	return fmt.Sprintf("wrote %d bytes to %s", n, path), nil
}
