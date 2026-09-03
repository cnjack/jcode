// Package automation implements jcode Automations: scheduled and manual agent
// runs. The package is a leaf domain layer — it owns the data model, validation,
// scheduling math, persistence (two-file + flock), built-in templates, and the
// single-owner scheduler loop. It does NOT depend on web/tui/runner; callers
// inject a Runner to actually execute a run. See internal-doc/automations-prd.md.
package automation

import "time"

// TriggerType is how an automation fires.
type TriggerType string

const (
	// TriggerSchedule fires on a recurring wall-clock cadence.
	TriggerSchedule TriggerType = "schedule"
	// TriggerManual never fires automatically; only via an explicit run.
	TriggerManual TriggerType = "manual"
	// TriggerOnce fires exactly once at a pinned wall-clock time (Trigger.At),
	// then the scheduler auto-disarms it (Enabled=false) — the definition is
	// kept so the run record and prompt stay reviewable/editable.
	TriggerOnce TriggerType = "once"
)

// Cadence is the recurrence granularity for a scheduled trigger.
type Cadence string

const (
	CadenceHourly Cadence = "hourly"
	CadenceDaily  Cadence = "daily"
	CadenceWeekly Cadence = "weekly"
	// CadenceCron fires on a 5-field cron expression (Trigger.Expr), evaluated
	// in the host's local timezone. Covers schedules the structured cadences
	// cannot express (weekday sets, sub-hour intervals, day-of-month pins).
	CadenceCron Cadence = "cron"
)

// Trigger describes when an automation fires. Times are interpreted in the
// host's local timezone (see ComputeNextRun).
type Trigger struct {
	Type    TriggerType `json:"type"`
	Cadence Cadence     `json:"cadence,omitempty"`
	Hour    int         `json:"hour,omitempty"`    // 0-23, used by daily/weekly
	Minute  int         `json:"minute,omitempty"`  // 0-59, used by all cadences
	Weekday int         `json:"weekday,omitempty"` // 0=Sun..6=Sat, used by weekly
	Expr    string      `json:"expr,omitempty"`    // 5-field cron expression (schedule + cron)
	At      string      `json:"at,omitempty"`      // RFC3339 pinned time (once)
}

// Run terminal-status values (mirrored onto session.SessionMeta.TerminalStatus).
const (
	StatusRunning     = "running"
	StatusSuccess     = "success"
	StatusError       = "error"
	StatusInterrupted = "interrupted"
	StatusSkipped     = "skipped"
)

// Trigger-kind values stamped onto a run's session.
const (
	KindScheduled = "scheduled"
	KindManual    = "manual"
)

// Source values record how an automation was created.
const (
	SourceManual = "manual"
	SourceAgent  = "agent"
	// Template/skill sources are recorded as "template:<id>" / "skill:<name>".
)

// Automation is the user-edited definition. It persists in
// ~/.jcode/automations.json (low-frequency, human writes). Volatile scheduler
// bookkeeping lives separately in RunState (automation-state.json) so the
// scheduler's frequent writes never collide with human edits.
type Automation struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Prompt      string  `json:"prompt"`
	Trigger     Trigger `json:"trigger"`
	ProjectPath string  `json:"project_path"` // required, must be a local path
	Mode        string  `json:"mode"`         // approval|plan|full_access
	Provider    string  `json:"provider,omitempty"`
	Model       string  `json:"model,omitempty"`
	RunInCloud  bool    `json:"run_in_cloud"` // reserved; always false in v1
	Enabled     bool    `json:"enabled"`
	Source      string  `json:"source"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// RunState is the volatile per-automation scheduler bookkeeping. It persists in
// ~/.jcode/automation-state.json, written frequently by the scheduler and the
// run-completion callback — kept apart from Automation so the two write paths
// don't clobber each other.
type RunState struct {
	LastRunAt        string `json:"last_run_at,omitempty"`
	LastStatus       string `json:"last_status,omitempty"` // running|success|error|interrupted|skipped
	LastError        string `json:"last_error,omitempty"`
	LastSessionID    string `json:"last_session_id,omitempty"`
	NextRunAt        string `json:"next_run_at,omitempty"`
	LastFiredSlot    string `json:"last_fired_slot,omitempty"` // SlotKey dedup guard (DST fall-back)
	ConsecutiveFails int    `json:"consecutive_fails,omitempty"`
}

// AutoDisableThreshold is the number of consecutive failures (missing project,
// provider gone, etc.) after which a scheduled automation auto-disables so it
// stops re-failing — and re-notifying — every night. (PRD §11 open item N.)
const AutoDisableThreshold = 5

// nowFunc is overridable in tests; production uses time.Now.
var nowFunc = time.Now
