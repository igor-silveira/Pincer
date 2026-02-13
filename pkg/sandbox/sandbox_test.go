package sandbox

import (
	"context"
	"testing"
	"time"
)

func TestProcessSandboxExec(t *testing.T) {
	sb := NewProcessSandbox("")
	ctx := context.Background()

	result, err := sb.Exec(ctx, Command{
		Program: "/bin/echo",
		Args:    []string{"hello world"},
	}, DefaultPolicy())

	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Stdout != "hello world\n" {
		t.Errorf("Stdout = %q, want %q", result.Stdout, "hello world\n")
	}
}

func TestProcessSandboxTimeout(t *testing.T) {
	sb := NewProcessSandbox("")
	ctx := context.Background()

	policy := Policy{Timeout: 100 * time.Millisecond}
	result, err := sb.Exec(ctx, Command{
		Program: "/bin/sleep",
		Args:    []string{"10"},
	}, policy)

	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1 (timeout)", result.ExitCode)
	}
	if result.Error == "" {
		t.Error("expected error message for timeout")
	}
}

func TestProcessSandboxEmptyProgram(t *testing.T) {
	sb := NewProcessSandbox("")
	ctx := context.Background()

	_, err := sb.Exec(ctx, Command{Program: ""}, DefaultPolicy())
	if err == nil {
		t.Fatal("expected error for empty program")
	}
}

func TestProcessSandboxNonZeroExit(t *testing.T) {
	sb := NewProcessSandbox("")
	ctx := context.Background()

	result, err := sb.Exec(ctx, Command{
		Program: "/bin/sh",
		Args:    []string{"-c", "exit 42"},
	}, DefaultPolicy())

	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", result.ExitCode)
	}
}

func TestProcessSandboxStdin(t *testing.T) {
	sb := NewProcessSandbox("")
	ctx := context.Background()

	result, err := sb.Exec(ctx, Command{
		Program: "/bin/cat",
		Stdin:   "input data",
	}, DefaultPolicy())

	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.Stdout != "input data" {
		t.Errorf("Stdout = %q, want %q", result.Stdout, "input data")
	}
}

func TestProcessSandboxTruncation(t *testing.T) {
	sb := NewProcessSandbox("")
	ctx := context.Background()

	policy := Policy{MaxOutputBytes: 10, Timeout: 5 * time.Second}
	result, err := sb.Exec(ctx, Command{
		Program: "/bin/sh",
		Args:    []string{"-c", "printf 'a]%.0s' {1..100}"},
	}, policy)

	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if len(result.Stdout) > 100 {
		t.Errorf("output not truncated: len = %d", len(result.Stdout))
	}
}

func TestProcessSandboxWorkDirAllowed(t *testing.T) {
	dir := t.TempDir()
	sb := NewProcessSandbox("")
	ctx := context.Background()

	policy := Policy{
		Timeout:      5 * time.Second,
		AllowedPaths: []string{dir},
	}
	result, err := sb.Exec(ctx, Command{
		Program: "/bin/echo",
		Args:    []string{"ok"},
		WorkDir: dir,
	}, policy)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
}

func TestProcessSandboxWorkDirDenied(t *testing.T) {
	sb := NewProcessSandbox("")
	ctx := context.Background()

	policy := Policy{
		Timeout:      5 * time.Second,
		AllowedPaths: []string{"/tmp/allowed-only"},
	}
	_, err := sb.Exec(ctx, Command{
		Program: "/bin/echo",
		Args:    []string{"should fail"},
		WorkDir: "/tmp/not-allowed",
	}, policy)
	if err == nil {
		t.Fatal("expected error for denied work directory")
	}
}

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()
	if p.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", p.Timeout)
	}
	if p.MaxOutputBytes != 1024*1024 {
		t.Errorf("MaxOutputBytes = %d, want 1MB", p.MaxOutputBytes)
	}
	if p.NetworkAccess != NetworkDeny {
		t.Errorf("NetworkAccess = %d, want NetworkDeny", p.NetworkAccess)
	}
}
