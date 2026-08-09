package model

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/cloudwego/eino/schema"
)

type providerRequestContextKey struct{}

type providerRequestContext struct {
	sessionID      string
	subagent       bool
	agentInitiated bool
	initiator      string
}

// WithProviderSessionID associates provider requests with one persisted JCode
// session. Managed providers may use the opaque value to derive stable request
// metadata, but it is never sent upstream verbatim.
func WithProviderSessionID(ctx context.Context, sessionID string) context.Context {
	metadata := providerRequestContextFrom(ctx)
	metadata.sessionID = sessionID
	return context.WithValue(ctx, providerRequestContextKey{}, metadata)
}

// WithProviderSubagent marks model calls as delegated work. GitHub Copilot uses
// this to keep child-agent traffic inside the parent interaction instead of
// charging it as a new user-initiated request.
func WithProviderSubagent(ctx context.Context) context.Context {
	metadata := providerRequestContextFrom(ctx)
	metadata.subagent = true
	return context.WithValue(ctx, providerRequestContextKey{}, metadata)
}

// WithProviderAgentInitiated marks an internal model request, such as context
// compaction, as part of the current user interaction rather than a new user
// action. It intentionally does not imply the subagent interaction type.
func WithProviderAgentInitiated(ctx context.Context) context.Context {
	metadata := providerRequestContextFrom(ctx)
	metadata.agentInitiated = true
	return context.WithValue(ctx, providerRequestContextKey{}, metadata)
}

func withCopilotModelRequest(ctx context.Context, input []*schema.Message) context.Context {
	metadata := providerRequestContextFrom(ctx)
	metadata.initiator = copilotInitiator(input, metadata.subagent || metadata.agentInitiated)
	return context.WithValue(ctx, providerRequestContextKey{}, metadata)
}

func providerRequestContextFrom(ctx context.Context) providerRequestContext {
	if ctx == nil {
		return providerRequestContext{}
	}
	metadata, _ := ctx.Value(providerRequestContextKey{}).(providerRequestContext)
	return metadata
}

func copilotInitiator(input []*schema.Message, subagent bool) string {
	if subagent {
		return "agent"
	}
	for index := len(input) - 1; index >= 0; index-- {
		message := input[index]
		if message == nil || message.Role == schema.System {
			continue
		}
		if message.Role == schema.Tool {
			return "agent"
		}
		return "user"
	}
	return "user"
}

func copilotRequestHeaders(ctx context.Context, source map[string]string) map[string]string {
	metadata := providerRequestContextFrom(ctx)
	headers := cloneStringMap(source)
	if headers == nil {
		headers = make(map[string]string, 3)
	}
	initiator := metadata.initiator
	if initiator == "" {
		initiator = "user"
	}
	headers["x-initiator"] = initiator
	if metadata.subagent {
		headers["x-interaction-type"] = "conversation-subagent"
	} else {
		headers["x-interaction-type"] = "conversation-agent"
	}
	if interactionID := deterministicCopilotInteractionID(metadata.sessionID); interactionID != "" {
		headers["x-interaction-id"] = interactionID
	}
	return headers
}

func deterministicCopilotInteractionID(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte("interaction:" + sessionID))
	bytes := digest[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16],
	)
}
