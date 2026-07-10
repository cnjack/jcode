package web

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/tools"
)

func (s *Server) handleGetTodos(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()
	if eng == nil || eng.todoStore == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, eng.todoStore.Items())
}

// handleGetGoal returns the current session goal (or null when none is set).
func (s *Server) handleGetGoal(w http.ResponseWriter, _ *http.Request) {
	eng := s.activeEngine()
	if eng == nil || eng.env == nil || eng.env.GoalStore == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, eng.env.GoalStore.Get())
}

// handleSetGoal sets (or replaces) the session goal. Unless start=false, it also
// kicks off an agent run so work begins immediately.
func (s *Server) handleSetGoal(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()
	if eng == nil || eng.env == nil || eng.env.GoalStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "goals not available"})
		return
	}
	var req struct {
		Objective string `json:"objective"`
		Start     *bool  `json:"start,omitempty"` // default true
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	objective, err := tools.ValidateGoalObjective(req.Objective)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	g := eng.env.GoalStore.Set(objective)

	if req.Start == nil || *req.Start {
		// Start working immediately when idle; if busy, the continuation guard
		// will pick the goal up after the current run finishes. Targets the active
		// task.
		if eng.running.CompareAndSwap(false, true) {
			s.submitMessage(eng, tools.GoalKickoffPrompt(objective), eng.curMode(), "", "", nil)
		}
	}
	writeJSON(w, http.StatusOK, g)
}

// handleClearGoal removes the session goal.
func (s *Server) handleClearGoal(w http.ResponseWriter, _ *http.Request) {
	if eng := s.activeEngine(); eng != nil && eng.env != nil && eng.env.GoalStore != nil {
		eng.env.GoalStore.Clear()
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (s *Server) handleApproval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string `json:"id"`
		TaskID     string `json:"task_id"`
		Approved   bool   `json:"approved"`
		ApproveAll bool   `json:"approve_all"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	// Route the resolve to the requesting task's handler. resolveEngine maps an
	// empty task_id to the active task (legacy clients) but a NON-empty unknown id
	// to nil — so a stray id can't resolve against the active task's handler-local
	// approval ids.
	reng := s.resolveEngine(req.TaskID)
	if reng == nil || reng.handler == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such task"})
		return
	}
	if err := reng.handler.ResolveApproval(req.ID, req.Approved, req.ApproveAll); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// "Allow all" promotes that task to auto-approve (the runner flips its
	// ApprovalState on resolve). Mirror it onto that task's mode + selector.
	s.syncModeAfterApproval(reng, req.Approved, req.ApproveAll)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// syncModeAfterApproval reflects an approve-all promotion onto the server's
// user-facing mode state and notifies connected clients. A plain single approve
// (or a deny) leaves the mode untouched. The runner's ApprovalState is the
// source of truth for the approval axis; this only projects it onto the unified
// selector the frontend renders.
func (s *Server) syncModeAfterApproval(eng *Engine, approved, approveAll bool) {
	if !approved || !approveAll || eng == nil {
		return
	}
	sm := mode.FullAccess
	eng.applyModeSwitch(sm.String(), nil)
	s.wsBroker.Broadcast(WSEvent{Type: "mode_changed", TaskID: eng.taskID, Data: map[string]string{
		"mode": sm.String(),
	}})
}

// handlePendingApproval returns approval requests still awaiting a decision.
// The frontend pulls this after rebuilding the timeline (page reload / session
// resume / WS reconnect) so an in-flight approval is re-attached as a card
// instead of leaving the agent blocked forever.
func (s *Server) handlePendingApproval(w http.ResponseWriter, r *http.Request) {
	// Empty task_id → active task; non-empty unknown → empty (don't leak another
	// task's pending requests under a stray id).
	eng := s.resolveEngine(r.URL.Query().Get("task_id"))
	if eng == nil || eng.handler == nil {
		writeJSON(w, http.StatusOK, []handler.WebApprovalRequestData{})
		return
	}
	writeJSON(w, http.StatusOK, eng.handler.PendingApprovalRequests())
}

// handleAskUser resolves a pending ask_user request with the user's answers,
// routed back to the blocked tool via WebHandler.ResolveAskUser. The "answers"
// array is parallel to the questions the frontend received in ask_user_request:
// each carries the question header plus either a free-text answer or selected
// option labels.
func (s *Server) handleAskUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		TaskID  string `json:"task_id"`
		Answers []struct {
			QuestionHeader string   `json:"question_header"`
			Answer         string   `json:"answer"`
			Selected       []string `json:"selected"`
		} `json:"answers"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	resp := tools.AskUserBatchResponse{}
	for _, a := range req.Answers {
		resp.Answers = append(resp.Answers, tools.AskUserAnswer{
			QuestionHeader: a.QuestionHeader,
			Answer:         a.Answer,
			Selected:       a.Selected,
		})
	}

	// Route the answer to the requesting task's handler. Empty task_id → active;
	// non-empty unknown → reject (ids are handler-local).
	eng := s.resolveEngine(req.TaskID)
	if eng == nil || eng.handler == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such task"})
		return
	}
	if err := eng.handler.ResolveAskUser(req.ID, resp); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handlePendingAskUser returns ask_user questions still awaiting an answer.
// The frontend pulls this after rebuilding the timeline (page reload / session
// resume) so an in-flight question is re-attached to its tool card instead of
// leaving the agent blocked forever.
func (s *Server) handlePendingAskUser(w http.ResponseWriter, r *http.Request) {
	eng := s.resolveEngine(r.URL.Query().Get("task_id"))
	if eng == nil || eng.handler == nil {
		writeJSON(w, http.StatusOK, []handler.WebAskUserRequestData{})
		return
	}
	writeJSON(w, http.StatusOK, eng.handler.PendingAskUserRequests())
}
