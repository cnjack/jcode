package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

// seedIndex points HOME at a temp dir and writes a sessions index with the
// given project→metas map, so the cross-project task handlers can be tested
// in-process without touching the real ~/.jcode.
func seedIndex(t *testing.T, sessions map[string][]session.SessionMeta) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jcode", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"sessions": sessions})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// P0-1: GET /api/workspace on a non-git directory returns empty branch + not dirty.
func TestWorkspaceNonGit(t *testing.T) {
	s := &Server{ctx: context.Background(), pwd: t.TempDir()}
	rec := httptest.NewRecorder()
	s.handleWorkspace(rec, httptest.NewRequest(http.MethodGet, "/api/workspace", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	var ws struct {
		Branch string `json:"branch"`
		Dirty  bool   `json:"dirty"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ws); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if ws.Branch != "" || ws.Dirty {
		t.Fatalf("non-git dir: want empty branch + not dirty, got %+v", ws)
	}
}

// P0-2: GET /api/tasks with no index returns an empty array (not null).
func TestListAllTasksEmpty(t *testing.T) {
	seedIndex(t, map[string][]session.SessionMeta{})
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleListAllTasks(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("want [], got %q", got)
	}
}

// P0-2: GET /api/tasks returns sessions across ALL projects, each tagged with
// its project path.
func TestListAllTasksMultiProject(t *testing.T) {
	seedIndex(t, map[string][]session.SessionMeta{
		"/work/tpm":   {{UUID: "u-a", Project: "/work/tpm", Title: "task A", Model: "glm-5.2", StartTime: "2026-06-16T10:00:00Z"}},
		"/work/jcode": {{UUID: "u-b", Project: "/work/jcode", Title: "task B"}, {UUID: "u-c", Project: "/work/jcode", Pinned: true}},
	})
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleListAllTasks(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	var items []struct {
		UUID    string `json:"uuid"`
		Project string `json:"project"`
		Title   string `json:"title"`
		Pinned  bool   `json:"pinned"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("want 3 tasks across projects, got %d: %+v", len(items), items)
	}
	byID := map[string]struct {
		project string
		pinned  bool
	}{}
	for _, it := range items {
		byID[it.UUID] = struct {
			project string
			pinned  bool
		}{it.Project, it.Pinned}
	}
	if byID["u-a"].project != "/work/tpm" {
		t.Fatalf("u-a project = %q", byID["u-a"].project)
	}
	if byID["u-c"].project != "/work/jcode" || !byID["u-c"].pinned {
		t.Fatalf("u-c should be pinned in /work/jcode, got %+v", byID["u-c"])
	}
}

// P0-3: PATCH /api/tasks/{id} updates pin/archive/unread/title and persists.
func TestUpdateTaskMeta(t *testing.T) {
	seedIndex(t, map[string][]session.SessionMeta{
		"/work/tpm": {{UUID: "u-a", Project: "/work/tpm", Title: "orig"}},
	})
	s := &Server{}

	patch := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/tasks/u-a", strings.NewReader(body))
		req.SetPathValue("id", "u-a")
		s.handleUpdateTask(rec, req)
		return rec
	}

	// pin
	if rec := patch(`{"pinned":true}`); rec.Code != http.StatusOK {
		t.Fatalf("pin: code=%d body=%q", rec.Code, rec.Body.String())
	}
	// archive + unread + rename
	if rec := patch(`{"archived":true,"unread":true,"title":"renamed"}`); rec.Code != http.StatusOK {
		t.Fatalf("multi: code=%d", rec.Code)
	}

	// Verify persisted via a fresh list.
	all, err := session.ListAllSessions()
	if err != nil {
		t.Fatal(err)
	}
	m := all["/work/tpm"][0]
	if !m.Pinned || !m.Archived || !m.Unread || m.Title != "renamed" {
		t.Fatalf("metadata not persisted: %+v", m)
	}

	// Unknown id -> 404.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/tasks/missing", strings.NewReader(`{"pinned":true}`))
	req.SetPathValue("id", "missing")
	s.handleUpdateTask(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id should be 404, got %d", rec.Code)
	}
}
