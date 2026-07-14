package review

import (
	"context"
	"fmt"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
)

// maxTrunkMessages bounds the reused reviewer conversation. When exceeded, the
// oldest action/verdict pairs are dropped (the system message is always kept).
// One trim causes a single cache miss on the shifted prefix, so keep it well
// above a typical session's review count.
const maxTrunkMessages = 41 // 1 system + 20 (action,verdict) pairs

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
	userMsg := schema.UserMessage(renderUserPrompt(req))

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
		e.trunk.messages = trimTrunk(committed)
		return res, meta
	}
	if lastErr != nil {
		config.Logger().Printf("[review] cached review escalating to user: %v", lastErr)
		meta.failReason = lastErr.Error()
	}
	return Result{Outcome: Escalate, Failed: true}, meta
}

// trimTrunk keeps the system message plus the most recent action/verdict pairs
// within maxTrunkMessages. It drops from the front (after system) in whole pairs
// so the transcript never starts mid-exchange.
func trimTrunk(msgs []*schema.Message) []*schema.Message {
	if len(msgs) <= maxTrunkMessages {
		return msgs
	}
	system := msgs[0]
	rest := msgs[1:]
	drop := len(msgs) - maxTrunkMessages
	if drop%2 != 0 {
		drop++ // drop whole (action,verdict) pairs
	}
	if drop > len(rest) {
		drop = len(rest)
	}
	out := make([]*schema.Message, 0, 1+len(rest)-drop)
	out = append(out, system)
	out = append(out, rest[drop:]...)
	return out
}
