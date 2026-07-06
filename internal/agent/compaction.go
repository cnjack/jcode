package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
)

// CompactionStrategy decides when and how to compact conversation history.
type CompactionStrategy interface {
	// ShouldCompact returns true when the current token count warrants compaction.
	ShouldCompact(currentTokens, limit int) bool
	// Compact compresses the messages slice, keeping the most recent keepRecent
	// messages intact and summarising the rest.
	Compact(ctx context.Context, messages []*schema.Message, keepRecent int) ([]*schema.Message, error)
}

// ThresholdCompactionStrategy triggers compaction when token usage exceeds
// a configurable fraction of the context limit and uses a chat model to
// generate a summary of older messages.
type ThresholdCompactionStrategy struct {
	threshold  float64 // fraction (0-1) e.g. 0.75
	summarizer einomodel.ToolCallingChatModel
	keepRecent int
}

// NewThresholdCompactionStrategy creates a compaction strategy.
// threshold is the fraction (0-1) of the context limit that triggers compaction.
// summarizer is the model used to generate summaries. keepRecent is the number
// of recent messages to preserve verbatim.
func NewThresholdCompactionStrategy(threshold float64, summarizer einomodel.ToolCallingChatModel, keepRecent int) *ThresholdCompactionStrategy {
	if threshold <= 0 || threshold > 1 {
		threshold = 0.75
	}
	if keepRecent < 2 {
		keepRecent = 6
	}
	return &ThresholdCompactionStrategy{
		threshold:  threshold,
		summarizer: summarizer,
		keepRecent: keepRecent,
	}
}

func (s *ThresholdCompactionStrategy) ShouldCompact(currentTokens, limit int) bool {
	if limit <= 0 {
		return false
	}
	return float64(currentTokens) >= float64(limit)*s.threshold
}

func (s *ThresholdCompactionStrategy) Compact(ctx context.Context, messages []*schema.Message, keepRecent int) ([]*schema.Message, error) {
	if keepRecent <= 0 {
		keepRecent = s.keepRecent
	}
	if len(messages) <= keepRecent+1 { // +1 for system message
		return messages, nil
	}

	// Separate system message(s) at the beginning from the rest.
	var systemMsgs []*schema.Message
	var conversationMsgs []*schema.Message
	for _, m := range messages {
		if m.Role == schema.System {
			systemMsgs = append(systemMsgs, m)
		} else {
			conversationMsgs = append(conversationMsgs, m)
		}
	}

	if len(conversationMsgs) <= keepRecent {
		return messages, nil
	}

	// Split: older messages to summarise, recent messages to keep.
	// Adjust the split boundary so we don't orphan tool-result messages.
	splitIdx := findToolBoundary(conversationMsgs, len(conversationMsgs)-keepRecent)
	toSummarise := conversationMsgs[:splitIdx]
	recentMsgs := conversationMsgs[splitIdx:]

	// Build summary prompt.
	var sb strings.Builder
	sb.WriteString("Summarise the following conversation history into a concise paragraph.\n")
	sb.WriteString("Preserve key decisions, file paths, code changes, and pending tasks.\n\n")
	for _, m := range toSummarise {
		fmt.Fprintf(&sb, "[%s]: %s\n", m.Role, truncate(m.Content, 500))
	}

	summaryInput := []*schema.Message{
		{Role: schema.User, Content: sb.String()},
	}

	summaryMsg, err := s.summarizer.Generate(ctx, summaryInput)
	if err != nil {
		config.Logger().Printf("[compaction] summarisation failed: %v", err)
		// Propagate the error so the middleware can count it towards its
		// fuse; the caller keeps the original messages (fail-open).
		return messages, err
	}

	summaryMessage := &schema.Message{
		Role:    schema.System,
		Content: "[Conversation Summary]\n" + summaryMsg.Content,
	}

	// Rebuild: system message(s) + summary + recent messages.
	result := make([]*schema.Message, 0, len(systemMsgs)+1+len(recentMsgs))
	result = append(result, systemMsgs...)
	result = append(result, summaryMessage)
	result = append(result, recentMsgs...)
	return result, nil
}

// truncate shortens a string to maxLen characters, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// maxConsecutiveCompactFails is the fuse limit: after this many consecutive
// compaction failures (summarizer errors or no-shrink results) the middleware
// stops attempting automatic compaction for the rest of the session, instead
// of burning tokens and latency on a summarizer that keeps failing.
const maxConsecutiveCompactFails = 3

// CompactionState tracks compaction history for diagnostics.
type CompactionState struct {
	mu               sync.Mutex
	compactionCount  int
	savedTokens      int
	consecutiveFails int
}

// CompactionCount returns how many compactions have occurred.
func (cs *CompactionState) CompactionCount() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.compactionCount
}

// SavedTokens returns the total estimated tokens saved by compaction.
func (cs *CompactionState) SavedTokens() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.savedTokens
}

// compactionMiddleware is a ChatModelAgentMiddleware that automatically
// compacts conversation history when token usage approaches the limit.
type compactionMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	strategy     CompactionStrategy
	state        *CompactionState
	contextLimit int
	onCompact    func(savedTokens int)
	tokenUsage   *internalmodel.TokenUsage
	// fuseLogOnce ensures the "compaction disabled" log fires once per session
	// instead of on every model call after the fuse blows.
	fuseLogOnce sync.Once
}

// NewCompactionMiddleware creates a ChatModelAgentMiddleware that monitors
// token usage and compacts the conversation when the strategy says to.
// tokenUsage is the per-agent tracker to read from.
// onCompact is an optional callback invoked after a successful compaction.
func NewCompactionMiddleware(strategy CompactionStrategy, contextLimit int, tokenUsage *internalmodel.TokenUsage, onCompact func(int)) adk.ChatModelAgentMiddleware {
	return &compactionMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		strategy:                     strategy,
		state:                        &CompactionState{},
		contextLimit:                 contextLimit,
		tokenUsage:                   tokenUsage,
		onCompact:                    onCompact,
	}
}

func (m *compactionMiddleware) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.ChatModelAgentState,
	mc *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	// Estimate current context occupancy from the per-agent tracker. Use the LAST
	// call's total (GetLastTotal), NOT the cumulative prompt sum: the agent
	// re-sends the whole context on every tool-loop call, so cumulative prompt
	// (e.g. 20k×5=100k) would trip compaction far too early while the real window
	// is still ~20k.
	var currentTokens int
	if m.tokenUsage != nil {
		currentTokens = int(m.tokenUsage.GetLastTotal())
	}

	if !m.strategy.ShouldCompact(currentTokens, m.contextLimit) {
		return ctx, state, nil
	}

	// Fuse: after repeated failures, stop retrying the summarizer for the
	// rest of the session — each retry costs tokens and latency for nothing.
	m.state.mu.Lock()
	fails := m.state.consecutiveFails
	m.state.mu.Unlock()
	if fails >= maxConsecutiveCompactFails {
		m.fuseLogOnce.Do(func() {
			config.Logger().Printf("[compaction] disabled for this session after %d consecutive failures", fails)
		})
		return ctx, state, nil
	}

	config.Logger().Printf("[compaction] triggered: tokens=%d, limit=%d", currentTokens, m.contextLimit)

	beforeLen := len(state.Messages)
	compacted, err := m.strategy.Compact(ctx, state.Messages, 0)
	if err != nil {
		config.Logger().Printf("[compaction] compact failed: %v", err)
		m.state.mu.Lock()
		m.state.consecutiveFails++
		m.state.mu.Unlock()
		return ctx, state, nil // fail-open
	}

	saved := beforeLen - len(compacted)
	if saved <= 0 || len(compacted) == 0 {
		// Compacted nothing (e.g. a single poison message larger than the
		// window) — or a strategy returned an empty slice with a nil error,
		// which would silently wipe the conversation if applied. Both are
		// non-error failures that still count towards the fuse.
		config.Logger().Printf("[compaction] no shrink: %d → %d messages", beforeLen, len(compacted))
		m.state.mu.Lock()
		m.state.consecutiveFails++
		m.state.mu.Unlock()
		return ctx, state, nil
	}
	state.Messages = compacted

	m.state.mu.Lock()
	m.state.compactionCount++
	m.state.savedTokens += saved
	m.state.consecutiveFails = 0
	m.state.mu.Unlock()

	config.Logger().Printf("[compaction] compacted %d messages → %d (saved %d)", beforeLen, len(compacted), saved)

	if m.onCompact != nil {
		m.onCompact(saved)
	}

	return ctx, state, nil
}
