package agent

import (
	"context"
	"fmt"
	"sync"
)

type ApprovalMode string

const (
	ApprovalAuto ApprovalMode = "auto"
	ApprovalAsk  ApprovalMode = "ask"
	ApprovalDeny ApprovalMode = "deny"
)

type ApprovalRequest struct {
	ID        string
	SessionID string
	ToolName  string
	Input     string
}

type ApprovalResponse struct {
	RequestID string
	Approved  bool
}

type Approver struct {
	mode      ApprovalMode
	pending   map[string]chan bool
	mu        sync.Mutex
	onRequest func(req ApprovalRequest)
}

func NewApprover(mode ApprovalMode, onRequest func(ApprovalRequest)) *Approver {
	if mode == "" {
		mode = ApprovalAsk
	}
	return &Approver{
		mode:      mode,
		pending:   make(map[string]chan bool),
		onRequest: onRequest,
	}
}

func (a *Approver) RequestApproval(ctx context.Context, req ApprovalRequest) (bool, error) {
	switch a.mode {
	case ApprovalAuto:
		return true, nil
	case ApprovalDeny:
		return false, nil
	}

	ch := make(chan bool, 1)
	a.mu.Lock()
	a.pending[req.ID] = ch
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		delete(a.pending, req.ID)
		a.mu.Unlock()
	}()

	if a.onRequest != nil {
		a.onRequest(req)
	}

	select {
	case approved := <-ch:
		return approved, nil
	case <-ctx.Done():
		return false, fmt.Errorf("approval request timed out")
	}
}

func (a *Approver) Respond(resp ApprovalResponse) {
	a.mu.Lock()
	ch, ok := a.pending[resp.RequestID]
	a.mu.Unlock()

	if ok {
		select {
		case ch <- resp.Approved:
		default:
		}
	}
}
