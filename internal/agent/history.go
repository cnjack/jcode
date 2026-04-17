package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
)

// CompactHistory summarizes the conversation history using the model,
// replacing all messages with a system summary + the last few messages.
func CompactHistory(ctx context.Context, cm einomodel.BaseChatModel, history []adk.Message) []adk.Message {
	if len(history) < 4 {
		return history // too short to compact
	}

	// Keep last 2 messages (most recent context).
	keepCount := 2
	if keepCount > len(history) {
		keepCount = len(history)
	}
	// Adjust the split boundary so we don't orphan tool-result messages.
	splitIdx := findToolBoundary(history, len(history)-keepCount)
	toSummarize := history[:splitIdx]
	kept := history[splitIdx:]

	// Build a summarization prompt from the older messages.
	var sb strings.Builder
	sb.WriteString("Summarize this conversation history concisely. Focus on:\n")
	sb.WriteString("- Key decisions made\n- Files modified and why\n- Current task status\n- Important context needed to continue\n\n")
	sb.WriteString("Conversation:\n")
	for _, msg := range toSummarize {
		if msg == nil {
			continue
		}
		fmt.Fprintf(&sb, "[%s]: %s\n", msg.Role, TruncateStr(msg.Content, 500))
	}

	resp, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage("You are a conversation summarizer. Produce a concise summary of the conversation history provided. Output only the summary, no preamble."),
		schema.UserMessage(sb.String()),
	})
	if err != nil {
		config.Logger().Printf("[compact] summarization failed: %v", err)
		return history // return original on error
	}

	var compacted []adk.Message
	compacted = append(compacted, schema.SystemMessage(
		"[Context Summary — previous conversation was compacted]\n\n"+resp.Content,
	))
	compacted = append(compacted, kept...)

	config.Logger().Printf("[compact] %d messages → %d messages", len(history), len(compacted))
	return compacted
}

// TruncateStr truncates a string to maxLen characters, appending "..." if truncated.
func TruncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// SummarizationCapture captures the result when Eino's summarization middleware
// fires, so that the application-level history can be synced afterwards.
type SummarizationCapture struct {
	fired      bool
	summary    string
	compactedN int
}

// Capture records a summarization event. Called from the Finalize callback.
func (c *SummarizationCapture) Capture(summary string, compactedN int) {
	c.fired = true
	c.summary = summary
	c.compactedN = compactedN
}

// drain returns and resets the captured state.
func (c *SummarizationCapture) drain() (fired bool, summary string, compactedN int) {
	fired = c.fired
	summary = c.summary
	compactedN = c.compactedN
	c.fired = false
	c.summary = ""
	c.compactedN = 0
	return
}

// SyncSummarization checks whether Eino's summarization middleware fired
// during the last runner.Run() and, if so, replaces history with the
// compacted version so the next turn starts from the summarized state.
func SyncSummarization(cap *SummarizationCapture, history []adk.Message, rec *session.Recorder) []adk.Message {
	fired, summary, compactedN := cap.drain()
	if !fired {
		return history
	}
	// Keep the most recent messages (typically latest user + assistant).
	keepCount := 2
	if keepCount > len(history) {
		keepCount = len(history)
	}
	// Adjust the split boundary so we don't orphan tool-result messages.
	splitIdx := findToolBoundary(history, len(history)-keepCount)
	kept := make([]adk.Message, len(history)-splitIdx)
	copy(kept, history[splitIdx:])

	var newHistory []adk.Message
	newHistory = append(newHistory, schema.SystemMessage(
		"[Context Summary — conversation was auto-summarized]\n\n"+summary,
	))
	newHistory = append(newHistory, kept...)

	if rec != nil {
		rec.RecordCompact(summary, compactedN)
	}
	config.Logger().Printf("[summarization] synced history: %d → %d messages", len(history), len(newHistory))
	return newHistory
}

// DrainBgNotifications injects any completed background task results into
// the conversation history so the agent is aware of them on the next turn.
func DrainBgNotifications(bm *tools.BackgroundManager, history []adk.Message) []adk.Message {
	notifs := bm.DrainNotifications()
	if len(notifs) == 0 {
		return history
	}
	var sb strings.Builder
	sb.WriteString("<background-results>\n")
	for _, n := range notifs {
		fmt.Fprintf(&sb, "[%s] %s — %s\n", n.TaskID, n.Status, TruncateStr(n.Output, 500))
	}
	sb.WriteString("</background-results>")

	history = append(history, schema.UserMessage(sb.String()))
	history = append(history, &schema.Message{Role: schema.Assistant, Content: "Noted background results."})
	return history
}
