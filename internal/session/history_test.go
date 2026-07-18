package session

import (
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// assertToolCallInvariant fails the test when any assistant message carries a
// tool_call that is not answered by a tool message in the group immediately
// following it — the exact condition model APIs reject with a 400.
func assertToolCallInvariant(t *testing.T, msgs []adk.Message) {
	t.Helper()
	for i, m := range msgs {
		if m.Role != schema.Assistant || len(m.ToolCalls) == 0 {
			continue
		}
		answered := make(map[string]bool, len(m.ToolCalls))
		for j := i + 1; j < len(msgs) && msgs[j].Role == schema.Tool; j++ {
			answered[msgs[j].ToolCallID] = true
		}
		for _, tc := range m.ToolCalls {
			if !answered[tc.ID] {
				t.Errorf("msg[%d]: tool_call %s(%s) has no answering tool message", i, tc.Function.Name, tc.ID)
			}
		}
	}
}

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

// TestReconstructState_ParallelToolResultInSubagentWindow reproduces the
// session-86e09b12 failure: the main agent issued grep+subagent in one
// parallel batch and the fast grep's result landed between subagent_start and
// subagent_result. The old subagentDepth skip window swallowed it, leaving a
// dangling grep tool_call the model API rejected on resume ("tool_call_ids
// did not have response messages: grep:53").
func TestReconstructState_ParallelToolResultInSubagentWindow(t *testing.T) {
	entries := []Entry{
		{Type: EntryUser, Content: "u"},
		{Type: EntryToolCall, Name: "grep", Args: "{}", ToolCallID: "c-grep"},
		{Type: EntryToolCall, Name: "subagent", Args: "{}", ToolCallID: "c-sub"},
		{Type: EntrySubagentStart, SubagentName: "s", SubagentType: "explore"},
		{Type: EntryToolResult, Name: "grep", Output: "grep-out", ToolCallID: "c-grep"},
		{Type: EntrySubagentResult, SubagentName: "s", Output: "sub-out"},
		{Type: EntryToolResult, Name: "subagent", Output: "sub-out", ToolCallID: "c-sub"},
		{Type: EntryAssistant, Content: "done"},
	}
	state := ReconstructState(entries)

	assertToolCallInvariant(t, state.History)
	// Both results must survive, in recorded order, right after the assistant.
	if len(state.History) != 5 {
		t.Fatalf("History length = %d, want 5 (user, assistant, 2 tool results, assistant)", len(state.History))
	}
	if state.History[2].Role != schema.Tool || state.History[2].ToolCallID != "c-grep" || state.History[2].Content != "grep-out" {
		t.Errorf("History[2] = %v %q, want grep tool result", state.History[2].Role, state.History[2].Content)
	}
	if state.History[3].Role != schema.Tool || state.History[3].ToolCallID != "c-sub" {
		t.Errorf("History[3] = %v, want subagent tool result", state.History[3].Role)
	}
}

// TestReconstructState_BackfillsInterruptedToolCall covers a session recorded
// mid-run (user stop / process kill): the tool_call is on disk but its result
// never arrived. Reconstruction must insert a placeholder tool message so the
// rebuilt history satisfies the model API's tool-call invariant.
func TestReconstructState_BackfillsInterruptedToolCall(t *testing.T) {
	entries := []Entry{
		{Type: EntryUser, Content: "u"},
		{Type: EntryToolCall, Name: "grep", Args: "{}", ToolCallID: "c1"},
		{Type: EntryToolCall, Name: "execute", Args: "{}", ToolCallID: "c2"},
		{Type: EntryToolResult, Name: "grep", Output: "ok", ToolCallID: "c1"},
		// execute never produced a result.
	}
	state := ReconstructState(entries)

	assertToolCallInvariant(t, state.History)
	if len(state.History) != 4 {
		t.Fatalf("History length = %d, want 4 (user, assistant, 2 tool messages)", len(state.History))
	}
	fill := state.History[3]
	if fill.Role != schema.Tool || fill.ToolCallID != "c2" || fill.ToolName != "execute" {
		t.Errorf("backfilled message = %v name=%q id=%q, want execute tool message for c2", fill.Role, fill.ToolName, fill.ToolCallID)
	}
	if fill.Content != InterruptedToolOutput {
		t.Errorf("backfilled content = %q, want %q", fill.Content, InterruptedToolOutput)
	}
}

// TestReconstructState_BackfillPreservesFollowingMessages ensures the
// placeholder lands inside the right tool group when later turns exist.
func TestReconstructState_BackfillPreservesFollowingMessages(t *testing.T) {
	entries := []Entry{
		{Type: EntryUser, Content: "u1"},
		{Type: EntryToolCall, Name: "grep", Args: "{}", ToolCallID: "c1"},
		// interrupted here; then the session was resumed and continued.
		{Type: EntryUser, Content: "u2"},
		{Type: EntryAssistant, Content: "a2"},
	}
	state := ReconstructState(entries)

	assertToolCallInvariant(t, state.History)
	want := []struct {
		role    schema.RoleType
		content string
	}{
		{schema.User, "u1"},
		{schema.Assistant, ""},
		{schema.Tool, InterruptedToolOutput},
		{schema.User, "u2"},
		{schema.Assistant, "a2"},
	}
	if len(state.History) != len(want) {
		t.Fatalf("History length = %d, want %d", len(state.History), len(want))
	}
	for i, w := range want {
		if state.History[i].Role != w.role || state.History[i].Content != w.content {
			t.Errorf("History[%d] = %v %q, want %v %q", i, state.History[i].Role, state.History[i].Content, w.role, w.content)
		}
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
