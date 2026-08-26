package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/runner"
)

func newTaskScopedControlServer(t *testing.T) (*Server, *Engine, *Engine) {
	t.Helper()
	newControlEngine := func(id, pwd, provider, modelName string) *Engine {
		return &Engine{
			taskID: id, pwd: pwd, providerName: provider, modelName: modelName,
			mode: "approval", handler: handler.NewWebHandler(),
			approvalState: runner.NewApprovalStateWithMode(pwd, mode.Approval),
			createAgent: func(string, string) (*adk.ChatModelAgent, error) {
				return &adk.ChatModelAgent{}, nil
			},
			rebuildForRole: func(name, provider, modelName string) (*AgentRoleBuild, error) {
				return &AgentRoleBuild{Agent: &adk.ChatModelAgent{}, Provider: provider, Model: modelName}, nil
			},
		}
	}
	a := newControlEngine("task-a", t.TempDir(), "provider-a", "model-a")
	b := newControlEngine("task-b", t.TempDir(), "provider-b", "model-b")
	writeAgentRole(t, b.pwd, "reviewer", "Review task B", "")
	s := &Server{
		Engine:   a,
		tasks:    map[string]*Engine{a.taskID: a, b.taskID: b},
		wsBroker: NewWSBroker(),
	}
	return s, a, b
}

func TestModelAndAgentControlsResolveExplicitTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, a, b := newTaskScopedControlServer(t)

	modelsRec := httptest.NewRecorder()
	s.handleListModels(modelsRec, httptest.NewRequest(http.MethodGet, "/api/models?task_id=task-b", nil))
	if modelsRec.Code != http.StatusOK {
		t.Fatalf("models code=%d body=%s", modelsRec.Code, modelsRec.Body.String())
	}
	var modelsBody struct {
		Current map[string]string `json:"current"`
	}
	if err := json.Unmarshal(modelsRec.Body.Bytes(), &modelsBody); err != nil {
		t.Fatal(err)
	}
	if modelsBody.Current["provider"] != "provider-b" || modelsBody.Current["model"] != "model-b" {
		t.Fatalf("task-b current model=%v", modelsBody.Current)
	}

	switchModelRec := httptest.NewRecorder()
	s.handleSwitchModel(switchModelRec, httptest.NewRequest(
		http.MethodPost,
		"/api/model",
		strings.NewReader(`{"provider":"provider-next","model":"model-next","task_id":"task-b"}`),
	))
	if switchModelRec.Code != http.StatusOK {
		t.Fatalf("switch model code=%d body=%s", switchModelRec.Code, switchModelRec.Body.String())
	}
	if provider, modelName, _ := b.modelSnapshot(); provider != "provider-next" || modelName != "model-next" {
		t.Fatalf("task-b model=%s/%s", provider, modelName)
	}
	if provider, modelName, _ := a.modelSnapshot(); provider != "provider-a" || modelName != "model-a" {
		t.Fatalf("task-a model was changed=%s/%s", provider, modelName)
	}

	agentsRec := httptest.NewRecorder()
	s.handleListAgents(agentsRec, httptest.NewRequest(http.MethodGet, "/api/agents?task_id=task-b", nil))
	if agentsRec.Code != http.StatusOK || !strings.Contains(agentsRec.Body.String(), "reviewer") {
		t.Fatalf("agents code=%d body=%s", agentsRec.Code, agentsRec.Body.String())
	}

	switchAgentRec := httptest.NewRecorder()
	s.handleSwitchAgent(switchAgentRec, httptest.NewRequest(
		http.MethodPost,
		"/api/agent",
		strings.NewReader(`{"agent":"reviewer","task_id":"task-b"}`),
	))
	if switchAgentRec.Code != http.StatusOK || b.curAgentRole() != "reviewer" || a.curAgentRole() != "" {
		t.Fatalf("agent switch leaked: code=%d a=%q b=%q body=%s", switchAgentRec.Code, a.curAgentRole(), b.curAgentRole(), switchAgentRec.Body.String())
	}
}

func TestApprovalModeResolvesExplicitTaskAndUnknownTasksFailClosed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, a, b := newTaskScopedControlServer(t)
	a.approvalState.SetSessionMode(mode.FullAccess)
	b.approvalState.SetSessionMode(mode.FullAccess)

	getRec := httptest.NewRecorder()
	s.handleGetApprovalMode(getRec, httptest.NewRequest(http.MethodGet, "/api/approval/mode?task_id=task-b", nil))
	if getRec.Code != http.StatusOK || !strings.Contains(getRec.Body.String(), `"auto_approve":true`) {
		t.Fatalf("approval mode code=%d body=%s", getRec.Code, getRec.Body.String())
	}

	setRec := httptest.NewRecorder()
	s.handleSetApprovalMode(setRec, httptest.NewRequest(
		http.MethodPost,
		"/api/approval/mode",
		strings.NewReader(`{"auto_approve":false,"task_id":"task-b"}`),
	))
	if setRec.Code != http.StatusOK {
		t.Fatalf("set approval mode code=%d body=%s", setRec.Code, setRec.Body.String())
	}
	if b.approvalState.GetMode() != handler.ModeManual || a.approvalState.GetMode() != handler.ModeAuto {
		t.Fatalf("unexpected approval modes: a=%v b=%v", a.approvalState.GetMode(), b.approvalState.GetMode())
	}

	for _, request := range []struct {
		name   string
		handle func(http.ResponseWriter, *http.Request)
		req    *http.Request
	}{
		{"models", s.handleListModels, httptest.NewRequest(http.MethodGet, "/api/models?task_id=missing", nil)},
		{"agents", s.handleListAgents, httptest.NewRequest(http.MethodGet, "/api/agents?task_id=missing", nil)},
		{"approval", s.handleGetApprovalMode, httptest.NewRequest(http.MethodGet, "/api/approval/mode?task_id=missing", nil)},
	} {
		rec := httptest.NewRecorder()
		request.handle(rec, request.req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s unknown task code=%d body=%s", request.name, rec.Code, rec.Body.String())
		}
	}
}
