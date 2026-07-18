package agent

import (
	"context"
	"fmt"

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
// All tools passed to this compatibility entry point are disclosed directly to
// the model. Use NewAgentWithToolPlan to progressively disclose deferred tools.
// ModelRetryConfig is always enabled (5 retries with smart backoff).
func NewAgent(
	ctx context.Context,
	chatmodel model.ToolCallingChatModel,
	tools []tool.BaseTool,
	instruction string,
	approvalFunc ApprovalFunc,
	middlewares []adk.ChatModelAgentMiddleware,
	handlers []adk.ChatModelAgentMiddleware,
) (*adk.ChatModelAgent, error) {
	return newAgent(
		ctx, chatmodel, tools, nil, toolDisclosureGroups{},
		instruction, approvalFunc, middlewares, handlers,
	)
}

func newAgent(
	ctx context.Context,
	chatmodel model.ToolCallingChatModel,
	directTools []tool.BaseTool,
	deferredTools []tool.BaseTool,
	disclosureGroups toolDisclosureGroups,
	instruction string,
	approvalFunc ApprovalFunc,
	middlewares []adk.ChatModelAgentMiddleware,
	handlers []adk.ChatModelAgentMiddleware,
) (*adk.ChatModelAgent, error) {
	// Handler order is outermost → innermost: tracing middlewares first, then the
	// tool-search middleware, then the caller's state-rewriting handlers, and
	// finally the hook + approval + safe-tool-error stack. Tool search must inspect
	// its result before summarization/reduction can remove that result from history.
	enhanced := append([]adk.ChatModelAgentMiddleware{}, middlewares...)
	toolSearchMiddleware, err := newToolSearchMiddleware(ctx, deferredTools)
	if err != nil {
		return nil, err
	}
	if toolSearchMiddleware != nil {
		enhanced = append(enhanced, toolSearchMiddleware)
	}
	// Observe the final model-visible schema after progressive disclosure has
	// rewritten it, but before compaction/recovery/approval handlers. The sink is
	// opt-in through the runner context and records metadata only.
	toolObservationMiddleware, err := newToolObservationMiddleware(ctx, deferredTools)
	if err != nil {
		return nil, fmt.Errorf("agent tool observation: %w", err)
	}
	enhanced = append(enhanced, toolObservationMiddleware)
	// Observation stays outside group expansion so recorded search matches are
	// exactly the names Eino will recover from history on the next generation.
	// The wrapper is still outside caller handlers and the approval stack.
	if disclosureMiddleware := newToolSearchDisclosureMiddleware(disclosureGroups); disclosureMiddleware != nil {
		enhanced = append(enhanced, disclosureMiddleware)
	}
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
				Tools: append([]tool.BaseTool(nil), directTools...),
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
