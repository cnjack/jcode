package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"

	"github.com/cnjack/jcode/internal/agent"
	internaltools "github.com/cnjack/jcode/internal/tools"
)

type commandToolPolicy struct {
	exposure        agent.ToolExposure
	source          string
	bundle          string
	disclosureGroup string
	transports      []string
	modes           []string
	approvalClass   string
}

var commandToolPolicies = map[string]commandToolPolicy{
	"read":             directPolicy("core.files", allModes, "read"),
	"grep":             directPolicy("core.files", allModes, "read"),
	"edit":             directPolicy("core.files", normalMode, "write"),
	"write":            directPolicy("core.files", normalMode, "write"),
	"execute":          directPolicy("core.shell", allModes, "execute"),
	"check_background": directPolicy("core.shell", normalMode, "read"),
	"todowrite":        directPolicy("session.todo", allModes, "session"),
	"todoread":         directPolicy("session.todo", allModes, "read"),
	"ask_user": scopedDirectPolicy(
		"session.question", allModes, tuiAndWebTransports, "interactive",
	),
	"load_skill": directPolicy("skills", normalMode, "read"),
	"subagent":   directPolicy("delegation.subagent", normalMode, "orchestration"),
	"show_artifact": scopedDirectPolicy(
		"web.artifact", allModes, webTransport, "read",
	),

	"goal_set":    deferredPolicy("session.goal", normalMode, "session"),
	"goal_get":    deferredPolicy("session.goal", allModes, "read"),
	"goal_update": deferredPolicy("session.goal", allModes, "session"),

	"automation_create": deferredPolicy("automation", normalMode, "write"),
	"workflow_run":      deferredPolicy("delegation.workflow", normalMode, "orchestration"),
	"switch_env":        deferredPolicy("environment", normalMode, "environment"),
	"memory_note":       deferredPolicy("memory", normalMode, "write"),

	"team_create":       teamPolicy("orchestration"),
	"team_spawn":        teamPolicy("orchestration"),
	"team_send_message": teamPolicy("orchestration"),
	"team_list":         teamPolicy("read"),
	"team_delete":       teamPolicy("orchestration"),

	"browser_open":       groupedBrowserPolicy(allModes, "browser_navigate"),
	"browser_snapshot":   groupedBrowserPolicy(allModes, "browser_read"),
	"browser_screenshot": browserPolicy("browser.visual", allModes, "browser_read"),
	"browser_act":        groupedBrowserPolicy(normalMode, "browser_interact"),
	"browser_read":       groupedBrowserPolicy(allModes, "browser_read"),
	"browser_tabs":       browserPolicy("browser.core", allModes, "browser_mixed"),
	"browser_eval":       browserPolicy("browser.dev", normalMode, "browser_high_risk"),

	"computer_open":       groupedComputerPolicy(allModes, "computer_launch"),
	"computer_snapshot":   groupedComputerPolicy(allModes, "computer_read"),
	"computer_screenshot": computerPolicy("computer.visual", allModes, "computer_read"),
	"computer_act":        groupedComputerPolicy(normalMode, "computer_interact"),
	"computer_read":       computerPolicy("computer.sensitive", normalMode, "computer_sensitive"),
	"computer_apps":       groupedComputerPolicy(allModes, "computer_read"),
}

var (
	normalMode = []string{agent.ToolModeNormal}
	allModes   = []string{agent.ToolModeNormal, agent.ToolModePlan}

	tuiTransport        = []string{agent.ToolTransportTUI}
	webTransport        = []string{agent.ToolTransportWeb}
	tuiAndWebTransports = []string{agent.ToolTransportTUI, agent.ToolTransportWeb}
)

// buildCommandToolPlan applies the shared command-layer exposure policy to
// capability-gated tool candidates. MCP candidates are deliberately separate:
// arbitrary MCP names are accepted but are always deferred and normal-mode only.
func buildCommandToolPlan(
	ctx context.Context,
	candidates, mcpCandidates []tool.BaseTool,
	transport, mode string,
) (agent.ToolPlan, error) {
	descriptors := make([]agent.ToolDescriptor, 0, len(candidates)+len(mcpCandidates))
	for i, candidate := range candidates {
		name, err := commandCandidateName(ctx, candidate, "builtin", i)
		if err != nil {
			return agent.ToolPlan{}, err
		}
		policy, ok := commandToolPolicies[name]
		if !ok {
			return agent.ToolPlan{}, fmt.Errorf("command tool catalog: unknown builtin tool %q", name)
		}
		descriptors = append(descriptors, agent.ToolDescriptor{
			Tool:            candidate,
			Name:            name,
			Source:          policy.source,
			Bundle:          policy.bundle,
			DisclosureGroup: policy.disclosureGroup,
			Exposure:        policy.exposure,
			Transports:      policy.transports,
			Modes:           policy.modes,
			ApprovalClass:   policy.approvalClass,
		})
	}

	for i, candidate := range mcpCandidates {
		name, err := commandCandidateName(ctx, candidate, "MCP", i)
		if err != nil {
			return agent.ToolPlan{}, err
		}
		source, bundle := "mcp", "mcp"
		if server, ok := internaltools.MCPServerForTool(name); ok {
			source = "mcp:" + server
			bundle = "mcp." + server
		}
		descriptors = append(descriptors, agent.ToolDescriptor{
			Tool:          candidate,
			Name:          name,
			Source:        source,
			Bundle:        bundle,
			Exposure:      agent.ToolExposureDeferred,
			Modes:         normalMode,
			ApprovalClass: "mcp_unknown",
		})
	}

	return agent.NewToolPlanBuilder(descriptors).Build(ctx, agent.ToolPlanContext{
		Transport: transport,
		Mode:      mode,
	})
}

func commandCandidateName(ctx context.Context, candidate tool.BaseTool, kind string, index int) (string, error) {
	if candidate == nil {
		return "", fmt.Errorf("command tool catalog: %s candidate %d is nil", kind, index)
	}
	info, err := candidate.Info(ctx)
	if err != nil {
		return "", fmt.Errorf("command tool catalog: read %s candidate %d info: %w", kind, index, err)
	}
	if info == nil {
		return "", fmt.Errorf("command tool catalog: %s candidate %d returned nil ToolInfo", kind, index)
	}
	name := strings.TrimSpace(info.Name)
	if name == "" {
		return "", fmt.Errorf("command tool catalog: %s candidate %d returned empty tool name", kind, index)
	}
	return name, nil
}

func directPolicy(bundle string, modes []string, approvalClass string) commandToolPolicy {
	return commandToolPolicy{
		exposure: agent.ToolExposureDirect, source: "builtin", bundle: bundle,
		modes: modes, approvalClass: approvalClass,
	}
}

func scopedDirectPolicy(
	bundle string,
	modes, transports []string,
	approvalClass string,
) commandToolPolicy {
	policy := directPolicy(bundle, modes, approvalClass)
	policy.transports = transports
	return policy
}

func deferredPolicy(bundle string, modes []string, approvalClass string) commandToolPolicy {
	return commandToolPolicy{
		exposure: agent.ToolExposureDeferred, source: "builtin", bundle: bundle,
		modes: modes, approvalClass: approvalClass,
	}
}

func teamPolicy(approvalClass string) commandToolPolicy {
	policy := deferredPolicy("team", normalMode, approvalClass)
	policy.transports = tuiTransport
	return policy
}

func browserPolicy(bundle string, modes []string, approvalClass string) commandToolPolicy {
	policy := deferredPolicy(bundle, modes, approvalClass)
	policy.source = "browser"
	policy.transports = tuiAndWebTransports
	return policy
}

func groupedBrowserPolicy(modes []string, approvalClass string) commandToolPolicy {
	policy := browserPolicy("browser.core", modes, approvalClass)
	policy.disclosureGroup = "browser.workflow"
	return policy
}

func computerPolicy(bundle string, modes []string, approvalClass string) commandToolPolicy {
	policy := deferredPolicy(bundle, modes, approvalClass)
	policy.source = "computer"
	return policy
}

func groupedComputerPolicy(modes []string, approvalClass string) commandToolPolicy {
	policy := computerPolicy("computer.core", modes, approvalClass)
	policy.disclosureGroup = "computer.workflow"
	return policy
}
