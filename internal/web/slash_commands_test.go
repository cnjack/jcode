package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cnjack/jcode/internal/flow"
)

// TestHandleSlashCommands_IncludesFlows verifies the slash-command endpoint
// advertises workflow slashes (marked type:"flow") alongside skills, so the web
// autocomplete can surface and badge them.
func TestHandleSlashCommands_IncludesFlows(t *testing.T) {
	s := &Server{flowLoader: flow.NewLoader()} // builtins only

	rec := httptest.NewRecorder()
	s.handleSlashCommands(rec, httptest.NewRequest(http.MethodGet, "/api/slash-commands", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}

	var items []struct {
		Slash string `json:"slash"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}

	// The three builtin workflows must appear, each tagged as a flow.
	want := map[string]bool{"/repo-audit": false, "/pr-review": false, "/roundtable": false}
	for _, it := range items {
		if _, ok := want[it.Slash]; ok {
			if it.Type != "flow" {
				t.Errorf("%s advertised as type %q, want \"flow\"", it.Slash, it.Type)
			}
			want[it.Slash] = true
		}
	}
	for slash, seen := range want {
		if !seen {
			t.Errorf("builtin workflow slash %s not advertised", slash)
		}
	}
}

// TestHandleSlashCommands_TaskScoped verifies the endpoint resolves workflows
// from the requested task's project loader, so another tab's foreground task
// cannot replace its .jcode/workflows catalog.
func TestHandleSlashCommands_TaskScoped(t *testing.T) {
	// A project dir with one project-only workflow.
	proj := t.TempDir()
	wfDir := filepath.Join(proj, ".jcode", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `export const meta = { name: "task-only-wf", description: "scoped" };
return "ok";`
	if err := os.WriteFile(filepath.Join(wfDir, "task-only-wf.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	taskLoader := flow.NewLoader()
	taskLoader.LoadProject(proj)

	// Boot/active loaders (builtins only) must NOT know the project workflow; the
	// explicitly requested task's loader must.
	active := &Engine{taskID: "task-a", flowLoader: flow.NewLoader()}
	target := &Engine{taskID: "task-b", flowLoader: taskLoader}
	s := &Server{
		flowLoader: flow.NewLoader(),
		Engine:     active,
		tasks:      map[string]*Engine{"task-a": active, "task-b": target},
	}

	rec := httptest.NewRecorder()
	s.handleSlashCommands(rec, httptest.NewRequest(http.MethodGet, "/api/slash-commands?task_id=task-b", nil))

	var items []struct {
		Slash string `json:"slash"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	found := false
	for _, it := range items {
		if it.Slash == "/task-only-wf" {
			found = true
			if it.Type != "flow" {
				t.Errorf("task workflow type=%q, want \"flow\"", it.Type)
			}
		}
	}
	if !found {
		t.Error("requested task's project workflow /task-only-wf not advertised")
	}

	unknown := httptest.NewRecorder()
	s.handleSlashCommands(unknown, httptest.NewRequest(http.MethodGet, "/api/slash-commands?task_id=missing", nil))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown task code=%d body=%s", unknown.Code, unknown.Body.String())
	}
}

// TestSlashRunPrompt shapes the agent instruction a "/<workflow>" slash expands
// to: run by name via workflow_run, with trailing input handed over as args.
func TestSlashRunPrompt(t *testing.T) {
	p := flow.SlashRunPrompt("repo-audit", "internal/auth")
	if !contains(p, "workflow_run") || !contains(p, "repo-audit") {
		t.Errorf("prompt missing tool/name: %q", p)
	}
	if !contains(p, "internal/auth") {
		t.Errorf("prompt dropped user args: %q", p)
	}

	bare := flow.SlashRunPrompt("roundtable", "")
	if contains(bare, "args` from this input") {
		t.Errorf("empty input should not add an args line: %q", bare)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
