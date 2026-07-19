package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/memory"
	mempipeline "github.com/cnjack/jcode/internal/memory/pipeline"
	"github.com/cnjack/jcode/internal/tools"
)

func TestToolSearchStatusAndConfigRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		Engine: &Engine{toolSearchStats: func() ToolSearchCounts {
			return ToolSearchCounts{DirectCount: 8, DeferredCount: 13, MCPDeferredCount: 4}
		}},
		cfg: &config.Config{},
	}

	get := httptest.NewRecorder()
	s.handleToolSearchStatus(get, httptest.NewRequest(http.MethodGet, "/api/tool-search/status", nil))
	var initial struct {
		Available        bool `json:"available"`
		Enabled          bool `json:"enabled"`
		Direct           int  `json:"direct_count"`
		Deferred         int  `json:"deferred_count"`
		MCPDeferredCount int  `json:"mcp_deferred_count"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if !initial.Available || initial.Enabled || initial.Direct != 8 || initial.Deferred != 13 || initial.MCPDeferredCount != 4 {
		t.Fatalf("unexpected initial status: %#v", initial)
	}

	post := httptest.NewRecorder()
	s.handleToolSearchConfig(post, httptest.NewRequest(http.MethodPost, "/api/tool-search/config",
		strings.NewReader(`{"enabled":true}`)))
	if post.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", post.Code, post.Body.String())
	}
	if !config.ToolSearchEnabled(s.cfg) {
		t.Fatal("tool search setting was not published")
	}

	get = httptest.NewRecorder()
	s.handleToolSearchStatus(get, httptest.NewRequest(http.MethodGet, "/api/tool-search/status", nil))
	if err := json.Unmarshal(get.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if !initial.Enabled {
		t.Fatal("GET did not return saved setting")
	}
}

func TestToolSearchConfigSaveFailureRollsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	s := &Server{cfg: cfg}

	rec := httptest.NewRecorder()
	s.handleToolSearchConfig(rec, httptest.NewRequest(http.MethodPost, "/api/tool-search/config",
		strings.NewReader(`{"enabled":true}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s, want 500", rec.Code, rec.Body.String())
	}
	if config.ToolSearchEnabled(cfg) {
		t.Fatal("failed save changed live config")
	}
	if cfg.ToolSearchConfigSnapshot() != nil {
		t.Fatal("failed save replaced an absent ToolSearch block")
	}
}

func TestToolSearchConfigRequiresEnabled(t *testing.T) {
	s := &Server{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	s.handleToolSearchConfig(rec, httptest.NewRequest(http.MethodPost, "/api/tool-search/config",
		strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestToolSearchConfigReturnsRebuildWarningAfterCommit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		Engine: &Engine{taskID: "active", createAgent: func(string, string) (*adk.ChatModelAgent, error) {
			return nil, errors.New("rebuild failed")
		}},
		cfg: &config.Config{},
	}
	rec := httptest.NewRecorder()
	s.handleToolSearchConfig(rec, httptest.NewRequest(http.MethodPost, "/api/tool-search/config",
		strings.NewReader(`{"enabled":true}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"warning_code":"agent_refresh_failed"`) {
		t.Fatalf("status=%d body=%s, want committed warning", rec.Code, rec.Body.String())
	}
	if !config.ToolSearchEnabled(s.cfg) {
		t.Fatal("rebuild failure rolled back a successfully persisted setting")
	}
}

func fullMemoryConfigJSON(enabled bool) string {
	value := "false"
	if enabled {
		value = "true"
	}
	return `{"enabled":` + value + `,"generate":true,"model":"provider/memory-model",` +
		`"daily_token_budget":12345,"cooldown_hours":8,"max_age_days":31,` +
		`"max_unused_days":46,"phase2_top_n":41,"summary_inject_tokens":1300}`
}

func localMemoryServer(project string, cfg *config.Config) *Server {
	return &Server{
		Engine:     &Engine{pwd: project, env: tools.NewEnv(project, "test")},
		cfg:        cfg,
		memoryRuns: make(map[string]bool),
	}
}

func memorySyncRequest(project string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/memory/sync",
		strings.NewReader(`{"project":`+strconv.Quote(project)+`}`))
}

func memoryClearRequest(project string) *http.Request {
	return httptest.NewRequest(http.MethodDelete,
		"/api/memory?scope=project&project="+url.QueryEscape(project), nil)
}

func TestMemoryConfigValidationRoundTripAndRollback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	cfg := &config.Config{}
	s := localMemoryServer(project, cfg)

	bad := httptest.NewRecorder()
	s.handleMemoryConfig(bad, httptest.NewRequest(http.MethodPost, "/api/memory/config",
		strings.NewReader(strings.Replace(fullMemoryConfigJSON(true), `"cooldown_hours":8`, `"cooldown_hours":0`, 1))))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid POST status=%d body=%s", bad.Code, bad.Body.String())
	}

	rec := httptest.NewRecorder()
	s.handleMemoryConfig(rec, httptest.NewRequest(http.MethodPost, "/api/memory/config",
		strings.NewReader(fullMemoryConfigJSON(false))))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored := cfg.MemorySettings()
	if config.MemoryEnabled(cfg) || stored.Model != "provider/memory-model" || stored.DailyTokenBudget != 12345 {
		t.Fatalf("stored memory config=%#v", stored)
	}

	// Turn the config directory into an unwritable shape and verify the next
	// failed save restores the previously committed settings.
	if err := os.RemoveAll(filepath.Join(home, ".jcode")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	s.handleMemoryConfig(rec, httptest.NewRequest(http.MethodPost, "/api/memory/config",
		strings.NewReader(fullMemoryConfigJSON(true))))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("rollback POST status=%d body=%s", rec.Code, rec.Body.String())
	}
	if config.MemoryEnabled(cfg) || cfg.MemorySettings().Model != "provider/memory-model" {
		t.Fatalf("save failure did not restore previous settings: %#v", cfg.MemorySettings())
	}
}

func TestMemoryConfigRequiresBooleanFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := localMemoryServer(t.TempDir(), &config.Config{})
	for _, body := range []string{
		strings.Replace(fullMemoryConfigJSON(true), `"enabled":true,`, "", 1),
		strings.Replace(fullMemoryConfigJSON(true), `"generate":true,`, "", 1),
	} {
		rec := httptest.NewRecorder()
		s.handleMemoryConfig(rec, httptest.NewRequest(http.MethodPost, "/api/memory/config", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
		}
	}
}

func TestMemoryStatusPreservesGeneratePreferenceWhenDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	enabled, generate := false, true
	cfg := &config.Config{}
	cfg.SetMemory(&config.MemoryConfig{Enabled: &enabled, Generate: &generate})
	s := localMemoryServer(t.TempDir(), cfg)
	rec := httptest.NewRecorder()
	s.handleMemoryStatus(rec, httptest.NewRequest(http.MethodGet, "/api/memory/status", nil))
	var status struct {
		EffectiveGenerate bool `json:"effective_generate"`
		Config            struct {
			Enabled  bool `json:"enabled"`
			Generate bool `json:"generate"`
		} `json:"config"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Config.Enabled || !status.Config.Generate || status.EffectiveGenerate {
		t.Fatalf("unexpected disabled preference status: %+v", status)
	}
}

func TestMemoryConfigSaveFailureRestoresAbsentBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	s := localMemoryServer(t.TempDir(), cfg)
	rec := httptest.NewRecorder()
	s.handleMemoryConfig(rec, httptest.NewRequest(http.MethodPost, "/api/memory/config",
		strings.NewReader(fullMemoryConfigJSON(true))))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s, want 500", rec.Code, rec.Body.String())
	}
	if cfg.MemoryConfigSnapshot() != nil {
		t.Fatal("failed save replaced an absent Memory block")
	}
}

func TestMemoryStatusIsMetadataOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	s := localMemoryServer(project, &config.Config{})
	root := memory.ProjectRoot(project)
	if err := memory.EnsureScope(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, memory.SummaryFile), []byte("secret summary body"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.WriteNote(memory.Note{Cwd: project, Text: "secret note body"}); err != nil {
		t.Fatal(err)
	}
	if err := memory.UpdateState(root, func(st *memory.State) error {
		st.Files["MEMORY.md"] = &memory.FileUsage{UsageCount: 2}
		st.Extracted = map[string]*memory.ExtractRecord{
			"ok":     {At: time.Now().Format(time.RFC3339)},
			"failed": {At: time.Now().Format(time.RFC3339), Failed: true, Error: "credential-canary"},
		}
		st.Budget = map[string]int64{}
		st.Budget[time.Now().Format("2006-01-02")] = 77
		st.LastPipelineAt = "2026-07-19T01:02:03Z"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleMemoryStatus(rec, httptest.NewRequest(http.MethodGet, "/api/memory/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret summary") || strings.Contains(body, "secret note") || strings.Contains(body, "credential-canary") {
		t.Fatalf("memory content/error leaked through status: %s", body)
	}
	var status struct {
		Available      bool `json:"available"`
		SummaryExists  bool `json:"summary_exists"`
		SummarySize    int  `json:"summary_size"`
		NotesCount     int  `json:"notes_count"`
		TrackedFiles   int  `json:"tracked_files"`
		ExtractedCount int  `json:"extracted_count"`
		FailedCount    int  `json:"failed_count"`
		TodayTokens    int  `json:"today_tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Available || !status.SummaryExists || status.SummarySize == 0 || status.NotesCount != 1 ||
		status.TrackedFiles != 1 || status.ExtractedCount != 2 || status.FailedCount != 1 || status.TodayTokens != 77 {
		t.Fatalf("unexpected memory metadata: %#v", status)
	}
}

func TestMemorySyncIsAsyncAndRejectsDuplicate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	s := localMemoryServer(project, &config.Config{})
	started := make(chan struct{})
	release := make(chan struct{})
	s.memoryStart = func(ctx context.Context, got string) (<-chan error, error) {
		if got != project {
			t.Errorf("project=%q want %q", got, project)
		}
		close(started)
		done := make(chan error, 1)
		go func() {
			select {
			case <-release:
				done <- nil
			case <-ctx.Done():
				done <- ctx.Err()
			}
			close(done)
		}()
		return done, nil
	}

	first := httptest.NewRecorder()
	s.handleMemorySync(first, memorySyncRequest(project))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("async sync did not start")
	}
	second := httptest.NewRecorder()
	s.handleMemorySync(second, memorySyncRequest(project))
	if second.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", second.Code, second.Body.String())
	}
	close(release)
}

func TestMemorySyncReportsCrossProcessBusy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	s := localMemoryServer(project, &config.Config{})
	s.memoryStart = func(context.Context, string) (<-chan error, error) {
		return nil, mempipeline.ErrAlreadyRunning
	}

	rec := httptest.NewRecorder()
	s.handleMemorySync(rec, memorySyncRequest(project))
	if rec.Code != http.StatusConflict {
		t.Fatalf("busy sync status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMemorySyncReportsStartFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	s := localMemoryServer(project, &config.Config{})
	s.memoryStart = func(context.Context, string) (<-chan error, error) {
		return nil, errors.New("start failed")
	}
	rec := httptest.NewRecorder()
	s.handleMemorySync(rec, memorySyncRequest(project))
	if rec.Code != http.StatusInternalServerError || s.memoryRunning(project) {
		t.Fatalf("status=%d running=%v body=%s", rec.Code, s.memoryRunning(project), rec.Body.String())
	}
}

func waitMemoryIdle(t *testing.T, s *Server, project string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.memoryRunning(project) {
		if time.Now().After(deadline) {
			t.Fatal("memory operation did not become idle")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMemorySyncSuccessRebuildsAgents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	done := make(chan error, 1)
	rebuilt := make(chan struct{}, 1)
	s := localMemoryServer(project, &config.Config{})
	s.createAgent = func(string, string) (*adk.ChatModelAgent, error) {
		rebuilt <- struct{}{}
		return new(adk.ChatModelAgent), nil
	}
	s.memoryStart = func(context.Context, string) (<-chan error, error) {
		return done, nil
	}

	rec := httptest.NewRecorder()
	s.handleMemorySync(rec, memorySyncRequest(project))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	done <- nil
	close(done)
	select {
	case <-rebuilt:
	case <-time.After(2 * time.Second):
		t.Fatal("successful memory sync did not rebuild active agents")
	}
	waitMemoryIdle(t, s, project)
}

func TestMemorySyncAsyncFailureSurfacesStatusWarning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	done := make(chan error, 1)
	var rebuilds atomic.Int32
	s := localMemoryServer(project, &config.Config{})
	s.createAgent = func(string, string) (*adk.ChatModelAgent, error) {
		rebuilds.Add(1)
		return new(adk.ChatModelAgent), nil
	}
	s.memoryStart = func(context.Context, string) (<-chan error, error) {
		return done, nil
	}

	rec := httptest.NewRecorder()
	s.handleMemorySync(rec, memorySyncRequest(project))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	done <- errors.New("credential-canary")
	close(done)
	waitMemoryIdle(t, s, project)

	status := httptest.NewRecorder()
	s.handleMemoryStatus(status, httptest.NewRequest(http.MethodGet, "/api/memory/status", nil))
	if !strings.Contains(status.Body.String(), `"warning":"`+memorySyncFailedWarning+`"`) {
		t.Fatalf("async failure warning missing: %s", status.Body.String())
	}
	if strings.Contains(status.Body.String(), "credential-canary") {
		t.Fatalf("raw pipeline error leaked through status: %s", status.Body.String())
	}
	if rebuilds.Load() != 0 {
		t.Fatalf("failed sync rebuilt agents %d times", rebuilds.Load())
	}
}

func TestMemoryClearRebuildsAgentsAndReportsRefreshFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	if _, err := memory.WriteNote(memory.Note{Cwd: project, Text: "delete me"}); err != nil {
		t.Fatal(err)
	}
	s := localMemoryServer(project, &config.Config{})
	s.createAgent = func(string, string) (*adk.ChatModelAgent, error) {
		return nil, errors.New("rebuild failed")
	}
	rec := httptest.NewRecorder()
	s.handleMemoryClear(rec, memoryClearRequest(project))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"warning_code":"agent_refresh_failed"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if warning := s.memoryWarning(project); warning != memoryRefreshWarning {
		t.Fatalf("warning=%q, want %q", warning, memoryRefreshWarning)
	}
}

func TestMemoryClearProjectAndRemoteBoundary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	s := localMemoryServer(project, &config.Config{})
	if _, err := memory.WriteNote(memory.Note{Cwd: project, Text: "delete me"}); err != nil {
		t.Fatal(err)
	}

	wrongScope := httptest.NewRecorder()
	s.handleMemoryClear(wrongScope, httptest.NewRequest(http.MethodDelete, "/api/memory?scope=all", nil))
	if wrongScope.Code != http.StatusBadRequest {
		t.Fatalf("scope=all status=%d", wrongScope.Code)
	}
	release, locked, err := memory.TryLockPipeline(memory.ProjectRoot(project))
	if err != nil || !locked {
		t.Fatalf("take pipeline lock: locked=%v err=%v", locked, err)
	}
	busy := httptest.NewRecorder()
	s.handleMemoryClear(busy, memoryClearRequest(project))
	if busy.Code != http.StatusConflict {
		t.Fatalf("busy clear status=%d body=%s", busy.Code, busy.Body.String())
	}
	release()
	clear := httptest.NewRecorder()
	s.handleMemoryClear(clear, memoryClearRequest(project))
	if clear.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", clear.Code, clear.Body.String())
	}
	if _, err := os.Stat(memory.ProjectRoot(project)); !os.IsNotExist(err) {
		t.Fatalf("project memory still exists: %v", err)
	}

	remote := &Server{
		Engine:     &Engine{pwd: "/remote/project", env: &tools.Env{}}, // nil Exec is non-local
		cfg:        &config.Config{},
		memoryRuns: make(map[string]bool),
	}
	status := httptest.NewRecorder()
	remote.handleMemoryStatus(status, httptest.NewRequest(http.MethodGet, "/api/memory/status", nil))
	if !strings.Contains(status.Body.String(), `"remote":true`) ||
		!strings.Contains(status.Body.String(), `"supported":false`) ||
		!strings.Contains(status.Body.String(), `"available":true`) {
		t.Fatalf("remote status boundary missing: %s", status.Body.String())
	}
	configRec := httptest.NewRecorder()
	remote.handleMemoryConfig(configRec, httptest.NewRequest(http.MethodPost, "/api/memory/config",
		strings.NewReader(fullMemoryConfigJSON(true))))
	if configRec.Code != http.StatusOK {
		t.Fatalf("remote global config status=%d body=%s", configRec.Code, configRec.Body.String())
	}
	syncRec := httptest.NewRecorder()
	remote.handleMemorySync(syncRec, memorySyncRequest("/remote/project"))
	if syncRec.Code != http.StatusBadRequest {
		t.Fatalf("remote sync status=%d body=%s", syncRec.Code, syncRec.Body.String())
	}
}

func TestMemoryActionsRejectStaleProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	projectA, projectB := t.TempDir(), t.TempDir()
	if _, err := memory.WriteNote(memory.Note{Cwd: projectA, Text: "keep A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.WriteNote(memory.Note{Cwd: projectB, Text: "keep B"}); err != nil {
		t.Fatal(err)
	}
	s := localMemoryServer(projectB, &config.Config{})
	starts := 0
	s.memoryStart = func(context.Context, string) (<-chan error, error) {
		starts++
		return nil, errors.New("must not start")
	}

	clear := httptest.NewRecorder()
	s.handleMemoryClear(clear, memoryClearRequest(projectA))
	if clear.Code != http.StatusConflict {
		t.Fatalf("stale clear status=%d body=%s", clear.Code, clear.Body.String())
	}
	syncRec := httptest.NewRecorder()
	s.handleMemorySync(syncRec, memorySyncRequest(projectA))
	if syncRec.Code != http.StatusConflict || starts != 0 {
		t.Fatalf("stale sync status=%d starts=%d body=%s", syncRec.Code, starts, syncRec.Body.String())
	}
	for _, project := range []string{projectA, projectB} {
		if len(memory.RecentNotes(memory.ProjectRoot(project), 0)) != 1 {
			t.Fatalf("memory for %s was changed by stale action", project)
		}
	}
}
