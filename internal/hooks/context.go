package hooks

import "context"

type ctxKey int

const (
	preApprovedKey ctxKey = iota
	dispatcherKey
)

// WithDispatcher stores the session's Dispatcher on the context so the tool hook
// middleware and the runner's continuation loop can reach it without threading it
// through every signature. Command surfaces (TUI/Web/ACP) inject it into the ctx
// they hand to the agent run.
func WithDispatcher(ctx context.Context, d Dispatcher) context.Context {
	return context.WithValue(ctx, dispatcherKey, d)
}

// DispatcherFromContext returns the Dispatcher on the context, or a no-op
// dispatcher when none was injected, so callers never need a nil check.
func DispatcherFromContext(ctx context.Context) Dispatcher {
	if d, ok := ctx.Value(dispatcherKey).(Dispatcher); ok && d != nil {
		return d
	}
	return nopDispatcher{}
}

// WithPreApproved marks the context so a downstream approval gate skips its user
// prompt. The PreToolUse hook middleware sets this when a hook returns
// permissionDecision=allow; the approval layer reads it via IsPreApproved.
//
// It is intentionally per-call (context.WithValue, not persisted across
// interrupt/resume): a pre-approval only applies to the single tool invocation
// whose ctx carries it.
func WithPreApproved(ctx context.Context) context.Context {
	return context.WithValue(ctx, preApprovedKey, true)
}

// IsPreApproved reports whether a PreToolUse hook already authorized this call.
func IsPreApproved(ctx context.Context) bool {
	v, _ := ctx.Value(preApprovedKey).(bool)
	return v
}
