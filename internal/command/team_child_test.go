package command

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"github.com/cnjack/jcode/internal/team"
	internaltools "github.com/cnjack/jcode/internal/tools"
)

func TestBuildTeamChildToolsPermissionMatrix(t *testing.T) {
	env := internaltools.NewEnv(t.TempDir(), "darwin/arm64")
	planNames := []string{"read", "grep", "execute"}
	writeNames := []string{"read", "edit", "write", "execute", "grep", "todowrite", "todoread"}
	tests := []struct {
		name       string
		agentType  string
		permission string
		wantNames  []string
		wantPlan   bool
	}{
		{name: "explore normal", agentType: team.AgentTypeExplore, permission: team.PermissionNormal, wantNames: planNames, wantPlan: true},
		{name: "explore plan", agentType: team.AgentTypeExplore, permission: team.PermissionPlan, wantNames: planNames, wantPlan: true},
		{name: "explore auto", agentType: team.AgentTypeExplore, permission: team.PermissionAuto, wantNames: planNames, wantPlan: true},
		{name: "general plan", agentType: team.AgentTypeGeneral, permission: team.PermissionPlan, wantNames: planNames, wantPlan: true},
		{name: "coder plan", agentType: team.AgentTypeCoder, permission: team.PermissionPlan, wantNames: planNames, wantPlan: true},
		{name: "general normal", agentType: team.AgentTypeGeneral, permission: team.PermissionNormal, wantNames: writeNames},
		{name: "general auto", agentType: team.AgentTypeGeneral, permission: team.PermissionAuto, wantNames: writeNames},
		{name: "coder normal", agentType: team.AgentTypeCoder, permission: team.PermissionNormal, wantNames: writeNames},
		{name: "coder auto", agentType: team.AgentTypeCoder, permission: team.PermissionAuto, wantNames: writeNames},
		{name: "defaults", wantNames: writeNames},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildTeamChildTools(env, tt.agentType, tt.permission)
			if names := commandTeamToolNames(t, got); !reflect.DeepEqual(names, tt.wantNames) {
				t.Fatalf("tool names = %v, want %v", names, tt.wantNames)
			}
			if gotPlan := commandTeamExecuteIsPlan(t, got); gotPlan != tt.wantPlan {
				t.Fatalf("execute plan profile = %v, want %v", gotPlan, tt.wantPlan)
			}
		})
	}
}

func TestBuildTeamChildToolsInvalidInputFailsClosed(t *testing.T) {
	env := internaltools.NewEnv(t.TempDir(), "darwin/arm64")
	for _, got := range [][]tool.BaseTool{
		buildTeamChildTools(nil, team.AgentTypeGeneral, team.PermissionNormal),
		buildTeamChildTools(env, "writer", team.PermissionNormal),
		buildTeamChildTools(env, team.AgentTypeGeneral, "unsafe"),
	} {
		if got != nil {
			t.Fatalf("invalid teammate profile returned tools: %v", commandTeamToolNames(t, got))
		}
	}
}

func TestBuildTeamChildPromptUsesPlanProfile(t *testing.T) {
	pwd := t.TempDir()
	tests := []struct {
		name       string
		agentType  string
		permission string
		wantPlan   bool
	}{
		{name: "explore normal", agentType: team.AgentTypeExplore, permission: team.PermissionNormal, wantPlan: true},
		{name: "explore auto", agentType: team.AgentTypeExplore, permission: team.PermissionAuto, wantPlan: true},
		{name: "general plan", agentType: team.AgentTypeGeneral, permission: team.PermissionPlan, wantPlan: true},
		{name: "coder plan", agentType: team.AgentTypeCoder, permission: team.PermissionPlan, wantPlan: true},
		{name: "general normal", agentType: team.AgentTypeGeneral, permission: team.PermissionNormal},
		{name: "coder auto", agentType: team.AgentTypeCoder, permission: team.PermissionAuto},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := buildTeamChildPrompt(tt.agentType, tt.permission, "darwin/arm64", pwd)
			if prompt == "" {
				t.Fatal("prompt is empty")
			}
			if got := strings.Contains(prompt, "**Plan Mode**"); got != tt.wantPlan {
				t.Fatalf("Plan prompt = %v, want %v", got, tt.wantPlan)
			}
		})
	}
}

func commandTeamToolNames(t *testing.T, tools []tool.BaseTool) []string {
	t.Helper()
	names := make([]string, 0, len(tools))
	for _, base := range tools {
		info, err := base.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, info.Name)
	}
	return names
}

func commandTeamExecuteIsPlan(t *testing.T, tools []tool.BaseTool) bool {
	t.Helper()
	for _, base := range tools {
		info, err := base.Info(context.Background())
		if err != nil || info.Name != "execute" {
			continue
		}
		js, err := info.ToJSONSchema()
		if err != nil {
			t.Fatal(err)
		}
		return js.Properties.Value("background") == nil
	}
	t.Fatal("execute tool is missing")
	return false
}
