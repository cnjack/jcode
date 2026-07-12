package agent

import "context"

// toolCallIDKey carries the LLM tool-call id through the tool-execution
// context chain. The approval middleware stamps it before invoking the
// ApprovalFunc so approval-layer bookkeeping (denied flag, wait-time
// accounting) can be keyed per call — the ApprovalFunc signature
// (ctx, toolName, toolArgs) carries no call identity of its own, and
// name+args matching is ambiguous for concurrent identical calls.
type toolCallIDKey struct{}

// WithToolCallID returns a context carrying the tool-call id of the
// invocation currently flowing through the middleware chain.
func WithToolCallID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, toolCallIDKey{}, id)
}

// ToolCallIDFromContext returns the tool-call id stamped by the approval
// middleware, or "" when the context is not part of a tool invocation.
func ToolCallIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(toolCallIDKey{}).(string)
	return id
}
