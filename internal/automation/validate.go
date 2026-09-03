package automation

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxNameLen   = 200
	maxPromptLen = 8000
)

// validModes mirrors internal/mode wire ids (approval|plan|full_access). Kept as
// a local set to avoid importing mode just for validation.
var validModes = map[string]bool{"approval": true, "plan": true, "full_access": true}

// ValidateAutomation is the single validation rule shared by every creation
// path (HTTP API, agent tool, CLI) — mirroring tools.ValidateGoalObjective. It
// mutates nothing; callers assign ID/timestamps/defaults around it.
//
// Key invariants (PRD §7.5, §10.4): a local non-empty ProjectPath is required
// (no-project headless runs are unsafe and unsupported); remote (ssh://docker://)
// targets are rejected for v1.
func ValidateAutomation(a *Automation) error {
	if a == nil {
		return fmt.Errorf("automation is nil")
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(a.Name) > maxNameLen {
		return fmt.Errorf("name too long (max %d)", maxNameLen)
	}
	if strings.TrimSpace(a.Prompt) == "" {
		return fmt.Errorf("prompt is required")
	}
	if len(a.Prompt) > maxPromptLen {
		return fmt.Errorf("prompt too long (max %d)", maxPromptLen)
	}
	if a.Mode != "" && !validModes[a.Mode] {
		return fmt.Errorf("invalid mode %q (want approval|plan|full_access)", a.Mode)
	}
	if err := validateProjectPath(a.ProjectPath); err != nil {
		return err
	}
	return validateTrigger(a.Trigger)
}

func validateProjectPath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("project is required (no-project automations cannot run unattended)")
	}
	if strings.Contains(p, "://") {
		return fmt.Errorf("remote workspaces (ssh:// / docker://) are not supported for automations yet")
	}
	// A relative path would fire against whatever cwd the scheduling process
	// happens to have, not the user's intended project. Require an absolute path.
	if !filepath.IsAbs(p) {
		return fmt.Errorf("project must be an absolute path (got %q)", p)
	}
	return nil
}

func validateTrigger(t Trigger) error {
	switch t.Type {
	case TriggerManual:
		return nil
	case TriggerOnce:
		// Well-formedness only — past-ness is a create-time check (see
		// Store.Create) so an expired once-automation stays editable.
		if _, err := time.Parse(time.RFC3339, t.At); err != nil {
			return fmt.Errorf("once trigger needs a valid RFC3339 at time (got %q)", t.At)
		}
		return nil
	case TriggerSchedule:
		// fallthrough to cadence checks
	default:
		return fmt.Errorf("invalid trigger type %q (want schedule|manual|once)", t.Type)
	}
	if t.Minute < 0 || t.Minute > 59 {
		return fmt.Errorf("minute out of range 0-59")
	}
	switch t.Cadence {
	case CadenceHourly:
		// minute only
	case CadenceDaily:
		if t.Hour < 0 || t.Hour > 23 {
			return fmt.Errorf("hour out of range 0-23")
		}
	case CadenceWeekly:
		if t.Hour < 0 || t.Hour > 23 {
			return fmt.Errorf("hour out of range 0-23")
		}
		if t.Weekday < 0 || t.Weekday > 6 {
			return fmt.Errorf("weekday out of range 0-6")
		}
	case CadenceCron:
		expr, err := ParseCronExpr(t.Expr)
		if err != nil {
			return err
		}
		// Reject legal-but-never-fires expressions (`0 0 31 2 *`) so a broken
		// schedule can't sit enabled forever without running. The 5-year scan
		// window makes this near-permanent for any expression.
		if _, ok := NextCronFire(nowFunc(), expr); !ok {
			return fmt.Errorf("cron expression %q has no fire within %d years", t.Expr, cronMaxSearchYears)
		}
	default:
		return fmt.Errorf("invalid cadence %q (want hourly|daily|weekly|cron)", t.Cadence)
	}
	return nil
}

// IsLocalPath reports whether p is a usable local project path (non-empty, not a
// remote scheme). Used at fire time to skip+disable an automation pointed at a
// vanished or remote target.
func IsLocalPath(p string) bool {
	p = strings.TrimSpace(p)
	return p != "" && !strings.Contains(p, "://")
}
