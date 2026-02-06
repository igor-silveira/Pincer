package channels

import "context"

type InboundMessage struct {
	ChannelName string
	SessionID   string
	PeerID      string
	Content     string
}

type OutboundMessage struct {
	SessionID string
	Content   string
}

type ChannelCaps struct {
	SupportsStreaming bool
	SupportsMedia     bool
	SupportsReactions bool
}

type Adapter interface {
	Name() string

	Start(ctx context.Context) error

	Stop(ctx context.Context) error

	Send(ctx context.Context, msg OutboundMessage) error

	Receive() <-chan InboundMessage

	Capabilities() ChannelCaps
}
