package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/tools"
)

func failedMCPConfigServer(t *testing.T) *Server {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Server{
		cfg: &config.Config{MCPServers: map[string]*config.MCPServer{
			"existing": {Type: "stdio", Command: "before"},
		}},
		mcpStatuses: map[string]tools.MCPStatus{"existing": {Name: "existing"}},
		mcpLogins:   map[string]*mcpLoginState{"existing": {Status: "authorized"}},
		wsBroker:    NewWSBroker(),
		needsSetup:  true,
	}
}

func TestMCPConfigSaveFailuresRollBack(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		s := failedMCPConfigServer(t)
		rec := httptest.NewRecorder()
		s.handleCreateMCP(rec, httptest.NewRequest(http.MethodPost, "/api/mcp/servers",
			strings.NewReader(`{"name":"new","type":"stdio","command":"new-command"}`)))
		if rec.Code != http.StatusInternalServerError || s.cfg.MCPServers["new"] != nil || len(s.cfg.MCPServers) != 1 {
			t.Fatalf("status=%d servers=%v body=%s", rec.Code, s.cfg.MCPServers, rec.Body.String())
		}
	})

	t.Run("update", func(t *testing.T) {
		s := failedMCPConfigServer(t)
		req := httptest.NewRequest(http.MethodPut, "/api/mcp/servers/existing",
			strings.NewReader(`{"type":"stdio","command":"after"}`))
		req.SetPathValue("name", "existing")
		rec := httptest.NewRecorder()
		s.handleUpdateMCP(rec, req)
		if rec.Code != http.StatusInternalServerError || s.cfg.MCPServers["existing"].Command != "before" {
			t.Fatalf("status=%d server=%+v body=%s", rec.Code, s.cfg.MCPServers["existing"], rec.Body.String())
		}
	})

	t.Run("toggle", func(t *testing.T) {
		s := failedMCPConfigServer(t)
		req := httptest.NewRequest(http.MethodPost, "/api/mcp/existing/toggle", strings.NewReader(`{"enabled":false}`))
		req.SetPathValue("name", "existing")
		rec := httptest.NewRecorder()
		s.handleToggleMCP(rec, req)
		if rec.Code != http.StatusInternalServerError || s.cfg.MCPServers["existing"].Disabled {
			t.Fatalf("status=%d server=%+v body=%s", rec.Code, s.cfg.MCPServers["existing"], rec.Body.String())
		}
	})

	t.Run("delete", func(t *testing.T) {
		s := failedMCPConfigServer(t)
		req := httptest.NewRequest(http.MethodDelete, "/api/mcp/servers/existing", nil)
		req.SetPathValue("name", "existing")
		rec := httptest.NewRecorder()
		s.handleDeleteMCP(rec, req)
		if rec.Code != http.StatusInternalServerError || s.cfg.MCPServers["existing"] == nil {
			t.Fatalf("status=%d servers=%v body=%s", rec.Code, s.cfg.MCPServers, rec.Body.String())
		}
		if s.mcpStatuses["existing"].Name == "" || s.mcpLogins["existing"] == nil {
			t.Fatal("failed delete removed runtime status")
		}
	})
}

func TestMCPConfigConcurrentListReloadAndToggle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		cfg: &config.Config{MCPServers: map[string]*config.MCPServer{
			"alpha": {Type: "stdio", Command: "alpha", Headers: map[string]string{"stable": "yes"}},
		}},
		mcpStatuses: map[string]tools.MCPStatus{},
		mcpLogins:   map[string]*mcpLoginState{},
		wsBroker:    NewWSBroker(),
		needsSetup:  true,
		reloadMCP: func(servers map[string]*config.MCPServer) ([]tools.MCPStatus, error) {
			if srv := servers["alpha"]; srv != nil {
				srv.Headers["stable"] = "mutated-copy"
			}
			return nil, nil
		},
	}

	errCh := make(chan string, 128)
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		for i := 0; i < 16; i++ {
			req := httptest.NewRequest(http.MethodPost, "/api/mcp/alpha/toggle",
				strings.NewReader(map[bool]string{true: `{"enabled":true}`, false: `{"enabled":false}`}[i%2 == 0]))
			req.SetPathValue("name", "alpha")
			rec := httptest.NewRecorder()
			s.handleToggleMCP(rec, req)
			if rec.Code != http.StatusOK {
				errCh <- rec.Body.String()
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 32; i++ {
			rec := httptest.NewRecorder()
			s.handleListMCP(rec, httptest.NewRequest(http.MethodGet, "/api/mcp", nil))
			if rec.Code != http.StatusOK {
				errCh <- rec.Body.String()
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 32; i++ {
			if err := s.reloadMCPAndRebuild(); err != nil {
				errCh <- err.Error()
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 16; i++ {
			body := map[bool]string{true: `{"enabled":true}`, false: `{"enabled":false}`}[i%2 == 0]
			rec := httptest.NewRecorder()
			s.handleToolSearchConfig(rec, httptest.NewRequest(http.MethodPost, "/api/tool-search/config", strings.NewReader(body)))
			if rec.Code != http.StatusOK {
				errCh <- rec.Body.String()
			}
		}
	}()
	wg.Wait()
	close(errCh)
	for msg := range errCh {
		t.Errorf("concurrent MCP operation failed: %s", msg)
	}
	if got := s.cfg.MCPServers["alpha"].Headers["stable"]; got != "yes" {
		t.Fatalf("reload mutated live config through an escaped pointer: %q", got)
	}
	if s.cfg.ToolSearchConfigSnapshot() == nil {
		t.Fatal("concurrent MCP saves dropped the ToolSearch settings block")
	}
}

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

func TestMCPReloadSerializesCatalogPublication(t *testing.T) {
	oldStarted := make(chan struct{})
	newStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	s := &Server{
		cfg: &config.Config{MCPServers: map[string]*config.MCPServer{
			"catalog": {Type: "stdio", Command: "old"},
		}},
		needsSetup:  true,
		mcpStatuses: map[string]tools.MCPStatus{},
	}
	s.reloadMCP = func(servers map[string]*config.MCPServer) ([]tools.MCPStatus, error) {
		if servers["catalog"].Command == "old" {
			close(oldStarted)
			<-releaseOld
			return []tools.MCPStatus{{Name: "catalog", ToolCount: 1}}, nil
		}
		close(newStarted)
		return []tools.MCPStatus{{Name: "catalog", ToolCount: 2}}, nil
	}

	oldDone := make(chan error, 1)
	go func() { oldDone <- s.reloadMCPAndRebuild() }()
	<-oldStarted
	s.cfgMu.Lock()
	s.cfg.MCPServers["catalog"].Command = "new"
	s.cfgMu.Unlock()

	newDone := make(chan error, 1)
	go func() { newDone <- s.reloadMCPAndRebuild() }()
	select {
	case <-newStarted:
		// Without serialization the newer reload can publish first; let it finish
		// before the old one so the stale final state is deterministic.
		if err := <-newDone; err != nil {
			t.Fatal(err)
		}
		close(releaseOld)
		if err := <-oldDone; err != nil {
			t.Fatal(err)
		}
	case <-time.After(50 * time.Millisecond):
		// The newer reload is correctly waiting for the older publication.
		close(releaseOld)
		if err := <-oldDone; err != nil {
			t.Fatal(err)
		}
		<-newStarted
		if err := <-newDone; err != nil {
			t.Fatal(err)
		}
	}

	s.mu.RLock()
	got := s.mcpStatuses["catalog"].ToolCount
	s.mu.RUnlock()
	if got != 2 {
		t.Fatalf("final MCP catalog tool count = %d, want newest reload count 2", got)
	}
}

func TestMCPLoginStatusSnapshotsConcurrentUpdates(t *testing.T) {
	s := &Server{mcpLogins: map[string]*mcpLoginState{
		"server": {Status: "0", AuthURL: "0", Message: "0"},
	}}
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for i := 1; i <= 500; i++ {
			value := fmt.Sprintf("%d", i)
			s.mu.Lock()
			*s.mcpLogins["server"] = mcpLoginState{Status: value, AuthURL: value, Message: value}
			s.mu.Unlock()
		}
	}()

	for i := 0; i < 500; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/mcp/server/login/status", nil)
		req.SetPathValue("name", "server")
		rec := httptest.NewRecorder()
		s.handleMCPLoginStatus(rec, req)
		var got mcpLoginState
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != got.AuthURL || got.Status != got.Message {
			t.Fatalf("torn login snapshot: %+v", got)
		}
	}
	<-writerDone
}
