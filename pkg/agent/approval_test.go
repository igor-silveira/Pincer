package agent

import (
	"context"
	"testing"
	"time"
)

func TestApprover_AutoMode(t *testing.T) {
	a := NewApprover(ApprovalAuto, nil)
	approved, err := a.RequestApproval(context.Background(), ApprovalRequest{ID: "1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Error("auto mode should approve")
	}
}

func TestApprover_DenyMode(t *testing.T) {
	a := NewApprover(ApprovalDeny, nil)
	approved, err := a.RequestApproval(context.Background(), ApprovalRequest{ID: "1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Error("deny mode should deny")
	}
}

func TestApprover_AskMode_Approved(t *testing.T) {
	a := NewApprover(ApprovalAsk, nil)
	req := ApprovalRequest{ID: "test-1", ToolName: "shell"}

	go func() {
		time.Sleep(10 * time.Millisecond)
		a.Respond(ApprovalResponse{RequestID: "test-1", Approved: true})
	}()

	approved, err := a.RequestApproval(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approved {
		t.Error("expected approval")
	}
}

func TestApprover_AskMode_Denied(t *testing.T) {
	a := NewApprover(ApprovalAsk, nil)
	req := ApprovalRequest{ID: "test-2", ToolName: "shell"}

	go func() {
		time.Sleep(10 * time.Millisecond)
		a.Respond(ApprovalResponse{RequestID: "test-2", Approved: false})
	}()

	approved, err := a.RequestApproval(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approved {
		t.Error("expected denial")
	}
}

func TestApprover_AskMode_Timeout(t *testing.T) {
	a := NewApprover(ApprovalAsk, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := a.RequestApproval(ctx, ApprovalRequest{ID: "timeout-1"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestApprover_AskMode_CallbackInvoked(t *testing.T) {
	var received ApprovalRequest
	a := NewApprover(ApprovalAsk, func(req ApprovalRequest) {
		received = req
	})

	go func() {
		time.Sleep(10 * time.Millisecond)
		a.Respond(ApprovalResponse{RequestID: "cb-1", Approved: true})
	}()

	req := ApprovalRequest{ID: "cb-1", SessionID: "sess-1", ToolName: "http_request", Input: `{"url":"test"}`}
	_, _ = a.RequestApproval(context.Background(), req)

	if received.ID != "cb-1" {
		t.Errorf("callback ID = %q, want %q", received.ID, "cb-1")
	}
	if received.ToolName != "http_request" {
		t.Errorf("callback ToolName = %q, want %q", received.ToolName, "http_request")
	}
}

func TestApprover_Respond_UnknownID(t *testing.T) {
	a := NewApprover(ApprovalAsk, nil)
	a.Respond(ApprovalResponse{RequestID: "nonexistent", Approved: true})
}

func TestNewApprover_DefaultMode(t *testing.T) {
	a := NewApprover("", nil)
	if a.mode != ApprovalAsk {
		t.Errorf("mode = %q, want %q", a.mode, ApprovalAsk)
	}
}
