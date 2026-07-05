package agent

import (
	"context"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Per-turn aggregate budget for tool results.
//
// The reduction middleware caps each INDIVIDUAL tool result (MaxLengthForTrunc,
// 50k chars), but N parallel tool calls can each contribute a full-size result
// with no aggregate ceiling — 8 parallel reads are ~400k chars (~100k tokens)
// in a single request, enough to overflow a 200k window before any clearing
// can help (reduction's clear phase structurally protects the newest
// assistant+tool batch, which is exactly the oversized one).
//
// This middleware enforces an aggregate character budget on the TRAILING batch
// of tool results (the ones about to be sent for the first time), truncating
// the largest results middle-out (head+tail kept) until the batch fits. It is
// registered after the reduction middleware, so each result it sees is already
// per-result capped and — when it was >50k — fully offloaded to a file the
// model can re-read.

// DefaultTurnToolResultBudget is the default aggregate character budget for
// one turn's new tool results: three full-size (50k) results, ~37k tokens.
const DefaultTurnToolResultBudget = 150_000

// turnBudgetKeepEdge is how many bytes of head and tail survive when a result
// is truncated by the per-turn budget.
const turnBudgetKeepEdge = 2000

type turnBudgetMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	maxChars int
	exclude  map[string]struct{}
}

// NewTurnToolResultBudgetMiddleware creates a middleware enforcing an
// aggregate character budget across the trailing batch of tool results.
// maxChars <= 0 selects DefaultTurnToolResultBudget. Tools in
// ReductionExcludeTools are never truncated.
func NewTurnToolResultBudgetMiddleware(maxChars int) adk.ChatModelAgentMiddleware {
	if maxChars <= 0 {
		maxChars = DefaultTurnToolResultBudget
	}
	exclude := make(map[string]struct{}, len(ReductionExcludeTools))
	for _, name := range ReductionExcludeTools {
		exclude[name] = struct{}{}
	}
	return &turnBudgetMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		maxChars:                     maxChars,
		exclude:                      exclude,
	}
}

// BeforeModelRewriteState trims the trailing batch of tool results down to the
// aggregate budget. Only message Content is rewritten (copy-on-write — the
// originals may be shared with the session history); message count, order and
// tool_call/result pairing are never touched.
func (m *turnBudgetMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	_ *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	msgs := state.Messages

	// The trailing batch: consecutive tool results at the very end of the
	// window, i.e. the outputs of the last assistant tool-call round that are
	// about to be sent for the first time. Anything earlier is history and
	// reduction's territory.
	end := len(msgs)
	start := end
	for start > 0 && msgs[start-1] != nil && msgs[start-1].Role == schema.Tool {
		start--
	}
	if start == end {
		return ctx, state, nil
	}

	total := 0
	for i := start; i < end; i++ {
		total += len(msgs[i].Content)
	}
	if total <= m.maxChars {
		return ctx, state, nil
	}

	// Map ToolCallID → tool name from the issuing assistant message so the
	// exclusion list works even when the tool message lacks ToolName.
	callNames := map[string]string{}
	if start > 0 && msgs[start-1] != nil && msgs[start-1].Role == schema.Assistant {
		for _, tc := range msgs[start-1].ToolCalls {
			callNames[tc.ID] = tc.Function.Name
		}
	}

	// Candidates: non-excluded results, largest first.
	type candidate struct{ idx, size int }
	var candidates []candidate
	for i := start; i < end; i++ {
		name := msgs[i].ToolName
		if name == "" {
			name = callNames[msgs[i].ToolCallID]
		}
		if _, skip := m.exclude[name]; skip {
			continue
		}
		candidates = append(candidates, candidate{idx: i, size: len(msgs[i].Content)})
	}
	sort.SliceStable(candidates, func(a, b int) bool { return candidates[a].size > candidates[b].size })

	cloned := false
	for _, cand := range candidates {
		if total <= m.maxChars {
			break
		}
		orig := msgs[cand.idx]
		replacement := truncateMiddle(orig.Content, turnBudgetKeepEdge)
		if len(replacement) >= len(orig.Content) {
			continue // too small for middle-out truncation to help
		}
		if !cloned {
			// Copy-on-write: never mutate the caller's slice or messages.
			msgs = append([]*schema.Message(nil), msgs...)
			cloned = true
		}
		clone := *orig
		clone.Content = replacement
		msgs[cand.idx] = &clone
		total -= len(orig.Content) - len(replacement)
	}
	if cloned {
		state.Messages = msgs
	}
	return ctx, state, nil
}

// truncateMiddle keeps the first and last keep bytes of s (aligned to rune
// boundaries) and replaces the middle with a marker explaining what was
// dropped and how to get it back. Returns s unchanged when truncation would
// not shrink it.
func truncateMiddle(s string, keep int) string {
	if len(s) <= 2*keep {
		return s
	}
	headEnd := keep
	for headEnd > 0 && !utf8.RuneStart(s[headEnd]) {
		headEnd--
	}
	tailStart := len(s) - keep
	for tailStart < len(s) && !utf8.RuneStart(s[tailStart]) {
		tailStart++
	}
	dropped := tailStart - headEnd
	marker := fmt.Sprintf(
		"\n\n[jcode: tool result truncated by per-turn budget: dropped %d of %d bytes; full output was offloaded by reduction if >50k — read the offload file or re-run with narrower scope]\n\n",
		dropped, len(s))
	if dropped <= len(marker) {
		return s
	}
	return s[:headEnd] + marker + s[tailStart:]
}
