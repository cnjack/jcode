package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/cnjack/jcode/internal/automation"
	"github.com/cnjack/jcode/internal/session"
)

// defaultRunsLimit bounds how many automation runs handleListAutomationRuns
// returns when the client does not pass an explicit ?limit.
const defaultRunsLimit = 100

// automationItem is an automation definition plus derived display fields and its
// volatile run-state, as returned to the web UI.
type automationItem struct {
	automation.Automation
	HumanSchedule string              `json:"human_schedule"`
	Badge         string              `json:"badge"`
	State         automation.RunState `json:"state"`
}

func (s *Server) autoStore(w http.ResponseWriter) (*automation.Store, bool) {
	if s.automations == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "automations unavailable (setup mode)"})
		return nil, false
	}
	return s.automations, true
}

func toItem(st *automation.Store, a *automation.Automation) automationItem {
	return automationItem{
		Automation:    *a,
		HumanSchedule: automation.HumanSchedule(a.Trigger),
		Badge:         automation.Badge(a.Trigger),
		State:         st.State(a.ID),
	}
}

func (s *Server) handleListAutomations(w http.ResponseWriter, r *http.Request) {
	st, ok := s.autoStore(w)
	if !ok {
		return
	}
	list := st.List()
	items := make([]automationItem, 0, len(list))
	for _, a := range list {
		items = append(items, toItem(st, a))
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetAutomation(w http.ResponseWriter, r *http.Request) {
	st, ok := s.autoStore(w)
	if !ok {
		return
	}
	a := st.Get(r.PathValue("id"))
	if a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "automation not found"})
		return
	}
	writeJSON(w, http.StatusOK, toItem(st, a))
}

func (s *Server) handleCreateAutomation(w http.ResponseWriter, r *http.Request) {
	st, ok := s.autoStore(w)
	if !ok {
		return
	}
	var req struct {
		automation.Automation
		RunNow bool `json:"run_now"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Source == "" {
		req.Source = automation.SourceManual
	}
	created, err := st.Create(req.Automation)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.RunNow {
		// A freshly-created automation has a brand-new id, so the claim always
		// succeeds; ignore the result.
		s.runAutomationAsync(created)
	}
	writeJSON(w, http.StatusOK, toItem(st, created))
}

func (s *Server) handleUpdateAutomation(w http.ResponseWriter, r *http.Request) {
	st, ok := s.autoStore(w)
	if !ok {
		return
	}
	// PUT is a partial patch (the TS client sends Partial<Automation>): a field
	// is only overwritten when present in the body. Pointer fields distinguish
	// "omitted" from "zero value", so editing an automation that carries a
	// provider/model override — or is paused — never silently clears it.
	var req struct {
		Name        *string             `json:"name"`
		Prompt      *string             `json:"prompt"`
		Trigger     *automation.Trigger `json:"trigger"`
		ProjectPath *string             `json:"project_path"`
		Mode        *string             `json:"mode"`
		Provider    *string             `json:"provider"`
		Model       *string             `json:"model"`
		Enabled     *bool               `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	updated, err := st.Update(r.PathValue("id"), func(a *automation.Automation) {
		if req.Name != nil {
			a.Name = *req.Name
		}
		if req.Prompt != nil {
			a.Prompt = *req.Prompt
		}
		if req.Trigger != nil {
			a.Trigger = *req.Trigger
		}
		if req.ProjectPath != nil {
			a.ProjectPath = *req.ProjectPath
		}
		if req.Mode != nil {
			a.Mode = *req.Mode
		}
		if req.Provider != nil {
			a.Provider = *req.Provider
		}
		if req.Model != nil {
			a.Model = *req.Model
		}
		if req.Enabled != nil {
			a.Enabled = *req.Enabled
		}
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, automation.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, toItem(st, updated))
}

func (s *Server) handleDeleteAutomation(w http.ResponseWriter, r *http.Request) {
	st, ok := s.autoStore(w)
	if !ok {
		return
	}
	if err := st.Delete(r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleRunAutomation(w http.ResponseWriter, r *http.Request) {
	st, ok := s.autoStore(w)
	if !ok {
		return
	}
	a := st.Get(r.PathValue("id"))
	if a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "automation not found"})
		return
	}
	// Reject a manual run if one is already in flight (this server's manual guard)
	// or a scheduled run is currently executing (shared run-state), so a
	// double-click or a run-now racing a scheduled fire can't spawn parallel
	// sessions mutating the same project.
	if st.State(a.ID).LastStatus == automation.StatusRunning || !s.runAutomationAsync(a) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a run is already in progress for this automation"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// runAutomationAsync fires a manual run in the background, recording terminal
// state via the shared ExecuteRun bookkeeping. It claims a per-automation
// in-flight slot first and returns false without starting if one is already
// held (concurrency guard); the slot is released when the run completes.
func (s *Server) runAutomationAsync(a *automation.Automation) bool {
	s.autoRunMu.Lock()
	if s.autoRunInflight == nil {
		s.autoRunInflight = make(map[string]bool)
	}
	if s.autoRunInflight[a.ID] {
		s.autoRunMu.Unlock()
		return false
	}
	s.autoRunInflight[a.ID] = true
	s.autoRunMu.Unlock()

	go func() {
		defer func() {
			s.autoRunMu.Lock()
			delete(s.autoRunInflight, a.ID)
			s.autoRunMu.Unlock()
		}()
		ctx := s.rootCtx()
		if ctx == nil {
			return
		}
		_, _ = automation.ExecuteRun(ctx, s.automations, s.AutomationRunner(), a, automation.KindManual)
	}()
	return true
}

func (s *Server) handleAutomationTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, automation.BuiltinTemplates())
}

// automationRun is one entry in "Recent runs".
type automationRun struct {
	SessionID      string `json:"session_id"`
	AutomationID   string `json:"automation_id"`
	Title          string `json:"title"`
	Project        string `json:"project"`
	TriggerKind    string `json:"trigger_kind"`
	StartTime      string `json:"start_time"`
	EndTime        string `json:"end_time,omitempty"`
	TerminalStatus string `json:"terminal_status,omitempty"`
	Status         string `json:"status,omitempty"`
	ErrorReason    string `json:"error_reason,omitempty"`
}

func (s *Server) handleListAutomationRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := q.Get("automation_id")
	before := q.Get("before") // RFC3339 cursor: only runs that started strictly before
	limit := defaultRunsLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	all, err := session.ListAllSessions()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	runs := make([]automationRun, 0)
	for project, metas := range all {
		for _, m := range metas {
			if m.AutomationID == "" {
				continue
			}
			if filter != "" && m.AutomationID != filter {
				continue
			}
			if before != "" && m.StartTime >= before {
				continue
			}
			runs = append(runs, automationRun{
				SessionID:      m.UUID,
				AutomationID:   m.AutomationID,
				Title:          m.Title,
				Project:        project,
				TriggerKind:    m.TriggerKind,
				StartTime:      m.StartTime,
				EndTime:        m.EndTime,
				TerminalStatus: m.TerminalStatus,
				Status:         m.Status,
				ErrorReason:    m.ErrorReason,
			})
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartTime > runs[j].StartTime })
	// Bound the response (newest first). The underlying scan is still O(total
	// sessions); a dedicated automation-runs index is a documented follow-up.
	if len(runs) > limit {
		runs = runs[:limit]
	}
	writeJSON(w, http.StatusOK, runs)
}
