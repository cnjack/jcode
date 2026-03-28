# Middleware Pipeline Hardening

> **Last verified**: 2026-03-28 against codebase at `github.com/cloudwego/eino v0.7.37`
> **Priority**: P1 — High (safe tool error handling + ModelRetryConfig are prerequisites for reliable operation)
> **Scope reduction**: The middleware ordering and `safeToolMiddleware` extraction are the high-value items. Circuit breaker / generic middleware framework are **not needed** — Eino's `ModelRetryConfig` covers retry, and the ordering is just documentation.

## 1. Problem Statement

The current agent loop has fragile error handling and no resilience against transient API failures.

The approval middleware in `internal/agent/agent.go` is implemented via `adk.AgentMiddleware.WrapToolCall` using `compose.ToolMiddleware` — it wraps `Invokable` only. Consequences:

- Tool errors are caught manually inside the approval wrapper and converted to strings. Streaming tool errors are silently dropped (no `Streamable` wrapper).
- `compose.IsInterruptRerunError` is never checked — interrupt signals meant to propagate to the agent are accidentally swallowed.
- Model API errors (429 rate-limit, connection reset) terminate the entire agent run with no retry.
- Multiple middlewares are composed ad hoc; there is no declared ordering contract.

## 2. Goals & Non-Goals

**Goals**
- Replace manual tool-error handling with proper `compose.ToolMiddleware` wrapping that covers both `Invokable` and `Streamable` tool calls.
- Add a `ModelRetryConfig` to the `ChatModelAgentConfig` for automatic back-off on retriable API errors.
- Establish a documented, deterministic middleware composition order.

**Non-Goals**
- Does not add new tools or change approval logic rules.
- Does not change session recording or TUI message types.
- Does not introduce telemetry concerns (handled in a separate design).

## 3. Current State

### `internal/agent/agent.go`

```go
middlewares = append(middlewares, adk.AgentMiddleware{
    WrapToolCall: compose.ToolMiddleware{
        Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
            return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
                // approval check ...
                out, err := next(ctx, input)
                if err != nil {
                    // error silently converted to string — no interrupt check
                    return &compose.ToolOutput{Result: fmt.Sprintf("Tool execution failed: %v", err)}, nil
                }
                return out, nil
            }
        },
    },
})

return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    // ...
    Middlewares: middlewares,
    // No ModelRetryConfig
})
```

Issues:
1. `Streamable` field of `compose.ToolMiddleware` is not set — streaming tool errors are not caught.
2. `compose.IsInterruptRerunError(err)` is never consulted — interrupt signals cannot propagate.
3. `ModelRetryConfig` is absent; any 429 or network blip kills the run.

## 4. Proposed Design

### 4.1 Safe Tool Error Handling via `compose.ToolMiddleware`

Eino v0.7.37's `compose.ToolMiddleware` struct has four fields:
- `Invokable` — wraps non-streaming tool calls
- `Streamable` — wraps streaming tool calls
- `EnhancedInvokable` — wraps enhanced non-streaming tool calls
- `EnhancedStreamable` — wraps enhanced streaming tool calls

The safe-tool concern should be implemented as a separate `adk.AgentMiddleware` with a `compose.ToolMiddleware` that covers both `Invokable` and `Streamable`:

```go
// internal/agent/middleware.go

// safeToolMiddleware returns an AgentMiddleware that converts tool errors into
// agent-readable strings, preserving interrupt errors so they can propagate.
func safeToolMiddleware() adk.AgentMiddleware {
    return adk.AgentMiddleware{
        WrapToolCall: compose.ToolMiddleware{
            Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
                return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
                    out, err := next(ctx, input)
                    if err != nil {
                        if _, ok := compose.IsInterruptRerunError(err); ok {
                            return nil, err // must propagate
                        }
                        return &compose.ToolOutput{
                            Result: fmt.Sprintf("[tool error] %v", err),
                        }, nil
                    }
                    return out, nil
                }
            },
            Streamable: func(next compose.StreamableToolEndpoint) compose.StreamableToolEndpoint {
                return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutputStream, error) {
                    sr, err := next(ctx, input)
                    if err != nil {
                        if _, ok := compose.IsInterruptRerunError(err); ok {
                            return nil, err
                        }
                        return singleChunkStream(fmt.Sprintf("[tool error] %v", err)), nil
                    }
                    return wrapSafeStream(sr), nil
                }
            },
        },
    }
}
```

The approval middleware is similarly updated to set both `Invokable` and `Streamable` wrappers with the same approval logic.

### 4.2 Model Retry Config

`ModelRetryConfig` exists on `adk.ChatModelAgentConfig` (verified in Eino v0.7.37):

```go
type ModelRetryConfig struct {
    MaxRetries  int
    IsRetryAble func(ctx context.Context, err error) bool
    BackoffFunc func(ctx context.Context, attempt int) time.Duration
}
```

Added to the agent config:

```go
ModelRetryConfig: &adk.ModelRetryConfig{
    MaxRetries: 3,
    IsRetryAble: func(_ context.Context, err error) bool {
        s := err.Error()
        return strings.Contains(s, "429") ||
            strings.Contains(s, "Too Many Requests") ||
            strings.Contains(s, "rate limit") ||
            strings.Contains(s, "connection reset by peer")
    },
},
```

`MaxRetries` defaults to 3. Eino provides `BackoffFunc` for custom back-off; when nil, it uses its internal default.

### 4.3 Optional config field

```json
// ~/.jcoding/config.json
{
  "MaxRetries": 3
}
```

Zero value means use the hardcoded default.

### 4.4 Middleware Composition Order

All middlewares use the single `Middlewares` field (type `[]adk.AgentMiddleware`) on `ChatModelAgentConfig`:

```go
// internal/agent/agent.go — declared order
Middlewares: []adk.AgentMiddleware{
    langfuseMiddleware,      // outermost: telemetry wraps everything
    approvalMiddleware,      // approval before execution
    safeToolMiddleware(),    // innermost: catches errors closest to the tool
},

// ModelRetryConfig is a separate top-level field, not a middleware:
ModelRetryConfig: &adk.ModelRetryConfig{...},
```

Execution flow for a tool call:
```
request → langfuse → approval → safeTool → actual tool
response ← langfuse ← approval ← safeTool ← actual tool
```

`safeToolMiddleware` is innermost so all other middlewares receive clean results or properly propagated interrupt errors — not raw Go errors.

## 5. Alternatives Considered

### Keep `Invokable`-only wrapper and skip streaming handling
Rejected. The current code silently drops streaming tool errors. The `Streamable` field of `compose.ToolMiddleware` exists specifically for this purpose.

### Use `BeforeChatModel` / `AfterChatModel` for error handling
Rejected. These hooks fire before/after the model call, not around individual tool calls. Tool-level error wrapping must use `WrapToolCall` with `compose.ToolMiddleware`.

### Separate approval into a standalone `AgentMiddleware`
Yes, this is part of the proposal. The approval logic itself does not change — only the wrapper is extended to cover `Streamable` tool calls and check `compose.IsInterruptRerunError`.

## 6. Migration & Compatibility

- `internal/agent/middleware.go` is a new file; no existing files are deleted.
- `internal/agent/agent.go` is updated to extract the inline `compose.ToolMiddleware` block into `safeToolMiddleware()` and add `ModelRetryConfig`.
- The `MaxRetries` config field defaults to 0 (meaning use the code default of 3) — no forced migration.
- Behaviour change: streaming tool errors are now surfaced to the model as `[tool error] ...` strings instead of being silently dropped. This is strictly an improvement.

## 7. Codebase Verification Notes

- **Confirmed**: `adk.AgentMiddleware` is a struct with fields `AdditionalInstruction`, `AdditionalTools`, `BeforeChatModel`, `AfterChatModel`, `WrapToolCall`. There is no `ChatModelAgentMiddleware` interface in Eino v0.7.37.
- **Confirmed**: `compose.ToolMiddleware` has `Invokable`, `Streamable`, `EnhancedInvokable`, `EnhancedStreamable` fields.
- **Confirmed**: `compose.IsInterruptRerunError(err)` exists in `compose/interrupt.go`.
- **Confirmed**: `adk.ModelRetryConfig` exists with `MaxRetries`, `IsRetryAble`, `BackoffFunc`.
- **Confirmed**: Current `agent.go` only sets `Invokable` — `Streamable` is not set.
- **Confirmed**: `ChatModelAgentConfig.Middlewares` is `[]AgentMiddleware` — there is no separate `Handlers` field.

## 8. Resolved Questions

1. **Eino interface**: The current Eino v0.7.37 uses `adk.AgentMiddleware` struct (not an interface). `ChatModelAgentMiddleware` does not exist as a type — only as a comment reference. All middleware functionality is expressed through `AgentMiddleware` struct fields.
2. **Per-provider retries**: Deferred. `MaxRetries` is global for now. A per-provider override could be added to `ProviderConfig` in a future iteration.
