package model

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestCopilotRequestHeadersClassifyUserToolAndSubagent(t *testing.T) {
	sessionContext := WithProviderSessionID(context.Background(), "session-123")
	userContext := withCopilotModelRequest(sessionContext, []*schema.Message{
		schema.UserMessage("start"),
	})
	userHeaders := copilotRequestHeaders(userContext, nil)
	if userHeaders["x-initiator"] != "user" {
		t.Fatalf("user initiator = %q", userHeaders["x-initiator"])
	}
	if userHeaders["x-interaction-type"] != "conversation-agent" {
		t.Fatalf("user interaction type = %q", userHeaders["x-interaction-type"])
	}
	interactionID := userHeaders["x-interaction-id"]
	if interactionID == "" {
		t.Fatal("expected stable interaction id")
	}

	toolContext := withCopilotModelRequest(sessionContext, []*schema.Message{
		schema.UserMessage("start"),
		schema.ToolMessage("result", "call-1"),
	})
	toolHeaders := copilotRequestHeaders(toolContext, nil)
	if toolHeaders["x-initiator"] != "agent" {
		t.Fatalf("tool initiator = %q", toolHeaders["x-initiator"])
	}
	if toolHeaders["x-interaction-id"] != interactionID {
		t.Fatalf("interaction id changed: %q != %q", toolHeaders["x-interaction-id"], interactionID)
	}

	subagentContext := WithProviderSubagent(sessionContext)
	subagentContext = withCopilotModelRequest(subagentContext, []*schema.Message{
		schema.UserMessage("delegated task"),
	})
	subagentHeaders := copilotRequestHeaders(subagentContext, nil)
	if subagentHeaders["x-initiator"] != "agent" {
		t.Fatalf("subagent initiator = %q", subagentHeaders["x-initiator"])
	}
	if subagentHeaders["x-interaction-type"] != "conversation-subagent" {
		t.Fatalf("subagent interaction type = %q", subagentHeaders["x-interaction-type"])
	}

	compactContext := WithProviderAgentInitiated(sessionContext)
	compactContext = withCopilotModelRequest(compactContext, []*schema.Message{
		schema.UserMessage("internal summary prompt"),
	})
	compactHeaders := copilotRequestHeaders(compactContext, nil)
	if compactHeaders["x-initiator"] != "agent" {
		t.Fatalf("compact initiator = %q", compactHeaders["x-initiator"])
	}
	if compactHeaders["x-interaction-type"] != "conversation-agent" {
		t.Fatalf("compact interaction type = %q", compactHeaders["x-interaction-type"])
	}
}

func TestDeterministicCopilotInteractionID(t *testing.T) {
	first := deterministicCopilotInteractionID("session-a")
	if first == "" || first != deterministicCopilotInteractionID("session-a") {
		t.Fatalf("interaction id is not stable: %q", first)
	}
	if first == deterministicCopilotInteractionID("session-b") {
		t.Fatal("different sessions produced the same interaction id")
	}
	if deterministicCopilotInteractionID("") != "" {
		t.Fatal("empty session must not create a fragmented interaction id")
	}
}
