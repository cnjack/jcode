package review

import (
	"time"

	"github.com/cnjack/jcode/internal/config"
)

// BuildFromConfig constructs a Reviewer from config, or returns nil when the
// auto-reviewer is disabled. Frontends call this once per session and, when
// non-nil, install it on the ApprovalState. Returning a typed nil-free
// interface is intentional: callers compare the result against nil to decide
// whether to wire the reviewer at all.
func BuildFromConfig(cfg *config.Config, platform string) Reviewer {
	if cfg == nil || !cfg.ApprovalReviewEnabled() {
		return nil
	}
	rc := cfg.ApprovalReview
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
