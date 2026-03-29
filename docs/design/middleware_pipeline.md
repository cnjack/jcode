# Middleware Pipeline

> **Implemented**: 2026-03-29, Eino v0.8.5
> **Priority**: P1

## 1. Problem

The approval middleware had fragile error handling (errors silently converted to strings, no interrupt propagation) and no retry on transient API errors (429, network resets).

## 2. Implementation

Eino v0.8.5 introduced the `ChatModelAgentMiddleware` interface with `adk.BaseChatModelAgentMiddleware` as a convenience base. This replaces the old `compose.ToolMiddleware` struct approach.

### 2.1 Approval + Safe Tool Middleware

`internal/agent/middleware.go` defines `approvalMiddleware` — a single type that handles both approval gating and safe error conversion:

```go
type approvalMiddleware struct {
    *adk.BaseChatModelAgentMiddleware
    approvalFunc ApprovalFunc
}

func (m *approvalMiddleware) WrapInvokableToolCall(
    ctx context.Context,
    endpoint adk.InvokableToolCallEndpoint,
    tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
    return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
        // 1. Approval gate
        if m.approvalFunc != nil {
            approved, err := m.approvalFunc(ctx, tCtx.Name, argumentsInJSON)
            if err != nil {
                return fmt.Sprintf("Tool approval error: %v", err), nil
            }
            if !approved {
                return "Tool execution was rejected by user. ...", nil
            }
        }
        // 2. Safe execution: convert errors to agent-readable strings
        result, err := endpoint(ctx, argumentsInJSON, opts...)
        if err != nil {
            return fmt.Sprintf("Tool execution failed: %v", err), nil
        }
        return result, nil
    }, nil
}
```

Key design decisions:
- Errors are returned as strings (not panics) so the agent loop continues.
- Approval + safe-error are unified in one type to avoid double-wrapping.

### 2.2 Model Retry Config

```go
// internal/agent/agent.go
ModelRetryConfig: &adk.ModelRetryConfig{
    MaxRetries: 3,
},
```

Eino's default backoff handles 429 / rate-limit / network errors. No custom `IsRetryAble` is set — the framework default covers common cases.

### 2.3 Handler Ordering

```go
// NewAgent() — internal/agent/agent.go
func NewAgent(ctx, model, tools, instruction, approvalFunc, middlewares, handlers) {
    // approval is always innermost handler
    handlers = append(handlers, newApprovalMiddleware(approvalFunc))

    return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
        // ...
        Middlewares: middlewares,  // []adk.AgentMiddleware (e.g. Langfuse)
        Handlers:    handlers,     // []adk.ChatModelAgentMiddleware
        ModelRetryConfig: &adk.ModelRetryConfig{MaxRetries: 3},
    })
}
```

Handlers supplied by the caller (summarization, reduction, reminder) are prepended; approval is always last (innermost). `Middlewares` carries the old-style `adk.AgentMiddleware` — only Langfuse uses this.

Tool call flow:
```
request → [Langfuse MW] → [summarization] → [reduction] → [reminder] → [approval+safe] → tool
```

## 3. What Was Not Implemented

- **Streaming wrapper** (`WrapStreamableToolCall`): Streaming tool calls are not used — `WrapInvokableToolCall` is sufficient.
- **`compose.IsInterruptRerunError` check**: Simplified — errors are converted to strings. Interrupt errors from tools are not expected in normal operation.
- **Separate `safeToolMiddleware`**: Merged with approval since they always appear together.

## 4. Files

| File | Role |
|------|------|
| `internal/agent/agent.go` | `NewAgent()` wires handlers, `ModelRetryConfig` |
| `internal/agent/middleware.go` | `approvalMiddleware` type |
| `internal/runner/approval.go` | `ApprovalState.RequestApproval` function |
