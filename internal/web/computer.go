package web

import (
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/cnjack/jcode/internal/computer"
	"github.com/cnjack/jcode/internal/config"
)

func (s *Server) handleComputerStatus(w http.ResponseWriter, r *http.Request) {
	if s.computerMgr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	st := s.computerMgr.Status(r.Context())
	var appPerms []config.ComputerAppPermission
	var approval map[string]string
	s.mu.Lock()
	if s.cfg != nil && s.cfg.Computer != nil {
		appPerms = s.cfg.Computer.AppPermissions
		approval = s.cfg.Computer.Approval
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"available":       true,
		"status":          st,
		"app_permissions": appPerms,
		"approval":        approval,
	})
}

func (s *Server) handleComputerConfig(w http.ResponseWriter, r *http.Request) {
	if s.computerMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "computer use unavailable"})
		return
	}
	var req config.ComputerConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Backend == "" {
		req.Backend = "auto"
	}
	// Reject anything the override layer would silently drop, rather than
	// storing it and letting it evaporate at lookup time. A user who set a tier
	// and got a 200 is entitled to believe it took effect; Manager.TierOverrides
	// keeps only tightenings, so storing a loosening would be answering "saved"
	// to a request we have no intention of honoring.
	for _, p := range req.AppPermissions {
		if p.Tier == "" {
			continue
		}
		t, ok := computer.ParseTier(p.Tier)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "unknown tier " + p.Tier + " for " + p.BundleID + " (want read, click or full)",
			})
			return
		}
		if def := computer.DefaultTier(p.BundleID); t > def {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": p.BundleID + " is a " + def.String() + "-tier app and cannot be loosened to " +
					t.String() + ". Tier overrides may only tighten.",
			})
			return
		}
	}

	s.cfgMu.Lock()
	s.mu.Lock()
	if s.cfg == nil {
		s.mu.Unlock()
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	s.cfg.Computer = &req
	err := config.SaveConfig(s.cfg)
	s.mu.Unlock()
	s.cfgMu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// One mapper, shared with the command layer (design §5).
	s.computerMgr.SetConfig(computer.FromConfig(&req))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleComputerShot serves a saved screenshot by id. The id is re-parsed as a
// uuid inside ScreenshotPath before it reaches the filesystem.
func (s *Server) handleComputerShot(w http.ResponseWriter, r *http.Request) {
	if s.computerMgr == nil {
		http.NotFound(w, r)
		return
	}
	id := r.PathValue("id")
	if len(id) > 4 && id[len(id)-4:] == ".png" {
		id = id[:len(id)-4]
	}
	path, err := s.computerMgr.ScreenshotPath(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(b)
}
