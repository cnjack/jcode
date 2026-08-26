package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/tools"
)

func (s *Server) handleGetTodos(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	eng := s.resolveEngine(taskID)
	if taskID != "" && eng == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task todos not available"})
		return
	}
	if eng == nil || eng.todoStore == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, eng.todoStore.Items())
}

// handleGetGoal returns the current session goal (or null when none is set).
func (s *Server) handleGetGoal(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	eng := s.resolveEngine(taskID)
	if taskID != "" && eng == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task goals not available"})
		return
	}
	if eng == nil || eng.env == nil || eng.env.GoalStore == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, eng.env.GoalStore.Get())
}

// handleSetGoal sets (or replaces) the session goal. Unless start=false, it also
// kicks off an agent run so work begins immediately.
func (s *Server) handleSetGoal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Objective string `json:"objective"`
		Start     *bool  `json:"start,omitempty"` // default true
		TaskID    string `json:"task_id,omitempty"`
		Source    string `json:"source,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	eng := s.resolveEngine(req.TaskID)
	if eng == nil || eng.env == nil || eng.env.GoalStore == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task goals not available"})
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
		// will pick the goal up after the current run finishes. The explicit task
		// id keeps multi-tab callers from inheriting whichever task is active.
		if s.tryStartEngine(eng) {
			if _, err := s.submitMessage(
				eng, tools.GoalKickoffPrompt(objective), eng.curMode(), req.Source, req.TaskID, nil,
			); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, g)
}

// handleClearGoal removes the session goal.
func (s *Server) handleClearGoal(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	eng := s.resolveEngine(taskID)
	if taskID != "" && eng == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task goals not available"})
		return
	}
	if eng != nil && eng.env != nil && eng.env.GoalStore != nil {
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
		OptionID   string `json:"option_id"`
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
	resolvedOptionID := ""
	approved, approveAll := req.Approved, req.ApproveAll
	if req.OptionID != "" {
		response, resolveErr := reng.handler.ResolveApprovalOption(req.ID, req.OptionID)
		if resolveErr != nil {
			if errors.Is(resolveErr, handler.ErrApprovalModePromotion) {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist mode change"})
				return
			}
			writeJSON(w, http.StatusNotFound, map[string]string{"error": resolveErr.Error()})
			return
		}
		resolvedOptionID = response.ResolvedOptionID
		approved = response.Approved
		approveAll = response.Approved && response.Mode == handler.ModeAuto
	} else if err := reng.handler.ResolveApproval(req.ID, req.Approved, req.ApproveAll); err != nil {
		if errors.Is(err, handler.ErrApprovalModePromotion) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist mode change"})
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// The registered WebHandler guard normally committed Allow-all before the
	// response reached the runner. Keep this idempotent call for handlers built
	// directly by embedders/tests without the registration hook.
	if err := s.syncModeAfterApproval(reng, approved, approveAll); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist mode change"})
		return
	}
	response := map[string]string{"status": "ok"}
	if resolvedOptionID != "" {
		response["resolved_option_id"] = resolvedOptionID
	}
	writeJSON(w, http.StatusOK, response)
}

// syncModeAfterApproval commits an approve-all promotion, updates the task's
// approval/engine state, and notifies connected clients. A plain single approve
// (or a deny) leaves the mode untouched. WebHandler invokes this as a pre-send
// guard, so the runner cannot observe ModeAuto until the Full access journal
// entry is durable.
func (s *Server) syncModeAfterApproval(eng *Engine, approved, approveAll bool) error {
	if !approved || !approveAll || eng == nil {
		return nil
	}
	sm := mode.FullAccess
	// An approve-all promotion changes the mode/revision while preserving the
	// current agent. Serialize it with schema/prompt rebuilds so a rebuild that
	// already captured the previous revision can finish and publish its refreshed
	// agent before the promotion advances the revision. Without this lock, the
	// rebuild is discarded as stale and the task keeps its old tool schema.
	eng.rebuildMu.Lock()
	if eng.curMode() == sm.String() {
		eng.rebuildMu.Unlock()
		return nil
	}
	if err := eng.recordModeChange(sm.String()); err != nil {
		eng.rebuildMu.Unlock()
		config.Logger().Printf("[web] allow-all mode journal commit failed for task %s: %v", eng.taskID, err)
		return err
	}
	if eng.approvalState != nil {
		eng.approvalState.SetSessionMode(sm)
	}
	eng.applyModeSwitch(sm.String(), nil)
	eng.rebuildMu.Unlock()
	s.wsBroker.Broadcast(WSEvent{Type: "mode_changed", TaskID: eng.taskID, Data: map[string]string{
		"mode": sm.String(),
	}})
	return nil
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
