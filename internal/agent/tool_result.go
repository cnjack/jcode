package agent

import (
	"strings"

	"github.com/cloudwego/eino/schema"
)

// textToolResult is the enhanced-tool equivalent of returning a plain string.
func textToolResult(text string) *schema.ToolResult {
	return &schema.ToolResult{Parts: []schema.ToolOutputPart{{
		Type: schema.ToolPartTypeText,
		Text: text,
	}}}
}

// toolResultText projects an enhanced result onto the text-only contracts used
// by approval tracing and hooks. Media is deliberately omitted: these callers
// historically received strings, and forwarding base64 pixels would both leak
// sensitive screenshots and make trace/hook payloads unexpectedly huge.
func toolResultText(result *schema.ToolResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range result.Parts {
		if part.Type == schema.ToolPartTypeText {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// appendToolResultContext preserves every existing part and appends context as
// text. It follows appendHookContext's blank-line separation when the result
// already contains text, without mutating the endpoint-owned result.
func appendToolResultContext(result *schema.ToolResult, contextText string) *schema.ToolResult {
	if contextText == "" {
		return result
	}
	if toolResultText(result) != "" {
		contextText = "\n\n" + contextText
	}
	if result == nil {
		return textToolResult(contextText)
	}
	parts := append([]schema.ToolOutputPart(nil), result.Parts...)
	parts = append(parts, schema.ToolOutputPart{Type: schema.ToolPartTypeText, Text: contextText})
	return &schema.ToolResult{Parts: parts}
}
