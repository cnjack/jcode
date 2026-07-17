package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cnjack/jcode/internal/computer"
	"github.com/cnjack/jcode/internal/config"
)

func TestComputerStatusReturnsCanonicalGrantConfig(t *testing.T) {
	cfg := &config.Config{Computer: &config.ComputerConfig{
		Enabled:         false,
		Backend:         "fake", // migration-only field must never reach REST
		ClipboardRead:   true,
		ClipboardWrite:  true,
		SystemKeyCombos: true,
		Approval:        map[string]string{"launch": "always_allow"},
		AppPermissions:  []config.ComputerAppPermission{{BundleID: "com.apple.Notes", Interact: "always_allow"}},
	}}
	mgr := computer.NewManager(computer.FromConfig(cfg.Computer), t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })
	s := &Server{cfg: cfg, computerMgr: mgr}

	rec := httptest.NewRecorder()
	s.handleComputerStatus(rec, httptest.NewRequest(http.MethodGet, "/api/computer/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got, ok := body["config"].(map[string]any)
	if !ok {
		t.Fatalf("canonical config missing: %#v", body)
	}
	for _, field := range []string{"clipboard_read", "clipboard_write", "system_key_combos"} {
		if got[field] != true {
			t.Errorf("config.%s=%v, want true", field, got[field])
		}
	}
	if _, leaked := got["backend"]; leaked {
		t.Errorf("deprecated backend leaked through API: %#v", got)
	}
	status, ok := body["status"].(map[string]any)
	if !ok {
		t.Fatalf("status missing: %#v", body)
	}
	if _, leaked := status["backend"]; leaked {
		t.Errorf("backend implementation leaked through public status: %#v", status)
	}
	if _, leaked := status["backend_kind"]; leaked {
		t.Errorf("backend kind leaked through public status: %#v", status)
	}
}

func TestComputerConfigRejectsBackendSelector(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	s.handleComputerConfig(rec, httptest.NewRequest(http.MethodPost, "/api/computer/config",
		strings.NewReader(`{"enabled":false,"backend":"fake"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown field") {
		t.Fatalf("error is not actionable: %s", rec.Body.String())
	}
}

func TestComputerConfigGrantRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr := computer.NewManager(computer.Config{}, t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })
	s := &Server{cfg: &config.Config{}, computerMgr: mgr}
	body := `{
		"enabled": false,
		"approval": {"launch":"always_allow"},
		"app_permissions": [{"bundle_id":"com.apple.Notes","interact":"always_allow"}],
		"max_actions_per_batch": 9,
		"clipboard_read": true,
		"clipboard_write": true,
		"system_key_combos": true
	}`
	rec := httptest.NewRecorder()
	s.handleComputerConfig(rec, httptest.NewRequest(http.MethodPost, "/api/computer/config", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", rec.Code, rec.Body.String())
	}
	if c := s.cfg.Computer; c == nil || !c.ClipboardRead || !c.ClipboardWrite || !c.SystemKeyCombos || c.MaxActionsPerBatch != 9 {
		t.Fatalf("live config lost grants: %#v", c)
	}

	get := httptest.NewRecorder()
	s.handleComputerStatus(get, httptest.NewRequest(http.MethodGet, "/api/computer/status", nil))
	var response struct {
		Config computerConfigPayload `json:"config"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Config.ClipboardRead || !response.Config.ClipboardWrite || !response.Config.SystemKeyCombos {
		t.Fatalf("round-trip lost grants: %#v", response.Config)
	}
}

func TestComputerConfigSaveFailureRollsBackLiveConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Make ~/.jcode impossible to create as a directory.
	if err := os.WriteFile(filepath.Join(home, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := &config.ComputerConfig{Enabled: false, ClipboardRead: true}
	mgr := computer.NewManager(computer.FromConfig(previous), home)
	t.Cleanup(func() { _ = mgr.Close() })
	s := &Server{cfg: &config.Config{Computer: previous}, computerMgr: mgr}

	rec := httptest.NewRecorder()
	s.handleComputerConfig(rec, httptest.NewRequest(http.MethodPost, "/api/computer/config",
		strings.NewReader(`{"enabled":false,"clipboard_read":false}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s, want 500", rec.Code, rec.Body.String())
	}
	if s.cfg.Computer != previous || !s.cfg.Computer.ClipboardRead {
		t.Fatalf("failed save changed live config: %#v", s.cfg.Computer)
	}
	if !mgr.GetConfig().ClipboardRead {
		t.Fatal("failed save changed Manager policy")
	}
}

func TestComputerConfigReturnsStableAgentRefreshWarningCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	eng := &Engine{
		taskID: "active",
		createAgent: func(_, _ string) (*adk.ChatModelAgent, error) {
			return nil, errors.New("provider unavailable")
		},
	}
	s := &Server{Engine: eng, cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	s.handleComputerConfig(rec, httptest.NewRequest(http.MethodPost, "/api/computer/config",
		strings.NewReader(`{"enabled":false}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		WarningCode string `json:"warning_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.WarningCode != "agent_refresh_failed" {
		t.Fatalf("warning_code=%q, want agent_refresh_failed", response.WarningCode)
	}
}

func TestValidateComputerEnableRejectsUnsupportedPlatform(t *testing.T) {
	if err := validateComputerEnable(true, false); err == nil || !strings.Contains(err.Error(), "macOS 14.0") {
		t.Fatalf("enable on unsupported platform error=%v", err)
	}
	if err := validateComputerEnable(false, false); err != nil {
		t.Fatalf("disabling must remain possible on unsupported platform: %v", err)
	}
}

func TestComputerConfigRebuildsAllLiveTaskToolSchemas(t *testing.T) {
	calls := map[string]int{}
	makeEngine := func(id string) *Engine {
		return &Engine{
			taskID: id,
			createAgent: func(_, _ string) (*adk.ChatModelAgent, error) {
				calls[id]++
				return nil, nil
			},
		}
	}
	active := makeEngine("active")
	background := makeEngine("background")
	s := &Server{
		Engine: active,
		tasks: map[string]*Engine{
			"active":     active, // duplicated intentionally; rebuild must dedupe
			"background": background,
		},
	}
	if err := s.rebuildComputerAgent(); err != nil {
		t.Fatal(err)
	}
	if calls["active"] != 1 || calls["background"] != 1 {
		t.Fatalf("rebuild calls=%v, want each live task exactly once", calls)
	}
}

func TestComputerAgentRebuildDoesNotOverwriteConcurrentModeSwitch(t *testing.T) {
	staleAgent := new(adk.ChatModelAgent)
	newAgent := new(adk.ChatModelAgent)
	started := make(chan struct{})
	release := make(chan struct{})
	eng := &Engine{
		taskID:       "active",
		mode:         "build",
		providerName: "provider-a",
		modelName:    "model-a",
		createAgent: func(_, _ string) (*adk.ChatModelAgent, error) {
			close(started)
			<-release
			return staleAgent, nil
		},
	}
	s := &Server{Engine: eng}
	done := make(chan error, 1)
	go func() { done <- s.rebuildComputerAgent() }()
	<-started
	eng.applyModeSwitch("plan", newAgent)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	eng.emu.Lock()
	defer eng.emu.Unlock()
	if eng.agent != newAgent || eng.mode != "plan" {
		t.Fatalf("stale rebuild overwrote concurrent switch: agent=%p mode=%q", eng.agent, eng.mode)
	}
}

func TestComputerPermissionRequestValidation(t *testing.T) {
	supported := computer.Supported()

	// A server without the manager cannot ask for anything.
	nilSrv := &Server{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	nilSrv.handleComputerPermissionRequest(rec, httptest.NewRequest(http.MethodPost,
		"/api/computer/permissions", strings.NewReader(`{"accessibility":true}`)))
	if supported {
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("nil-manager status=%d body=%s, want 503", rec.Code, rec.Body.String())
		}
	} else if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported-platform status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}

	mgr := computer.NewManager(computer.Config{}, t.TempDir())
	t.Cleanup(func() { _ = mgr.Close() })
	s := &Server{cfg: &config.Config{}, computerMgr: mgr}
	for name, body := range map[string]string{
		// Rejected before the helper is ever dialed.
		"empty flags":   `{}`,
		"malformed":     `{"accessibility":`,
		"unknown field": `{"a11y":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.handleComputerPermissionRequest(rec, httptest.NewRequest(http.MethodPost,
				"/api/computer/permissions", strings.NewReader(body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleComputerShotServesOpenedFileHandle(t *testing.T) {
	home := t.TempDir()
	mgr := computer.NewManager(computer.Config{}, home)
	t.Cleanup(func() { _ = mgr.Close() })
	id, err := mgr.SaveScreenshot([]byte("0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{computerMgr: mgr}
	req := httptest.NewRequest(http.MethodGet, "/api/computer/shots/"+id+".png", nil)
	req.Header.Set("Range", "bytes=2-5")
	req.SetPathValue("id", id+".png")
	rec := httptest.NewRecorder()

	s.handleComputerShot(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type=%q, want image/png", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=3600" {
		t.Fatalf("Cache-Control=%q", got)
	}
	if got := rec.Body.String(); got != "2345" {
		t.Fatalf("range body=%q, want %q", got, "2345")
	}
}
