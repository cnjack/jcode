package web

import (
	"testing"

	"github.com/cloudwego/eino/adk"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/tools"
)

// TestMCPReloadRebuildsEveryLiveTask prevents a revoked MCP endpoint from
// remaining executable in a background task. Active and background engines
// each receive a new agent generation from the atomically published catalog;
// the active engine's duplicate tasks-map entry is rebuilt only once.
func TestMCPReloadRebuildsEveryLiveTask(t *testing.T) {
	calls := map[string]int{}
	makeEngine := func(id string) *Engine {
		return &Engine{
			taskID:       id,
			providerName: "provider",
			modelName:    "model",
			createAgent: func(_, _ string) (*adk.ChatModelAgent, error) {
				calls[id]++
				return new(adk.ChatModelAgent), nil
			},
		}
	}
	active := makeEngine("active")
	background := makeEngine("background")
	reloads := 0
	s := &Server{
		Engine: active,
		tasks: map[string]*Engine{
			"active":     active,
			"background": background,
		},
		cfg: &config.Config{},
		reloadMCP: func(map[string]*config.MCPServer) ([]tools.MCPStatus, error) {
			reloads++
			return nil, nil
		},
	}

	if err := s.reloadMCPAndRebuild(); err != nil {
		t.Fatalf("reloadMCPAndRebuild() error = %v", err)
	}
	if reloads != 1 {
		t.Fatalf("MCP catalog reloads = %d, want 1", reloads)
	}
	if calls["active"] != 1 || calls["background"] != 1 {
		t.Fatalf("agent rebuild calls = %v, want each live task exactly once", calls)
	}
}

// TestMCPReloadDoesNotOverwriteConcurrentModeSwitch exercises the revision
// guard through the MCP entry point. A slower catalog rebuild must not install
// its normal-mode agent after a concurrent switch has already installed a Plan
// agent with a stricter runtime registry.
func TestMCPReloadDoesNotOverwriteConcurrentModeSwitch(t *testing.T) {
	staleAgent := new(adk.ChatModelAgent)
	planAgent := new(adk.ChatModelAgent)
	started := make(chan struct{})
	release := make(chan struct{})
	eng := &Engine{
		taskID:       "active",
		mode:         "approval",
		providerName: "provider",
		modelName:    "model",
		createAgent: func(_, _ string) (*adk.ChatModelAgent, error) {
			close(started)
			<-release
			return staleAgent, nil
		},
	}
	s := &Server{Engine: eng, cfg: &config.Config{}}

	done := make(chan error, 1)
	go func() { done <- s.reloadMCPAndRebuild() }()
	<-started
	eng.applyModeSwitch("plan", planAgent)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("reloadMCPAndRebuild() error = %v", err)
	}

	eng.emu.Lock()
	defer eng.emu.Unlock()
	if eng.agent != planAgent || eng.mode != "plan" {
		t.Fatalf("stale MCP rebuild overwrote mode switch: agent=%p mode=%q", eng.agent, eng.mode)
	}
}
