package web

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/theme"
)

var supportedAccountLanguages = map[string]bool{
	"en": true, "zh-Hans": true, "zh-Hant": true, "ja": true, "ko": true,
}

// handleAccountPreferences persists browser-owned portable preferences in the
// real config file, then queues an encrypted account sync. Pointer fields keep
// PATCH semantics (omitted is distinct from empty).
func (s *Server) handleAccountPreferences(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Language *string `json:"language"`
		Theme    *string `json:"theme"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || (req.Language == nil && req.Theme == nil) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid account preferences"})
		return
	}
	if req.Language != nil && !supportedAccountLanguages[*req.Language] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported language"})
		return
	}
	if req.Theme != nil && *req.Theme != "system" {
		if _, ok := theme.Get(*req.Theme); !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown theme"})
			return
		}
	}

	s.cfgMu.Lock()
	if s.cfg == nil {
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	previousLanguage, previousTheme := s.cfg.Language, s.cfg.Theme
	if req.Language != nil {
		s.cfg.Language = *req.Language
	}
	if req.Theme != nil {
		if *req.Theme == "system" {
			s.cfg.Theme = ""
		} else {
			s.cfg.Theme = *req.Theme
		}
	}
	if err := config.SaveConfig(s.cfg); err != nil {
		s.cfg.Language, s.cfg.Theme = previousLanguage, previousTheme
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config: " + err.Error()})
		return
	}
	language, themeID := s.cfg.Language, s.cfg.Theme
	s.cfgMu.Unlock()

	s.syncAccountSettingsBestEffort(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"language": language, "theme": themeID,
	})
}

func (s *Server) syncAccountSettingsBestEffort(r *http.Request) {
	if s.cloudSupervisor == nil {
		return
	}
	if err := s.cloudSupervisor.SyncAccountSettings(r.Context()); err != nil {
		config.Logger().Printf("[web] account settings sync queued with error: %v", err)
	}
}
