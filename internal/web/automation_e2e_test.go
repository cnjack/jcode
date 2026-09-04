package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/automation"
	"github.com/cnjack/jcode/internal/session"
	managedworkspace "github.com/cnjack/jcode/internal/workspace"
)

// ---- once + cron trigger HTTP coverage ----

func TestAutomationAPI_CreateOnce(t *testing.T) {
	s := newAutomationTestServer(t)
	proj := t.TempDir()

	future := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	body := `{"name":"One-shot","prompt":"run smoke test","project_path":"` + proj +
		`","trigger":{"type":"once","at":"` + future + `"},"enabled":true}`
	rec := httptest.NewRecorder()
	s.handleCreateAutomation(rec, httptest.NewRequest(http.MethodPost, "/api/automations", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("create once: status %d body %s", rec.Code, rec.Body.String())
	}
	var item struct {
		HumanSchedule string `json:"human_schedule"`
		Badge         string `json:"badge"`
		Trigger       struct {
			Type string `json:"type"`
			At   string `json:"at"`
		} `json:"trigger"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.Badge != "Once" || !strings.HasPrefix(item.HumanSchedule, "Once at ") {
		t.Fatalf("unexpected item: %+v", item)
	}

	// Past pinned time is rejected at create time.
	past := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
	body = `{"name":"Old","prompt":"p","project_path":"` + proj +
		`","trigger":{"type":"once","at":"` + past + `"},"enabled":true}`
	rec = httptest.NewRecorder()
	s.handleCreateAutomation(rec, httptest.NewRequest(http.MethodPost, "/api/automations", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("past once: status %d body %s", rec.Code, rec.Body.String())
	}

	// Unparseable time is rejected by validation.
	body = `{"name":"Bad","prompt":"p","project_path":"` + proj +
		`","trigger":{"type":"once","at":"tomorrow"},"enabled":true}`
	rec = httptest.NewRecorder()
	s.handleCreateAutomation(rec, httptest.NewRequest(http.MethodPost, "/api/automations", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad once: status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestAutomationAPI_CreateCron(t *testing.T) {
	s := newAutomationTestServer(t)
	proj := t.TempDir()

	body := `{"name":"Weekdays","prompt":"triage","project_path":"` + proj +
		`","trigger":{"type":"schedule","cadence":"cron","expr":"0 9 * * 1-5"},"enabled":true}`
	rec := httptest.NewRecorder()
	s.handleCreateAutomation(rec, httptest.NewRequest(http.MethodPost, "/api/automations", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("create cron: status %d body %s", rec.Code, rec.Body.String())
	}
	var item struct {
		HumanSchedule string `json:"human_schedule"`
		Badge         string `json:"badge"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.Badge != "Cron" || item.HumanSchedule != `Cron "0 9 * * 1-5"` {
		t.Fatalf("unexpected item: %+v", item)
	}

	// Malformed and never-firing expressions are 400s.
	for _, expr := range []string{"* * * *", "0 0 31 2 *"} {
		body := `{"name":"Bad","prompt":"p","project_path":"` + proj +
			`","trigger":{"type":"schedule","cadence":"cron","expr":"` + expr + `"},"enabled":true}`
		rec := httptest.NewRecorder()
		s.handleCreateAutomation(rec, httptest.NewRequest(http.MethodPost, "/api/automations", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad cron %q: status %d", expr, rec.Code)
		}
	}
}

func TestAutomationAPI_ConversationOwnerMustExistAndMatchProject(t *testing.T) {
	project := t.TempDir()
	secondProject := t.TempDir()
	seedIndex(t, map[string][]session.SessionMeta{
		project: {
			{UUID: "owner-session", Project: project},
			{UUID: "automation-run", Project: project, AutomationID: "another-automation"},
		},
		secondProject: {{UUID: "second-owner", Project: secondProject}},
	})
	scratch, err := managedworkspace.CreateScratch(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	scratchRecorder, err := session.NewRecorder(scratch, "openai", "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	scratchRecorder.SetUUID("scratch-owner")
	scratchRecorder.SetWorkspaceKind(session.WorkspaceScratch)
	scratchRecorder.RecordUser("scratch owner")
	scratchRecorder.Close()
	s := newAutomationTestServer(t)
	post := func(owner, automationProject string) *httptest.ResponseRecorder {
		body := `{"name":"Bound","prompt":"p","project_path":"` + automationProject +
			`","context_policy":"conversation","owner_session_id":"` + owner +
			`","trigger":{"type":"manual"},"enabled":true}`
		rec := httptest.NewRecorder()
		s.handleCreateAutomation(rec, httptest.NewRequest(http.MethodPost, "/api/automations", strings.NewReader(body)))
		return rec
	}

	if rec := post("missing", project); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing owner status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := post("owner-session", t.TempDir()); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong project status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec := post("owner-session", project)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid owner status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	updateRec := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPut, "/api/automations/"+created.ID,
		strings.NewReader(`{"project_path":"`+t.TempDir()+`"}`))
	updateReq.SetPathValue("id", created.ID)
	s.handleUpdateAutomation(updateRec, updateReq)
	if updateRec.Code != http.StatusBadRequest {
		t.Fatalf("conversation project move status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}

	updateOwner := func(owner string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/automations/"+created.ID,
			strings.NewReader(`{"owner_session_id":"`+owner+`"}`))
		req.SetPathValue("id", created.ID)
		s.handleUpdateAutomation(rec, req)
		return rec
	}
	if ok, err := s.automations.TryMarkRunning(created.ID); err != nil || !ok {
		t.Fatalf("claim automation: ok=%v err=%v", ok, err)
	}
	if rec := updateOwner("second-owner"); rec.Code != http.StatusConflict {
		t.Fatalf("running owner switch status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := s.automations.UpdateState(created.ID, func(state *automation.RunState) {
		state.LastStatus = automation.StatusSuccess
	}); err != nil {
		t.Fatal(err)
	}

	if rec := updateOwner("second-owner"); rec.Code != http.StatusOK {
		t.Fatalf("owner switch status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := s.automations.Get(created.ID); got.OwnerSessionID != "second-owner" || got.ProjectPath != secondProject {
		t.Fatalf("owner switch did not derive project: %+v", got)
	}
	if rec := updateOwner("scratch-owner"); rec.Code != http.StatusOK {
		t.Fatalf("scratch owner switch status=%d body=%s", rec.Code, rec.Body.String())
	} else {
		var item struct {
			OwnerSessionID string                `json:"owner_session_id"`
			ProjectPath    string                `json:"project_path"`
			WorkspaceKind  session.WorkspaceKind `json:"workspace_kind"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
			t.Fatal(err)
		}
		if item.OwnerSessionID != "scratch-owner" || item.ProjectPath != scratch || item.WorkspaceKind != session.WorkspaceScratch {
			t.Fatalf("scratch owner response=%+v", item)
		}
	}
	if rec := updateOwner("missing"); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing owner switch status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := updateOwner("automation-run"); rec.Code != http.StatusBadRequest {
		t.Fatalf("automation-run owner switch status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := s.automations.Get(created.ID); got.OwnerSessionID != "scratch-owner" || got.ProjectPath != scratch {
		t.Fatalf("failed owner switch changed definition: %+v", got)
	}
}

func TestAutomationAPI_NoProjectWorkspaceIsExposedAndLocked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	scratch, err := managedworkspace.CreateScratch(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := newAutomationTestServer(t)
	body := `{"name":"Scratch","prompt":"p","project_path":"` + scratch +
		`","trigger":{"type":"manual"},"enabled":true}`
	rec := httptest.NewRecorder()
	s.handleCreateAutomation(rec, httptest.NewRequest(http.MethodPost, "/api/automations", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("create scratch automation status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID            string                `json:"id"`
		WorkspaceKind session.WorkspaceKind `json:"workspace_kind"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.WorkspaceKind != session.WorkspaceScratch {
		t.Fatalf("workspace_kind=%q, want %q", created.WorkspaceKind, session.WorkspaceScratch)
	}

	updateRec := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPut, "/api/automations/"+created.ID,
		strings.NewReader(`{"project_path":"`+t.TempDir()+`"}`))
	updateReq.SetPathValue("id", created.ID)
	s.handleUpdateAutomation(updateRec, updateReq)
	if updateRec.Code != http.StatusBadRequest {
		t.Fatalf("scratch project move status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}
	if !strings.Contains(updateRec.Body.String(), "no-project automation cannot move") {
		t.Fatalf("scratch project move body=%s", updateRec.Body.String())
	}
}

// ---- integration e2e: HTTP create → store → scheduler Run → runner → disarm ----
//
// The scheduler is driven through its real entry point (Run) so the test
// exercises the same election + tick path production uses.

type recordingRunner struct {
	mu     sync.Mutex
	starts []string // "automationID:kind" per StartRun
}

func (r *recordingRunner) StartRun(_ context.Context, a *automation.Automation, kind string) (string, error) {
	recorder, err := session.NewRecorder(a.ProjectPath, "test", "test")
	if err != nil {
		return "", err
	}
	recorder.RecordUser(a.Prompt)
	defer recorder.Close()
	sessionID := recorder.UUID()
	meta, err := session.UpdateSessionMeta(sessionID, func(m *session.SessionMeta) {
		m.AutomationID = a.ID
		m.TriggerKind = kind
		m.TerminalStatus = automation.StatusSuccess
		m.EndTime = time.Now().Format(time.RFC3339)
		m.UpdatedAt = m.EndTime
	})
	if err != nil {
		return "", err
	}
	if meta == nil {
		return "", fmt.Errorf("record automation session %q: session index entry missing", sessionID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, a.ID+":"+kind)
	return sessionID, nil
}

func (r *recordingRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.starts)
}

func waitForE2E(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func TestAutomationE2E_HTTPCreateToScheduledFire(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newAutomationTestServer(t)
	proj := t.TempDir()
	runner := &recordingRunner{}

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleCreateAutomation(rec, httptest.NewRequest(http.MethodPost, "/api/automations", strings.NewReader(body)))
		return rec
	}

	// 1. Create a once automation via the REST API, enabled.
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	rec := post(`{"name":"e2e once","prompt":"say hi","project_path":"` + proj +
		`","trigger":{"type":"once","at":"` + future + `"},"enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	// 2. Create a cron automation via the REST API, enabled.
	rec = post(`{"name":"e2e cron","prompt":"say cron","project_path":"` + proj +
		`","trigger":{"type":"schedule","cadence":"cron","expr":"*/15 * * * *"},"enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create cron: %d %s", rec.Code, rec.Body.String())
	}
	var cronCreated struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cronCreated); err != nil {
		t.Fatal(err)
	}

	// 3. First scheduler pass (real Run) seeds state, nothing fires.
	runOnce := func(while func() bool) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() {
			sched := automation.NewScheduler(s.automations, runner)
			sched.Run(ctx)
			close(done)
		}()
		waitForE2E(t, while)
		cancel()
		<-done
	}
	runOnce(func() bool {
		st := s.automations.State(created.ID)
		st2 := s.automations.State(cronCreated.ID)
		return st.NextRunAt != "" && st2.NextRunAt != ""
	})
	if runner.count() != 0 {
		t.Fatal("seed pass must not fire")
	}

	// 4. Age both triggers to due; the next pass fires both exactly once.
	due := time.Now().Add(-time.Minute)
	for _, id := range []string{created.ID, cronCreated.ID} {
		if err := s.automations.UpdateState(id, func(rs *automation.RunState) {
			rs.NextRunAt = due.Format(time.RFC3339)
			rs.LastFiredSlot = ""
		}); err != nil {
			t.Fatal(err)
		}
	}
	runOnce(func() bool { return runner.count() >= 2 })
	if runner.count() != 2 {
		t.Fatalf("want 2 scheduled fires, got %d", runner.count())
	}

	// 5. The once automation is auto-disarmed after its scheduled fire.
	got := s.automations.Get(created.ID)
	if got.Enabled {
		t.Fatal("once automation must be disarmed after firing")
	}
	waitForE2E(t, func() bool {
		return s.automations.State(created.ID).LastStatus == automation.StatusSuccess
	})

	// 6. A cron automation stays armed and its NextRunAt advanced to a valid
	// future */15 slot. (The exact slot depends on the scheduler's tick instant
	// — if the tick crosses a slot boundary after `due` was computed, the next
	// slot legitimately shifts by one interval — so assert the invariant, not
	// a timestamp derived from a stale baseline.)
	cronItem := s.automations.Get(cronCreated.ID)
	if !cronItem.Enabled {
		t.Fatal("cron automation must stay armed")
	}
	gotAt := s.automations.State(cronCreated.ID).NextRunAt
	nextAt, err := time.Parse(time.RFC3339, gotAt)
	if err != nil {
		t.Fatalf("cron next_run_at unparsable: %q", gotAt)
	}
	if !nextAt.After(due) {
		t.Fatalf("cron next_run_at %v must be after the fired slot %v", nextAt, due)
	}
	switch nextAt.Minute() {
	case 0, 15, 30, 45:
	default:
		t.Fatalf("cron next_run_at %v is not a */15 slot", nextAt)
	}

	// 7. Verify both runs are exposed through the runs API.
	rec = httptest.NewRecorder()
	s.handleListAutomationRuns(rec, httptest.NewRequest(http.MethodGet, "/api/automations/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("runs list: %d", rec.Code)
	}
	var runs []automationRun
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) < 2 {
		t.Fatalf("runs API returned %d runs, want >= 2", len(runs))
	}
}
