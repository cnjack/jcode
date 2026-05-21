package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"

	"github.com/cnjack/jcode/internal/telemetry"
)

// approvalMiddleware implements adk.ChatModelAgentMiddleware with both
// approval gating and safe tool-error handling (converting panics/errors to
// agent-visible strings instead of aborting the agent loop).
type approvalMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	approvalFunc ApprovalFunc
}

// newApprovalMiddleware creates a ChatModelAgentMiddleware that wraps tool calls
// with approval gating and safe error handling.
func newApprovalMiddleware(approvalFunc ApprovalFunc) adk.ChatModelAgentMiddleware {
	return &approvalMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		approvalFunc:                 approvalFunc,
	}
}

func (m *approvalMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (result string, retErr error) {
		// Recover from panics in tool execution so a single buggy tool
		// cannot crash the entire agent loop.
		defer func() {
			if r := recover(); r != nil {
				result = fmt.Sprintf("Tool execution panicked: %v", r)
				retErr = nil // surface as agent-visible string, not error
			}
		}()

		subSpan := telemetry.SubSpanFromContext(ctx)

		// Approval gate — traced as a separate "approval" span.
		if m.approvalFunc != nil {
			var finishApproval func(string)
			if subSpan != nil {
				finishApproval = subSpan("approval")
			}

			approved, err := m.approvalFunc(ctx, tCtx.Name, argumentsInJSON)

			if err != nil {
				msg := fmt.Sprintf("Tool approval error: %v", err)
				if finishApproval != nil {
					finishApproval(msg)
				}
				return msg, nil
			}
			if !approved {
				msg := "Tool execution was rejected by user. " +
					"IMPORTANT: The user has explicitly denied this operation. " +
					"Do NOT attempt to perform the same action using alternative tools, different commands, or workarounds. " +
					"Respect the user's decision and either ask the user how they would like to proceed or move on to a different task."
				if finishApproval != nil {
					finishApproval("rejected")
				}
				return msg, nil
			}
			if finishApproval != nil {
				finishApproval("approved")
			}
		}

		// Execution — traced as a separate "execution" span.
		var finishExec func(string)
		if subSpan != nil {
			finishExec = subSpan("execution")
		}

		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			if result != "" {
				result = fmt.Sprintf("%s\n\nTool execution failed: %v", result, err)
			} else {
				result = fmt.Sprintf("Tool execution failed: %v", err)
			}
			if finishExec != nil {
				finishExec(result)
			}
			return result, nil
		}
		if finishExec != nil {
			finishExec(result)
		}
		return result, nil
	}, nil
}

// NewTeammateHandlers returns the middleware stack for a teammate agent.
// It includes the approval + safe-tool-error middleware with the given approval function.
func NewTeammateHandlers(approvalFunc ApprovalFunc) []adk.ChatModelAgentMiddleware {
	return []adk.ChatModelAgentMiddleware{
		newApprovalMiddleware(approvalFunc),
	}
}
