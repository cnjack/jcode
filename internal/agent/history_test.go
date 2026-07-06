package agent

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/session"
)

// TestSyncSummarization_ReplacesHistoryWithSummaryAndTail locks the behaviour
// of the high-fidelity sync path (the only surviving auto-compaction path once
// the lossy 500-char middleware is demoted to a fallback): the synced history
// is [system summary] + verbatim tail, and the tail boundary never orphans a
// tool result from its assistant tool-call message.
func TestSyncSummarization_ReplacesHistoryWithSummaryAndTail(t *testing.T) {
	cap := &SummarizationCapture{}
	cap.Capture("the summary", 5)

	history := []adk.Message{
		schema.UserMessage("u1"),
		&schema.Message{Role: schema.Assistant, Content: "a1"},
		schema.UserMessage("u2"),
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "tc1", Function: schema.FunctionCall{Name: "read"}}}},
		schema.ToolMessage("result", "tc1", schema.WithToolName("read")),
		&schema.Message{Role: schema.Assistant, Content: "a2"},
	}

	got := SyncSummarization(cap, history, nil)

	// keepCount=2 → desired split at index 4, which is a tool result; the
	// boundary walks back to index 3 so the assistant tool-call comes along.
	if len(got) != 4 {
		t.Fatalf("synced history length = %d, want 4 (summary + 3 tail)", len(got))
	}
	if got[0].Role != schema.System || !strings.Contains(got[0].Content, "the summary") {
		t.Errorf("got[0] = %v %q, want system message containing the summary", got[0].Role, got[0].Content)
	}
	if got[1].Role != schema.Assistant || len(got[1].ToolCalls) != 1 {
		t.Errorf("got[1] = %v (toolcalls=%d), want assistant with its tool call preserved", got[1].Role, len(got[1].ToolCalls))
	}
	if got[2].Role != schema.Tool {
		t.Errorf("got[2].Role = %v, want tool", got[2].Role)
	}
	if got[3].Role != schema.Assistant || got[3].Content != "a2" {
		t.Errorf("got[3] = %v %q, want assistant %q", got[3].Role, got[3].Content, "a2")
	}

	// The capture must be drained: a second sync is a no-op.
	again := SyncSummarization(cap, got, nil)
	if len(again) != len(got) {
		t.Errorf("second sync changed history: %d → %d messages", len(got), len(again))
	}
}

// TestSyncSummarization_RecordsKeptN verifies the compact event persisted to
// the session file carries the number of tail messages kept verbatim, so a
// later resume can re-attach the same tail instead of dropping it.
func TestSyncSummarization_RecordsKeptN(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	rec, err := session.NewRecorder("/proj/kept-n", "prov", "model")
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	defer rec.Close()

	cap := &SummarizationCapture{}
	cap.Capture("sum", 5)

	history := []adk.Message{
		schema.UserMessage("u1"),
		&schema.Message{Role: schema.Assistant, Content: "a1"},
		schema.UserMessage("u2"),
		&schema.Message{Role: schema.Assistant, Content: "a2"},
	}
	_ = SyncSummarization(cap, history, rec)

	entries, err := session.LoadSession(rec.UUID())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	var compact *session.Entry
	for i := range entries {
		if entries[i].Type == session.EntryCompact {
			compact = &entries[i]
			break
		}
	}
	if compact == nil {
		t.Fatal("no compact entry recorded")
	}
	if compact.Summary != "sum" {
		t.Errorf("Summary = %q, want %q", compact.Summary, "sum")
	}
	if compact.CompactedN != 5 {
		t.Errorf("CompactedN = %d, want 5", compact.CompactedN)
	}
	if compact.KeptN != 2 {
		t.Errorf("KeptN = %d, want 2", compact.KeptN)
	}
}
