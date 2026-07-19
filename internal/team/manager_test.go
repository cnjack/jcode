package team

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type managerTestModel struct{}

func (m *managerTestModel) Generate(
	context.Context, []*schema.Message, ...einomodel.Option,
) (*schema.Message, error) {
	return schema.AssistantMessage("done", nil), nil
}

func (m *managerTestModel) Stream(
	context.Context, []*schema.Message, ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (m *managerTestModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

type managerTestTool string

func (t managerTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: string(t)}, nil
}

type managerTestEnv struct{ cwd string }

func (e *managerTestEnv) Pwd() string { return e.cwd }

func validManagerDeps() ManagerDeps {
	model := &managerTestModel{}
	return ManagerDeps{
		EnvFactory: func(cwd string) any { return &managerTestEnv{cwd: cwd} },
		ToolBuilder: func(any, string, string) []tool.BaseTool {
			return []tool.BaseTool{managerTestTool("read")}
		},
		ModelFactory: func(context.Context, string) (any, error) { return model, nil },
		DefaultModel: model,
		PromptBuilder: func(string, string, string, string) string {
			return "safe teammate prompt"
		},
		HandlersFactory: func(string, string, string) []adk.ChatModelAgentMiddleware {
			return []adk.ChatModelAgentMiddleware{&adk.BaseChatModelAgentMiddleware{}}
		},
	}
}

func TestSpawnTeammateStoresNormalizedProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	deps := validManagerDeps()
	m := NewManager(&deps)
	if err := m.CreateTeam("normalize", "test"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.DissolveTeam(context.Background()) }()

	agentID, err := m.SpawnTeammate(context.Background(), SpawnConfig{
		Name: "worker", Prompt: "inspect the repository",
	})
	if err != nil {
		t.Fatal(err)
	}
	state := m.GetTeammateState(agentID)
	if state == nil {
		t.Fatal("spawned teammate state is missing")
	}
	if state.AgentType != AgentTypeGeneral || state.Permission != PermissionNormal {
		t.Fatalf("normalized state = type %q permission %q", state.AgentType, state.Permission)
	}

	m.mu.RLock()
	member := m.teamFile.Members[len(m.teamFile.Members)-1]
	m.mu.RUnlock()
	if member.AgentType != AgentTypeGeneral || member.Permission != PermissionNormal {
		t.Fatalf("persisted member = type %q permission %q", member.AgentType, member.Permission)
	}
}

func TestSpawnTeammateRejectsInvalidProfileBeforeFactory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	deps := validManagerDeps()
	factoryCalls := 0
	deps.EnvFactory = func(cwd string) any {
		factoryCalls++
		return &managerTestEnv{cwd: cwd}
	}
	m := NewManager(&deps)
	if err := m.CreateTeam("invalid-profile", "test"); err != nil {
		t.Fatal(err)
	}

	for _, cfg := range []SpawnConfig{
		{Name: "bad-type", Prompt: "x", AgentType: "writer"},
		{Name: "bad-mode", Prompt: "x", Permission: "unsafe"},
	} {
		if _, err := m.SpawnTeammate(context.Background(), cfg); err == nil {
			t.Fatalf("SpawnTeammate(%+v) unexpectedly succeeded", cfg)
		}
	}
	if factoryCalls != 0 {
		t.Fatalf("environment factory called %d times for invalid profiles", factoryCalls)
	}
	if got := len(m.ListTeammates()); got != 0 {
		t.Fatalf("invalid profiles created %d teammates", got)
	}
}

func TestSpawnTeammateFailsClosedWhenDependencyMissing(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ManagerDeps)
		cfg    SpawnConfig
		want   string
	}{
		{name: "environment factory", mutate: func(d *ManagerDeps) { d.EnvFactory = nil }, want: "environment factory"},
		{name: "tool builder", mutate: func(d *ManagerDeps) { d.ToolBuilder = nil }, want: "tool builder"},
		{name: "prompt builder", mutate: func(d *ManagerDeps) { d.PromptBuilder = nil }, want: "prompt builder"},
		{name: "handlers factory", mutate: func(d *ManagerDeps) { d.HandlersFactory = nil }, want: "handlers factory"},
		{name: "default model", mutate: func(d *ManagerDeps) { d.DefaultModel = struct{}{} }, want: "default chat model"},
		{
			name:   "override model factory",
			mutate: func(d *ManagerDeps) { d.ModelFactory = nil },
			cfg:    SpawnConfig{Model: "provider/model"}, want: "model factory",
		},
		{
			name:   "nil environment",
			mutate: func(d *ManagerDeps) { d.EnvFactory = func(string) any { return nil } },
			want:   "returned nil",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			deps := validManagerDeps()
			tt.mutate(&deps)
			m := NewManager(&deps)
			teamName := "missing-" + strings.ReplaceAll(tt.name, " ", "-")
			if err := m.CreateTeam(teamName, "test"); err != nil {
				t.Fatal(err)
			}
			cfg := tt.cfg
			cfg.Name = "worker"
			cfg.Prompt = "task"
			if _, err := m.SpawnTeammate(context.Background(), cfg); err == nil ||
				!strings.Contains(err.Error(), tt.want) {
				t.Fatalf("SpawnTeammate() error = %v, want containing %q", err, tt.want)
			}
			if got := len(m.ListTeammates()); got != 0 {
				t.Fatalf("failed spawn created %d teammates", got)
			}
		})
	}
}
