package web

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

type agentRoleView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Model       string `json:"model,omitempty"`
}

func (s *Server) handleListAgents(w http.ResponseWriter, _ *http.Request) {
	eng := s.activeEngine()
	if eng == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no active task"})
		return
	}
	roles := config.LoadAgentRoles(eng.pwd)
	names := make([]string, 0, len(roles))
	for name := range roles {
		names = append(names, name)
	}
	sort.Strings(names)
	agents := make([]agentRoleView, 0, len(names))
	for _, name := range names {
		role := roles[name]
		agents = append(agents, agentRoleView{
			Name:        name,
			Description: role.Description,
			Model:       role.Model,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agents":  agents,
		"current": eng.curAgentRole(),
	})
}

func (s *Server) handleSwitchAgent(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()
	if eng == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no active task"})
		return
	}
	var req struct {
		Agent string `json:"agent"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	req.Agent = strings.TrimSpace(req.Agent)
	if req.Agent != "" {
		if _, ok := config.LoadAgentRoles(eng.pwd)[req.Agent]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown custom agent"})
			return
		}
	}
	if req.Agent == eng.curAgentRole() {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "agent": req.Agent})
		return
	}
	if eng.rebuildForRole == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "custom agent switching is not supported"})
		return
	}

	eng.rebuildMu.Lock()
	oldProvider, oldModel, _ := eng.modelSnapshot()
	built, err := eng.rebuildForRole(req.Agent, oldProvider, oldModel)
	if err != nil {
		eng.rebuildMu.Unlock()
		config.Logger().Printf("[web] custom agent switch rebuild error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to switch custom agent"})
		return
	}
	eng.applyAgentRoleSwitch(req.Agent, built)
	eng.rebuildMu.Unlock()

	s.wsBroker.Broadcast(WSEvent{Type: "agent_changed", TaskID: eng.taskID, Data: map[string]string{
		"agent": req.Agent,
	}})
	if built.Provider != oldProvider || built.Model != oldModel {
		s.wsBroker.Broadcast(WSEvent{Type: "model_changed", TaskID: eng.taskID, Data: map[string]string{
			"provider": built.Provider,
			"model":    built.Model,
		}})
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok", "agent": req.Agent,
		"provider": built.Provider, "model": built.Model,
	})
}
