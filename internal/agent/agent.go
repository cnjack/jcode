package agent

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

const maxIterations = 1000

type ApprovalFunc func(ctx context.Context, toolName, toolArgs string) (bool, error)

// NewAgent creates a ChatModelAgent with the following middleware stack
// (outermost to innermost):
//
//	Middlewares (old-style): [langfuse]
//	Handlers (new-style):   [summarization, reduction, approval+safeTool]
//
// ModelRetryConfig is always enabled (3 retries with default exponential backoff).
func NewAgent(
	ctx context.Context,
	chatmodel model.ToolCallingChatModel,
	tools []tool.BaseTool,
	instruction string,
	approvalFunc ApprovalFunc,
	middlewares []adk.AgentMiddleware,
	handlers []adk.ChatModelAgentMiddleware,
) (*adk.ChatModelAgent, error) {
	// Approval + safe-tool-error middleware is always the innermost handler
	// so that summarization/reduction see the raw tool output first.
	handlers = append(handlers, newApprovalMiddleware(approvalFunc))

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
		Middlewares:   middlewares,
		Handlers:      handlers,
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries: 3,
		},
	})
}
