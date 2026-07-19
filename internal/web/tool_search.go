package web

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/cnjack/jcode/internal/config"
)

// ToolSearchCounts is the active task's effective progressive-disclosure
// catalog. MCP tools are a subset of DeferredCount.
type ToolSearchCounts struct {
	DirectCount      int `json:"direct_count"`
	DeferredCount    int `json:"deferred_count"`
	MCPDeferredCount int `json:"mcp_deferred_count"`
}

type toolSearchConfigPayload struct {
	Enabled *bool `json:"enabled"`
}

func (s *Server) handleToolSearchStatus(w http.ResponseWriter, _ *http.Request) {
	counts := ToolSearchCounts{}
	if eng := s.activeEngine(); eng != nil && eng.toolSearchStats != nil {
		counts = eng.toolSearchStats()
	}

	s.cfgMu.Lock()
	enabled := config.ToolSearchEnabled(s.cfg)
	available := s.cfg != nil
	s.cfgMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"available":          available,
		"enabled":            enabled,
		"direct_count":       counts.DirectCount,
		"deferred_count":     counts.DeferredCount,
		"mcp_deferred_count": counts.MCPDeferredCount,
	})
}

func (s *Server) handleToolSearchConfig(w http.ResponseWriter, r *http.Request) {
	var req toolSearchConfigPayload
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tool search config: " + err.Error()})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return
	}
	if req.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled is required"})
		return
	}

	enabled := *req.Enabled
	stored := config.ToolSearchConfig{Enabled: &enabled}
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if s.cfg == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	previous := s.cfg.ToolSearchConfigSnapshot()
	s.cfg.SetToolSearch(&stored)
	if err := config.SaveConfig(s.cfg); err != nil {
		s.cfg.SetToolSearch(previous)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response := map[string]any{"status": "ok", "enabled": enabled}
	if err := s.rebuildToolAgents(); err != nil {
		config.Logger().Printf("[tool-search] config saved but active-agent tool refresh failed: %v", err)
		response["warning_code"] = "agent_refresh_failed"
		response["refresh_warning"] = "saved, but active agents could not be refreshed"
	}
	writeJSON(w, http.StatusOK, response)
}
