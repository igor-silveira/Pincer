package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/igorsilveira/pincer/pkg/llm"
)

func TestClassifyToolError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want llm.ToolErrorKind
	}{
		{"deadline exceeded", context.DeadlineExceeded, llm.ToolErrorTransient},
		{"canceled", context.Canceled, llm.ToolErrorTransient},
		{"timeout in message", fmt.Errorf("command timeout after 30s"), llm.ToolErrorTransient},
		{"temporary in message", fmt.Errorf("temporary failure"), llm.ToolErrorTransient},
		{"connection refused", fmt.Errorf("dial tcp: connection refused"), llm.ToolErrorTransient},
		{"wrapped deadline", fmt.Errorf("tool exec: %w", context.DeadlineExceeded), llm.ToolErrorTransient},
		{"permission denied", fmt.Errorf("permission denied"), llm.ToolErrorPermanent},
		{"not found", fmt.Errorf("file not found"), llm.ToolErrorPermanent},
		{"generic error", fmt.Errorf("something broke"), llm.ToolErrorPermanent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyToolError(tt.err)
			if got != tt.want {
				t.Errorf("classifyToolError(%v) = %d, want %d", tt.err, got, tt.want)
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
