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
	return &Server{
		Engine: &Engine{
			env:     tools.NewEnv("/tmp", "darwin/arm64"),
			handler: handler.NewWebHandler(),
		},
	}
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
