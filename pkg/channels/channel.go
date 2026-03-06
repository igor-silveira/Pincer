package channels

import "context"

type InboundMessage struct {
	ChannelName      string
	SessionID        string
	PeerID           string
	Content          string
	ApprovalResponse *InboundApprovalResponse
}

type OutboundMessage struct {
	SessionID string
	Content   string
}

type ChannelCaps struct {
	SupportsStreaming bool
	SupportsMedia     bool
	SupportsReactions bool
	SupportsEditing   bool
}

type ApprovalRequest struct {
	RequestID string
	SessionID string
	ToolName  string
	Input     string
}

type InboundApprovalResponse struct {
	RequestID string
	Approved  bool
}

type ApprovalSender interface {
	SendApprovalRequest(ctx context.Context, req ApprovalRequest) error
}

type TypingIndicator interface {
	SendTyping(ctx context.Context, sessionID string) error
}

type ToolPhase int

const (
	PhaseRunning ToolPhase = iota
	PhaseRetrying
	PhaseCompleted
	PhaseFailed
)

type ToolProgress struct {
	ToolName string
	Phase    ToolPhase
	Message  string
}

type ProgressRenderer interface {
	SendProgress(ctx context.Context, sessionID string, progress ToolProgress) error
}

type Adapter interface {
	Name() string

	Start(ctx context.Context) error

	Stop(ctx context.Context) error

	Send(ctx context.Context, msg OutboundMessage) error

	Receive() <-chan InboundMessage

	Capabilities() ChannelCaps
}
