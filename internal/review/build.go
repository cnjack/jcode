package review

import (
	"time"

	"github.com/cnjack/jcode/internal/config"
)

// BuildFromConfig constructs a Reviewer from config. It never returns nil:
// when approval_review settings are absent it falls back to sensible defaults
// (small alias model, default timeout, no investigation, no session reuse).
// Callers install the returned Reviewer on ApprovalState whenever the session
// is in Auto mode.
func BuildFromConfig(cfg *config.Config, platform string) Reviewer {
	// Snapshot rather than reading cfg.ApprovalReview directly: the web settings
	// handler can publish a new block on this same live config concurrently.
	rc := cfg.ApprovalReviewSettings()
	return New(Options{
		Config:      cfg,
		ModelRef:    rc.Model,
		Policy:      rc.Policy,
		Timeout:     time.Duration(rc.TimeoutSeconds) * time.Second,
		AuditPath:   rc.AuditPath,
		Investigate: rc.Investigate,
		ReuseCache:  rc.ReuseSession,
		Platform:    platform,
	})
}
