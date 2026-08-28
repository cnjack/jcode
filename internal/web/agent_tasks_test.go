package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/tasks"
	"github.com/cnjack/jcode/internal/tools"
)

func newAgentTaskTestServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return &Server{
		Engine: &Engine{pwd: t.TempDir(), taskID: "web-test-task"},
		tasks:  map[string]*Engine{},
	}
}

func agentTaskRequest(t *testing.T, method, target string, body any) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	rec := httptest.NewRecorder()
	return rec, req
}

func getAgentTask(t *testing.T, s *Server, ref string) *httptest.ResponseRecorder {
	t.Helper()
	rec, req := agentTaskRequest(t, http.MethodGet, "/api/agent-tasks/"+ref, nil)
	req.SetPathValue("ref", ref) // direct handler calls bypass the mux
	s.handleGetAgentTask(rec, req)
	return rec
}

func TestAgentTasksCRUDFlow(t *testing.T) {
	s := newAgentTaskTestServer(t)

	// Create.
	rec, req := agentTaskRequest(t, http.MethodPost, "/api/agent-tasks",
		map[string]string{"name": "web-followup", "description": "from web"})
	s.handleCreateAgentTask(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	var created tasks.Record
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !tasks.ValidateRef(created.Ref) || created.Status != tasks.StatusCreated {
		t.Fatalf("created record wrong: %+v", created)
	}

	// Read (by name resolution).
	rec = getAgentTask(t, s, "web-followup")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), created.Ref) {
		t.Fatalf("read code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Message (exactly-once with key).
	for i := 0; i < 3; i++ {
		rec, req = agentTaskRequest(t, http.MethodPost, "/api/agent-tasks/"+created.Ref+"/messages",
			map[string]string{"message": "check again", "idempotency_key": "web-k1"})
		req.SetPathValue("ref", created.Ref)
		s.handleMessageAgentTask(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("message %d code=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	rec = getAgentTask(t, s, created.Ref)
	if strings.Count(rec.Body.String(), "check again") != 1 {
		t.Fatalf("message not exactly-once: %s", rec.Body.String())
	}

	// List contains it.
	rec, req = agentTaskRequest(t, http.MethodGet, "/api/agent-tasks", nil)
	s.handleListAgentTasks(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "web-followup") {
		t.Fatalf("list code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentTaskErrorsMapped(t *testing.T) {
	s := newAgentTaskTestServer(t)

	// Unknown ref → 404 with guidance.
	rec := getAgentTask(t, s, "task_0123456789abcdef")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found code=%d", rec.Code)
	}

	// Cross-project ref → 403.
	root := t.TempDir()
	other, err := tasks.NewStore(root, "/proj/other")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := other.Create(tasks.CreateInput{Name: "foreign"})
	if err != nil {
		t.Fatal(err)
	}
	mine, err := tasks.NewStore(root, "/proj/mine")
	if err != nil {
		t.Fatal(err)
	}
	s.agentTasks = mine
	rec = getAgentTask(t, s, foreign.Ref)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-project code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Terminal task message → 409.
	rec, req := agentTaskRequest(t, http.MethodPost, "/api/agent-tasks",
		map[string]string{"name": "finished"})
	s.handleCreateAgentTask(rec, req)
	var done tasks.Record
	_ = json.Unmarshal(rec.Body.Bytes(), &done)
	_ = mine.SetStatus(done.Ref, tasks.StatusCompleted, "ok", "")
	rec, req = agentTaskRequest(t, http.MethodPost, "/api/agent-tasks/"+done.Ref+"/messages",
		map[string]string{"message": "hi"})
	req.SetPathValue("ref", done.Ref)
	s.handleMessageAgentTask(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("terminal code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentTaskStopLiveAndRemote(t *testing.T) {
	s := newAgentTaskTestServer(t)
	store, err := tasks.OpenDefault(s.pwd)
	if err != nil {
		t.Fatal(err)
	}

	// Live task in an engine's manager: stop succeeds through the engine.
	block := make(chan struct{})
	live, err := store.Create(tasks.CreateInput{Name: "live", Kind: tasks.KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	_ = store.SetStatus(live.Ref, tasks.StatusRunning, "", "")
	mgr := tools.NewSubagentTaskManager(2, 10)
	_, _, err = mgr.Submit(context.Background(), &tools.SubagentTask{
		Name: "live", Ref: live.Ref,
		OnFinish: func(status tools.SubagentTaskStatus, output, errMsg string) {
			_ = store.SetStatus(live.Ref, tasks.Status(status), output, errMsg)
		},
	}, func(ctx context.Context) (string, error) {
		<-block
		return "", ctx.Err()
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	s.taskHub = tools.NewTaskHub(nil, mgr, nil)

	rec, req := agentTaskRequest(t, http.MethodPost, "/api/agent-tasks/"+live.Ref+"/stop", nil)
	req.SetPathValue("ref", live.Ref)
	s.handleStopAgentTask(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "stopped") {
		t.Fatalf("live stop code=%d body=%s", rec.Code, rec.Body.String())
	}
	close(block)

	// Running record owned by nobody here → explicit conflict error.
	remote, err := store.Create(tasks.CreateInput{Name: "remote-owner", OwnerPID: 1, Hostname: "some-host"})
	if err != nil {
		t.Fatal(err)
	}
	_ = store.SetStatus(remote.Ref, tasks.StatusRunning, "", "")
	rec, req = agentTaskRequest(t, http.MethodPost, "/api/agent-tasks/"+remote.Ref+"/stop", nil)
	req.SetPathValue("ref", remote.Ref)
	s.handleStopAgentTask(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "another session") {
		t.Fatalf("remote stop code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentTaskArchive(t *testing.T) {
	s := newAgentTaskTestServer(t)
	rec, req := agentTaskRequest(t, http.MethodPost, "/api/agent-tasks",
		map[string]string{"name": "to-archive"})
	s.handleCreateAgentTask(rec, req)
	var created tasks.Record
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec, req = agentTaskRequest(t, http.MethodPost, "/api/agent-tasks/"+created.Ref+"/archive", nil)
	req.SetPathValue("ref", created.Ref)
	s.handleArchiveAgentTask(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Messages to archived task → 409.
	rec, req = agentTaskRequest(t, http.MethodPost, "/api/agent-tasks/"+created.Ref+"/messages",
		map[string]string{"message": "hi"})
	req.SetPathValue("ref", created.Ref)
	s.handleMessageAgentTask(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("archived message code=%d", rec.Code)
	}
}
