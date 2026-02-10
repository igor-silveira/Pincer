package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ContainerSandbox struct {
	runtime string
	image   string
	workDir string
}

type ContainerConfig struct {
	Runtime string
	Image   string
	WorkDir string
}

func NewContainerSandbox(cfg ContainerConfig) (*ContainerSandbox, error) {
	runtime := cfg.Runtime
	if runtime == "" {
		runtime = detectRuntime()
	}
	if runtime == "" {
		return nil, fmt.Errorf("sandbox: no container runtime found (install docker, podman, or nerdctl)")
	}

	image := cfg.Image
	if image == "" {
		image = "alpine:latest"
	}

	return &ContainerSandbox{
		runtime: runtime,
		image:   image,
		workDir: cfg.WorkDir,
	}, nil
}

func (s *ContainerSandbox) Exec(ctx context.Context, cmd Command, policy Policy) (*Result, error) {
	if cmd.Program == "" {
		return nil, fmt.Errorf("sandbox: empty program")
	}

	timeout := policy.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	args := s.buildRunArgs(cmd, policy)
	args = append(args, cmd.Program)
	args = append(args, cmd.Args...)

	proc := exec.CommandContext(ctx, s.runtime, args...)

	if cmd.Stdin != "" {
		proc.Stdin = strings.NewReader(cmd.Stdin)
	}

	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr

	err := proc.Run()
	duration := time.Since(start)

	maxOut := policy.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = 1024 * 1024
	}

	result := &Result{
		Stdout:   truncate(stdout.String(), maxOut),
		Stderr:   truncate(stderr.String(), maxOut),
		Duration: duration,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.ExitCode = -1
			result.Error = fmt.Sprintf("container execution timed out after %s", timeout)
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

func (s *ContainerSandbox) buildRunArgs(cmd Command, policy Policy) []string {
	args := []string{
		"run",
		"--rm",
		"--read-only",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=64m",
		"--memory", "256m",
		"--cpus", "1",
		"--pids-limit", "64",
	}

	if policy.NetworkAccess == NetworkDeny {
		args = append(args, "--network", "none")
	}

	workDir := cmd.WorkDir
	if workDir == "" {
		workDir = s.workDir
	}
	if workDir != "" {
		args = append(args, "-v", fmt.Sprintf("%s:/workspace:rw", workDir))
		args = append(args, "-w", "/workspace")
	}

	for _, p := range policy.ReadOnlyPaths {
		args = append(args, "-v", fmt.Sprintf("%s:%s:ro", p, p))
	}

	for _, e := range cmd.Env {
		args = append(args, "-e", e)
	}

	args = append(args, s.image)

	return args
}

func detectRuntime() string {
	for _, rt := range []string{"docker", "podman", "nerdctl"} {
		if _, err := exec.LookPath(rt); err == nil {
			return rt
		}
	}
	return ""
}
