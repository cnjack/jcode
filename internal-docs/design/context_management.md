# Context Management: Tool Output Reduction & History Summarization

> **Implemented**: 2026-03-29, Eino v0.8.5
> **Priority**: P0

## 1. Problem

Long sessions overflow the model's context window, terminating the agent with a hard API error. Two failure modes:

- **Single tool explosion**: A `read` on a 5000-line file produces 200k chars in one result.
- **History accumulation**: Many back-and-forth turns, grep/read chains, and large execute outputs gradually fill the context.

## 2. Implementation

Two middlewares run as `ChatModelAgentMiddleware` handlers (v0.8's new interface), applied in order: **summarization → reduction → approval**.

### 2.1 Summarization Middleware (`adk/middlewares/summarization`)

Eino v0.8.5 provides a built-in summarization middleware. When the conversation history exceeds the token threshold it calls the model to produce a compressed summary, replacing the old messages.

```go
summMw, err := summarization.New(ctx, &summarization.Config{
    Model: chatModel,
    Trigger: &summarization.TriggerCondition{
        ContextTokens: int(float64(contextLimit) * 0.75),
    },
    TranscriptFilePath: filepath.Join(config.ConfigDir(), "transcript.txt"),
})
handlers = append(handlers, summMw)
```

Fires at **75% context usage**. The full conversation so far is written to `~/.jcoding/transcript.txt` before summarization for debugging.

### 2.2 Reduction Middleware (`adk/middlewares/reduction`)

Prevents individual tool results from consuming all context. Two strategies:

1. **Truncation+offload**: When a single tool result exceeds `MaxLengthForTrunc` characters, it is written to `~/.jcoding/reduction/` and replaced with a pointer. The agent uses the `read` tool to retrieve it if needed.
2. **Clearing**: When cumulative tool result tokens exceed `MaxTokensForClear`, older tool results are cleared to keep the context clean.

```go
reductionMw, err := reduction.New(ctx, &reduction.Config{
    Backend:           &localReductionBackend{rootDir: config.ConfigDir()},
    RootDir:           filepath.Join(config.ConfigDir(), "reduction"),
    MaxLengthForTrunc: 50000,
    MaxTokensForClear: int64(float64(contextLimit) * 0.60),
    ReadFileToolName:  "read",
    ToolConfig: map[string]*reduction.ToolReductionConfig{
        "read": {SkipClear: true}, // never clear read-tool results
    },
})
handlers = append(handlers, reductionMw)
```

### 2.3 Reduction Backend

`localReductionBackend` (in `cmd/coding/main.go`) implements `reduction.Backend` by writing files to `~/.jcoding/reduction/`. The interface requires:

```go
type localReductionBackend struct{ rootDir string }

func (b *localReductionBackend) Write(ctx context.Context, req *filesystem.WriteRequest) error
func (b *localReductionBackend) Read(ctx context.Context, req *filesystem.ReadRequest) (*filesystem.ReadResponse, error)
```

Files persist across sessions; the agent can re-read them by path if needed.

### 2.4 Model Retry

`ModelRetryConfig` on the agent config handles transient API failures:

```go
ModelRetryConfig: &adk.ModelRetryConfig{
    MaxRetries: 3,
},
```

### 2.5 Handler Ordering

```go
// cmd/coding/main.go — createAgent()
handlers := []adk.ChatModelAgentMiddleware{
    summMw,        // outermost: summarization at 75% tokens
    reductionMw,   // tool output truncation/clearing at 60%
    reminderMw,    // system reminders (see system_reminders.md)
}
// approval middleware is appended by NewAgent as innermost
```

The layered protection with system reminders:
```
Token usage:
0% ──── 60% ──── 75% ──── 85% ──── 100%
         │        │         │
    reduction  summ fires  reminder:
    clears old  (model)   "finish soon"
    tool results
```

## 3. Files

| File | Role |
|------|------|
| `cmd/coding/main.go` | Constructs `summMw`, `reductionMw`, `localReductionBackend` inside `createAgent()` |
| `internal/model/chatmodel.go` | `GetModelContextLimit(model)` returns per-model limit |

## 4. Non-Goals

- No new config.json fields; thresholds are hardcoded (models vary too much for user-tunable defaults to be meaningful).
- Session JSONL format unchanged.
- `~/.jcoding/reduction/` cleanup (TTL-based) deferred.
