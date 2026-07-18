package command

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/agent"
	internaltools "github.com/cnjack/jcode/internal/tools"
)

type catalogTestTool string

func (t catalogTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: string(t)}, nil
}

var allCommandBuiltinNames = []string{
	"write", "read", "grep", "edit", "execute", "check_background",
	"todowrite", "todoread", "ask_user", "load_skill", "subagent",
	"goal_set", "goal_get", "goal_update", "automation_create", "workflow_run",
	"switch_env", "memory_note", "team_create", "team_spawn", "team_send_message",
	"team_list", "team_delete", "browser_open", "browser_snapshot",
	"browser_screenshot", "browser_act", "browser_read", "browser_tabs", "browser_eval",
	"computer_open", "computer_snapshot", "computer_screenshot", "computer_act",
	"computer_read", "computer_apps",
}

func TestBuildCommandToolPlanMatrix(t *testing.T) {
	normalDirect := []string{
		"ask_user", "check_background", "edit", "execute", "grep", "load_skill",
		"read", "subagent", "todoread", "todowrite", "write",
	}
	planDirect := []string{"ask_user", "execute", "grep", "read", "todoread", "todowrite"}
	normalDeferred := []string{
		"automation_create", "browser_act", "browser_eval", "browser_open", "browser_read",
		"browser_screenshot", "browser_snapshot", "browser_tabs", "computer_act", "computer_apps",
		"computer_open", "computer_read", "computer_screenshot", "computer_snapshot",
		"goal_get", "goal_set", "goal_update", "memory_note", "switch_env", "team_create",
		"team_delete", "team_list", "team_send_message", "team_spawn", "workflow_run",
	}
	planDeferred := []string{
		"browser_open", "browser_read", "browser_screenshot", "browser_snapshot", "browser_tabs",
		"computer_apps", "computer_open", "computer_screenshot", "computer_snapshot",
		"goal_get", "goal_update",
	}

	tests := []struct {
		name      string
		transport string
		mode      string
		direct    []string
		deferred  []string
	}{
		{name: "tui normal", transport: agent.ToolTransportTUI, mode: agent.ToolModeNormal,
			direct: normalDirect, deferred: normalDeferred},
		{name: "web normal", transport: agent.ToolTransportWeb, mode: agent.ToolModeNormal,
			direct: normalDirect, deferred: withoutPrefixes(normalDeferred, "team_")},
		{name: "acp normal", transport: agent.ToolTransportACP, mode: agent.ToolModeNormal,
			direct:   withoutNames(normalDirect, "ask_user"),
			deferred: withoutPrefixes(normalDeferred, "browser_", "team_")},
		{name: "tui plan", transport: agent.ToolTransportTUI, mode: agent.ToolModePlan,
			direct: planDirect, deferred: planDeferred},
		{name: "web plan", transport: agent.ToolTransportWeb, mode: agent.ToolModePlan,
			direct: planDirect, deferred: planDeferred},
		{name: "acp plan", transport: agent.ToolTransportACP, mode: agent.ToolModePlan,
			direct:   withoutNames(planDirect, "ask_user"),
			deferred: withoutPrefixes(planDeferred, "browser_")},
	}

	candidates := catalogTools(allCommandBuiltinNames...)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := buildCommandToolPlan(
				context.Background(), candidates, nil, tt.transport, tt.mode,
			)
			if err != nil {
				t.Fatalf("buildCommandToolPlan() error = %v", err)
			}
			assertCatalogDescriptorNames(t, "direct", plan.Direct, tt.direct)
			assertCatalogDescriptorNames(t, "deferred", plan.Deferred, tt.deferred)
			if got, want := len(plan.Direct)+1, len(tt.direct)+1; got != want {
				t.Fatalf("first-round count including tool_search = %d, want %d", got, want)
			}
			assertCatalogToolNames(t, "runtime", plan.AllRuntimeTools(),
				sortedUnion(tt.direct, tt.deferred))
		})
	}
}

func TestBuildCommandToolPlanMCPIsDeferredAndNormalOnly(t *testing.T) {
	internaltools.RegisterMCPToolServer("catalog_mcp_alpha", "alpha-server")
	mcp := catalogTools("catalog_mcp_zeta", "catalog_mcp_alpha")

	normal, err := buildCommandToolPlan(context.Background(), catalogTools("read"), mcp,
		agent.ToolTransportWeb, agent.ToolModeNormal)
	if err != nil {
		t.Fatalf("normal plan error = %v", err)
	}
	assertCatalogDescriptorNames(t, "deferred MCP", normal.Deferred,
		[]string{"catalog_mcp_alpha", "catalog_mcp_zeta"})
	alpha := normal.Deferred[0]
	if alpha.Source != "mcp:alpha-server" || alpha.Bundle != "mcp.alpha-server" ||
		alpha.ApprovalClass != "mcp_unknown" {
		t.Fatalf("MCP metadata = source %q bundle %q approval %q",
			alpha.Source, alpha.Bundle, alpha.ApprovalClass)
	}

	planning, err := buildCommandToolPlan(context.Background(), catalogTools("read"), mcp,
		agent.ToolTransportWeb, agent.ToolModePlan)
	if err != nil {
		t.Fatalf("plan plan error = %v", err)
	}
	assertCatalogDescriptorNames(t, "hidden MCP", planning.Hidden,
		[]string{"catalog_mcp_alpha", "catalog_mcp_zeta"})
	assertCatalogToolNames(t, "plan runtime", planning.AllRuntimeTools(), []string{"read"})
}

func TestBuildCommandToolPlanRejectsUnknownAndCollisions(t *testing.T) {
	tests := []struct {
		name       string
		candidates []tool.BaseTool
		mcp        []tool.BaseTool
		want       string
	}{
		{name: "unknown builtin", candidates: catalogTools("mystery"), want: "unknown builtin tool"},
		{name: "builtin MCP collision", candidates: catalogTools("read"), mcp: catalogTools("read"), want: "duplicate tool name"},
		{name: "MCP collision", mcp: catalogTools("same_mcp", "same_mcp"), want: "duplicate tool name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildCommandToolPlan(context.Background(), tt.candidates, tt.mcp,
				agent.ToolTransportTUI, agent.ToolModeNormal)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestBuildCommandToolPlanScopeIsRuntimeBoundary(t *testing.T) {
	plan, err := buildCommandToolPlan(context.Background(), catalogTools(
		"read", "ask_user", "team_spawn", "browser_open", "computer_open", "edit",
	), nil, agent.ToolTransportACP, agent.ToolModePlan)
	if err != nil {
		t.Fatalf("buildCommandToolPlan() error = %v", err)
	}
	assertCatalogDescriptorNames(t, "hidden", plan.Hidden,
		[]string{"ask_user", "browser_open", "edit", "team_spawn"})
	assertCatalogToolNames(t, "runtime", plan.AllRuntimeTools(), []string{"computer_open", "read"})
}

func TestBuildCommandToolPlanRebuildUsesCurrentCandidates(t *testing.T) {
	first, err := buildCommandToolPlan(
		context.Background(),
		catalogTools("read", "browser_open"),
		catalogTools("old_mcp_tool"),
		agent.ToolTransportWeb,
		agent.ToolModeNormal,
	)
	if err != nil {
		t.Fatalf("first buildCommandToolPlan() error = %v", err)
	}
	second, err := buildCommandToolPlan(
		context.Background(),
		catalogTools("read"),
		catalogTools("new_mcp_tool"),
		agent.ToolTransportWeb,
		agent.ToolModeNormal,
	)
	if err != nil {
		t.Fatalf("second buildCommandToolPlan() error = %v", err)
	}

	assertCatalogToolNames(t, "first runtime", first.AllRuntimeTools(),
		[]string{"browser_open", "old_mcp_tool", "read"})
	assertCatalogToolNames(t, "rebuilt runtime", second.AllRuntimeTools(),
		[]string{"new_mcp_tool", "read"})
}

func TestCommandToolPolicyCoversExpectedBuiltins(t *testing.T) {
	want := append([]string(nil), allCommandBuiltinNames...)
	sort.Strings(want)
	got := make([]string, 0, len(commandToolPolicies))
	for name := range commandToolPolicies {
		got = append(got, name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("policy names = %v, want %v", got, want)
	}
}

func catalogTools(names ...string) []tool.BaseTool {
	result := make([]tool.BaseTool, len(names))
	for i, name := range names {
		result[i] = catalogTestTool(name)
	}
	return result
}

func assertCatalogDescriptorNames(t *testing.T, label string, descriptors []agent.ToolDescriptor, want []string) {
	t.Helper()
	got := make([]string, len(descriptors))
	for i, descriptor := range descriptors {
		got[i] = descriptor.Name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func assertCatalogToolNames(t *testing.T, label string, candidates []tool.BaseTool, want []string) {
	t.Helper()
	got := make([]string, len(candidates))
	for i, candidate := range candidates {
		info, err := candidate.Info(context.Background())
		if err != nil {
			t.Fatalf("%s candidate %d Info() error = %v", label, i, err)
		}
		got[i] = info.Name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func withoutNames(input []string, names ...string) []string {
	excluded := make(map[string]bool, len(names))
	for _, name := range names {
		excluded[name] = true
	}
	result := make([]string, 0, len(input))
	for _, name := range input {
		if !excluded[name] {
			result = append(result, name)
		}
	}
	return result
}

func withoutPrefixes(input []string, prefixes ...string) []string {
	result := make([]string, 0, len(input))
	for _, name := range input {
		excluded := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(name, prefix) {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, name)
		}
	}
	return result
}

func sortedUnion(groups ...[]string) []string {
	var result []string
	for _, group := range groups {
		result = append(result, group...)
	}
	sort.Strings(result)
	return result
}

var _ tool.BaseTool = catalogTestTool("")
