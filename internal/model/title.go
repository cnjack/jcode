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
	return sanitizeTitle(out.Content)
}

// sanitizeTitle normalizes model output to a single clean list-friendly line.
func sanitizeTitle(s string) string {
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
