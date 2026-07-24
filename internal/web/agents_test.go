package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
)

func writeAgentRole(t *testing.T, project, name, description, model string) {
	t.Helper()
	dir := filepath.Join(project, ".jcode", "agents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	modelLine := ""
	if model != "" {
		modelLine = "model: " + model + "\n"
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n" +
		modelLine + "---\n\nInstructions for " + name + ".\n"
	if err := os.WriteFile(filepath.Join(dir, name+".agent.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListAgentsSortedAndReportsCurrent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	writeAgentRole(t, project, "reviewer", "Review changes", "other/special")
	writeAgentRole(t, project, "builder", "Build changes", "")
	s := &Server{Engine: &Engine{pwd: project, agentRole: "reviewer"}}

	rec := httptest.NewRecorder()
	s.handleListAgents(rec, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	var got struct {
		Agents  []agentRoleView `json:"agents"`
		Current string          `json:"current"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Current != "reviewer" {
		t.Fatalf("current=%q, want reviewer", got.Current)
	}
	if len(got.Agents) != 2 || got.Agents[0].Name != "builder" || got.Agents[1].Name != "reviewer" {
		t.Fatalf("agents=%+v, want builder then reviewer", got.Agents)
	}
}

func TestSwitchAgentAndRestoreDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	writeAgentRole(t, project, "reviewer", "Review changes", "")
	var rebuilt []string
	eng := &Engine{
		pwd:          project,
		providerName: "test",
		modelName:    "model",
		rebuildForRole: func(name, provider, model string) (*AgentRoleBuild, error) {
			rebuilt = append(rebuilt, name)
			if name == "reviewer" {
				provider, model = "other", "special"
			}
			return &AgentRoleBuild{
				Agent: &adk.ChatModelAgent{}, Provider: provider, Model: model,
			}, nil
		},
	}
	s := &Server{Engine: eng, wsBroker: NewWSBroker()}

	rec := httptest.NewRecorder()
	s.handleSwitchAgent(rec, httptest.NewRequest(http.MethodPost, "/api/agent", strings.NewReader(`{"agent":"reviewer"}`)))
	if rec.Code != http.StatusOK || eng.curAgentRole() != "reviewer" {
		t.Fatalf("select: code=%d role=%q body=%q", rec.Code, eng.curAgentRole(), rec.Body.String())
	}
	if provider, model, _ := eng.modelSnapshot(); provider != "other" || model != "special" {
		t.Fatalf("selected model=%s/%s, want other/special", provider, model)
	}
	rec = httptest.NewRecorder()
	s.handleSwitchAgent(rec, httptest.NewRequest(http.MethodPost, "/api/agent", strings.NewReader(`{"agent":""}`)))
	if rec.Code != http.StatusOK || eng.curAgentRole() != "" {
		t.Fatalf("default: code=%d role=%q body=%q", rec.Code, eng.curAgentRole(), rec.Body.String())
	}
	if len(rebuilt) != 2 || rebuilt[0] != "reviewer" || rebuilt[1] != "" {
		t.Fatalf("rebuilt=%v", rebuilt)
	}
}

func TestSwitchAgentRejectsUnknown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{Engine: &Engine{pwd: t.TempDir()}, wsBroker: NewWSBroker()}
	rec := httptest.NewRecorder()
	s.handleSwitchAgent(rec, httptest.NewRequest(http.MethodPost, "/api/agent", strings.NewReader(`{"agent":"missing"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}
