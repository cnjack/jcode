# Context Management: Tool Output Reduction & History Compaction

> **Last verified**: 2026-03-28 against codebase at `github.com/cloudwego/eino v0.7.37`
> **Priority**: P0 — Must have (sessions crash without this)
> **Note**: This is the #1 priority. Without context compaction, long sessions hit context-window limits and die. Claude Code + OpenCode both have auto-compact. Phase 1 uses deterministic trimming; Phase 2 adds model-based summarization (like Claude Code's conversation summarization subagent).

## 1. Problem Statement

Long conversations and large tool outputs cause context-window overflow, which terminates the agent run with a hard API error. There is currently no mechanism to compress history or truncate oversized tool results.

Two concrete failure scenarios:

**Scenario A — History Overflow**: A coding session with many back-and-forth exchanges, multiple file reads, and large execute outputs gradually accumulates tokens. When the total exceeds the model's context window (e.g. 200k tokens for Claude, 128k for GPT-4o, 1M+ for Gemini), the next `Generate()` call fails with a context-length error and the entire session is lost.

**Scenario B — Single Tool Explosion**: The user asks the agent to read a 5000-line file. The `read` tool returns ~200k characters. Even before context accumulates, this single result blows the limit.

Without any mitigation, both are permanent failures requiring a full restart.

**2026 AI Landscape Note**: While context windows have grown dramatically (Claude 200k, Gemini 1M+), context management remains critical: (1) larger contexts increase cost linearly; (2) model performance degrades on long contexts ("lost in the middle" problem); (3) many API providers charge per-token, making unnecessary context expensive. Some providers now offer server-side prompt caching (Anthropic, Google), which reduces cost of repeated prefixes but does not eliminate the need for reduction.

## 2. Goals & Non-Goals

**Goals**
- Truncate and offload oversized individual tool outputs using Eino's built-in reduction middleware.
- Implement a custom `BeforeChatModel` hook for history compaction when the token count approaches the context window limit.
- Make thresholds configurable with safe defaults.

**Non-Goals**
- Does not change tool interfaces or add new tools (beyond the existing `read` tool needed to retrieve offloaded content).
- Does not change session JSONL format (the compressed history is internal to the agent run, not persisted).

## 3. Current State

`internal/agent/agent.go` creates the agent with `Middlewares` only containing the approval middleware:

```go
return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    // ...
    Middlewares:   middlewares,
    MaxIterations: maxIterations,
    // No ModelRetryConfig
    // No reduction or history compaction
})
```

There is no protection against context-window exhaustion. The `MaxIterations: 1000` cap may never be reached because the model errors out first.

Token usage is tracked globally in `internal/model/chatmodel.go` via `TokenTracker`, but this value is only displayed in the TUI — the agent itself has no awareness of how close it is to the limit.

## 4. Proposed Design

### 4.1 Eino Built-in Reduction Middleware

Eino v0.7.37 provides a tool-result reduction middleware at `github.com/cloudwego/eino/adk/middlewares/reduction`. This returns an `adk.AgentMiddleware` struct that can be added to the `Middlewares` slice.

> **Important**: Eino v0.7.37 does **not** provide a built-in summarization middleware (`adk/middlewares/summarization` does not exist). History compaction must be implemented as a custom `BeforeChatModel` hook.

```go
import "github.com/cloudwego/eino/adk/middlewares/reduction"
```

### 4.2 Tool Output Reduction Middleware

Uses Eino's `reduction.NewToolResultMiddleware` to prevent oversized tool results from consuming all context. It combines two strategies:

1. **Offloading**: When a single tool result exceeds a character threshold, it is written to the filesystem and replaced with a summary directing the agent to use the `read` tool.
2. **Clearing**: When the total token count of all tool results exceeds a threshold, old tool results (outside a "keep recent" window) are replaced with a placeholder.

```go
reductionMW, err := reduction.NewToolResultMiddleware(ctx, &reduction.ToolResultConfig{
    Backend:                    reductionBackend(), // ~/.jcoding/reduction/
    ClearingTokenThreshold:     20_000,             // total tool-result tokens before clearing old ones
    KeepRecentTokens:           40_000,             // recent messages to keep intact
    OffloadingTokenLimit:       20_000,             // single tool result token threshold
    ReadFileToolName:           "read",             // matches our existing read tool name
    ClearToolResultPlaceholder: "[Old tool result content cleared]",
})
```

`reductionBackend()` creates a `reduction.Backend` implementation rooted at `~/.jcoding/reduction/`. This directory persists across sessions, allowing the agent to reference offloaded content if needed.

### 4.3 Custom History Compaction (BeforeChatModel Hook)

Since Eino has no built-in summarization middleware, we implement a lightweight `BeforeChatModel` hook that trims old messages when the token budget is near exhaustion. This is simpler and cheaper than model-based summarization.

```go
func historyCompactionHook(contextLimit int) func(context.Context, *adk.ChatModelAgentState) error {
    threshold := int(float64(contextLimit) * 0.75)
    if contextLimit <= 0 {
        threshold = 100_000
    }
    return func(ctx context.Context, state *adk.ChatModelAgentState) error {
        estimated := estimateTokens(state.Messages)
        if estimated < threshold {
            return nil
        }
        // Keep system message (first) + recent messages within budget
        state.Messages = trimOldMessages(state.Messages, threshold)
        return nil
    }
}
```

The `trimOldMessages` function preserves the system message and the most recent messages that fit within the threshold, discarding oldest non-system messages first. This is a deterministic, zero-cost operation (no extra API call).

**Future Enhancement**: A model-based summarization step could be added before trimming to produce a compressed summary of discarded messages. This would require an additional model call per compaction event.

Context limit is read from `internalmodel.GetModelContextLimit(cfg.Model)` (already implemented).

### 4.4 New Config Fields

```json
// ~/.jcoding/config.json
{
  "ClearingTokenThreshold": 20000,
  "KeepRecentTokens": 40000,
  "OffloadingTokenLimit": 20000
}
```

All default to zero, meaning use the code-level defaults above.

```go
// internal/config/config.go
type Config struct {
    // existing fields ...
    ClearingTokenThreshold int `json:"clearing_token_threshold,omitempty"`
    KeepRecentTokens       int `json:"keep_recent_tokens,omitempty"`
    OffloadingTokenLimit   int `json:"offloading_token_limit,omitempty"`
}
```

### 4.5 Integration in `agent.go`

All middlewares are passed via the `Middlewares` field (type `[]adk.AgentMiddleware`). There is no separate `Handlers` field in `ChatModelAgentConfig`.

```go
// History compaction middleware
compactionMW := adk.AgentMiddleware{
    BeforeChatModel: historyCompactionHook(contextLimit),
}

return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    // ...
    Middlewares: []adk.AgentMiddleware{
        langfuseMW,     // outermost: telemetry
        compactionMW,   // history compaction at 75% token usage
        reductionMW,    // tool output offloading and clearing
        approvalMW,     // approval + safe tool error handling
    },
})
```

### 4.6 Token Budget Awareness (Coordination with System Reminders)

The System Reminders design (separate document) injects token-usage warnings at 60% and 85% via `BeforeChatModel`. This creates a cooperative layered protection:

```
Token usage:
0% ──────── 60% ────── 75% ──────── 85% ──────── 100%
            │           │             │
     Reminder:       Compaction     Reminder:
   "watch length"   (trim old msgs) "finish soon"
```

The reminders inform the agent to produce shorter outputs *before* compaction fires, reducing the frequency of history trimming.

## 5. Alternatives Considered

### Model-based summarization via custom middleware
Considered but deferred. Using the same model to generate summaries of discarded history is more context-preserving but adds cost (one extra API call per compaction event) and latency. The simpler trim-based approach is implemented first; model-based summarization can be layered on top later.

### Use `/tmp` instead of `~/.jcoding/reduction/` for offloaded tool output
`~/.jcoding/reduction/` is preferred because: (1) offloaded content survives session boundaries and can be referenced across multiple turns; (2) the user can inspect oversized outputs for debugging. `/tmp` is cleaned automatically and is unsuitable for content referenced in the conversation history. A TTL-based cleanup of `~/.jcoding/reduction/` can be added later.

### Fixed 100k-character limit regardless of model
Rejected. Different models have different context windows. Tying the threshold to `GetModelContextLimit()` ensures the behaviour adapts when the user switches models via the TUI.

## 6. Migration & Compatibility

- `internal/agent/agent.go` updated to include reduction and compaction middlewares.
- `internal/config/config.go` gains three optional fields with zero-value fallbacks.
- The `~/.jcoding/reduction/` directory is created on first use if absent.
- No change to session JSONL, TUI messages, or tool interfaces.

## 7. Resolved Questions

1. **Eino version**: `github.com/cloudwego/eino v0.7.37` (pinned in `go.mod`) provides `adk/middlewares/reduction` with `NewToolResultMiddleware`. There is **no** built-in summarization middleware — only `reduction`, `filesystem`, and `skill` packages exist under `adk/middlewares/`.
2. **Summarization cost**: Moot for now — we use deterministic message trimming instead. Model-based summarization remains a future option, gated behind a config flag.
3. **Reduction backend directory**: Deferred to a follow-up. `~/.jcoding/reduction/` will accumulate files; a TTL-based cleanup (e.g. files older than 7 days) is recommended but not blocking.
