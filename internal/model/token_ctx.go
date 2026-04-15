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
