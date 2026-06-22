package model

import "context"

type tokenCtxKey struct{}

// WithTokenTracker attaches a per-agent TokenUsage to the context.
// chatModel.Generate/Stream will increment this tracker in addition to the global TokenTracker.
func WithTokenTracker(ctx context.Context, t *TokenUsage) context.Context {
	return context.WithValue(ctx, tokenCtxKey{}, t)
}

// TokenTrackerFromContext retrieves the per-agent TokenUsage from the context, if any.
func TokenTrackerFromContext(ctx context.Context) *TokenUsage {
	v, _ := ctx.Value(tokenCtxKey{}).(*TokenUsage)
	return v
}

type usageNotifierKey struct{}

// WithUsageNotifier attaches a callback that chatModel.Generate/Stream invokes
// after each API call's usage has been recorded. UIs use it to refresh the
// token/context display in real time during a run, not just at turn end. The
// model layer stays provider/UI-agnostic — it only fires the opaque callback.
func WithUsageNotifier(ctx context.Context, fn func()) context.Context {
	return context.WithValue(ctx, usageNotifierKey{}, fn)
}

// UsageNotifierFromContext retrieves the per-call usage notifier, if any.
func UsageNotifierFromContext(ctx context.Context) func() {
	fn, _ := ctx.Value(usageNotifierKey{}).(func())
	return fn
}
