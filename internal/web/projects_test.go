package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

// seedProjects writes a projects.json (per-project last-activity timestamps)
// next to the session index seeded by seedIndex, so handleListProjects can be
// tested in-process without touching the real ~/.jcode.
func seedProjects(t *testing.T, projects map[string]session.ProjectMeta) {
	t.Helper()
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("seedProjects must run after seedIndex (HOME unset)")
	}
	dir := filepath.Join(home, ".jcode", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(projects)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "projects.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// GET /api/projects with no projects file (legacy install) returns an empty
// array, not null — the sidebar iterates the response directly.
func TestListProjectsEmpty(t *testing.T) {
	seedIndex(t, map[string][]session.SessionMeta{})
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleListProjects(rec, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("want [], got %q", got)
	}
}

// GET /api/projects echoes the persisted per-project timestamps.
func TestListProjectsReturnsTimestamps(t *testing.T) {
	seedIndex(t, map[string][]session.SessionMeta{})
	seedProjects(t, map[string]session.ProjectMeta{
		"/proj/a": {UpdatedAt: "2026-07-19T18:00:00+08:00"},
		"/proj/b": {UpdatedAt: "2026-07-16T03:00:00Z"},
	})
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleListProjects(rec, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	var items []projectItem
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 projects, got %+v", items)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	if items[0].Path != "/proj/a" || items[0].UpdatedAt != "2026-07-19T18:00:00+08:00" {
		t.Fatalf("unexpected /proj/a item: %+v", items[0])
	}
	if items[1].Path != "/proj/b" || items[1].UpdatedAt != "2026-07-16T03:00:00Z" {
		t.Fatalf("unexpected /proj/b item: %+v", items[1])
	}
}
