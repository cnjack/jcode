package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	internalmodel "github.com/cnjack/jcode/internal/model"
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

func screenshotToolResult(id, ref, pixels string) *schema.Message {
	msg := schema.ToolMessage("", id, schema.WithToolName("computer_screenshot"))
	msg.UserInputMultiContent = []schema.MessageInputPart{
		{Type: schema.ChatMessagePartTypeText, Text: "image_ref=" + ref},
		{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
				MIMEType: "image/png", Base64Data: &pixels,
			}},
		},
	}
	return msg
}

func retainedToolImageBytes(msgs []*schema.Message) int {
	total := 0
	for _, msg := range msgs {
		if msg == nil || msg.Role != schema.Tool {
			continue
		}
		for _, part := range msg.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeImageURL && part.Image != nil && part.Image.Base64Data != nil {
				total += len(*part.Image.Base64Data)
			}
		}
	}
	return total
}

func retainedToolImageCount(msgs []*schema.Message) int {
	total := 0
	for _, msg := range msgs {
		if msg == nil || msg.Role != schema.Tool {
			continue
		}
		for _, part := range msg.UserInputMultiContent {
			if part.Type == schema.ChatMessagePartTypeImageURL && part.Image != nil {
				total++
			}
		}
	}
	return total
}

func multiContentText(msg *schema.Message) string {
	var values []string
	for _, part := range msg.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText && part.Text != "" {
			values = append(values, part.Text)
		}
	}
	return strings.Join(values, "\n")
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

func TestTurnBudget_ReleasesConsumedScreenshotImagesCopyOnWrite(t *testing.T) {
	// Large enough to make the retained-byte assertion meaningful without making
	// the test itself expensive. Four images enter; only the two in the trailing
	// unconsumed batch may remain in the active model state.
	pixels := strings.Repeat("A", 256<<10)
	old1 := screenshotToolResult("old-1", "/shots/old-1.png", pixels)
	old2 := screenshotToolResult("old-2", "/shots/old-2.png", pixels)
	current1 := screenshotToolResult("new-1", "/shots/new-1.png", pixels)
	current2 := screenshotToolResult("new-2", "/shots/new-2.png", pixels)
	msgs := []*schema.Message{
		schema.UserMessage("inspect twice"),
		assistantCalls("old-1", "computer_screenshot"),
		old1,
		schema.AssistantMessage("first image consumed", nil),
		assistantCalls("old-2", "computer_screenshot"),
		old2,
		schema.UserMessage("continue"),
		assistantCalls("new-1", "computer_screenshot", "read-1", "read", "new-2", "computer_screenshot"),
		current1,
		toolResult("read-1", "read", "parallel text result"),
		current2,
		schema.SystemMessage("fresh tool-loop reminder"),
	}

	beforeBytes := retainedToolImageBytes(msgs)
	out := runTurnBudget(t, 1_000_000, msgs)
	if beforeBytes != 4*len(pixels) {
		t.Fatalf("fixture retained bytes=%d, want %d", beforeBytes, 4*len(pixels))
	}
	if got := retainedToolImageBytes(out); got != 2*len(pixels) {
		t.Fatalf("active history retained %d image bytes, want only trailing %d", got, 2*len(pixels))
	}

	// Consumed messages are clones reduced to their text/image_ref.
	for _, tc := range []struct {
		idx  int
		orig *schema.Message
		ref  string
	}{{2, old1, "/shots/old-1.png"}, {5, old2, "/shots/old-2.png"}} {
		got := out[tc.idx]
		if got == tc.orig {
			t.Fatalf("consumed screenshot %d was mutated in place", tc.idx)
		}
		if len(got.UserInputMultiContent) != 0 || !strings.Contains(got.Content, tc.ref) {
			t.Fatalf("consumed screenshot %d not reduced to text ref: %#v", tc.idx, got)
		}
		if len(tc.orig.UserInputMultiContent) != 2 {
			t.Fatalf("copy-on-write mutated original screenshot %d", tc.idx)
		}
	}

	// The upcoming call still needs every image in its trailing parallel batch;
	// unrelated messages and the caller's backing slice also remain untouched.
	if out[8] != current1 || out[10] != current2 || out[9] != msgs[9] || out[11] != msgs[11] {
		t.Fatal("trailing batch or unrelated message identity changed")
	}
	if &out[0] == &msgs[0] {
		t.Fatal("image release reused the caller's message-slice backing array")
	}
}

func TestTurnBudget_ReleasesAllImagesWhenNoToolBatchIsPending(t *testing.T) {
	shot := screenshotToolResult("shot", "/shots/consumed.png", "pixels")
	msgs := []*schema.Message{
		assistantCalls("shot", "computer_screenshot"),
		shot,
		schema.AssistantMessage("done inspecting", nil),
	}
	out := runTurnBudget(t, 1000, msgs)
	if retainedToolImageBytes(out) != 0 {
		t.Fatal("a consumed image survived with no pending tool batch")
	}
	if out[1] == shot || !strings.Contains(out[1].Content, "image_ref=/shots/consumed.png") ||
		!strings.Contains(out[1].Content, "no longer attached") {
		t.Fatalf("consumed image was not copy-on-write reduced: %#v", out[1])
	}
}

func TestTurnBudget_AppliesToPendingBatchBeforeSystemReminder(t *testing.T) {
	large := sentinelContent(60_000)
	msgs := []*schema.Message{
		assistantCalls("c1", "read"),
		toolResult("c1", "read", large),
		schema.SystemMessage("fresh reminder"),
	}
	out := runTurnBudget(t, 50_000, msgs)
	if out[1].Content == large || !strings.Contains(out[1].Content, "truncated by per-turn budget") {
		t.Fatal("a trailing system reminder hid the pending tool batch from budgeting")
	}
	if out[2] != msgs[2] {
		t.Fatal("system reminder was rewritten")
	}
}

func TestTurnBudget_CapsParallelTrailingScreenshotImagesInState(t *testing.T) {
	const encodedPixel = "AAAA" // three decoded bytes
	msgs := []*schema.Message{assistantCalls(
		"shot-1", "computer_screenshot",
		"shot-2", "computer_screenshot",
		"shot-3", "computer_screenshot",
		"shot-4", "computer_screenshot",
		"shot-5", "computer_screenshot",
		"shot-6", "computer_screenshot",
	)}
	for i := 1; i <= internalmodel.MaxModelImagesPerRequest+2; i++ {
		msgs = append(msgs, screenshotToolResult(
			fmt.Sprintf("shot-%d", i),
			fmt.Sprintf("/shots/%d.png", i),
			encodedPixel,
		))
	}

	out := runTurnBudget(t, 1_000_000, msgs)
	if got := retainedToolImageCount(out); got != internalmodel.MaxModelImagesPerRequest {
		t.Fatalf("live state retained %d trailing images, want %d", got, internalmodel.MaxModelImagesPerRequest)
	}
	if got := retainedToolImageBytes(out); got != internalmodel.MaxModelImagesPerRequest*len(encodedPixel) {
		t.Fatalf("live state retained %d encoded bytes after count cap", got)
	}

	for i := 1; i <= internalmodel.MaxModelImagesPerRequest; i++ {
		if out[i] != msgs[i] {
			t.Fatalf("admitted screenshot %d was unnecessarily cloned", i)
		}
	}
	for i := internalmodel.MaxModelImagesPerRequest + 1; i < len(out); i++ {
		if out[i] == msgs[i] {
			t.Fatalf("omitted screenshot %d was mutated in place", i)
		}
		if hasToolImage(out[i]) {
			t.Fatalf("over-budget screenshot %d still retains pixels", i)
		}
		note := multiContentText(out[i])
		if !strings.Contains(note, "1 image(s) omitted before model call") ||
			!strings.Contains(note, "4 images / 20 MiB") {
			t.Fatalf("screenshot %d omission note=%q", i, note)
		}
		if len(msgs[i].UserInputMultiContent) != 2 || !hasToolImage(msgs[i]) {
			t.Fatalf("copy-on-write mutated original screenshot %d", i)
		}
	}
}

func TestTrimTrailingToolImagesEnforcesDecodedByteBudget(t *testing.T) {
	encodedPixel := "AAAA" // three decoded bytes per image
	msg := screenshotToolResult("many", "/shots/many.png", encodedPixel)
	for range 2 {
		msg.UserInputMultiContent = append(msg.UserInputMultiContent, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
				MIMEType: "image/png", Base64Data: &encodedPixel,
			}},
		})
	}
	msgs := []*schema.Message{assistantCalls("many", "computer_screenshot"), msg}

	out, cloned := trimTrailingToolImagesWithBudget(
		msgs,
		false,
		internalmodel.NewModelImageBudgetWithLimits(10, 5),
	)
	if !cloned || out[1] == msg {
		t.Fatal("byte-budget trimming did not use copy-on-write")
	}
	if got := retainedToolImageCount(out); got != 1 {
		t.Fatalf("retained images=%d, want first 3-byte image only", got)
	}
	if note := multiContentText(out[1]); !strings.Contains(note, "2 image(s) omitted before model call") ||
		!strings.Contains(note, "10 images / 5 bytes") {
		t.Fatalf("byte-budget omission note=%q", note)
	}
	if got := retainedToolImageCount(msgs); got != 3 {
		t.Fatalf("copy-on-write mutated original message: retained=%d", got)
	}
}
