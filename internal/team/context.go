package team

import (
	"context"
)

type contextKey string

const (
	keyTeammateIdentity contextKey = "teammate_identity"
)

// WithTeammateContext injects teammate identity into the context.
func WithTeammateContext(ctx context.Context, identity TeammateIdentity) context.Context {
	return context.WithValue(ctx, keyTeammateIdentity, identity)
}

// GetTeammateIdentity retrieves the teammate identity from the context.
func GetTeammateIdentity(ctx context.Context) (TeammateIdentity, bool) {
	id, ok := ctx.Value(keyTeammateIdentity).(TeammateIdentity)
	return id, ok
}

// IsTeammate returns true if the context belongs to a teammate (not the leader).
func IsTeammate(ctx context.Context) bool {
	_, ok := GetTeammateIdentity(ctx)
	return ok
}

// GetAgentName returns the agent name from the context, or TeamLeadName if not a teammate.
func GetAgentName(ctx context.Context) string {
	if id, ok := GetTeammateIdentity(ctx); ok {
		return id.AgentName
	}
	return TeamLeadName
}
