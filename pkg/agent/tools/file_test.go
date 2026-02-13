package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/igorsilveira/pincer/pkg/sandbox"
)

func TestFileReadTool_Allowed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0644)

	tool := &FileReadTool{}
	input, _ := json.Marshal(fileReadInput{Path: path})
	policy := sandbox.Policy{AllowedPaths: []string{dir}}

	result, err := tool.Execute(context.Background(), input, nil, policy)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "hello" {
		t.Errorf("result = %q, want %q", result, "hello")
	}
}

func TestFileReadTool_Denied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("secret"), 0644)

	tool := &FileReadTool{}
	input, _ := json.Marshal(fileReadInput{Path: path})
	policy := sandbox.Policy{AllowedPaths: []string{"/nonexistent"}}

	_, err := tool.Execute(context.Background(), input, nil, policy)
	if err == nil {
		t.Fatal("expected error for denied path")
	}
}

func TestFileReadTool_EmptyPolicyAllowsAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("data"), 0644)

	tool := &FileReadTool{}
	input, _ := json.Marshal(fileReadInput{Path: path})
	policy := sandbox.Policy{}

	result, err := tool.Execute(context.Background(), input, nil, policy)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "data" {
		t.Errorf("result = %q, want %q", result, "data")
	}
}

func TestFileWriteTool_Allowed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	tool := &FileWriteTool{}
	input, _ := json.Marshal(fileWriteInput{Path: path, Content: "written"})
	policy := sandbox.Policy{AllowedPaths: []string{dir}}

	result, err := tool.Execute(context.Background(), input, nil, policy)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	data, _ := os.ReadFile(path)
	if string(data) != "written" {
		t.Errorf("file content = %q, want %q", string(data), "written")
	}
}

func TestFileWriteTool_Denied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	tool := &FileWriteTool{}
	input, _ := json.Marshal(fileWriteInput{Path: path, Content: "nope"})
	policy := sandbox.Policy{AllowedPaths: []string{"/nonexistent"}}

	_, err := tool.Execute(context.Background(), input, nil, policy)
	if err == nil {
		t.Fatal("expected error for denied path")
	}
}

func TestFileWriteTool_ReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	tool := &FileWriteTool{}
	input, _ := json.Marshal(fileWriteInput{Path: path, Content: "nope"})
	policy := sandbox.Policy{ReadOnlyPaths: []string{dir}}

	_, err := tool.Execute(context.Background(), input, nil, policy)
	if err == nil {
		t.Fatal("expected error for read-only path")
	}
}
