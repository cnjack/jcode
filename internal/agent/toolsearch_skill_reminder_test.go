package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestToolSearchSkillReminderIsConditionalAndOneShot(t *testing.T) {
	deferred := &agentToolSearchTestTool{name: "computer_open"}
	middleware, err := newToolSearchSkillReminderMiddleware(context.Background(), []tool.BaseTool{deferred})
	if err != nil {
		t.Fatalf("newToolSearchSkillReminderMiddleware() error = %v", err)
	}
	if middleware == nil {
		t.Fatal("deferred catalog did not create reminder middleware")
	}

	tests := []struct {
		name      string
		messages  []*schema.Message
		toolInfos []*schema.ToolInfo
		want      bool
	}{
		{
			name: "exact hidden name",
			messages: []*schema.Message{
				schema.UserMessage("use Notes"),
				schema.ToolMessage("Use `computer_open` for native UI.", "skill-1", schema.WithToolName(loadSkillToolName)),
			},
			toolInfos: []*schema.ToolInfo{{Name: ToolSearchReservedName}},
			want:      true,
		},
		{
			name: "already visible",
			messages: []*schema.Message{
				schema.ToolMessage("Use `computer_open`.", "skill-2", schema.WithToolName(loadSkillToolName)),
			},
			toolInfos: []*schema.ToolInfo{{Name: ToolSearchReservedName}, {Name: "computer_open"}},
		},
		{
			name: "similar substring",
			messages: []*schema.Message{
				schema.ToolMessage("Use computer_open_extra.", "skill-3", schema.WithToolName(loadSkillToolName)),
			},
			toolInfos: []*schema.ToolInfo{{Name: ToolSearchReservedName}},
		},
		{
			name: "tool search unavailable",
			messages: []*schema.Message{
				schema.ToolMessage("Use computer_open.", "skill-4", schema.WithToolName(loadSkillToolName)),
			},
		},
		{
			name: "latest result is another tool",
			messages: []*schema.Message{
				schema.ToolMessage("Use computer_open.", "skill-5", schema.WithToolName(loadSkillToolName)),
				schema.AssistantMessage("", []schema.ToolCall{{ID: "exec-1"}}),
				schema.ToolMessage("ok", "exec-1", schema.WithToolName("execute")),
			},
			toolInfos: []*schema.ToolInfo{{Name: ToolSearchReservedName}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &adk.ChatModelAgentState{Messages: tt.messages, ToolInfos: tt.toolInfos}
			beforeLen := len(state.Messages)
			_, state, err = middleware.BeforeModelRewriteState(context.Background(), state, nil)
			if err != nil {
				t.Fatalf("BeforeModelRewriteState() error = %v", err)
			}
			if len(state.Messages) != beforeLen {
				t.Fatalf("routing note changed message count: before=%d after=%d", beforeLen, len(state.Messages))
			}
			got := countToolSearchSkillReminders(state.Messages) == 1
			if got != tt.want {
				t.Fatalf("reminder present = %v, want %v; messages=%#v", got, tt.want, state.Messages)
			}
			if !tt.want {
				return
			}
			trailing := trailingToolResults(state.Messages)
			if len(trailing) != 1 || trailing[0].Role != schema.Tool {
				t.Fatalf("routing note was not retained on tool result: %#v", trailing)
			}
			loaded := trailing[0]
			text := loaded.Content
			for _, wantText := range []string{"computer_open", "separate tool-call batch", "before substituting execute"} {
				if !strings.Contains(text, wantText) {
					t.Errorf("reminder missing %q: %q", wantText, text)
				}
			}
			before := loaded.Content
			_, state, err = middleware.BeforeModelRewriteState(context.Background(), state, nil)
			if err != nil {
				t.Fatalf("second BeforeModelRewriteState() error = %v", err)
			}
			if loaded.Content != before || countToolSearchSkillReminders(state.Messages) != 1 {
				t.Fatalf("second rewrite changed one-shot routing note: before=%q after=%q", before, loaded.Content)
			}
		})
	}
}

func TestToolSearchSkillReminderCoversEveryLoadSkillInTrailingBatch(t *testing.T) {
	middleware, err := newToolSearchSkillReminderMiddleware(context.Background(), []tool.BaseTool{
		&agentToolSearchTestTool{name: "computer_open"},
		&agentToolSearchTestTool{name: "browser_open"},
	})
	if err != nil {
		t.Fatalf("newToolSearchSkillReminderMiddleware() error = %v", err)
	}
	state := &adk.ChatModelAgentState{
		Messages: []*schema.Message{
			schema.AssistantMessage("", []schema.ToolCall{
				{ID: "skill-a"}, {ID: "read-a"}, {ID: "skill-b"},
			}),
			schema.ToolMessage("Use computer_open.", "skill-a", schema.WithToolName(loadSkillToolName)),
			schema.ToolMessage("ordinary result", "read-a", schema.WithToolName("read")),
			schema.ToolMessage("Use browser_open.", "skill-b", schema.WithToolName(loadSkillToolName)),
		},
		ToolInfos: []*schema.ToolInfo{{Name: ToolSearchReservedName}},
	}
	trailing := trailingToolResults(state.Messages)
	if len(trailing) != 3 {
		t.Fatalf("trailing tool results = %#v, want 3", trailing)
	}
	if !containsExactToolName(trailing[0].Content, "computer_open") ||
		!containsExactToolName(trailing[2].Content, "browser_open") {
		t.Fatalf("exact tool-name matching rejected trailing skills: %#v", trailing)
	}
	_, state, err = middleware.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState() error = %v", err)
	}
	if got := countToolSearchSkillReminders(state.Messages); got != 2 {
		t.Fatalf("processed load_skill results = %d, want 2", got)
	}
	notes := 0
	for _, message := range state.Messages {
		notes += strings.Count(message.Content, "<tool-routing-reminder>")
	}
	if notes != 1 {
		t.Fatalf("routing notes = %d, want one merged note", notes)
	}
}

func countToolSearchSkillReminders(messages []*schema.Message) int {
	count := 0
	for _, message := range messages {
		if hasToolSearchSkillReminder(message) {
			count++
		}
	}
	return count
}

func TestToolSearchSkillReminderNotBuiltWithoutDeferredTools(t *testing.T) {
	middleware, err := newToolSearchSkillReminderMiddleware(context.Background(), nil)
	if err != nil {
		t.Fatalf("newToolSearchSkillReminderMiddleware() error = %v", err)
	}
	if middleware != nil {
		t.Fatal("empty deferred catalog created reminder middleware")
	}
}
