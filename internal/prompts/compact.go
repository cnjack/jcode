package prompts

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
)

//go:embed compact_prompt.md
var compactPromptTemplate string

// CompactConfig controls context compaction behavior.
type CompactConfig struct {
	Threshold    float64 // fraction of context limit that triggers compaction (0-1)
	MaxRetries   int     // max consecutive failures before disabling
	BufferTokens int     // token headroom to reserve
}

// CompactResult describes the outcome of a compaction.
type CompactResult struct {
	Summary           string
	OriginalTokens    int64
	CompactedTokens   int64
	PreservedMsgCount int
}

// ContextCompactor manages automatic context window compression.
type ContextCompactor struct {
	cfg              CompactConfig
	consecutiveFails int
	tripped          bool
}

// NewContextCompactor creates a compactor with the given config.
func NewContextCompactor(cfg CompactConfig) *ContextCompactor {
	if cfg.Threshold <= 0 || cfg.Threshold > 1 {
		cfg.Threshold = 0.75
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.BufferTokens <= 0 {
		cfg.BufferTokens = 1000
	}
	return &ContextCompactor{cfg: cfg}
}

// ShouldCompact returns true when token usage exceeds the configured threshold.
func (c *ContextCompactor) ShouldCompact(tokensUsed int64, contextLimit int) bool {
	if c.tripped || contextLimit <= 0 {
		return false
	}
	return float64(tokensUsed) >= float64(contextLimit)*c.cfg.Threshold
}

// Compact compresses the message history using the provided model.
// It keeps the system message and the most recent messages, summarising the rest.
func (c *ContextCompactor) Compact(
	ctx context.Context,
	messages []*schema.Message,
	model einomodel.ChatModel,
	contextLimit int,
) ([]*schema.Message, *CompactResult, error) {
	if len(messages) < 4 {
		return messages, nil, nil
	}

	// Separate system messages from conversation.
	var systemMsgs []*schema.Message
	var convMsgs []*schema.Message
	for _, m := range messages {
		if m.Role == schema.System {
			systemMsgs = append(systemMsgs, m)
		} else {
			convMsgs = append(convMsgs, m)
		}
	}

	// Determine how many recent messages to keep (at least 4).
	keepRecent := 6
	if keepRecent > len(convMsgs) {
		return messages, nil, nil
	}

	toSummarise := convMsgs[:len(convMsgs)-keepRecent]
	recentMsgs := convMsgs[len(convMsgs)-keepRecent:]

	// Build the compaction prompt.
	var sb strings.Builder
	sb.WriteString(compactPromptTemplate)
	sb.WriteString("\n\n---\n\nConversation to summarise:\n\n")
	for _, m := range toSummarise {
		content := m.Content
		if len(content) > 800 {
			content = content[:800] + "…"
		}
		sb.WriteString(fmt.Sprintf("[%s]: %s\n\n", m.Role, content))
	}

	summaryInput := []*schema.Message{
		{Role: schema.User, Content: sb.String()},
	}

	summaryMsg, err := model.Generate(ctx, summaryInput)
	if err != nil {
		c.consecutiveFails++
		if c.consecutiveFails >= c.cfg.MaxRetries {
			c.tripped = true
			config.Logger().Printf("[compactor] disabled after %d consecutive failures", c.consecutiveFails)
		}
		config.Logger().Printf("[compactor] summarisation failed: %v", err)
		return messages, nil, fmt.Errorf("compaction failed: %w", err)
	}

	c.consecutiveFails = 0

	summaryMessage := &schema.Message{
		Role:    schema.System,
		Content: "[Conversation Summary]\n" + summaryMsg.Content,
	}

	result := make([]*schema.Message, 0, len(systemMsgs)+1+len(recentMsgs))
	result = append(result, systemMsgs...)
	result = append(result, summaryMessage)
	result = append(result, recentMsgs...)

	// Estimate token savings (rough: 4 chars per token).
	var origChars int64
	for _, m := range messages {
		origChars += int64(len(m.Content))
	}
	var compactedChars int64
	for _, m := range result {
		compactedChars += int64(len(m.Content))
	}

	compactResult := &CompactResult{
		Summary:           summaryMsg.Content,
		OriginalTokens:    origChars / 4,
		CompactedTokens:   compactedChars / 4,
		PreservedMsgCount: len(recentMsgs),
	}

	config.Logger().Printf("[compactor] compacted %d→%d messages, ~%d→~%d tokens",
		len(messages), len(result), compactResult.OriginalTokens, compactResult.CompactedTokens)

	return result, compactResult, nil
}

// IsTripped returns true if compaction has been disabled due to repeated failures.
func (c *ContextCompactor) IsTripped() bool {
	return c.tripped
}

// Reset clears the failure counter and re-enables compaction.
func (c *ContextCompactor) Reset() {
	c.consecutiveFails = 0
	c.tripped = false
}
