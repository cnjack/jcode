package review

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
)

const (
	// maxTrunkMessages bounds the reused reviewer conversation by message count.
	// When exceeded, the oldest action/verdict pairs are dropped (the system
	// message is always kept). One trim causes a single cache miss on the shifted
	// prefix, so keep it well above a typical session's review count.
	maxTrunkMessages = 41 // 1 system + 20 (action,verdict) pairs
	// maxTrunkBytes bounds the trunk by SIZE as well. A count-only bound is not
	// enough: each committed message can carry a rendered transcript, so 20 pairs
	// could reach hundreds of KB and blow the context window — making V3 more
	// expensive than V1, the opposite of its purpose. ~60KB ≈ 15k tokens.
	maxTrunkBytes = 60_000
)

// reviewerSession is a reused reviewer conversation. Holding the growing message
// list lets the provider serve the large policy prefix (and prior verdicts) from
// its prompt cache across reviews within one jcode session. It is fully separate
// from the main agent conversation — different model, different prefix — so it
// cannot evict or corrupt the main conversation's cache in any correctness sense.
//
// Reviews serialize on mu: concurrent Generate calls against a shared growing
// list would corrupt turn ordering, and serialization is what makes the cached
// prefix deterministic. Approvals are mostly sequential, so this rarely blocks.
type reviewerSession struct {
	mu       sync.Mutex
	messages []*schema.Message // [system, user(action1), assistant(verdict1), ...]
	// lastTranscriptKey fingerprints the conversation evidence already embedded
	// in the trunk. While it is unchanged, later reviews send only the new action
	// instead of re-embedding a near-identical transcript every time (the
	// frontends only extend history between turns, so within a turn this is
	// stable). Empty means "not in the trunk — send it".
	lastTranscriptKey string
}

func newReviewerSession() *reviewerSession { return &reviewerSession{} }

// reviewCached is the V3 path: adjudicate against a reused conversation so the
// policy prefix is served from the provider's prompt cache. On any failure the
// trunk is left unchanged (a clean prefix is preserved) and the call escalates.
func (e *Engine) reviewCached(ctx context.Context, req Request, cm einomodel.ToolCallingChatModel) (Result, reviewMeta) {
	meta := reviewMeta{}
	e.trunk.mu.Lock()
	defer e.trunk.mu.Unlock()

	// Seed the stable system prefix once. Everything before the new user message
	// is unchanged across reviews, which is exactly what the prompt cache keys on.
	if len(e.trunk.messages) == 0 {
		e.trunk.messages = []*schema.Message{schema.SystemMessage(e.system)}
	}
	base := e.trunk.messages

	// Incremental evidence: only re-send the transcript when it actually changed
	// since the last review in this session. Otherwise it is already in the
	// cached prefix and re-embedding it would grow the trunk by ~a full
	// transcript per review for no added information.
	key := transcriptKey(req.Transcript)
	userText := renderActionSection(req)
	if key != e.trunk.lastTranscriptKey {
		userText = renderTranscriptSection(req.Transcript) + userText
	}
	userMsg := schema.UserMessage(userText)

	var lastErr error
	for attempt := 0; attempt < parseAttempts; attempt++ {
		reqMsgs := make([]*schema.Message, 0, len(base)+2)
		reqMsgs = append(reqMsgs, base...)
		reqMsgs = append(reqMsgs, userMsg)
		if attempt > 0 {
			// The nudge lives only in the throwaway request, never in the trunk,
			// so the committed prefix stays a clean action/verdict transcript.
			reqMsgs = append(reqMsgs, schema.UserMessage("Your previous reply was not valid JSON. Reply with ONLY the JSON value."))
		}
		meta.calls++
		out, err := cm.Generate(ctx, reqMsgs)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			continue
		}
		if out == nil {
			lastErr = fmt.Errorf("nil model output")
			continue
		}
		a, ok := parseAssessment(out.Content)
		if !ok {
			lastErr = fmt.Errorf("unparseable output")
			continue
		}
		meta.userAuth = a.UserAuthorization
		res, ok := mapOutcome(a)
		if !ok {
			lastErr = fmt.Errorf("missing/invalid outcome")
			continue
		}
		// Commit a clean (action, verdict) pair to the trunk.
		verdict := &schema.Message{Role: schema.Assistant, Content: out.Content}
		committed := make([]*schema.Message, 0, len(base)+2)
		committed = append(committed, base...)
		committed = append(committed, userMsg, verdict)
		trimmed, didTrim := trimTrunk(committed)
		e.trunk.messages = trimmed
		e.trunk.lastTranscriptKey = key
		if didTrim {
			// A trim may have dropped the pair that carried the transcript, so the
			// evidence is no longer guaranteed to be in the prefix. Force the next
			// review to re-send it rather than silently reviewing without context.
			e.trunk.lastTranscriptKey = ""
		}
		return res, meta
	}
	if lastErr != nil {
		config.Logger().Printf("[review] cached review escalating to user: %v", lastErr)
		meta.failReason = lastErr.Error()
	}
	return Result{Outcome: Escalate, Failed: true}, meta
}

// trimTrunk keeps the system message plus the most recent action/verdict pairs,
// bounded by BOTH message count and total size. It drops from the front (after
// system) in whole pairs so the transcript never starts mid-exchange. It reports
// whether anything was dropped.
func trimTrunk(msgs []*schema.Message) ([]*schema.Message, bool) {
	if len(msgs) <= maxTrunkMessages && trunkBytes(msgs) <= maxTrunkBytes {
		return msgs, false
	}
	system := msgs[0]
	rest := append([]*schema.Message{}, msgs[1:]...)
	trimmed := false
	// Drop oldest whole pairs until both budgets are satisfied. Always keep at
	// least the newest pair, so a single oversized review still gets judged
	// rather than being reduced to a bare system prompt.
	for len(rest) > 2 {
		out := append([]*schema.Message{system}, rest...)
		if len(out) <= maxTrunkMessages && trunkBytes(out) <= maxTrunkBytes {
			break
		}
		rest = rest[2:]
		trimmed = true
	}
	return append([]*schema.Message{system}, rest...), trimmed
}

// trunkBytes approximates the trunk's size by summing message content lengths.
func trunkBytes(msgs []*schema.Message) int {
	n := 0
	for _, m := range msgs {
		if m != nil {
			n += len(m.Content)
		}
	}
	return n
}

// transcriptKey fingerprints conversation evidence so the cached path can tell
// "unchanged since the last review" from "new evidence to send". Returns "" for
// an empty transcript, which never matches a committed key.
func transcriptKey(msgs []Msg) string {
	if len(msgs) == 0 {
		return ""
	}
	h := fnv.New64a()
	for _, m := range msgs {
		_, _ = h.Write([]byte(m.Role))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(m.Content))
		_, _ = h.Write([]byte{1})
	}
	return strconv.FormatUint(h.Sum64(), 16)
}
