package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

func TestHealthReportsActiveBootstrapWithoutImplicitRestore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	recorder, err := session.NewRecorder(project, "test-provider", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()

	eng := &Engine{taskID: recorder.UUID(), pwd: project, recorder: recorder}
	srv := &Server{Engine: eng}

	fresh := readHealthPayload(t, srv)
	if got := fresh["session_id"]; got != recorder.UUID() {
		t.Fatalf("fresh session_id = %v, want active %s", got, recorder.UUID())
	}
	if got := fresh["fresh_session"]; got != true {
		t.Fatalf("fresh_session = %v, want true", got)
	}

	recorder.RecordUser("materialize this conversation")
	session.SaveLastSession(project, recorder.UUID())
	durable := readHealthPayload(t, srv)
	if got := durable["session_id"]; got != recorder.UUID() {
		t.Fatalf("durable session_id = %v, want active %s", got, recorder.UUID())
	}
	if got := durable["fresh_session"]; got != false {
		t.Fatalf("fresh_session after RecordUser = %v, want false", got)
	}
	if got := durable["recent_project"]; got != project {
		t.Fatalf("recent_project = %v, want %s", got, project)
	}
	if got := durable["recent_workspace_kind"]; got != string(session.WorkspaceProject) {
		t.Fatalf("recent_workspace_kind = %v, want project", got)
	}
}

func readHealthPayload(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	srv.handleHealth(recorder, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("health status = %d, body=%q", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	return payload
}
