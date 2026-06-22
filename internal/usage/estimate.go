package usage

// Token estimation for the per-task context-capacity breakdown. There is no
// universal tokenizer across providers (GLM, Anthropic, OpenAI all differ), and
// bundling a tokenizer is a heavy dependency for what is only a relative
// breakdown. ~4 bytes/token is the well-known rough average for English+code;
// the UI labels these numbers as estimates. Consistency across buckets matters
// more than absolute accuracy here.

// EstimateBytes approximates the token count of a byte length.
func EstimateBytes(n int) int {
	if n <= 0 {
		return 0
	}
	return (n + 3) / 4
}

// Estimate approximates the token count of a string.
func Estimate(s string) int { return EstimateBytes(len(s)) }

// ContextBreakdown partitions a context window into the categories that make it
// up. The four static buckets (system prompt / tools / MCP tools / skills) are
// computed from the live agent assembly; MessagesTokens and ContextLimit are
// filled in at query time.
type ContextBreakdown struct {
	SystemPromptTokens int `json:"system_prompt_tokens"`
	SystemToolsTokens  int `json:"system_tools_tokens"`
	MCPToolsTokens     int `json:"mcp_tools_tokens"`
	SkillsTokens       int `json:"skills_tokens"`
	MessagesTokens     int `json:"messages_tokens"`
	ContextLimit       int `json:"context_limit"`
}

// StaticTotal is the sum of the four assembly-time buckets (everything except
// the conversation messages).
func (b ContextBreakdown) StaticTotal() int {
	return b.SystemPromptTokens + b.SystemToolsTokens + b.MCPToolsTokens + b.SkillsTokens
}
