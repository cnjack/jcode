package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/dynamictool/toolsearch"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
)

const deferredToolBatchInstruction = `When tool_search is available:
- A purpose-built capability required by the user's task or an applicable loaded skill may be deferred when its schema is not attached. Search for it before substituting execute or another generic tool.
- If the user or an applicable loaded skill gives exact tool names, use select:<tool_name> (comma-separate a small related set); otherwise use a short capability keyword.
- Call tool_search in a separate tool-call batch and wait for its result before calling any newly loaded target tool. If search returns no match, do not repeat the same search or imitate the missing capability with a shell/UI workaround. Use a legitimate already-attached alternative when appropriate; otherwise report the capability as unavailable.`

// NewAgentWithToolPlan creates a ChatModelAgent from a validated exposure plan.
// Direct tools are disclosed on the first model call. Deferred tools are kept in
// the executable tool registry but their schemas are disclosed only after the
// model calls tool_search. Hidden tools are neither disclosed nor registered.
//
// DirectModelOnly is deliberately unsupported by the current chat-model path:
// it has no safe way to expose a schema without also registering an executable
// endpoint. A non-empty partition therefore fails closed.
func NewAgentWithToolPlan(
	ctx context.Context,
	chatmodel model.ToolCallingChatModel,
	plan ToolPlan,
	instruction string,
	approvalFunc ApprovalFunc,
	middlewares []adk.ChatModelAgentMiddleware,
	handlers []adk.ChatModelAgentMiddleware,
) (*adk.ChatModelAgent, error) {
	direct, deferred, disclosureGroups, err := executableToolsFromPlan(ctx, plan)
	if err != nil {
		return nil, err
	}
	if len(deferred) > 0 {
		instruction = strings.TrimRight(instruction, "\n") + "\n\n" + deferredToolBatchInstruction
	}
	return newAgent(
		ctx, chatmodel, direct, deferred, disclosureGroups,
		instruction, approvalFunc, middlewares, handlers,
	)
}

func newToolSearchMiddleware(ctx context.Context, deferred []tool.BaseTool) (adk.ChatModelAgentMiddleware, error) {
	if len(deferred) == 0 {
		return nil, nil
	}

	middleware, err := toolsearch.New(ctx, &toolsearch.Config{
		DynamicTools:       append([]tool.BaseTool(nil), deferred...),
		UseModelToolSearch: false,
	})
	if err != nil {
		return nil, fmt.Errorf("agent tool search: %w", err)
	}
	return middleware, nil
}

func executableToolsFromPlan(
	ctx context.Context,
	plan ToolPlan,
) ([]tool.BaseTool, []tool.BaseTool, toolDisclosureGroups, error) {
	groups := []struct {
		name     string
		exposure ToolExposure
		tools    []ToolDescriptor
	}{
		{name: "direct", exposure: ToolExposureDirect, tools: plan.Direct},
		{name: "deferred", exposure: ToolExposureDeferred, tools: plan.Deferred},
		{name: "hidden", exposure: ToolExposureHidden, tools: plan.Hidden},
		{name: "direct_model_only", exposure: ToolExposureDirectModelOnly, tools: plan.DirectModelOnly},
	}

	all := make([]ToolDescriptor, 0,
		len(plan.Direct)+len(plan.Deferred)+len(plan.Hidden)+len(plan.DirectModelOnly))
	for _, group := range groups {
		for _, descriptor := range group.tools {
			if descriptor.Exposure != group.exposure {
				return nil, nil, toolDisclosureGroups{}, fmt.Errorf(
					"agent tool plan: tool %q is in %s but declares exposure %q",
					descriptor.Name, group.name, descriptor.Exposure,
				)
			}
		}
		all = append(all, group.tools...)
	}

	validated, err := validateDescriptors(ctx, all)
	if err != nil {
		return nil, nil, toolDisclosureGroups{}, fmt.Errorf("agent tool plan: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return nil, nil, toolDisclosureGroups{}, fmt.Errorf("agent tool plan: %w", err)
	}

	if len(plan.DirectModelOnly) > 0 {
		names := make([]string, len(plan.DirectModelOnly))
		for i, descriptor := range plan.DirectModelOnly {
			names[i] = strings.TrimSpace(descriptor.Name)
		}
		sort.Strings(names)
		return nil, nil, toolDisclosureGroups{}, fmt.Errorf(
			"agent tool plan: direct_model_only tools are unsupported by the chat model path: %s",
			strings.Join(names, ", "),
		)
	}

	directEnd := len(plan.Direct)
	deferredEnd := directEnd + len(plan.Deferred)
	direct := descriptorTools(validated[:directEnd])
	deferredDescriptors := validated[directEnd:deferredEnd]
	deferred := descriptorTools(deferredDescriptors)
	return direct, deferred, disclosureGroupsFromDescriptors(deferredDescriptors), nil
}

func descriptorTools(descriptors []ToolDescriptor) []tool.BaseTool {
	if len(descriptors) == 0 {
		return nil
	}
	result := make([]tool.BaseTool, len(descriptors))
	for i, descriptor := range descriptors {
		result[i] = descriptor.Tool
	}
	return result
}
