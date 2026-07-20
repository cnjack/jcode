package web

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

// Cloud API: relay status + the auto_connect toggle, backed by the cloud
// supervisor wired in from the command layer. The endpoints mirror the
// */status + */config shape of tool_search.go.

type cloudConfigPayload struct {
	AutoConnect *bool `json:"auto_connect"`
}

// handleCloudStatus serves GET /api/cloud/status. Without a supervisor (e.g.
// headless builds) the status is synthesized from the on-disk credentials and
// config with state "offline".
func (s *Server) handleCloudStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cloudStatus())
}

// cloudStatus returns the live supervisor status, or — when no supervisor is
// wired — an offline snapshot derived from ~/.jcode/cloud.json + config.
func (s *Server) cloudStatus() cloud.Status {
	if s.cloudSupervisor != nil {
		return s.cloudSupervisor.Status()
	}
	// A never-started supervisor reports exactly the synthesized offline
	// status: credentials re-read from disk, no connector, state "offline".
	return cloud.NewSupervisor(s.cfg, 0, "").Status()
}

// handleCloudConfig serves POST /api/cloud/config: persists cloud.auto_connect
// and hot-applies it (start/stop the relay connector without a restart). On a
// SaveConfig failure the in-memory config is rolled back (done inside
// SetAutoConnect) and a 500 returned.
func (s *Server) handleCloudConfig(w http.ResponseWriter, r *http.Request) {
	var req cloudConfigPayload
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cloud config: " + err.Error()})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return
	}
	if req.AutoConnect == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "auto_connect is required"})
		return
	}

	enabled := *req.AutoConnect
	if s.cloudSupervisor != nil {
		if err := s.cloudSupervisor.SetAutoConnect(enabled); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, s.cloudSupervisor.Status())
		return
	}

	// Nil supervisor: persist only (there is no live connector to hot-apply),
	// with the same read/modify/write + rollback pattern.
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if s.cfg == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	previous := s.cfg.CloudSettings()
	s.cfg.SetCloud(&config.CloudConfig{
		Enabled:     previous.Enabled,
		URL:         previous.URL,
		AutoConnect: &enabled,
		E2EE:        previous.E2EE,
	})
	if err := config.SaveConfig(s.cfg); err != nil {
		if previous == (config.CloudConfig{}) {
			s.cfg.SetCloud(nil)
		} else {
			s.cfg.SetCloud(&previous)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.cloudStatus())
}
