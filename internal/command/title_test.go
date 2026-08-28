package command

import (
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// TestTitleTurnsFromHistory locks the leak boundary for /rename suggestions:
// only user and assistant text reaches the title model. System prompts, tool
// calls and tool results (MCP servers, teammates, Guardian) are dropped.
func TestTitleTurnsFromHistory(t *testing.T) {
	history := []adk.Message{
		schema.SystemMessage("big internal system prompt"),
		schema.UserMessage("fix the login timeout"),
		&schema.Message{Role: schema.Assistant, Content: "", ToolCalls: []schema.ToolCall{
			{ID: "1", Function: schema.FunctionCall{Name: "read", Arguments: `{"path":"/etc/secrets"}`}},
		}},
		&schema.Message{Role: schema.Tool, Content: "tool output with credentials"},
		schema.AssistantMessage("fixed the pool leak", nil),
	}
	turns := titleTurnsFromHistory(history)
	if len(turns) != 2 {
		t.Fatalf("want 2 turns (user+assistant text), got %d: %+v", len(turns), turns)
	}
	if turns[0].Role != "user" || turns[0].Content != "fix the login timeout" {
		t.Errorf("turn 0: %+v", turns[0])
	}
	if turns[1].Role != "assistant" || turns[1].Content != "fixed the pool leak" {
		t.Errorf("turn 1: %+v", turns[1])
	}
}
