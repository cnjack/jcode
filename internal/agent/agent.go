package agent

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"

	"github.com/cnjack/jcode/internal/memory"
	internalmodel "github.com/cnjack/jcode/internal/model"
)

const maxIterations = 1000

type ApprovalFunc func(ctx context.Context, toolName, toolArgs string) (bool, error)

// ReviewDeniedError is returned by an ApprovalFunc when the automatic safety
// reviewer (not the user) denied a call. The approval middleware special-cases
// it to surface the reviewer's rationale to the model with distinct guidance,
// instead of the generic "rejected by user" message.
type ReviewDeniedError struct{ Reason string }

func (e *ReviewDeniedError) Error() string {
	if e == nil || e.Reason == "" {
		return "automatic safety reviewer denied the action"
	}
	return e.Reason
}

// NewAgent creates a ChatModelAgent with the following handler stack
// (outermost to innermost):
//
//	Handlers: [langfuse tracing, ...caller handlers, approval+safeTool]
//
// ModelRetryConfig is always enabled (3 retries with default exponential backoff).
func NewAgent(
	ctx context.Context,
	chatmodel model.ToolCallingChatModel,
	tools []tool.BaseTool,
	instruction string,
	approvalFunc ApprovalFunc,
	middlewares []adk.ChatModelAgentMiddleware,
	handlers []adk.ChatModelAgentMiddleware,
) (*adk.ChatModelAgent, error) {
	// Handler order is outermost → innermost: tracing middlewares first, then the
	// caller's handlers, then the hook + approval + safe-tool-error stack innermost
	// so that summarization/reduction see the raw tool output first.
	enhanced := append([]adk.ChatModelAgentMiddleware{}, middlewares...)
	enhanced = append(enhanced, handlers...)
	// PreToolUse hook sits OUTSIDE approval: it can deny, rewrite args, or mark the
	// call pre-approved (so approval skips its prompt) before the gate runs.
	enhanced = append(enhanced, newPreHookMiddleware())
	enhanced = append(enhanced, newApprovalMiddleware(approvalFunc))
	// PostToolUse hook sits INSIDE approval, wrapping the raw tool, so it sees the
	// true execution error and can rewrite the result.
	enhanced = append(enhanced, newPostHookMiddleware())
	// Innermost: memory usage accounting observes approved executions only
	// and sees raw endpoint errors (a failed read is not memory usage).
	enhanced = append(enhanced, memory.NewUsageMiddleware())

	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "coding",
		Description: "A agent for coding",
		Instruction: instruction,
		Model:       chatmodel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		},
		MaxIterations: maxIterations,
		Handlers:      enhanced,
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries:  5,
			IsRetryAble: internalmodel.IsRetryable,
			BackoffFunc: internalmodel.SmartBackoff,
		},
	})
}
