package tools

import "context"

type contextKey int

const (
	ctxKeySessionID contextKey = iota
	ctxKeyAgentID
	ctxKeySubagentDepth
)

func WithSessionInfo(ctx context.Context, sessionID, agentID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeySessionID, sessionID)
	ctx = context.WithValue(ctx, ctxKeyAgentID, agentID)
	return ctx
}

func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeySessionID).(string); ok {
		return v
	}
	return ""
}

func AgentIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyAgentID).(string); ok && v != "" {
		return v
	}
	return "default"
}

func WithSubagentDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, ctxKeySubagentDepth, depth)
}

func SubagentDepthFromContext(ctx context.Context) int {
	if v, ok := ctx.Value(ctxKeySubagentDepth).(int); ok {
		return v
	}
	return 0
}
