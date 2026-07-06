package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
