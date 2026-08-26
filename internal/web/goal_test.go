package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/tools"
)

// newGoalTestServer builds a minimal Server exercising only the goal handlers
// (no network listener, no agent), so the HTTP layer can be tested in-process.
func newGoalTestServer() *Server {
	activeEnv := tools.NewEnv("/tmp/active", "darwin/arm64")
	active := &Engine{
		taskID: "active-task", env: activeEnv, todoStore: activeEnv.TodoStore,
		handler: handler.NewWebHandler(),
	}
	return &Server{Engine: active, tasks: map[string]*Engine{active.taskID: active}}
}

func TestWebGoalLifecycle(t *testing.T) {
	s := newGoalTestServer()

	// 1. GET with no goal -> null.
	rec := httptest.NewRecorder()
	s.handleGetGoal(rec, httptest.NewRequest(http.MethodGet, "/api/goal", nil))
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "null" {
		t.Fatalf("GET empty goal: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 2. POST sets the goal (start:false avoids needing an agent).
	rec = httptest.NewRecorder()
	body := strings.NewReader(`{"objective":"Refactor the parser","start":false}`)
	s.handleSetGoal(rec, httptest.NewRequest(http.MethodPost, "/api/goal", body))
	if rec.Code != 200 {
		t.Fatalf("POST goal: code=%d body=%q", rec.Code, rec.Body.String())
	}
	var g tools.Goal
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatalf("POST goal response not JSON: %v", err)
	}
	if g.Objective != "Refactor the parser" || g.Status != tools.GoalActive {
		t.Fatalf("unexpected goal: %+v", g)
	}
	if !s.env.GoalStore.IsActive() {
		t.Fatal("store should hold an active goal after POST")
	}

	// 3. GET now returns the goal.
	rec = httptest.NewRecorder()
	s.handleGetGoal(rec, httptest.NewRequest(http.MethodGet, "/api/goal", nil))
	if !strings.Contains(rec.Body.String(), "Refactor the parser") {
		t.Fatalf("GET goal missing objective: %s", rec.Body.String())
	}

	// 4. POST with empty objective -> 400.
	rec = httptest.NewRecorder()
	s.handleSetGoal(rec, httptest.NewRequest(http.MethodPost, "/api/goal", strings.NewReader(`{"objective":"","start":false}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty objective should be 400, got %d", rec.Code)
	}

	// 5. DELETE clears.
	rec = httptest.NewRecorder()
	s.handleClearGoal(rec, httptest.NewRequest(http.MethodDelete, "/api/goal", nil))
	if rec.Code != 200 {
		t.Fatalf("DELETE goal: code=%d", rec.Code)
	}
	if s.env.GoalStore.Has() {
		t.Fatal("store should be empty after DELETE")
	}

	// 6. GET -> null again.
	rec = httptest.NewRecorder()
	s.handleGetGoal(rec, httptest.NewRequest(http.MethodGet, "/api/goal", nil))
	if strings.TrimSpace(rec.Body.String()) != "null" {
		t.Fatalf("GET after clear should be null, got %q", rec.Body.String())
	}
}

func TestWebGoalAndTodosAreTaskScoped(t *testing.T) {
	s := newGoalTestServer()
	otherEnv := tools.NewEnv("/tmp/other", "darwin/arm64")
	other := &Engine{
		taskID: "other-task", env: otherEnv, todoStore: otherEnv.TodoStore,
		handler: handler.NewWebHandler(),
	}
	s.tasks[other.taskID] = other
	s.env.GoalStore.Set("active objective")
	other.env.GoalStore.Set("other objective")
	s.todoStore.Update([]tools.TodoItem{{ID: 1, Title: "Active todo", Status: tools.TodoPending}})
	other.todoStore.Update([]tools.TodoItem{{ID: 2, Title: "Other todo", Status: tools.TodoPending}})

	goalRec := httptest.NewRecorder()
	s.handleGetGoal(goalRec, httptest.NewRequest(http.MethodGet, "/api/goal?task_id=other-task", nil))
	if goalRec.Code != http.StatusOK || !strings.Contains(goalRec.Body.String(), "other objective") || strings.Contains(goalRec.Body.String(), "active objective") {
		t.Fatalf("task goal response: code=%d body=%s", goalRec.Code, goalRec.Body.String())
	}

	todoRec := httptest.NewRecorder()
	s.handleGetTodos(todoRec, httptest.NewRequest(http.MethodGet, "/api/todos?task_id=other-task", nil))
	if todoRec.Code != http.StatusOK || !strings.Contains(todoRec.Body.String(), "Other todo") || strings.Contains(todoRec.Body.String(), "Active todo") {
		t.Fatalf("task todos response: code=%d body=%s", todoRec.Code, todoRec.Body.String())
	}

	clearRec := httptest.NewRecorder()
	s.handleClearGoal(clearRec, httptest.NewRequest(http.MethodDelete, "/api/goal?task_id=other-task", nil))
	if clearRec.Code != http.StatusOK || other.env.GoalStore.Has() || !s.env.GoalStore.Has() {
		t.Fatalf("task goal clear leaked across engines: code=%d active=%v other=%v", clearRec.Code, s.env.GoalStore.Has(), other.env.GoalStore.Has())
	}

	missingRec := httptest.NewRecorder()
	s.handleGetGoal(missingRec, httptest.NewRequest(http.MethodGet, "/api/goal?task_id=missing", nil))
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("unknown task goal code=%d body=%s", missingRec.Code, missingRec.Body.String())
	}
}
