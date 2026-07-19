package command

import (
	"context"
	"reflect"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"github.com/cnjack/jcode/internal/agent"
	internaltools "github.com/cnjack/jcode/internal/tools"
)

// TestCommandToolLifecycleRebuildMatrix models consecutive agent generations.
// Each build is derived only from the current transport/mode/capability
// candidates; no endpoint from an older generation is carried forward.
func TestCommandToolLifecycleRebuildMatrix(t *testing.T) {
	const (
		oldMCP = "mcp__old_server__dangerous_write"
		newMCP = "mcp__new_server__safe_lookup"
	)
	internaltools.RegisterMCPToolIdentity(oldMCP, "old-server", "dangerous-write")
	internaltools.RegisterMCPToolIdentity(newMCP, "new-server", "safe-lookup")

	tests := []struct {
		name        string
		builtins    []tool.BaseTool
		mcp         []tool.BaseTool
		transport   string
		mode        string
		wantRuntime []string
		wantHidden  []string
	}{
		{
			name:        "web normal before revocation",
			builtins:    lifecycleCatalogTools("read", "browser_act", "computer_act"),
			mcp:         lifecycleCatalogTools(oldMCP),
			transport:   agent.ToolTransportWeb,
			mode:        agent.ToolModeNormal,
			wantRuntime: []string{"browser_act", "computer_act", oldMCP, "read"},
		},
		{
			name:        "web plan revokes mutation and MCP",
			builtins:    lifecycleCatalogTools("read", "browser_act", "computer_act"),
			mcp:         lifecycleCatalogTools(oldMCP),
			transport:   agent.ToolTransportWeb,
			mode:        agent.ToolModePlan,
			wantRuntime: []string{"read"},
			wantHidden:  []string{"browser_act", "computer_act", oldMCP},
		},
		{
			name:        "web capabilities disabled and MCP removed",
			builtins:    lifecycleCatalogTools("read"),
			transport:   agent.ToolTransportWeb,
			mode:        agent.ToolModeNormal,
			wantRuntime: []string{"read"},
		},
		{
			name:        "web MCP hot reload uses only new snapshot",
			builtins:    lifecycleCatalogTools("read"),
			mcp:         lifecycleCatalogTools(newMCP),
			transport:   agent.ToolTransportWeb,
			mode:        agent.ToolModeNormal,
			wantRuntime: []string{newMCP, "read"},
		},
		{
			name:        "ACP transport rejects browser but keeps computer and MCP",
			builtins:    lifecycleCatalogTools("read", "browser_open", "computer_open"),
			mcp:         lifecycleCatalogTools(newMCP),
			transport:   agent.ToolTransportACP,
			mode:        agent.ToolModeNormal,
			wantRuntime: []string{"computer_open", newMCP, "read"},
			wantHidden:  []string{"browser_open"},
		},
		{
			name:        "TUI transport keeps its browser and team capabilities",
			builtins:    lifecycleCatalogTools("read", "ask_user", "browser_open", "team_list"),
			transport:   agent.ToolTransportTUI,
			mode:        agent.ToolModeNormal,
			wantRuntime: []string{"ask_user", "browser_open", "read", "team_list"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := buildCommandToolPlan(
				context.Background(), tt.builtins, tt.mcp, tt.transport, tt.mode,
			)
			if err != nil {
				t.Fatalf("buildCommandToolPlan() error = %v", err)
			}
			if got := lifecycleToolNames(t, plan.AllRuntimeTools()); !reflect.DeepEqual(got, tt.wantRuntime) {
				t.Fatalf("runtime tools = %v, want %v", got, tt.wantRuntime)
			}
			if got := lifecycleDescriptorNames(plan.Hidden); !reflect.DeepEqual(got, tt.wantHidden) {
				t.Fatalf("hidden tools = %v, want %v", got, tt.wantHidden)
			}
		})
	}
}

func lifecycleCatalogTools(names ...string) []tool.BaseTool {
	result := make([]tool.BaseTool, len(names))
	for i, name := range names {
		result[i] = catalogTestTool(name)
	}
	return result
}

func lifecycleToolNames(t *testing.T, candidates []tool.BaseTool) []string {
	t.Helper()
	names := make([]string, len(candidates))
	for i, candidate := range candidates {
		info, err := candidate.Info(context.Background())
		if err != nil {
			t.Fatalf("candidate %d Info() error = %v", i, err)
		}
		names[i] = info.Name
	}
	return names
}

func lifecycleDescriptorNames(descriptors []agent.ToolDescriptor) []string {
	if len(descriptors) == 0 {
		return nil
	}
	names := make([]string, len(descriptors))
	for i, descriptor := range descriptors {
		names[i] = descriptor.Name
	}
	return names
}
