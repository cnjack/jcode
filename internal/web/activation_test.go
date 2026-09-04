package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
	managedworkspace "github.com/cnjack/jcode/internal/workspace"
)

func TestEffectiveSessionWorkspaceKindRepairsLegacyAutomationScratchRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	scratch, err := managedworkspace.CreateScratch(time.Date(2026, 9, 4, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	legacyRun := &session.SessionMeta{
		Project: scratch, WorkspaceKind: session.WorkspaceProject, AutomationID: "automation-1",
	}
	if got := effectiveSessionWorkspaceKind(legacyRun); got != session.WorkspaceScratch {
		t.Fatalf("legacy automation run kind=%q, want scratch", got)
	}
	normal := &session.SessionMeta{Project: scratch, WorkspaceKind: session.WorkspaceProject}
	if got := effectiveSessionWorkspaceKind(normal); got != session.WorkspaceProject {
		t.Fatalf("ordinary project session kind=%q, want project", got)
	}
}

type activationRemoteExecutor struct {
	project  string
	probeErr error
	closed   atomic.Bool
}

func (e *activationRemoteExecutor) ReadFile(context.Context, string) ([]byte, error) {
	return nil, os.ErrNotExist
}
func (e *activationRemoteExecutor) WriteFile(context.Context, string, []byte, os.FileMode) error {
	return nil
}
func (e *activationRemoteExecutor) MkdirAll(context.Context, string, os.FileMode) error {
	return nil
}
func (e *activationRemoteExecutor) Stat(context.Context, string) (*tools.FileInfo, error) {
	return nil, os.ErrNotExist
}
func (e *activationRemoteExecutor) Exec(context.Context, string, string, time.Duration) (string, string, error) {
	return "", "", nil
}
func (e *activationRemoteExecutor) Platform() string            { return "linux/amd64" }
func (e *activationRemoteExecutor) Label() string               { return e.project }
func (e *activationRemoteExecutor) ProjectLabel(string) string  { return e.project }
func (e *activationRemoteExecutor) Probe(context.Context) error { return e.probeErr }
func (e *activationRemoteExecutor) Close() error                { e.closed.Store(true); return nil }

func recordActivationSession(t *testing.T, id, project string) {
	recordActivationSessionWithModel(t, id, project, "test-provider", "test-model")
}

func recordActivationSessionWithModel(t *testing.T, id, project, provider, model string) {
	t.Helper()
	recorder, err := session.NewRecorder(project, provider, model)
	if err != nil {
		t.Fatal(err)
	}
	recorder.SetUUID(id)
	recorder.RecordUser("persisted turn")
	recorder.Close()
}

func activationTestServer(t *testing.T, blockRole <-chan struct{}) (*Server, *Engine, *atomic.Int32) {
	t.Helper()
	activePwd := t.TempDir()
	activeEnv := tools.NewEnv(activePwd, "darwin/arm64")
	active := newEngine(&EngineConfig{
		TaskID: "active-task", Pwd: activePwd, Mode: "approval",
		ProviderName: "test-provider", ModelName: "test-model",
		Env: activeEnv, TodoStore: activeEnv.TodoStore, Handler: handler.NewWebHandler(),
	})
	dials := &atomic.Int32{}
	s := &Server{
		Engine: active, tasks: map[string]*Engine{"active-task": active},
		wsBroker: NewWSBroker(), cfg: &config.Config{}, ptyMgr: newPTYManager(),
	}
	s.dialSSH = func(_ context.Context, host, user string) (tools.RemoteExecutor, error) {
		dials.Add(1)
		return &activationRemoteExecutor{project: "ssh://" + user + "@" + host + "/work"}, nil
	}
	s.newRemoteEngine = func(taskID string, exec tools.RemoteExecutor, pwd, modeName string) (*EngineConfig, error) {
		env := tools.NewEnv(pwd, exec.Platform())
		env.SetRemote(exec, pwd)
		recorder, _ := session.NewRecorder(exec.ProjectLabel(pwd), "test-provider", "test-model")
		recorder.SetUUID(taskID)
		cfg := &EngineConfig{
			TaskID: taskID, Pwd: pwd, Mode: modeName,
			ProviderName: "test-provider", ModelName: "test-model",
			Env: env, TodoStore: env.TodoStore, Recorder: recorder,
			Handler: handler.NewWebHandler(),
			CreateAgent: func(string, string) (*adk.ChatModelAgent, error) {
				return &adk.ChatModelAgent{}, nil
			},
		}
		if blockRole != nil {
			cfg.RebuildForRole = func(string, string, string) (*AgentRoleBuild, error) {
				<-blockRole
				return &AgentRoleBuild{Provider: "test-provider", Model: "test-model"}, nil
			}
		}
		return cfg, nil
	}
	t.Cleanup(s.CloseAllEngines)
	return s, active, dials
}

func TestEnsureConversationColdResumeUsesCurrentDefaultModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		id      = "remote-model-session"
		project = "ssh://alice@example.test:22/work"
	)
	// Session metadata describes the model used when the transcript was
	// created. Cold resume has historically followed the current/default model;
	// treating the old fields as a live pair can combine a stale provider with a
	// model id that now belongs to another provider.
	recordActivationSessionWithModel(t, id, project, "legacy-provider", "grok-4.5")
	s, _, _ := activationTestServer(t, nil)

	if _, err := s.ensureConversation(context.Background(), id, "", "desktop"); err != nil {
		t.Fatal(err)
	}
	provider, modelName, _ := s.resolveEngine(id).modelSnapshot()
	if provider != "test-provider" || modelName != "test-model" {
		t.Fatalf("cold resume model = %s/%s, want current default test-provider/test-model", provider, modelName)
	}
}

func TestEnsureConversationColdRemoteHydratesBeforePublish(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		id      = "remote-cold-session"
		project = "ssh://alice@example.test:22/work"
	)
	recordActivationSession(t, id, project)
	release := make(chan struct{})
	s, active, dials := activationTestServer(t, release)

	done := make(chan error, 1)
	go func() {
		_, err := s.ensureConversation(context.Background(), id, "", "cloud")
		done <- err
	}()
	// The factory has dialed, but role hydration is intentionally blocked. The
	// candidate must remain invisible until hydration finishes.
	deadline := time.Now().Add(time.Second)
	for dials.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := s.resolveEngine(id); got != nil {
		t.Fatalf("unhydrated candidate was published: %p", got)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	eng := s.resolveEngine(id)
	if eng == nil {
		t.Fatal("hydrated conversation was not published")
	}
	eng.emu.Lock()
	historyLen := len(eng.history)
	eng.emu.Unlock()
	if historyLen != 1 {
		t.Fatalf("history length = %d, want 1", historyLen)
	}
	if s.activeEngine() != active {
		t.Fatal("background activation changed the Desktop foreground")
	}
	if eng.env == nil || !eng.env.IsRemote() {
		t.Fatal("persisted remote session fell back to a local engine")
	}
}

func TestEnsureConversationReplacesIdleUnhealthyRuntime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		id      = "remote-stale-session"
		project = "ssh://alice@example.test:22/work"
	)
	recordActivationSession(t, id, project)
	s, _, dials := activationTestServer(t, nil)
	if _, err := s.ensureConversation(context.Background(), id, "", "cloud"); err != nil {
		t.Fatal(err)
	}
	old := s.resolveEngine(id)
	oldExec := old.env.Exec.(*activationRemoteExecutor)
	oldExec.probeErr = errors.New("transport closed")

	if _, err := s.ensureConversation(context.Background(), id, "", "cloud"); err != nil {
		t.Fatal(err)
	}
	if got := s.resolveEngine(id); got == nil || got == old {
		t.Fatal("idle unhealthy runtime was not atomically replaced")
	}
	if !oldExec.closed.Load() {
		t.Fatal("replaced runtime did not release its remote lease")
	}
	if dials.Load() != 2 {
		t.Fatalf("dial count = %d, want 2", dials.Load())
	}
}

func TestParseConversationTargetRejectsRemoteDowngrade(t *testing.T) {
	for _, project := range []string{
		"ftp://host/work",
		"ssh://host/work",
		"ssh://user:secret@host/work",
		"docker:///work",
	} {
		if _, err := parseConversationTarget(project); err == nil {
			t.Errorf("parseConversationTarget(%q) unexpectedly succeeded", project)
		}
	}
}

func TestRemoteConnRegistryClaimRestoreAndShutdown(t *testing.T) {
	rg := newRemoteConnRegistry()
	exec := &activationRemoteExecutor{project: "ssh://alice@example.test:22/work"}
	id := rg.add(&pendingConn{exec: exec, createdAt: time.Now()})

	claimed := rg.claim(id)
	if claimed == nil || claimed.exec != exec {
		t.Fatal("claim did not transfer the pending connection")
	}
	rg.closeAll()
	if exec.closed.Load() {
		t.Fatal("registry shutdown closed a connection owned by an in-flight claim")
	}
	rg.restore(id, claimed)
	rg.closeAll()
	if !exec.closed.Load() {
		t.Fatal("registry shutdown did not close a restored pending connection")
	}
	if got := rg.get(id); got != nil {
		t.Fatal("registry retained a connection after shutdown")
	}
}

func TestRemoteBindHydratesExistingSessionWithoutImplicitFocus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		id      = "remote-bind-session"
		project = "ssh://alice@example.test:22/work"
	)
	recordActivationSession(t, id, project)
	s, active, _ := activationTestServer(t, nil)
	s.remoteConns = newRemoteConnRegistry()
	exec := &activationRemoteExecutor{project: project}
	connectionID := s.remoteConns.add(&pendingConn{exec: exec, createdAt: time.Now()})
	body, _ := json.Marshal(map[string]any{
		"connection_id": connectionID,
		"path":          "/work",
		"session_id":    id,
	})
	recorder := httptest.NewRecorder()
	s.handleRemoteBind(recorder, httptest.NewRequest(http.MethodPost, "/api/remote/bind", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("bind code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if s.activeEngine() != active {
		t.Fatal("background existing-session bind changed the foreground")
	}
	eng := s.resolveEngine(id)
	if eng == nil || !eng.env.IsRemote() {
		t.Fatal("existing remote session was not published on its remote executor")
	}
	eng.emu.Lock()
	historyLen := len(eng.history)
	eng.emu.Unlock()
	if historyLen != 1 {
		t.Fatalf("hydrated history length=%d, want 1", historyLen)
	}
	if s.remoteConns.get(connectionID) != nil {
		t.Fatal("bound connection remained in the pending registry")
	}
}

func TestRemoteBindNewWorkspacePreservesLegacyImplicitFocus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const project = "ssh://alice@example.test:22/work"
	s, active, _ := activationTestServer(t, nil)
	s.remoteConns = newRemoteConnRegistry()
	exec := &activationRemoteExecutor{project: project}
	connectionID := s.remoteConns.add(&pendingConn{exec: exec, createdAt: time.Now()})
	body, _ := json.Marshal(map[string]any{
		"connection_id": connectionID,
		"path":          "/work",
	})
	recorder := httptest.NewRecorder()
	s.handleRemoteBind(recorder, httptest.NewRequest(http.MethodPost, "/api/remote/bind", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("bind code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if s.activeEngine() == active || engineProject(s.activeEngine()) != project {
		t.Fatalf("new workspace was not focused: active project=%q", engineProject(s.activeEngine()))
	}
	var result activationResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Focused {
		t.Fatal("new workspace bind response did not report implicit focus")
	}
}
