package web

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/cnjack/jcode/internal/config"
)

// devOptionsPayload mirrors the writable developer toggles. The toggles and
// langfuse block are both optional on a single request: a nil field / absent
// block means "leave that part unchanged".
type devOptionsPayload struct {
	LoggingEnabled *bool               `json:"logging_enabled,omitempty"`
	TracingEnabled *bool               `json:"tracing_enabled,omitempty"`
	Langfuse       *devOptionsLangfuse `json:"langfuse,omitempty"`
	// LangfuseClear, when true, removes the Langfuse block entirely. It takes
	// precedence over Langfuse and is the only way to wipe the stored secret
	// (the Langfuse block uses keep-on-empty semantics for secret_key).
	LangfuseClear bool `json:"langfuse_clear,omitempty"`
}

// devOptionsLangfuse mirrors config.LangfuseConfig. The JSON keys match the
// on-disk telemetry.langfuse.* spelling so the request payload round-trips
// cleanly. SecretKey is treated as keep-on-empty by the handler.
type devOptionsLangfuse struct {
	Host      string `json:"host,omitempty"`
	PublicKey string `json:"public_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
}

func (s *Server) handleDevOptionsStatus(w http.ResponseWriter, _ *http.Request) {
	s.cfgMu.Lock()
	dc := s.cfg.DeveloperSettings()
	lc := s.cfg.LangfuseSettings()
	langfuseConfigured := config.LangfuseConfigured(s.cfg)
	available := s.cfg != nil
	s.cfgMu.Unlock()

	// Reflect the platform default: a nil pointer reads as enabled.
	loggingEnabled := dc.EnableLogging == nil || *dc.EnableLogging
	tracingEnabled := dc.EnableTracing == nil || *dc.EnableTracing

	// Never leak secret_key; only its presence. host and public_key are not
	// sufficient to authenticate, so we expose host verbatim and the masked
	// public_key (matching providers.go's maskSecret discipline).
	writeJSON(w, http.StatusOK, map[string]any{
		"available":           available,
		"logging_enabled":     loggingEnabled,
		"tracing_enabled":     tracingEnabled,
		"langfuse_configured": langfuseConfigured,
		"langfuse": map[string]any{
			"host":           lc.Host,
			"public_key":     maskSecret(lc.PublicKey),
			"public_key_set": lc.PublicKey != "",
			"secret_key_set": lc.SecretKey != "",
			// Default host hint for the empty-input placeholder in the UI.
			"default_host": "https://cloud.langfuse.com",
		},
	})
}

func (s *Server) handleDevOptionsConfig(w http.ResponseWriter, r *http.Request) {
	var req devOptionsPayload
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid developer options: " + err.Error()})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return
	}
	if req.LoggingEnabled == nil && req.TracingEnabled == nil && req.Langfuse == nil && !req.LangfuseClear {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one of logging_enabled, tracing_enabled, langfuse, or langfuse_clear is required"})
		return
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if s.cfg == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}

	// ── developer toggles ────────────────────────────────────────────────
	previousDev := s.cfg.DeveloperConfigSnapshot()
	var merged config.DeveloperConfig
	if previousDev != nil {
		merged = *previousDev
	}
	if req.LoggingEnabled != nil {
		v := *req.LoggingEnabled
		merged.EnableLogging = &v
	}
	if req.TracingEnabled != nil {
		v := *req.TracingEnabled
		merged.EnableTracing = &v
	}
	s.cfg.SetDeveloper(&merged)

	// ── langfuse credentials ─────────────────────────────────────────────
	previousLangfuse := s.cfg.LangfuseSnapshot()
	switch {
	case req.LangfuseClear:
		s.cfg.SetLangfuse(nil)
	case req.Langfuse != nil:
		// Start from the prior block (may be nil); merge incoming fields with
		// keep-on-empty semantics for the secret_key so a masked display value
		// re-submitted by the UI does not wipe the stored key.
		next := config.LangfuseConfig{}
		if previousLangfuse != nil {
			next = *previousLangfuse
		}
		next.Host = req.Langfuse.Host
		if req.Langfuse.PublicKey != "" {
			next.PublicKey = req.Langfuse.PublicKey
		}
		if req.Langfuse.SecretKey != "" {
			next.SecretKey = req.Langfuse.SecretKey
		}
		s.cfg.SetLangfuse(&next)
	}

	if err := config.SaveConfig(s.cfg); err != nil {
		// Restore both blocks to their exact prior state.
		s.cfg.SetDeveloper(previousDev)
		s.cfg.SetLangfuse(previousLangfuse)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	loggingEnabled := merged.EnableLogging == nil || *merged.EnableLogging
	tracingEnabled := merged.EnableTracing == nil || *merged.EnableTracing

	// Both toggles and the Langfuse tracer are process-startup switches — the
	// running process keeps its current logger / tracer until the next
	// restart. Surface that explicitly so the UI can show a hint.
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"logging_enabled":  loggingEnabled,
		"tracing_enabled":  tracingEnabled,
		"restart_required": true,
	})
}
