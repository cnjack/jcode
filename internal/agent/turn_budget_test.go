package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const (
	headSentinel = "HEAD_SENTINEL_"
	tailSentinel = "_TAIL_SENTINEL"
)

// sentinelContent builds an ASCII payload of exactly n bytes with recognizable
// head/tail markers so tests can verify edge preservation after truncation.
func sentinelContent(n int) string {
	return headSentinel + strings.Repeat("x", n-len(headSentinel)-len(tailSentinel)) + tailSentinel
}

func toolResult(id, name, content string) *schema.Message {
	return &schema.Message{Role: schema.Tool, ToolCallID: id, ToolName: name, Content: content}
}

// assistantCalls builds the assistant message that issued the batch, mapping
// call IDs to tool names (pairs: id1, name1, id2, name2, ...).
func assistantCalls(pairs ...string) *schema.Message {
	msg := &schema.Message{Role: schema.Assistant}
	for i := 0; i+1 < len(pairs); i += 2 {
		msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{
			ID:       pairs[i],
			Function: schema.FunctionCall{Name: pairs[i+1]},
		})
	}
	return msg
}

// runTurnBudget drives BeforeModelRewriteState once, mirroring how the eino
// runner would invoke the middleware before a model call.
func runTurnBudget(t *testing.T, maxChars int, msgs []*schema.Message) []*schema.Message {
	t.Helper()
	mw, ok := NewTurnToolResultBudgetMiddleware(maxChars).(*turnBudgetMiddleware)
	if !ok {
		t.Fatalf("NewTurnToolResultBudgetMiddleware returned unexpected type")
	}
	state := &adk.ChatModelAgentState{Messages: msgs}
	_, state, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState: %v", err)
	}
	return state.Messages
}

// Under budget, every byte must survive untouched.
func TestTurnBudget_NoopUnderBudget(t *testing.T) {
	contents := []string{sentinelContent(10000), sentinelContent(10000), sentinelContent(10000)}
	msgs := []*schema.Message{
		{Role: schema.User, Content: "go"},
		assistantCalls("c1", "read", "c2", "read", "c3", "grep"),
		toolResult("c1", "read", contents[0]),
		toolResult("c2", "read", contents[1]),
		toolResult("c3", "grep", contents[2]),
	}
	out := runTurnBudget(t, 0, msgs) // 0 → default budget (150k)
	if len(out) != len(msgs) {
		t.Fatalf("message count changed: %d -> %d", len(msgs), len(out))
	}
	for i, want := range contents {
		if got := out[i+2].Content; got != want {
			t.Fatalf("message %d content changed under budget", i+2)
		}
	}
}

// Over budget, truncation starts with the largest result and stops as soon as
// the batch fits; head/tail edges and an explanatory marker must be kept, and
// the original messages must not be mutated in place (they may be shared with
// the session history).
func TestTurnBudget_TruncatesLargestFirstToBudget(t *testing.T) {
	sizes := []int{49000, 48000, 47000, 46000}
	msgs := []*schema.Message{
		assistantCalls("c1", "read", "c2", "read", "c3", "read", "c4", "read"),
	}
	originals := make([]string, len(sizes))
	for i, n := range sizes {
		originals[i] = sentinelContent(n)
		msgs = append(msgs, toolResult(fmt.Sprintf("c%d", i+1), "read", originals[i]))
	}

	out := runTurnBudget(t, 100000, msgs)

	sum := 0
	for i := 1; i < len(out); i++ {
		sum += len(out[i].Content)
	}
	if sum > 100000 {
		t.Fatalf("batch still over budget after truncation: %d > 100000", sum)
	}

	// Largest-first: the smallest (46k) result must survive intact.
	if out[4].Content != originals[3] {
		t.Errorf("smallest result was truncated before budget required it")
	}
	// The three larger ones are truncated with marker + preserved edges.
	for i := 1; i <= 3; i++ {
		got := out[i].Content
		size := sizes[i-1]
		if got == originals[i-1] {
			t.Fatalf("result %d (size %d) not truncated", i, size)
		}
		if !strings.Contains(got, "truncated by per-turn budget") {
			t.Errorf("result %d missing truncation marker", i)
		}
		if want := fmt.Sprintf("dropped %d of %d bytes", size-4000, size); !strings.Contains(got, want) {
			t.Errorf("result %d marker missing %q:\n%.300s", i, want, got)
		}
		if !strings.HasPrefix(got, headSentinel) {
			t.Errorf("result %d head edge not preserved", i)
		}
		if !strings.HasSuffix(got, tailSentinel) {
			t.Errorf("result %d tail edge not preserved", i)
		}
	}
	// Copy-on-write: caller's message objects are untouched.
	for i, want := range originals {
		if msgs[i+1].Content != want {
			t.Fatalf("original message %d mutated in place", i+1)
		}
	}
}

// Only the trailing batch (the results about to be sent for the first time) is
// governed; older history is reduction's job.
func TestTurnBudget_OnlyTrailingBatch(t *testing.T) {
	oldBig1 := sentinelContent(60000)
	oldBig2 := sentinelContent(60000)
	newSmall := sentinelContent(10000)
	msgs := []*schema.Message{
		{Role: schema.User, Content: "go"},
		assistantCalls("o1", "read"),
		toolResult("o1", "read", oldBig1),
		assistantCalls("o2", "read"),
		toolResult("o2", "read", oldBig2),
		assistantCalls("c1", "read"),
		toolResult("c1", "read", newSmall),
	}
	out := runTurnBudget(t, 50000, msgs)
	if out[2].Content != oldBig1 || out[4].Content != oldBig2 {
		t.Fatal("historical tool results were modified; only the trailing batch may be touched")
	}
	if out[6].Content != newSmall {
		t.Fatal("trailing result under budget was modified")
	}
}

// Excluded tools (ask_user, load_skill) carry irreplaceable user input and
// must never be truncated, even when they alone blow the budget.
func TestTurnBudget_ExcludedToolsUntouched(t *testing.T) {
	askContent := sentinelContent(200000)
	readContent := sentinelContent(100000)
	msgs := []*schema.Message{
		assistantCalls("c1", "ask_user", "c2", "read"),
		toolResult("c1", "ask_user", askContent),
		toolResult("c2", "read", readContent),
	}
	out := runTurnBudget(t, 50000, msgs)
	if out[1].Content != askContent {
		t.Fatal("ask_user result was truncated; excluded tools must be untouched")
	}
	if out[2].Content == readContent || !strings.Contains(out[2].Content, "truncated by per-turn budget") {
		t.Fatal("read result should have been truncated")
	}
}

// A single giant result collapses to head+marker+tail.
func TestTurnBudget_SingleGiantResult(t *testing.T) {
	giant := sentinelContent(300000)
	msgs := []*schema.Message{
		assistantCalls("c1", "execute"),
		toolResult("c1", "execute", giant),
	}
	out := runTurnBudget(t, 0, msgs) // default budget 150k
	got := out[1].Content
	if len(got) >= 10000 {
		t.Fatalf("giant result not collapsed: still %d bytes", len(got))
	}
	if want := fmt.Sprintf("dropped %d of %d bytes", 300000-4000, 300000); !strings.Contains(got, want) {
		t.Fatalf("marker missing %q:\n%.300s", want, got)
	}
	if !strings.HasPrefix(got, headSentinel) || !strings.HasSuffix(got, tailSentinel) {
		t.Fatal("head/tail edges not preserved")
	}
}

// Truncation must not disturb the tool_call/result pairing the provider
// validates: same message count, same order, same ToolCallIDs.
func TestTurnBudget_ToolCallPairingPreserved(t *testing.T) {
	msgs := []*schema.Message{
		assistantCalls("c1", "read", "c2", "read", "c3", "read"),
		toolResult("c1", "read", sentinelContent(60000)),
		toolResult("c2", "read", sentinelContent(60000)),
		toolResult("c3", "read", sentinelContent(60000)),
	}
	out := runTurnBudget(t, 100000, msgs)
	if len(out) != 4 {
		t.Fatalf("message count changed: %d", len(out))
	}
	if len(out[0].ToolCalls) != 3 {
		t.Fatalf("assistant tool calls changed: %d", len(out[0].ToolCalls))
	}
	for i, wantID := range []string{"c1", "c2", "c3"} {
		if out[i+1].Role != schema.Tool || out[i+1].ToolCallID != wantID {
			t.Fatalf("pairing broken at %d: role=%s id=%s", i+1, out[i+1].Role, out[i+1].ToolCallID)
		}
		if out[0].ToolCalls[i].ID != wantID {
			t.Fatalf("assistant call order changed at %d", i)
		}
	}
}
