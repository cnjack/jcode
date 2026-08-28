package model

import (
	"context"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	// titleInputCap bounds the prompt: the first user message is enough signal,
	// a pasted log dump is not worth the tokens.
	titleInputCap = 2000
	// titleMaxRunes matches the truncated-fallback cap in session.generateTitle
	// closely enough that either source renders well in the session lists.
	titleMaxRunes = 60
	titleTimeout  = 30 * time.Second
	// titleTurnCap bounds each conversation turn fed to the suggestion prompt.
	// Together with the structural first/last-per-role selection in TitleTurns
	// (at most 4 turns) it keeps a /rename suggestion cheap and leak-free:
	// never the whole transcript, never pasted code dumps.
	titleTurnCap = 800
)

// GenerateSessionTitle produces a concise session title from the first user
// message via a single non-streaming call, intended for the small model.
// Returns "" on any failure so callers keep the truncated fallback title.
func GenerateSessionTitle(ctx context.Context, cm einomodel.ToolCallingChatModel, firstUserMsg string) string {
	msg := strings.TrimSpace(firstUserMsg)
	if msg == "" || cm == nil {
		return ""
	}
	if runes := []rune(msg); len(runes) > titleInputCap {
		msg = string(runes[:titleInputCap])
	}
	ctx, cancel := context.WithTimeout(ctx, titleTimeout)
	defer cancel()
	out, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage("Generate a short title (at most 8 words) summarizing the user's request. " +
			"Reply with the title only — no quotes, no trailing punctuation, no explanation. " +
			"Write the title in the same language as the request."),
		schema.UserMessage(msg),
	})
	if err != nil || out == nil {
		return ""
	}
	return SanitizeTitle(out.Content)
}

// TitleMsg is one conversation turn considered when suggesting a session
// title. Only user and assistant text belongs here: system prompts, tool
// output, MCP/teammate internals and Guardian evidence must never leak into a
// title, so callers filter before building the slice.
type TitleMsg struct {
	Role    string // "user" or "assistant"
	Content string
}

// GenerateSessionTitleFromConversation suggests a concise session title from
// the conversation so far — the backing for TUI `/rename`. It feeds a bounded
// selection of turns (see titleTurns) to a single non-streaming small-model
// call and asks for the title in the conversation's own language. Returns ""
// on any failure so callers keep the existing title.
func GenerateSessionTitleFromConversation(ctx context.Context, cm einomodel.ToolCallingChatModel, msgs []TitleMsg) string {
	turns := TitleTurns(msgs)
	if len(turns) == 0 || cm == nil {
		return ""
	}
	var sb strings.Builder
	for i, t := range turns {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(t.Role)
		sb.WriteString(": ")
		sb.WriteString(t.Content)
	}
	ctx, cancel := context.WithTimeout(ctx, titleTimeout)
	defer cancel()
	out, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage("Generate a short title (at most 8 words) summarizing this conversation " +
			"between a user and their coding assistant. Reply with the title only — no quotes, " +
			"no trailing punctuation, no explanation. Write the title in the same language as the conversation."),
		schema.UserMessage(sb.String()),
	})
	if err != nil || out == nil {
		return ""
	}
	return SanitizeTitle(out.Content)
}

// TitleTurns selects the bounded, role-filtered turns that carry title signal:
// the first user message (the original ask), the first assistant reply (what
// was actually done), and the last of each role (where the conversation ended
// up) — deduped, in conversation order, each capped at titleTurnCap runes.
// Non-user/assistant roles and empty content are dropped.
func TitleTurns(msgs []TitleMsg) []TitleMsg {
	type turn struct {
		idx int
		m   TitleMsg
	}
	var turns []turn
	for i, raw := range msgs {
		t, ok := capTitleTurn(raw)
		if ok {
			turns = append(turns, turn{i, t})
		}
	}
	if len(turns) == 0 {
		return nil
	}
	firstUser, firstAssistant, lastUser, lastAssistant := -1, -1, -1, -1
	for _, t := range turns {
		switch t.m.Role {
		case "user":
			if firstUser < 0 {
				firstUser = t.idx
			}
			lastUser = t.idx
		case "assistant":
			if firstAssistant < 0 {
				firstAssistant = t.idx
			}
			lastAssistant = t.idx
		}
	}
	// A -1 slot never matches a real index, so unset roles are naturally
	// excluded; duplicate selections collapse to one entry.
	selected := map[int]bool{firstUser: true, firstAssistant: true, lastUser: true, lastAssistant: true}
	out := make([]TitleMsg, 0, len(selected))
	for _, t := range turns {
		if selected[t.idx] {
			out = append(out, t.m)
		}
	}
	return out
}

// capTitleTurn normalizes one turn: lowercase known roles only, trimmed
// non-empty content, capped at titleTurnCap runes. Reports false for turns
// that must never reach the suggestion prompt.
func capTitleTurn(m TitleMsg) (TitleMsg, bool) {
	m.Role = strings.ToLower(strings.TrimSpace(m.Role))
	if m.Role != "user" && m.Role != "assistant" {
		return TitleMsg{}, false
	}
	m.Content = strings.TrimSpace(m.Content)
	if m.Content == "" {
		return TitleMsg{}, false
	}
	if runes := []rune(m.Content); len(runes) > titleTurnCap {
		m.Content = string(runes[:titleTurnCap]) + "…"
	}
	return m, true
}

// SanitizeTitle normalizes model output (or raw user input) to a single clean
// list-friendly line. It is the one title rule shared by every transport
// (TUI /rename, web rename, async generation) so lengths and quote stripping
// cannot drift apart.
func SanitizeTitle(s string) string {
	title := ""
	for _, line := range strings.Split(s, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			title = l
			break
		}
	}
	title = strings.Trim(title, "\"'“”‘’「」『』#*` ")
	if runes := []rune(title); len(runes) > titleMaxRunes {
		title = string(runes[:titleMaxRunes]) + "…"
	}
	return strings.TrimSpace(title)
}
