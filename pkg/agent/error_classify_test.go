package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/igorsilveira/pincer/pkg/agent/executor"
	"github.com/igorsilveira/pincer/pkg/llm"
)

func TestClassifyToolError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want executor.ErrorKind
	}{
		{"deadline exceeded", context.DeadlineExceeded, executor.Transient},
		{"canceled", context.Canceled, executor.Transient},
		{"timeout in message", fmt.Errorf("command timeout after 30s"), executor.Transient},
		{"temporary in message", fmt.Errorf("temporary failure"), executor.Transient},
		{"connection refused", fmt.Errorf("dial tcp: connection refused"), executor.Transient},
		{"wrapped deadline", fmt.Errorf("tool exec: %w", context.DeadlineExceeded), executor.Transient},
		{"permission denied", fmt.Errorf("permission denied"), executor.Permanent},
		{"not found", fmt.Errorf("file not found"), executor.Permanent},
		{"generic error", fmt.Errorf("something broke"), executor.Permanent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executor.ClassifyError(tt.err)
			if got != tt.want {
				t.Errorf("ClassifyError(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestToolResultErrorKind(t *testing.T) {
	tr := llm.ToolResult{ToolCallID: "test", Content: "err", IsError: true}

	if tr.ErrorKind() != llm.ToolErrorPermanent {
		t.Errorf("default ErrorKind = %d, want ToolErrorPermanent (0)", tr.ErrorKind())
	}

	tr.SetErrorKind(llm.ToolErrorTransient)
	if tr.ErrorKind() != llm.ToolErrorTransient {
		t.Errorf("after SetErrorKind(Transient), ErrorKind = %d, want ToolErrorTransient (1)", tr.ErrorKind())
	}
}
