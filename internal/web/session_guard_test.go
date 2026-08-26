package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

func newGuardedSessionTestServer(t *testing.T) (*Server, *Engine) {
	t.Helper()
	s := stubFactoryServer(t)
	active, err := s.buildLocalEngine("active-session", t.TempDir(), "build")
	if err != nil {
		t.Fatal(err)
	}
	s.setActiveEngine(active)
	t.Cleanup(s.CloseAllEngines)
	return s, active
}

func freshSessionGuardReason(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code   string `json:"code"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode guard response: %v", err)
	}
	if body.Code != "fresh_session_guard_failed" {
		t.Fatalf("guard code = %q, body=%s", body.Code, rec.Body.String())
	}
	return body.Reason
}

func TestNewSessionGuardRejectsStaleExpectedSessionBeforeBuild(t *testing.T) {
	s, active := newGuardedSessionTestServer(t)
	baseFactory := s.newEngine
	var builds atomic.Int32
	s.newEngine = func(taskID, pwd, modeName string) (*EngineConfig, error) {
		builds.Add(1)
		return baseFactory(taskID, pwd, modeName)
	}

	rec := httptest.NewRecorder()
	s.handleNewSession(rec, httptest.NewRequest(
		http.MethodPost,
		"/api/sessions",
		strings.NewReader(`{"expected_session_id":"stale-session","require_idle":true}`),
	))

	if rec.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := freshSessionGuardReason(t, rec); got != string(activeEngineChanged) {
		t.Fatalf("reason=%q, want %q", got, activeEngineChanged)
	}
	if builds.Load() != 0 {
		t.Fatalf("fresh candidates built = %d, want 0", builds.Load())
	}
	if s.activeEngine() != active {
		t.Fatal("stale preflight changed the active engine")
	}
}

func TestNewSessionGuardDoesNotOverwriteConcurrentNavigation(t *testing.T) {
	s, _ := newGuardedSessionTestServer(t)
	baseFactory := s.newEngine
	buildStarted := make(chan struct{})
	releaseBuild := make(chan struct{})
	s.newEngine = func(taskID, pwd, modeName string) (*EngineConfig, error) {
		close(buildStarted)
		<-releaseBuild
		return baseFactory(taskID, pwd, modeName)
	}

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.handleNewSession(rec, httptest.NewRequest(
			http.MethodPost,
			"/api/sessions",
			strings.NewReader(`{"pwd":"`+t.TempDir()+`","expected_session_id":"active-session","require_idle":true}`),
		))
		close(done)
	}()
	<-buildStarted

	navConfig, err := baseFactory("navigated-session", t.TempDir(), "build")
	if err != nil {
		t.Fatal(err)
	}
	navigated := newEngine(navConfig)
	s.setActiveEngine(navigated)
	close(releaseBuild)
	<-done

	if rec.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := freshSessionGuardReason(t, rec); got != string(activeEngineChanged) {
		t.Fatalf("reason=%q, want %q", got, activeEngineChanged)
	}
	if s.activeEngine() != navigated {
		t.Fatal("slow fresh-session response overwrote the newer navigation")
	}
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()
	if len(s.tasks) != 1 || s.tasks[navigated.taskID] != navigated {
		t.Fatalf("guard failure leaked a candidate: tasks=%v", s.tasks)
	}
}

func TestNewSessionRequireIdleSnapshotsActiveSessionWhenExpectedIsOmitted(t *testing.T) {
	s, _ := newGuardedSessionTestServer(t)
	baseFactory := s.newEngine
	buildStarted := make(chan struct{})
	releaseBuild := make(chan struct{})
	s.newEngine = func(taskID, pwd, modeName string) (*EngineConfig, error) {
		close(buildStarted)
		<-releaseBuild
		return baseFactory(taskID, pwd, modeName)
	}

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.handleNewSession(rec, httptest.NewRequest(
			http.MethodPost,
			"/api/sessions",
			strings.NewReader(`{"pwd":"`+t.TempDir()+`","require_idle":true}`),
		))
		close(done)
	}()
	<-buildStarted

	navConfig, err := baseFactory("navigated-session", t.TempDir(), "build")
	if err != nil {
		t.Fatal(err)
	}
	navigated := newEngine(navConfig)
	s.setActiveEngine(navigated)
	close(releaseBuild)
	<-done

	if rec.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := freshSessionGuardReason(t, rec); got != string(activeEngineChanged) {
		t.Fatalf("reason=%q, want %q", got, activeEngineChanged)
	}
	if s.activeEngine() != navigated {
		t.Fatal("require_idle-only request overwrote the newer idle navigation")
	}
}

func TestNewSessionGuardCleansScratchCandidateWhenTaskStartsRunning(t *testing.T) {
	s, active := newGuardedSessionTestServer(t)
	baseFactory := s.newScratchEngine
	if baseFactory == nil {
		baseFactory = s.newEngine
	}
	buildStarted := make(chan string, 1)
	releaseBuild := make(chan struct{})
	s.newScratchEngine = func(taskID, pwd, modeName string) (*EngineConfig, error) {
		buildStarted <- pwd
		<-releaseBuild
		cfg, err := baseFactory(taskID, pwd, modeName)
		if cfg != nil {
			cfg.WorkspaceKind = session.WorkspaceScratch
			if cfg.Recorder != nil {
				cfg.Recorder.SetWorkspaceKind(session.WorkspaceScratch)
			}
		}
		return cfg, err
	}

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.handleNewSession(rec, httptest.NewRequest(
			http.MethodPost,
			"/api/sessions",
			strings.NewReader(`{"workspace_kind":"scratch","expected_session_id":"active-session","require_idle":true}`),
		))
		close(done)
	}()
	scratchPath := <-buildStarted
	if !s.tryStartEngine(active) {
		t.Fatal("failed to mark the expected task running")
	}
	close(releaseBuild)
	<-done
	active.running.Store(false)

	if rec.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := freshSessionGuardReason(t, rec); got != string(activeEngineBusy) {
		t.Fatalf("reason=%q, want %q", got, activeEngineBusy)
	}
	if s.activeEngine() != active {
		t.Fatal("slow fresh-session response replaced a newly running task")
	}
	if _, err := os.Stat(scratchPath); !os.IsNotExist(err) {
		t.Fatalf("scratch candidate path still exists: path=%q err=%v", scratchPath, err)
	}
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()
	if len(s.tasks) != 1 || s.tasks[active.taskID] != active {
		t.Fatalf("guard failure leaked a candidate: tasks=%v", s.tasks)
	}
}
