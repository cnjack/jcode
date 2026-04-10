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

// AgentOptions holds optional middlewares injected via functional options.
type AgentOptions struct {
	compaction adk.ChatModelAgentMiddleware
	budget     adk.ChatModelAgentMiddleware
	recovery   adk.ChatModelAgentMiddleware
}

// AgentOption is a functional option for NewAgent.
type AgentOption func(*AgentOptions)

// WithCompaction injects a context compaction middleware.
func WithCompaction(m adk.ChatModelAgentMiddleware) AgentOption {
	return func(o *AgentOptions) { o.compaction = m }
}

// WithBudget injects a budget tracking middleware.
func WithBudget(m adk.ChatModelAgentMiddleware) AgentOption {
	return func(o *AgentOptions) { o.budget = m }
}

// WithRecovery injects an error recovery middleware.
func WithRecovery(m adk.ChatModelAgentMiddleware) AgentOption {
	return func(o *AgentOptions) { o.recovery = m }
}

func defaultOptions() *AgentOptions {
	return &AgentOptions{}
}

// NewAgent creates a ChatModelAgent with the following middleware stack
// (outermost to innermost):
//
//	Middlewares (old-style): [langfuse]
//	Handlers (new-style):   [budget?, compaction?, recovery?, ..., approval+safeTool]
//
// ModelRetryConfig is always enabled (3 retries with default exponential backoff).
// The opts parameter is variadic so existing callers remain unchanged.
func NewAgent(
	ctx context.Context,
	chatmodel model.ToolCallingChatModel,
	tools []tool.BaseTool,
	instruction string,
	approvalFunc ApprovalFunc,
	middlewares []adk.AgentMiddleware,
	handlers []adk.ChatModelAgentMiddleware,
	opts ...AgentOption,
) (*adk.ChatModelAgent, error) {
	options := defaultOptions()
	for _, o := range opts {
		o(options)
	}

	// Inject optional middlewares before user-supplied handlers.
	// Order: budget → compaction → recovery → (caller handlers) → approval.
	var enhanced []adk.ChatModelAgentMiddleware
	if options.budget != nil {
		enhanced = append(enhanced, options.budget)
	}
	if options.compaction != nil {
		enhanced = append(enhanced, options.compaction)
	}
	if options.recovery != nil {
		enhanced = append(enhanced, options.recovery)
	}
	enhanced = append(enhanced, handlers...)

	// Approval + safe-tool-error middleware is always the innermost handler
	// so that summarization/reduction see the raw tool output first.
	enhanced = append(enhanced, newApprovalMiddleware(approvalFunc))

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
		Handlers:      enhanced,
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries: 3,
		},
	})
}
