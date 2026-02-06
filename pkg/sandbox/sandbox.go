package sandbox

import (
	"context"
	"time"
)

type NetworkPolicy int

const (
	NetworkDeny NetworkPolicy = iota
	NetworkAllowList
	NetworkAllow
)

type Policy struct {
	Timeout         time.Duration
	MaxOutputBytes  int
	NetworkAccess   NetworkPolicy
	AllowedPaths    []string
	ReadOnlyPaths   []string
	RequireApproval bool
}

func DefaultPolicy() Policy {
	return Policy{
		Timeout:         30 * time.Second,
		MaxOutputBytes:  1024 * 1024,
		NetworkAccess:   NetworkDeny,
		RequireApproval: true,
	}
}

type Command struct {
	Name    string
	Program string
	Args    []string
	Stdin   string
	WorkDir string
	Env     []string
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	Error    string
}

type Sandbox interface {
	Exec(ctx context.Context, cmd Command, policy Policy) (*Result, error)
}
