package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
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
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		// Approval gate
		if m.approvalFunc != nil {
			approved, err := m.approvalFunc(ctx, tCtx.Name, argumentsInJSON)
			if err != nil {
				return fmt.Sprintf("Tool approval error: %v", err), nil
			}
			if !approved {
				return "Tool execution was rejected by user. " +
					"IMPORTANT: The user has explicitly denied this operation. " +
					"Do NOT attempt to perform the same action using alternative tools, different commands, or workarounds. " +
					"Respect the user's decision and either ask the user how they would like to proceed or move on to a different task.", nil
			}
		}

		// Safe execution: convert errors to agent-visible strings
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			return fmt.Sprintf("Tool execution failed: %v", err), nil
		}
		return result, nil
	}, nil
}
