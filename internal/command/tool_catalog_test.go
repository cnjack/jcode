package command

import (
	"context"
	"fmt"
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
	"computer_read", "computer_apps", "show_artifact",
}

func TestBuildCommandToolPlanMatrix(t *testing.T) {
	normalDirect := []string{
		"ask_user", "check_background", "edit", "execute", "grep", "load_skill",
		"read", "subagent", "todoread", "todowrite", "write",
	}
	planDirect := []string{"ask_user", "execute", "grep", "read", "todoread", "todowrite"}
	webNormalDirect := []string{
		"ask_user", "check_background", "edit", "execute", "grep", "load_skill",
		"read", "show_artifact", "subagent", "todoread", "todowrite", "write",
	}
	webPlanDirect := []string{"ask_user", "execute", "grep", "read", "show_artifact", "todoread", "todowrite"}
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
			direct: webNormalDirect, deferred: withoutPrefixes(normalDeferred, "team_")},
		{name: "acp normal", transport: agent.ToolTransportACP, mode: agent.ToolModeNormal,
			direct:   withoutNames(normalDirect, "ask_user"),
			deferred: withoutPrefixes(normalDeferred, "browser_", "team_")},
		{name: "tui plan", transport: agent.ToolTransportTUI, mode: agent.ToolModePlan,
			direct: planDirect, deferred: planDeferred},
		{name: "web plan", transport: agent.ToolTransportWeb, mode: agent.ToolModePlan,
			direct: webPlanDirect, deferred: planDeferred},
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
			assertCatalogDisclosureGroup(
				t, plan.Deferred, "browser.workflow",
				orderedIntersection(tt.deferred, []string{
					"browser_act", "browser_open", "browser_read", "browser_snapshot",
				}),
			)
			assertCatalogDisclosureGroup(
				t, plan.Deferred, "computer.workflow",
				orderedIntersection(tt.deferred, []string{
					"computer_act", "computer_apps", "computer_open", "computer_snapshot",
				}),
			)
			if got, want := len(plan.Direct)+1, len(tt.direct)+1; got != want {
				t.Fatalf("first-round count including tool_search = %d, want %d", got, want)
			}
			assertCatalogToolNames(t, "runtime", plan.AllRuntimeTools(),
				sortedUnion(tt.direct, tt.deferred))
		})
	}
}

func TestBuildCommandToolPlanMCPIsDeferredAndNormalOnly(t *testing.T) {
	internaltools.RegisterMCPToolIdentity("mcp__alpha_server__catalog_alpha", "alpha-server", "catalog-alpha")
	internaltools.RegisterMCPToolIdentity("mcp__zeta_server__catalog_zeta", "zeta-server", "catalog-zeta")
	mcp := catalogTools("mcp__zeta_server__catalog_zeta", "mcp__alpha_server__catalog_alpha")

	normal, err := buildCommandToolPlan(context.Background(), catalogTools("read"), mcp,
		agent.ToolTransportWeb, agent.ToolModeNormal)
	if err != nil {
		t.Fatalf("normal plan error = %v", err)
	}
	assertCatalogDescriptorNames(t, "deferred MCP", normal.Deferred,
		[]string{"mcp__alpha_server__catalog_alpha", "mcp__zeta_server__catalog_zeta"})
	alpha := normal.Deferred[0]
	if alpha.Source != "mcp:alpha-server" || alpha.Bundle != "mcp.alpha-server" ||
		alpha.ApprovalClass != "mcp_unknown" || alpha.DisclosureGroup != "" {
		t.Fatalf("MCP metadata = source %q bundle %q approval %q",
			alpha.Source, alpha.Bundle, alpha.ApprovalClass)
	}

	planning, err := buildCommandToolPlan(context.Background(), catalogTools("read"), mcp,
		agent.ToolTransportWeb, agent.ToolModePlan)
	if err != nil {
		t.Fatalf("plan plan error = %v", err)
	}
	assertCatalogDescriptorNames(t, "hidden MCP", planning.Hidden,
		[]string{"mcp__alpha_server__catalog_alpha", "mcp__zeta_server__catalog_zeta"})
	assertCatalogToolNames(t, "plan runtime", planning.AllRuntimeTools(), []string{"read"})
}

func TestBuildCommandToolPlanLargeSameServerMCPNeverFormsDisclosureGroup(t *testing.T) {
	const (
		server      = "disclosure-review-server"
		catalogSize = 32
	)
	names := make([]string, 0, catalogSize)
	for i := 0; i < catalogSize; i++ {
		name := fmt.Sprintf("mcp__disclosure_review_server__catalog_%02d", i)
		internaltools.RegisterMCPToolIdentity(name, server, fmt.Sprintf("catalog-%02d", i))
		names = append(names, name)
	}

	normal, err := buildCommandToolPlan(
		context.Background(), catalogTools("read"), catalogTools(names...),
		agent.ToolTransportWeb, agent.ToolModeNormal,
	)
	if err != nil {
		t.Fatalf("normal plan error = %v", err)
	}
	if len(normal.Deferred) != catalogSize {
		t.Fatalf("normal deferred MCP count = %d, want %d", len(normal.Deferred), catalogSize)
	}
	assertSameServerMCPDescriptorsUngrouped(t, normal.Deferred, server)

	planning, err := buildCommandToolPlan(
		context.Background(), catalogTools("read"), catalogTools(names...),
		agent.ToolTransportWeb, agent.ToolModePlan,
	)
	if err != nil {
		t.Fatalf("plan mode error = %v", err)
	}
	if len(planning.Hidden) != catalogSize {
		t.Fatalf("plan hidden MCP count = %d, want %d", len(planning.Hidden), catalogSize)
	}
	assertSameServerMCPDescriptorsUngrouped(t, planning.Hidden, server)
}

func assertSameServerMCPDescriptorsUngrouped(
	t *testing.T,
	descriptors []agent.ToolDescriptor,
	server string,
) {
	t.Helper()
	for _, descriptor := range descriptors {
		if descriptor.Source != "mcp:"+server || descriptor.Bundle != "mcp."+server {
			t.Errorf(
				"MCP %q identity = source %q bundle %q, want canonical server %q",
				descriptor.Name, descriptor.Source, descriptor.Bundle, server,
			)
		}
		if descriptor.DisclosureGroup != "" {
			t.Errorf(
				"same-server MCP %q disclosure group = %q, want empty",
				descriptor.Name, descriptor.DisclosureGroup,
			)
		}
	}
}

func TestCommandToolDisclosureGroupsAreExplicitAndNarrow(t *testing.T) {
	want := map[string]string{
		"browser_open":      "browser.workflow",
		"browser_snapshot":  "browser.workflow",
		"browser_act":       "browser.workflow",
		"browser_read":      "browser.workflow",
		"computer_open":     "computer.workflow",
		"computer_snapshot": "computer.workflow",
		"computer_act":      "computer.workflow",
		"computer_apps":     "computer.workflow",
	}
	for _, name := range []string{
		"browser_open", "browser_snapshot", "browser_act", "browser_read",
		"browser_tabs", "browser_screenshot", "browser_eval",
		"computer_open", "computer_snapshot", "computer_act", "computer_apps",
		"computer_read", "computer_screenshot",
	} {
		if got := commandToolPolicies[name].disclosureGroup; got != want[name] {
			t.Errorf("tool %q disclosure group = %q, want %q", name, got, want[name])
		}
	}
}

func assertCatalogDisclosureGroup(
	t *testing.T,
	descriptors []agent.ToolDescriptor,
	group string,
	want []string,
) {
	t.Helper()
	var got []string
	for _, descriptor := range descriptors {
		if descriptor.DisclosureGroup == group {
			got = append(got, descriptor.Name)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("disclosure group %q = %v, want %v", group, got, want)
	}
}

func orderedIntersection(input, allowed []string) []string {
	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}
	var result []string
	for _, name := range input {
		if allowedSet[name] {
			result = append(result, name)
		}
	}
	return result
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
