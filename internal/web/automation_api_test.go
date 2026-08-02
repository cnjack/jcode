package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/automation"
	"github.com/cnjack/jcode/internal/session"
)

func newAutomationTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := automation.NewStoreDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Server{automations: store}
}

func TestAutomationRunsExposeArtifactSummary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	recorder, err := session.NewRecorder(project, "openai", "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	recorder.RecordUser("run")
	if _, err := session.UpdateSessionMeta(recorder.UUID(), func(meta *session.SessionMeta) {
		meta.AutomationID = "automation-1"
		meta.TriggerKind = "manual"
		meta.ArtifactCount = 2
		meta.ArtifactUnseen = true
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	w := httptest.NewRecorder()
	s.handleListAutomationRuns(w, httptest.NewRequest(http.MethodGet, "/api/automations/runs", nil))
	var runs []automationRun
	if err := json.Unmarshal(w.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ArtifactCount != 2 || !runs[0].ArtifactUnseen {
		t.Fatalf("runs = %#v", runs)
	}
}

func TestAutomationAPI_CRUD(t *testing.T) {
	s := newAutomationTestServer(t)
	proj := t.TempDir()

	// Create
	body := `{"name":"Nightly","prompt":"do the thing","project_path":"` + proj +
		`","trigger":{"type":"schedule","cadence":"daily","hour":9,"minute":0},"enabled":true}`
	rec := httptest.NewRecorder()
	s.handleCreateAutomation(rec, httptest.NewRequest(http.MethodPost, "/api/automations", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("create: status %d body %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		HumanSchedule string `json:"human_schedule"`
		Badge         string `json:"badge"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "Nightly" || created.Badge != "Daily" {
		t.Fatalf("unexpected created: %+v", created)
	}

	// List
	rec = httptest.NewRecorder()
	s.handleListAutomations(rec, httptest.NewRequest(http.MethodGet, "/api/automations", nil))
	var list []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("list: want 1 got %d", len(list))
	}

	// Update (rename)
	upd := `{"name":"Renamed","prompt":"do the thing","project_path":"` + proj +
		`","trigger":{"type":"schedule","cadence":"daily","hour":10,"minute":30},"enabled":false}`
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/automations/"+created.ID, strings.NewReader(upd))
	req.SetPathValue("id", created.ID)
	s.handleUpdateAutomation(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status %d body %s", rec.Code, rec.Body.String())
	}
	if s.automations.Get(created.ID).Name != "Renamed" {
		t.Fatal("rename not applied")
	}

	// Delete
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/automations/"+created.ID, nil)
	req.SetPathValue("id", created.ID)
	s.handleDeleteAutomation(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status %d", rec.Code)
	}
	if len(s.automations.List()) != 0 {
		t.Fatal("delete did not remove")
	}
}

func TestAutomationAPI_UpdateIsPartialPatch(t *testing.T) {
	s := newAutomationTestServer(t)
	proj := t.TempDir()
	// Create with a provider/model override, enabled.
	a, err := s.automations.Create(automation.Automation{
		Name: "N", Prompt: "p", ProjectPath: proj, Provider: "openai", Model: "gpt-x",
		Enabled: true, Trigger: automation.Trigger{Type: automation.TriggerSchedule, Cadence: automation.CadenceDaily, Hour: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	// PUT a body that OMITS provider/model/enabled (what the editor sends when
	// pausing/editing). They must be preserved, not wiped.
	body := `{"name":"N2","prompt":"p2","project_path":"` + proj +
		`","trigger":{"type":"schedule","cadence":"daily","hour":10,"minute":0}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/automations/"+a.ID, strings.NewReader(body))
	req.SetPathValue("id", a.ID)
	s.handleUpdateAutomation(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status %d body %s", rec.Code, rec.Body.String())
	}
	got := s.automations.Get(a.ID)
	if got.Name != "N2" || got.Trigger.Hour != 10 {
		t.Fatalf("patched fields not applied: %+v", got)
	}
	if got.Provider != "openai" || got.Model != "gpt-x" {
		t.Fatalf("provider/model override was wiped: %+v", got)
	}
	if !got.Enabled {
		t.Fatal("enabled was flipped by an omitted field")
	}
}

func TestAutomationAPI_CreateValidationError(t *testing.T) {
	s := newAutomationTestServer(t)
	// Empty project must be rejected (no-project automations can't run unattended).
	body := `{"name":"X","prompt":"p","project_path":"","trigger":{"type":"manual"}}`
	rec := httptest.NewRecorder()
	s.handleCreateAutomation(rec, httptest.NewRequest(http.MethodPost, "/api/automations", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// Updating a non-existent automation must return 404, not 400, so clients can
// distinguish a missing resource from a validation error.
func TestAutomationAPI_UpdateNotFound(t *testing.T) {
	s := newAutomationTestServer(t)
	body := `{"name":"x"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/automations/nope", strings.NewReader(body))
	req.SetPathValue("id", "nope")
	s.handleUpdateAutomation(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// A manual "Run Now" must be rejected with 409 while a run for the same
// automation is already in flight (scheduled run recorded as running, or a
// manual run already claimed), so a double-click can't spawn parallel sessions.
func TestAutomationAPI_RunNowConflict(t *testing.T) {
	s := newAutomationTestServer(t)
	proj := t.TempDir()
	a, err := s.automations.Create(automation.Automation{
		Name: "n", Prompt: "p", ProjectPath: proj, Trigger: automation.Trigger{Type: automation.TriggerManual},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Case 1: a scheduled (or prior) run is recorded as running.
	_ = s.automations.UpdateState(a.ID, func(rs *automation.RunState) { rs.LastStatus = automation.StatusRunning })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/automations/"+a.ID+"/run", nil)
	req.SetPathValue("id", a.ID)
	s.handleRunAutomation(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409 (run in progress), got %d (%s)", rec.Code, rec.Body.String())
	}

	// Case 2: state clear, but a manual run already holds the in-flight slot.
	_ = s.automations.UpdateState(a.ID, func(rs *automation.RunState) { rs.LastStatus = "" })
	s.autoRunMu.Lock()
	if s.autoRunInflight == nil {
		s.autoRunInflight = map[string]bool{}
	}
	s.autoRunInflight[a.ID] = true
	s.autoRunMu.Unlock()
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/automations/"+a.ID+"/run", nil)
	req.SetPathValue("id", a.ID)
	s.handleRunAutomation(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409 (already claimed), got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAutomationAPI_Templates(t *testing.T) {
	s := newAutomationTestServer(t)
	rec := httptest.NewRecorder()
	s.handleAutomationTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/automation-templates", nil))
	var tpls []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tpls); err != nil {
		t.Fatal(err)
	}
	if len(tpls) < 6 {
		t.Fatalf("want >=6 templates, got %d", len(tpls))
	}
}

func TestAutomationAPI_SetupModeUnavailable(t *testing.T) {
	s := &Server{} // no automations store (setup mode)
	rec := httptest.NewRecorder()
	s.handleListAutomations(rec, httptest.NewRequest(http.MethodGet, "/api/automations", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 in setup mode, got %d", rec.Code)
	}
}
