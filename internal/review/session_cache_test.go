package review

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
)

// fakeModel records the message slice passed to each Generate call so tests can
// assert what prefix the provider would see (and thus cache).
type fakeModel struct {
	reply       string
	gotMessages [][]*schema.Message
	calls       int
}

func (f *fakeModel) Generate(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	f.calls++
	cp := make([]*schema.Message, len(input))
	copy(cp, input)
	f.gotMessages = append(f.gotMessages, cp)
	return &schema.Message{Role: schema.Assistant, Content: f.reply}, nil
}

func (f *fakeModel) Stream(context.Context, []*schema.Message, ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("stream not used")
}

func (f *fakeModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return f, nil
}

func newCachedEngine() *Engine {
	return &Engine{
		cfg:     &config.Config{Model: "p/m"},
		system:  "SYSTEM-POLICY",
		timeout: time.Second,
		audit:   newAuditLog(""),
		trunk:   newReviewerSession(),
	}
}

func TestReviewCached_StablePrefixGrows(t *testing.T) {
	e := newCachedEngine()
	fm := &fakeModel{reply: `{"outcome":"allow"}`}

	r1, _ := e.reviewCached(context.Background(), Request{ToolName: "execute", ToolArgs: `{"command":"mkdir a"}`}, fm)
	r2, _ := e.reviewCached(context.Background(), Request{ToolName: "execute", ToolArgs: `{"command":"mkdir b"}`}, fm)

	if r1.Outcome != Allow || r2.Outcome != Allow {
		t.Fatalf("expected both allow, got %v %v", r1.Outcome, r2.Outcome)
	}

	// First request: [system, user(action1)].
	if got := len(fm.gotMessages[0]); got != 2 {
		t.Fatalf("review 1 sent %d messages, want 2", got)
	}
	// Second request: [system, user(action1), assistant(verdict1), user(action2)].
	if got := len(fm.gotMessages[1]); got != 4 {
		t.Fatalf("review 2 sent %d messages, want 4", got)
	}

	// The cache-critical property: review 2's prefix reproduces review 1's exact
	// messages, so the provider serves them from cache.
	req1, req2 := fm.gotMessages[0], fm.gotMessages[1]
	for i := range req1 {
		if req2[i].Role != req1[i].Role || req2[i].Content != req1[i].Content {
			t.Fatalf("prefix diverged at message %d: review2 sees a different prefix than review1", i)
		}
	}
	if req2[0].Role != schema.System || req2[0].Content != "SYSTEM-POLICY" {
		t.Fatalf("system prefix not stable")
	}
	if req2[2].Role != schema.Assistant || req2[2].Content != `{"outcome":"allow"}` {
		t.Fatalf("prior verdict not carried into the reused conversation")
	}
}

func TestReviewCached_FailureDoesNotCorruptTrunk(t *testing.T) {
	e := newCachedEngine()
	// Model returns garbage → all parse attempts fail → escalate; trunk must be
	// left with only the system message so the next review keeps a clean prefix.
	fm := &fakeModel{reply: "not json at all"}
	res, _ := e.reviewCached(context.Background(), Request{ToolName: "execute", ToolArgs: "x"}, fm)
	if res.Outcome != Escalate || !res.Failed {
		t.Fatalf("expected escalate+failed on unparseable output, got %+v", res)
	}
	if len(e.trunk.messages) != 1 || e.trunk.messages[0].Role != schema.System {
		t.Fatalf("failed review must not commit to the trunk; got %d messages", len(e.trunk.messages))
	}

	// A subsequent good review starts cleanly from [system, action].
	fm.reply = `{"outcome":"allow"}`
	if res, _ := e.reviewCached(context.Background(), Request{ToolName: "execute", ToolArgs: "y"}, fm); res.Outcome != Allow {
		t.Fatalf("expected allow after recovery, got %v", res.Outcome)
	}
	if len(e.trunk.messages) != 3 {
		t.Fatalf("expected [system,action,verdict] after recovery, got %d", len(e.trunk.messages))
	}
}

func TestTrimTrunk_ByMessageCount(t *testing.T) {
	// Build 1 system + N pairs beyond the cap.
	msgs := []*schema.Message{schema.SystemMessage("s")}
	for i := 0; i < maxTrunkMessages; i++ { // pushes total to 1 + 2*maxTrunkMessages
		msgs = append(msgs, schema.UserMessage("u"), &schema.Message{Role: schema.Assistant, Content: "a"})
	}
	out, trimmed := trimTrunk(msgs)
	if !trimmed {
		t.Fatalf("expected trim to report it dropped pairs")
	}
	if len(out) > maxTrunkMessages {
		t.Fatalf("trimmed to %d, want <= %d", len(out), maxTrunkMessages)
	}
	if out[0].Role != schema.System {
		t.Fatalf("system message must survive trim")
	}
	// After system, the transcript must start on a user (action), not mid-pair.
	if len(out) > 1 && out[1].Role != schema.User {
		t.Fatalf("trim broke a pair: message after system is %v", out[1].Role)
	}
}

func TestTrimTrunk_ByBytes(t *testing.T) {
	// Few messages, but huge ones: a count-only bound would let this through and
	// the trunk would grow until it blew the context window.
	big := strings.Repeat("x", 20_000)
	msgs := []*schema.Message{schema.SystemMessage("s")}
	for i := 0; i < 5; i++ { // 11 messages, ~200KB — under the count cap, over bytes
		msgs = append(msgs, schema.UserMessage(big), &schema.Message{Role: schema.Assistant, Content: big})
	}
	if len(msgs) > maxTrunkMessages {
		t.Fatalf("precondition: this test must stay under the message-count cap")
	}
	out, trimmed := trimTrunk(msgs)
	if !trimmed {
		t.Fatalf("expected byte budget to trigger a trim")
	}
	if trunkBytes(out) > maxTrunkBytes {
		t.Fatalf("trunk is %d bytes after trim, want <= %d", trunkBytes(out), maxTrunkBytes)
	}
	if out[0].Role != schema.System {
		t.Fatalf("system message must survive trim")
	}
}

func TestTranscriptKey(t *testing.T) {
	a := []Msg{{Role: "user", Content: "hello"}}
	b := []Msg{{Role: "user", Content: "hello"}}
	c := []Msg{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}}
	if transcriptKey(a) != transcriptKey(b) {
		t.Errorf("identical transcripts must share a key")
	}
	if transcriptKey(a) == transcriptKey(c) {
		t.Errorf("different transcripts must differ")
	}
	if transcriptKey(nil) != "" {
		t.Errorf("empty transcript key must be empty")
	}
	// Role/content boundaries must not be confusable by concatenation.
	x := []Msg{{Role: "user", Content: "ab"}}
	y := []Msg{{Role: "usera", Content: "b"}}
	if transcriptKey(x) == transcriptKey(y) {
		t.Errorf("role/content boundary collision")
	}
}

func TestReviewCached_SkipsUnchangedTranscript(t *testing.T) {
	e := newCachedEngine()
	fm := &fakeModel{reply: `{"outcome":"allow"}`}
	transcript := []Msg{{Role: "user", Content: "UNIQUE-EVIDENCE-MARKER please build"}}

	// Review 1 embeds the transcript.
	e.reviewCached(context.Background(), Request{ToolName: "execute", ToolArgs: `{"command":"mkdir a"}`, Transcript: transcript}, fm)
	// Review 2 with the SAME transcript must not re-embed it — within a turn the
	// frontends hand us an identical transcript, and re-sending it every review
	// would grow the trunk by a full transcript each time.
	e.reviewCached(context.Background(), Request{ToolName: "execute", ToolArgs: `{"command":"mkdir b"}`, Transcript: transcript}, fm)

	newUserMsg := fm.gotMessages[1][len(fm.gotMessages[1])-1]
	if strings.Contains(newUserMsg.Content, "UNIQUE-EVIDENCE-MARKER") {
		t.Errorf("review 2 re-embedded an unchanged transcript:\n%s", newUserMsg.Content)
	}
	if !strings.Contains(newUserMsg.Content, "mkdir b") {
		t.Errorf("review 2 must still carry the new action")
	}
	// The evidence is still available to the model via the cached prefix.
	if !strings.Contains(fm.gotMessages[1][1].Content, "UNIQUE-EVIDENCE-MARKER") {
		t.Errorf("transcript should remain in the trunk prefix from review 1")
	}

	// A CHANGED transcript must be re-sent.
	grown := append(append([]Msg{}, transcript...), Msg{Role: "user", Content: "SECOND-TURN-MARKER now deploy"})
	e.reviewCached(context.Background(), Request{ToolName: "execute", ToolArgs: `{"command":"mkdir c"}`, Transcript: grown}, fm)
	third := fm.gotMessages[2][len(fm.gotMessages[2])-1]
	if !strings.Contains(third.Content, "SECOND-TURN-MARKER") {
		t.Errorf("a changed transcript must be re-sent:\n%s", third.Content)
	}
}
