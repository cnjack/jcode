package tools

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/team"
)

type teamToolTestModel struct{}

func (m *teamToolTestModel) Generate(
	context.Context, []*schema.Message, ...einomodel.Option,
) (*schema.Message, error) {
	return schema.AssistantMessage("done", nil), nil
}

func (m *teamToolTestModel) Stream(
	context.Context, []*schema.Message, ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (m *teamToolTestModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func TestTeamSpawnSchemaDeclaresProfilesDefaultsAndGrant(t *testing.T) {
	info, err := NewTeamSpawnTool(nil).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	js, err := info.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	agentType := js.Properties.Value("agent_type")
	if agentType == nil {
		t.Fatal("agent_type schema is missing")
	}
	if want := []any{team.AgentTypeExplore, team.AgentTypeGeneral, team.AgentTypeCoder}; !reflect.DeepEqual(agentType.Enum, want) {
		t.Fatalf("agent_type enum = %v, want %v", agentType.Enum, want)
	}
	if agentType.Default != team.AgentTypeGeneral {
		t.Fatalf("agent_type default = %v, want %q", agentType.Default, team.AgentTypeGeneral)
	}

	permission := js.Properties.Value("mode")
	if permission == nil {
		t.Fatal("mode schema is missing")
	}
	if want := []any{team.PermissionNormal, team.PermissionPlan, team.PermissionAuto}; !reflect.DeepEqual(permission.Enum, want) {
		t.Fatalf("mode enum = %v, want %v", permission.Enum, want)
	}
	if permission.Default != team.PermissionNormal {
		t.Fatalf("mode default = %v, want %q", permission.Default, team.PermissionNormal)
	}
	if !strings.Contains(permission.Description, "one-time delegated-write grant") ||
		!strings.Contains(info.Desc, "one-time delegated-write grant") {
		t.Fatalf("schema does not explain the auto-mode grant: mode=%q desc=%q",
			permission.Description, info.Desc)
	}
}

func TestTeamSpawnSchemaAdvertisesCustomRoles(t *testing.T) {
	manager := team.NewManager(&team.ManagerDeps{AgentRoles: map[string]config.AgentRoleConfig{
		"reviewer": {Description: "Review a patch", Profile: "explore", Instructions: "read only"},
	}})
	info, err := NewTeamSpawnTool(manager).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	js, err := info.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	agentType := js.Properties.Value("agent_type")
	if want := []any{"explore", "general", "coder", "reviewer"}; !reflect.DeepEqual(agentType.Enum, want) {
		t.Fatalf("agent_type enum = %v, want %v", agentType.Enum, want)
	}
}

func TestTeamSpawnResultShowsNormalizedTypeAndMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	model := &teamToolTestModel{}
	deps := &team.ManagerDeps{
		EnvFactory: func(cwd string) any { return NewEnv(cwd, "darwin/arm64") },
		ToolBuilder: func(env any, _, _ string) []tool.BaseTool {
			return []tool.BaseTool{env.(*Env).NewReadTool()}
		},
		DefaultModel: model,
		PromptBuilder: func(string, string, string, string) string {
			return "safe teammate prompt"
		},
		HandlersFactory: func(string, string, string) []adk.ChatModelAgentMiddleware {
			return []adk.ChatModelAgentMiddleware{&adk.BaseChatModelAgentMiddleware{}}
		},
	}
	manager := team.NewManager(deps)
	if err := manager.CreateTeam("tool-result", "test"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.DissolveTeam(context.Background()) }()

	result, err := NewTeamSpawnTool(manager).InvokableRun(context.Background(),
		`{"name":"worker","prompt":"inspect"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "type: general, mode: normal") {
		t.Fatalf("spawn result does not show normalized profile: %q", result)
	}
}
