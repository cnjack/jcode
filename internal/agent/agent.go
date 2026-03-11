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

func NewAgent(ctx context.Context, chatmodel model.ToolCallingChatModel, tools []tool.BaseTool, instruction string, approvalFunc ApprovalFunc, middlewares ...adk.AgentMiddleware) (*adk.ChatModelAgent, error) {
	if approvalFunc != nil {
		middlewares = append(middlewares, adk.AgentMiddleware{
			WrapToolCall: compose.ToolMiddleware{
				Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
					return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
						approved, err := approvalFunc(ctx, input.Name, input.Arguments)
						if err != nil {
							return nil, err
						}
						if !approved {
							return &compose.ToolOutput{Result: "Tool execution was rejected by user"}, nil
						}
						return next(ctx, input)
					}
				},
			},
		})
	}

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
	})
}
