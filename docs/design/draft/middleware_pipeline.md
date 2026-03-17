# Middleware Pipeline Hardening

## 1. Problem Statement

The current agent loop has fragile error handling and no resilience against transient API failures.

The approval middleware in `internal/agent/agent.go` is implemented via `adk.AgentMiddleware.WrapToolCall` using `compose.ToolMiddleware` — a lower-level interface that bypasses Eino's `ChatModelAgentMiddleware` lifecycle hooks. Consequences:

- Tool errors are caught manually inside the approval wrapper. Streaming tool errors are silently dropped (no `WrapStreamableToolCall`).
- `compose.IsInterruptRerunError` is never checked — interrupt signals meant to propagate to the agent are accidentally swallowed.
- Model API errors (429 rate-limit, connection reset) terminate the entire agent run with no retry.
- Multiple middlewares are composed ad hoc; there is no declared ordering contract.

## 2. Goals & Non-Goals

**Goals**
- Replace manual tool-error handling with an Eino-native `ChatModelAgentMiddleware` that correctly wraps both invokable and streamable tool calls.
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
1. `WrapStreamableToolCall` is missing — streaming tool errors are not caught.
2. `compose.IsInterruptRerunError(err)` is never consulted — interrupt signals cannot propagate.
3. `ModelRetryConfig` is absent; any 429 or network blip kills the run.

## 4. Proposed Design

### 4.1 New file: `internal/agent/middleware.go`

Extract tool-safety concerns into a dedicated `ChatModelAgentMiddleware`:

```go
// safeToolMiddleware converts tool errors into agent-readable strings,
// preserving interrupt errors so they can propagate correctly.
type safeToolMiddleware struct {
    *adk.BaseChatModelAgentMiddleware
}

func (m *safeToolMiddleware) WrapInvokableToolCall(
    _ context.Context,
    endpoint adk.InvokableToolCallEndpoint,
    _ *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
    return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
        result, err := endpoint(ctx, args, opts...)
        if err != nil {
            if _, ok := compose.IsInterruptRerunError(err); ok {
                return "", err // must propagate
            }
            return fmt.Sprintf("[tool error] %v", err), nil
        }
        return result, nil
    }, nil
}

func (m *safeToolMiddleware) WrapStreamableToolCall(
    _ context.Context,
    endpoint adk.StreamableToolCallEndpoint,
    _ *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
    return func(ctx context.Context, args string, opts ...tool.Option) (*schema.StreamReader[string], error) {
        sr, err := endpoint(ctx, args, opts...)
        if err != nil {
            if _, ok := compose.IsInterruptRerunError(err); ok {
                return nil, err
            }
            return singleChunkReader(fmt.Sprintf("[tool error] %v", err)), nil
        }
        return wrapSafeReader(sr), nil
    }, nil
}
```

The approval middleware is migrated to implement `ChatModelAgentMiddleware` as well, keeping the same approval logic but now using `WrapInvokableToolCall` and `WrapStreamableToolCall`.

### 4.2 Model Retry Config

Added to `adk.ChatModelAgentConfig`:

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

`MaxRetries` defaults to 3. Eino uses exponential back-off internally.

### 4.3 Optional config field

```json
// ~/.jcoding/config.json
{
  "MaxRetries": 3
}
```

Zero value means use the hardcoded default.

### 4.4 Middleware Composition Order

```go
// internal/agent/agent.go — declared order
Middlewares: []adk.AgentMiddleware{
    langfuseMiddleware,      // outermost: telemetry wraps everything
    approvalMiddleware,      // approval before execution
    &safeToolMiddleware{},   // innermost: catches errors closest to the tool
},
```

Execution flow for a tool call:
```
request → langfuse → approval → safeTool → actual tool
response ← langfuse ← approval ← safeTool ← actual tool
```

`safeToolMiddleware` is innermost so all other middlewares receive clean results or properly propagated interrupt errors — not raw Go errors.

## 5. Alternatives Considered

### Keep `compose.ToolMiddleware` and add streaming handling there
Rejected. Adding `WrapStreamableToolCall` requires moving to `ChatModelAgentMiddleware` anyway. The lower-level `compose.ToolMiddleware` interface does not provide lifecycle hooks like `BeforeModelRewriteState` which other designs depend on.

### Separate approval into a standalone `ChatModelAgentMiddleware`
Yes, this is part of the proposal. The approval logic itself does not change — only the wrapper interface changes.

## 6. Migration & Compatibility

- `internal/agent/middleware.go` is a new file; no existing files are deleted.
- `internal/agent/agent.go` is updated to remove the inline `compose.ToolMiddleware` block and reference the new middleware types.
- The `MaxRetries` config field defaults to 0 (meaning use the code default of 3) — no forced migration.
- Behaviour change: streaming tool errors are now surfaced to the model as `[tool error] ...` strings instead of being silently dropped. This is strictly an improvement.

## 7. Open Questions

1. Does the current Eino version used in `go.mod` expose `adk.ChatModelAgentMiddleware` as the primary interface, or is `adk.AgentMiddleware` still in use? The migration depends on which interface `NewChatModelAgent` accepts.
2. Should `MaxRetries` be per-provider or global? Some providers (e.g. expensive ones) may want 0 retries.
