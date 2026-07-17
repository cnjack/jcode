package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	internalmodel "github.com/cnjack/jcode/internal/model"
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

// BeforeModelRewriteState releases consumed tool images and trims the trailing
// batch of tool results down to the aggregate budget. Messages are rewritten
// copy-on-write because originals may be shared with session history; message
// count, order, and tool_call/result pairing are never touched.
func (m *turnBudgetMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	_ *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	msgs := state.Messages
	// A screenshot's pixels are useful for exactly one model invocation. Once a
	// later conversation message proves that invocation consumed them, retain
	// only the text/image_ref and release the Base64 copy. Do this before the
	// character budget so both rewrites share one copy-on-write slice.
	msgs, cloned := releaseConsumedToolImages(msgs)
	// A parallel tool round can produce more screenshots than the request
	// converter is allowed to send. Drop those excess Base64 copies now, using
	// the converter's shared admission policy, instead of retaining pixels that
	// will be omitted moments later.
	msgs, cloned = trimTrailingToolImages(msgs, cloned)

	// The trailing batch: consecutive tool results at the end of the conversation,
	// i.e. the outputs of the last assistant tool-call round that are about to be
	// sent for the first time. System reminders appended by another middleware do
	// not consume that batch and therefore sit outside [start,end).
	start, end := trailingToolBatch(msgs)
	if start == end {
		if cloned {
			state.Messages = msgs
		}
		return ctx, state, nil
	}

	total := 0
	for i := start; i < end; i++ {
		total += len(msgs[i].Content)
	}
	if total <= m.maxChars {
		if cloned {
			state.Messages = msgs
		}
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

// trailingToolBatch returns the half-open range containing the only tool-result
// batch whose images have not yet been consumed by a model call. A trailing
// system reminder is metadata for that same call, not evidence of consumption.
func trailingToolBatch(msgs []*schema.Message) (start, end int) {
	end = len(msgs)
	for end > 0 {
		msg := msgs[end-1]
		if msg != nil && msg.Role != schema.System {
			break
		}
		end--
	}
	start = end
	for start > 0 {
		msg := msgs[start-1]
		if msg == nil || msg.Role != schema.Tool {
			break
		}
		start--
	}
	return start, end
}

// releaseConsumedToolImages copy-on-write downgrades historical enhanced tool
// results to ordinary text. It deliberately leaves the trailing unconsumed
// tool batch intact because those pixels are still needed by the next model
// invocation. User-attached images are outside this lifecycle and are untouched.
func releaseConsumedToolImages(msgs []*schema.Message) ([]*schema.Message, bool) {
	preserveStart, preserveEnd := trailingToolBatch(msgs)
	cloned := false
	for i, msg := range msgs {
		if msg == nil || msg.Role != schema.Tool || (i >= preserveStart && i < preserveEnd) || !hasToolImage(msg) {
			continue
		}
		if !cloned {
			msgs = append([]*schema.Message(nil), msgs...)
			cloned = true
		}
		clone := *msg
		clone.Content = toolResultTextReference(msg)
		clone.UserInputMultiContent = nil
		msgs[i] = &clone
	}
	return msgs, cloned
}

// trimTrailingToolImages enforces the same count/decoded-byte budget as the
// final model converter. Only the current trailing tool batch is eligible;
// historical images have already been released by releaseConsumedToolImages.
func trimTrailingToolImages(msgs []*schema.Message, cloned bool) ([]*schema.Message, bool) {
	return trimTrailingToolImagesWithBudget(msgs, cloned, internalmodel.NewModelImageBudget())
}

func trimTrailingToolImagesWithBudget(
	msgs []*schema.Message,
	cloned bool,
	budget *internalmodel.ModelImageBudget,
) ([]*schema.Message, bool) {
	maxCount, maxBytes := budget.Limits()
	start, end := trailingToolBatch(msgs)
	for i := start; i < end; i++ {
		msg := msgs[i]
		if msg == nil || !hasToolImage(msg) {
			continue
		}

		parts := make([]schema.MessageInputPart, 0, len(msg.UserInputMultiContent)+1)
		omitted := 0
		for _, part := range msg.UserInputMultiContent {
			if part.Type != schema.ChatMessagePartTypeImageURL || part.Image == nil {
				parts = append(parts, part)
				continue
			}
			payloadBytes, valid := internalmodel.ModelImagePayloadBytes(part.Image)
			if !valid || budget.Admit(payloadBytes) {
				// Preserve malformed/empty references exactly as the converter does:
				// it ignores them and they carry no Base64 payload to release.
				parts = append(parts, part)
				continue
			}
			omitted++
		}
		if omitted == 0 {
			continue
		}
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeText,
			Text: fmt.Sprintf(
				"[%d image(s) omitted before model call: visual payload budget is %d images / %s]",
				omitted,
				maxCount,
				formatImageByteLimit(maxBytes),
			),
		})
		if !cloned {
			msgs = append([]*schema.Message(nil), msgs...)
			cloned = true
		}
		clone := *msg
		clone.UserInputMultiContent = parts
		msgs[i] = &clone
	}
	return msgs, cloned
}

func formatImageByteLimit(n int64) string {
	if n >= 1<<20 && n%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", n>>20)
	}
	return fmt.Sprintf("%d bytes", n)
}

func hasToolImage(msg *schema.Message) bool {
	for _, part := range msg.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeImageURL && part.Image != nil {
			return true
		}
	}
	return false
}

// toolResultTextReference preserves the safe text emitted alongside an image
// (computer_screenshot includes its /api/computer/shots/<uuid>.png reference).
// Enhanced results normally leave Content empty, but prefer their text parts so
// a stale or duplicated Content field cannot hide the canonical image_ref.
func toolResultTextReference(msg *schema.Message) string {
	text := make([]string, 0, len(msg.UserInputMultiContent))
	for _, part := range msg.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText && part.Text != "" {
			text = append(text, part.Text)
		}
	}
	base := msg.Content
	if len(text) > 0 {
		base = strings.Join(text, "\n")
	}
	note := "[Image pixels were consumed by the previous model call and are no longer attached. " +
		"Run the tool again for current visual state.]"
	if base == "" {
		return note
	}
	return base + "\n" + note
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
