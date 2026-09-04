package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cnjack/jcode/internal/automation"
	"github.com/cnjack/jcode/internal/session"
)

func newSessionDeleteAutomation(t *testing.T, owner string) (*Server, *automation.Automation) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	seedIndex(t, map[string][]session.SessionMeta{
		project: {{UUID: owner, Project: project, Title: "Owner"}},
	})
	store, err := automation.NewStoreDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.Create(automation.Automation{
		Name: "Bound", Prompt: "continue", ProjectPath: project, Enabled: true,
		ContextPolicy: automation.ContextConversation, OwnerSessionID: owner,
		Trigger: automation.Trigger{Type: automation.TriggerManual},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{automations: store, tasks: make(map[string]*Engine)}, a
}

func TestSessionDeleteImpactAndRequiredPolicy(t *testing.T) {
	s, a := newSessionDeleteAutomation(t, "owner-1")

	impactRec := httptest.NewRecorder()
	impactReq := httptest.NewRequest(http.MethodGet, "/api/sessions/owner-1/delete-impact", nil)
	impactReq.SetPathValue("id", "owner-1")
	s.handleSessionDeleteImpact(impactRec, impactReq)
	if impactRec.Code != http.StatusOK {
		t.Fatalf("impact status=%d body=%s", impactRec.Code, impactRec.Body.String())
	}
	var impact sessionDeleteImpact
	if err := json.Unmarshal(impactRec.Body.Bytes(), &impact); err != nil {
		t.Fatal(err)
	}
	if len(impact.Automations) != 1 || impact.Automations[0].ID != a.ID {
		t.Fatalf("impact=%+v", impact)
	}

	deleteRec := httptest.NewRecorder()
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/sessions/owner-1", nil)
	deleteReq.SetPathValue("id", "owner-1")
	s.handleDeleteSession(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusConflict || !json.Valid(deleteRec.Body.Bytes()) {
		t.Fatalf("delete without policy status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	if s.automations.Get(a.ID) == nil {
		t.Fatal("policy conflict deleted the related automation")
	}
	if meta, err := session.FindSessionMeta("owner-1"); err != nil || meta == nil {
		t.Fatalf("policy conflict deleted the session: meta=%+v err=%v", meta, err)
	}
}

func TestDeleteSessionDetachesRelatedAutomations(t *testing.T) {
	s, a := newSessionDeleteAutomation(t, "owner-detach")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/owner-detach?automation_policy=detach", nil)
	req.SetPathValue("id", "owner-detach")
	s.handleDeleteSession(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detach delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := s.automations.Get(a.ID)
	if got == nil || got.ContextPolicy != automation.ContextIsolated || got.OwnerSessionID != "" || !got.Enabled {
		t.Fatalf("detached automation=%+v", got)
	}
	if meta, err := session.FindSessionMeta("owner-detach"); err != nil || meta != nil {
		t.Fatalf("session still exists: meta=%+v err=%v", meta, err)
	}
}

func TestDeleteSessionCascadesRelatedAutomations(t *testing.T) {
	s, a := newSessionDeleteAutomation(t, "owner-delete")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/owner-delete?automation_policy=delete", nil)
	req.SetPathValue("id", "owner-delete")
	s.handleDeleteSession(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cascade delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if s.automations.Get(a.ID) != nil {
		t.Fatal("cascade delete kept the related automation")
	}
}

func TestDeleteSessionRejectsRunningRelatedAutomation(t *testing.T) {
	s, a := newSessionDeleteAutomation(t, "owner-running")
	if err := s.automations.UpdateState(a.ID, func(st *automation.RunState) {
		st.LastStatus = automation.StatusRunning
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/owner-running?automation_policy=detach", nil)
	req.SetPathValue("id", "owner-running")
	s.handleDeleteSession(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("running automation delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := s.automations.Get(a.ID); got == nil || got.ContextPolicy != automation.ContextConversation {
		t.Fatalf("running automation was changed: %+v", got)
	}
}
