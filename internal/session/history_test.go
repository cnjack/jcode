package session

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

// TestReconstructState_CompactKeepsTail verifies that replaying a compact entry
// keeps the KeptN most recent messages after the summary, matching what the
// live agent kept in memory at compaction time (previously the tail was
// silently dropped on resume).
func TestReconstructState_CompactKeepsTail(t *testing.T) {
	entries := []Entry{
		{Type: EntryUser, Content: "u1"},
		{Type: EntryAssistant, Content: "a1"},
		{Type: EntryUser, Content: "u2"},
		{Type: EntryAssistant, Content: "a2"},
		{Type: EntryCompact, Summary: "S", KeptN: 2},
	}
	state := ReconstructState(entries)

	if len(state.History) != 3 {
		t.Fatalf("History length = %d, want 3 (summary + 2 kept)", len(state.History))
	}
	if state.History[0].Role != schema.System || state.History[0].Content != "S" {
		t.Errorf("History[0] = %v %q, want system summary %q", state.History[0].Role, state.History[0].Content, "S")
	}
	if state.History[1].Role != schema.User || state.History[1].Content != "u2" {
		t.Errorf("History[1] = %v %q, want user %q", state.History[1].Role, state.History[1].Content, "u2")
	}
	if state.History[2].Role != schema.Assistant || state.History[2].Content != "a2" {
		t.Errorf("History[2] = %v %q, want assistant %q", state.History[2].Role, state.History[2].Content, "a2")
	}
}

// TestReconstructState_CompactLegacyEntry locks backward compatibility: old
// session files carry no kept_n field (unmarshals to 0), and must replay
// exactly as before — summary only, no tail.
func TestReconstructState_CompactLegacyEntry(t *testing.T) {
	entries := []Entry{
		{Type: EntryUser, Content: "u1"},
		{Type: EntryAssistant, Content: "a1"},
		{Type: EntryCompact, Summary: "S"},
	}
	state := ReconstructState(entries)

	if len(state.History) != 1 {
		t.Fatalf("History length = %d, want 1 (legacy compact keeps nothing)", len(state.History))
	}
	if state.History[0].Role != schema.System || state.History[0].Content != "S" {
		t.Errorf("History[0] = %v %q, want system summary %q", state.History[0].Role, state.History[0].Content, "S")
	}
}

// TestReconstructState_CompactToolBoundary verifies the tail boundary walks
// backwards past tool results so a Tool message never appears without the
// assistant message carrying its tool call.
func TestReconstructState_CompactToolBoundary(t *testing.T) {
	entries := []Entry{
		{Type: EntryUser, Content: "u1"},
		{Type: EntryToolCall, Name: "read", Args: "{}", ToolCallID: "tc1"},
		{Type: EntryToolResult, Name: "read", Output: "file contents", ToolCallID: "tc1"},
		{Type: EntryCompact, Summary: "S", KeptN: 1},
	}
	state := ReconstructState(entries)

	// Accumulated msgs before compact: [user, assistant(tool_call), tool].
	// KeptN=1 lands on the tool result; the boundary must back up to include
	// the assistant tool-call message.
	if len(state.History) != 3 {
		t.Fatalf("History length = %d, want 3 (summary + assistant tool-call + tool result)", len(state.History))
	}
	if state.History[0].Role != schema.System || state.History[0].Content != "S" {
		t.Errorf("History[0] = %v %q, want system summary", state.History[0].Role, state.History[0].Content)
	}
	if state.History[1].Role != schema.Assistant || len(state.History[1].ToolCalls) != 1 {
		t.Errorf("History[1] = %v (toolcalls=%d), want assistant with 1 tool call", state.History[1].Role, len(state.History[1].ToolCalls))
	}
	if state.History[2].Role != schema.Tool {
		t.Errorf("History[2].Role = %v, want tool", state.History[2].Role)
	}
}

// TestReconstructState_CompactKeptNOverflow verifies a KeptN larger than the
// accumulated message count is clamped instead of panicking, keeping all
// messages after the summary.
func TestReconstructState_CompactKeptNOverflow(t *testing.T) {
	entries := []Entry{
		{Type: EntryUser, Content: "u1"},
		{Type: EntryAssistant, Content: "a1"},
		{Type: EntryCompact, Summary: "S", KeptN: 10},
	}
	state := ReconstructState(entries)

	if len(state.History) != 3 {
		t.Fatalf("History length = %d, want 3 (summary + all 2 messages)", len(state.History))
	}
	if state.History[0].Content != "S" {
		t.Errorf("History[0].Content = %q, want %q", state.History[0].Content, "S")
	}
	if state.History[1].Content != "u1" || state.History[2].Content != "a1" {
		t.Errorf("tail = %q,%q, want u1,a1", state.History[1].Content, state.History[2].Content)
	}
}

func TestPruneOldToolOutputsClearsScreenshotPixels(t *testing.T) {
	encoded := "base64-must-not-survive"
	shot := schema.ToolMessage("", "shot-call", schema.WithToolName("computer_screenshot"))
	shot.UserInputMultiContent = []schema.MessageInputPart{
		{Type: schema.ChatMessagePartTypeText, Text: "image_ref=/api/computer/shots/old.png"},
		{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
				MIMEType:   "image/png",
				Base64Data: &encoded,
			}},
		},
	}
	msgs := []*schema.Message{
		schema.UserMessage("old request"),
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "shot-call", Function: schema.FunctionCall{Name: "computer_screenshot"}}}},
		shot,
		schema.UserMessage("new request"),
	}

	PruneOldToolOutputs(msgs, 1)
	if shot.Content != "[Screenshot was captured previously. Run computer_screenshot again for current visual state.]" {
		t.Fatalf("unexpected screenshot placeholder: %q", shot.Content)
	}
	if shot.UserInputMultiContent != nil {
		t.Fatalf("old screenshot pixels survived pruning: %#v", shot.UserInputMultiContent)
	}
}

func TestPruneOldToolOutputsPreservesRecentScreenshot(t *testing.T) {
	shot := schema.ToolMessage("", "shot-call", schema.WithToolName("computer_screenshot"))
	shot.UserInputMultiContent = []schema.MessageInputPart{
		{Type: schema.ChatMessagePartTypeText, Text: "current shot"},
	}
	msgs := []*schema.Message{
		schema.UserMessage("old request"),
		schema.UserMessage("current request"),
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "shot-call", Function: schema.FunctionCall{Name: "computer_screenshot"}}}},
		shot,
	}

	PruneOldToolOutputs(msgs, 1)
	if shot.Content != "" || len(shot.UserInputMultiContent) != 1 {
		t.Fatalf("recent screenshot was pruned: %#v", shot)
	}
}
