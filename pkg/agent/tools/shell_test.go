package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/igorsilveira/pincer/pkg/sandbox"
)

func TestShellTool_Definition(t *testing.T) {
	tool := &ShellTool{}
	def := tool.Definition()
	if def.Name != "shell" {
		t.Errorf("Name = %q, want %q", def.Name, "shell")
	}
}

func TestShellTool_Success(t *testing.T) {
	sb := &fakeSandbox{
		result: &sandbox.Result{Stdout: "ok", ExitCode: 0},
	}
	tool := &ShellTool{}
	input, _ := json.Marshal(shellInput{Command: "echo ok"})

	output, err := tool.Execute(context.Background(), input, sb, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output != "ok" {
		t.Errorf("output = %q, want %q", output, "ok")
	}
}

func TestShellTool_WithStderr(t *testing.T) {
	sb := &fakeSandbox{
		result: &sandbox.Result{Stdout: "out", Stderr: "warn", ExitCode: 0},
	}
	tool := &ShellTool{}
	input, _ := json.Marshal(shellInput{Command: "test"})

	output, err := tool.Execute(context.Background(), input, sb, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "STDERR:") || !strings.Contains(output, "warn") {
		t.Errorf("output should contain stderr section, got %q", output)
	}
}

func TestShellTool_NonZeroExit(t *testing.T) {
	sb := &fakeSandbox{
		result: &sandbox.Result{Stdout: "", ExitCode: 1},
	}
	tool := &ShellTool{}
	input, _ := json.Marshal(shellInput{Command: "false"})

	output, err := tool.Execute(context.Background(), input, sb, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "exit code: 1") {
		t.Errorf("output should contain exit code, got %q", output)
	}
}

func TestShellTool_EmptyCommand(t *testing.T) {
	tool := &ShellTool{}
	input, _ := json.Marshal(shellInput{Command: ""})

	_, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestShellTool_SandboxError(t *testing.T) {
	sb := &fakeSandbox{
		err: fmt.Errorf("sandbox unavailable"),
	}
	tool := &ShellTool{}
	input, _ := json.Marshal(shellInput{Command: "ls"})

	_, err := tool.Execute(context.Background(), input, sb, sandbox.Policy{})
	if err == nil {
		t.Fatal("expected error from sandbox")
	}
	if !strings.Contains(err.Error(), "sandbox unavailable") {
		t.Errorf("error = %q, should wrap sandbox error", err.Error())
	}
}

func TestShellTool_WorkDir(t *testing.T) {
	sb := &fakeSandbox{
		result: &sandbox.Result{Stdout: "ok", ExitCode: 0},
	}
	tool := &ShellTool{}
	input, _ := json.Marshal(shellInput{Command: "ls", WorkDir: "/tmp/test"})

	_, err := tool.Execute(context.Background(), input, sb, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if sb.gotCmd.WorkDir != "/tmp/test" {
		t.Errorf("WorkDir = %q, want %q", sb.gotCmd.WorkDir, "/tmp/test")
	}
}
