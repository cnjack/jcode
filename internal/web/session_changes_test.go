package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

func changeFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}
func TestSessionChangesRequestedWorkspaceAndReadOnly(t *testing.T) {
	task := initGitWorkspace(t, "task-branch")
	active := initGitWorkspace(t, "active-branch")
	seedIndex(t, map[string][]session.SessionMeta{task: {{UUID: "task", Project: task}}})
	for name, body := range map[string]string{"staged.txt": "before\n", "unstaged.txt": "before\n"} {
		if err := os.WriteFile(filepath.Join(task, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	changeFixtureGit(t, task, "add", ".")
	changeFixtureGit(t, task, "commit", "-m", "fixture")
	for name, body := range map[string]string{"staged.txt": "after\n", "unstaged.txt": "changed\n", "new file.txt": "new\n"} {
		if err := os.WriteFile(filepath.Join(task, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	changeFixtureGit(t, task, "add", "staged.txt")
	changeFixtureGit(t, task, "remote", "add", "origin", "https://user:private-token@example.invalid/owner/repo.git?token=hidden")
	before := changeFixtureGit(t, task, "status", "--porcelain=v1")
	s := &Server{Engine: &Engine{pwd: active, taskID: "active"}}
	req := httptest.NewRequest("GET", "/api/sessions/task/changes", nil)
	req.SetPathValue("id", "task")
	rec := httptest.NewRecorder()
	s.handleSessionChanges(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var got sessionChanges
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "task" || got.Workspace != task || got.Branch != "task-branch" || len(got.Files) != 3 || got.Revision == "" {
		t.Fatalf("unexpected changes: %+v", got)
	}
	if strings.Contains(rec.Body.String(), "private-token") || strings.Contains(rec.Body.String(), "hidden") {
		t.Fatal("remote credentials exposed")
	}
	if after := changeFixtureGit(t, task, "status", "--porcelain=v1"); after != before {
		t.Fatal("read changed index or workspace")
	}
	for _, f := range got.Files {
		if f.Additions != 1 || f.Patch == "" {
			t.Fatalf("missing patch: %+v", f)
		}
	}
}
func TestSessionChangesMissingDoesNotUseActiveWorkspace(t *testing.T) {
	seedIndex(t, map[string][]session.SessionMeta{})
	s := &Server{Engine: &Engine{pwd: initGitWorkspace(t, "active"), taskID: "active"}}
	req := httptest.NewRequest("GET", "/api/sessions/missing/changes", nil)
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()
	s.handleSessionChanges(rec, req)
	if rec.Code != 404 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
func TestSessionChangesLimitAndBinary(t *testing.T) {
	dir := initGitWorkspace(t, "task")
	seedIndex(t, map[string][]session.SessionMeta{dir: {{UUID: "task", Project: dir}}})
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), []byte(strings.Repeat("a", changesLimit+100)), 0600); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/sessions/task/changes", nil)
	req.SetPathValue("id", "task")
	rec := httptest.NewRecorder()
	(&Server{}).handleSessionChanges(rec, req)
	var got sessionChanges
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || !got.Truncated || got.Revision != "" {
		t.Fatalf("limit not visible: %d %+v", rec.Code, got)
	}
}
