package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ProcessSandbox struct {
	defaultWorkDir string
}

func NewProcessSandbox(workDir string) *ProcessSandbox {
	return &ProcessSandbox{defaultWorkDir: workDir}
}

func (s *ProcessSandbox) Exec(ctx context.Context, cmd Command, policy Policy) (*Result, error) {
	if cmd.Program == "" {
		return nil, fmt.Errorf("sandbox: empty program")
	}

	workDir := cmd.WorkDir
	if workDir == "" {
		workDir = s.defaultWorkDir
	}
	if workDir != "" {
		if err := CheckPathAllowed(workDir, policy.AllowedPaths); err != nil {
			return nil, fmt.Errorf("sandbox: work directory not allowed: %w", err)
		}
	}

	timeout := policy.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	proc := exec.CommandContext(ctx, cmd.Program, cmd.Args...)

	if workDir != "" {
		proc.Dir = workDir
	}

	if len(cmd.Env) > 0 {
		proc.Env = cmd.Env
	}

	if cmd.Stdin != "" {
		proc.Stdin = strings.NewReader(cmd.Stdin)
	}

	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr

	err := proc.Run()
	duration := time.Since(start)

	result := &Result{
		Duration: duration,
	}

	maxOut := policy.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = 1024 * 1024
	}

	result.Stdout = truncate(stdout.String(), maxOut)
	result.Stderr = truncate(stderr.String(), maxOut)

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = -1
			result.Error = fmt.Sprintf("tool execution timed out after %s", timeout)
			return result, nil
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.Error = err.Error()
			result.ExitCode = -1
		}
	}

	return result, nil
}

func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n... (output truncated)"
}
