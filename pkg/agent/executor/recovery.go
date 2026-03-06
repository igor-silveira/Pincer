package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type RecoveryAction int

const (
	ActionSkip    RecoveryAction = iota
	ActionRetry
	ActionReplan
)

type RecoveryStrategy interface {
	Decide(result Result, attempt int) RecoveryAction
	Backoff(attempt int) time.Duration
}

type ErrorKind int

const (
	Permanent ErrorKind = iota
	Transient
)

func ClassifyError(err error) ErrorKind {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return Transient
	}
	msg := err.Error()
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "temporary") || strings.Contains(msg, "connection refused") {
		return Transient
	}
	return Permanent
}

type DefaultRecovery struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

func (d *DefaultRecovery) Decide(result Result, attempt int) RecoveryAction {
	if result.Err == nil {
		return ActionSkip
	}
	kind := ClassifyError(result.Err)
	if kind == Transient && attempt < d.MaxRetries {
		return ActionRetry
	}
	if kind == Transient {
		return ActionReplan
	}
	return ActionSkip
}

func (d *DefaultRecovery) Backoff(attempt int) time.Duration {
	delay := d.BaseDelay * (1 << attempt)
	if delay > d.MaxDelay {
		delay = d.MaxDelay
	}
	return delay
}

type ErrorSummary struct {
	FailedTools []FailedToolInfo
}

type FailedToolInfo struct {
	Name    string
	Error   string
	Retries int
}

func (es ErrorSummary) String() string {
	var b strings.Builder
	b.WriteString("The following tools failed after retries:\n")
	for _, f := range es.FailedTools {
		fmt.Fprintf(&b, "- %s: %q (%d retries exhausted)\n", f.Name, f.Error, f.Retries)
	}
	b.WriteString("Try a different approach. Do not re-call the same tools with identical parameters.")
	return b.String()
}
