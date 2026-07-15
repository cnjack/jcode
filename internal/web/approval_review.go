package web

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/review"
)

// handleGetApprovalReviewConfig returns the current approval_review tuning
// configuration. The fields are not sensitive, so they are returned as-is.
//
// Stored values are returned unresolved — an empty field means "follow the
// built-in default" and must stay empty so a later change to that default still
// reaches the user. The resolved defaults ride along in a separate "defaults"
// block so the settings UI can show what an empty field actually does instead
// of restating the defaults itself.
func (s *Server) handleGetApprovalReviewConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	arc := cfg.ApprovalReviewSettings()

	writeJSON(w, http.StatusOK, map[string]any{
		"model":           arc.Model,
		"policy":          arc.Policy,
		"timeout_seconds": arc.TimeoutSeconds,
		"investigate":     arc.Investigate,
		"reuse_session":   arc.ReuseSession,
		"audit_path":      arc.AuditPath,
		"defaults": map[string]any{
			"timeout_seconds": int(review.DefaultTimeout / time.Second),
			"audit_path":      review.DefaultAuditPath(),
		},
	})
}

// handleSetApprovalReviewConfig updates the approval_review tuning configuration
// and persists it to disk. It mutates the live shared config in place so the
// change is visible to new reviewer sessions immediately.
func (s *Server) handleSetApprovalReviewConfig(w http.ResponseWriter, r *http.Request) {
	var req config.ApprovalReviewConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.TimeoutSeconds < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "timeout_seconds must be non-negative"})
		return
	}

	s.cfgMu.Lock()
	s.mu.Lock()
	if s.cfg == nil {
		s.mu.Unlock()
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	// Publish through SetApprovalReview: a task goroutine may be reading this
	// same block to build its reviewer, and it holds none of the locks here.
	prev := s.cfg.ApprovalReview
	s.cfg.SetApprovalReview(&req)
	err := config.SaveConfig(s.cfg)
	if err != nil {
		// Keep memory consistent with disk on failure.
		s.cfg.SetApprovalReview(prev)
	}
	s.mu.Unlock()
	s.cfgMu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
