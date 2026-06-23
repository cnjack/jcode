package automation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

// schedulerLockFile is held by the single process that owns periodic firing. It
// is intentionally SEPARATE from the storage write lock (automation.lock): the
// election lock is held for the whole process lifetime, while storage writes
// take their lock briefly — conflating them would let the long-held election
// lock starve short writes.
const schedulerLockFile = "automation-scheduler.lock"

// ScheduledRunCeiling bounds a scheduled (headless) run's wall-clock time. This
// is a liveness bound, not a safety guardrail: with ask_user/automation_create
// excluded from headless runs, the remaining hang vector is an agent loop, and
// an unbounded run would hold an engine forever. Manual runs are not capped.
const ScheduledRunCeiling = 30 * time.Minute

// Runner executes one automation run to completion. Implementations (internal/web)
// reuse the Engine: build a headless engine for the automation's project + mode,
// inject the prompt, and block until the agent is done. The returned sessionID
// identifies the recorded session.
type Runner interface {
	StartRun(ctx context.Context, a *Automation, kind string) (sessionID string, err error)
}

// SkipNotifier is called when the scheduler skips a fire without running (e.g.
// the bound project is gone). Optional.
type SkipNotifier func(a *Automation, reason string)

// Scheduler owns periodic firing for the process that wins the election lock.
// Non-owner processes can still manage definitions and trigger manual runs
// (which bypass the scheduler entirely).
type Scheduler struct {
	store    *Store
	runner   Runner
	interval time.Duration
	onSkip   SkipNotifier

	mu       sync.Mutex
	inflight map[string]bool
}

// NewScheduler builds a scheduler. interval<=0 defaults to 30s.
func NewScheduler(store *Store, runner Runner) *Scheduler {
	return &Scheduler{
		store:    store,
		runner:   runner,
		interval: 30 * time.Second,
		inflight: map[string]bool{},
	}
}

// SetInterval overrides the tick interval (used in tests).
func (s *Scheduler) SetInterval(d time.Duration) {
	if d > 0 {
		s.interval = d
	}
}

// SetSkipNotifier registers a callback for skipped fires.
func (s *Scheduler) SetSkipNotifier(fn SkipNotifier) { s.onSkip = fn }

// Run blocks until ctx is cancelled. It first contends for the election lock; if
// another process owns it, Run returns immediately (this process won't fire
// scheduled runs, but manual runs still work). The flock is released by the OS
// on process exit, so a crashed owner never deadlocks the election.
func (s *Scheduler) Run(ctx context.Context) {
	lockPath := filepath.Join(s.store.dir, schedulerLockFile)
	lock, ok, err := tryAcquireLock(lockPath)
	if err != nil {
		config.Logger().Printf("[automation] scheduler lock error, not scheduling: %v", err)
		return
	}
	if !ok {
		config.Logger().Printf("[automation] another process owns scheduling; periodic runs disabled here")
		return
	}
	defer func() { _ = lock.release() }()
	config.Logger().Printf("[automation] scheduler started (owner)")

	// Acquiring the election lock means any prior owner is gone: reconcile runs
	// it left marked "running" (zombies) so skip-if-running and the UI recover.
	s.reconcileStale()

	t := time.NewTicker(s.interval)
	defer t.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

// reconcileStale resets scheduled run-state left in "running" by a crashed
// owner. It is scoped to TriggerSchedule automations only: a manual run may be
// executing right now in a DIFFERENT process (manual runs bypass the election),
// and winning the election lock proves only that the prior SCHEDULER owner is
// gone — not that someone's manual run died. Resetting those would briefly show
// a bogus "interrupted" for a live cross-process run.
func (s *Scheduler) reconcileStale() {
	for _, a := range s.store.List() {
		if a.Trigger.Type != TriggerSchedule {
			continue
		}
		if s.store.State(a.ID).LastStatus == StatusRunning {
			_ = s.store.UpdateState(a.ID, func(st *RunState) {
				st.LastStatus = StatusInterrupted
				st.LastError = "previous run interrupted (process restart)"
			})
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	now := nowFunc()
	for _, a := range s.store.List() {
		if !a.Enabled || a.Trigger.Type != TriggerSchedule {
			continue
		}
		st := s.store.State(a.ID)

		next, err := time.Parse(time.RFC3339, st.NextRunAt)
		if st.NextRunAt == "" || err != nil {
			// First time we see it (or unparseable): seed NextRunAt, don't fire.
			_ = s.store.UpdateState(a.ID, func(rs *RunState) {
				rs.NextRunAt = nextRunString(now, a.Trigger)
			})
			continue
		}
		if now.Before(next) {
			continue // not due yet
		}

		slot := SlotKey(next)
		if slot == st.LastFiredSlot {
			// Already fired this wall-clock minute (DST fall-back guard); advance.
			_ = s.store.UpdateState(a.ID, func(rs *RunState) {
				rs.NextRunAt = nextRunString(now, a.Trigger)
			})
			continue
		}

		s.mu.Lock()
		busy := s.inflight[a.ID]
		s.mu.Unlock()
		if busy {
			continue // overlap: previous run still in flight, skip this tick
		}

		// Fire-time precheck: the bound project must still exist locally.
		if !projectUsable(a.ProjectPath) {
			s.skipAndMaybeDisable(a, "project path is missing or not local: "+a.ProjectPath)
			_ = s.store.UpdateState(a.ID, func(rs *RunState) {
				rs.NextRunAt = nextRunString(now, a.Trigger)
			})
			continue
		}

		s.fire(ctx, a, slot, now)
	}
}

func (s *Scheduler) fire(ctx context.Context, a *Automation, slot string, now time.Time) {
	s.mu.Lock()
	s.inflight[a.ID] = true
	s.mu.Unlock()

	_ = s.store.UpdateState(a.ID, func(rs *RunState) {
		rs.LastFiredSlot = slot
		rs.NextRunAt = nextRunString(now, a.Trigger)
	})

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.inflight, a.ID)
			s.mu.Unlock()
		}()
		runCtx, cancel := context.WithTimeout(ctx, ScheduledRunCeiling)
		defer cancel()
		_, _ = ExecuteRun(runCtx, s.store, s.runner, a, KindScheduled)
	}()
}

func (s *Scheduler) skipAndMaybeDisable(a *Automation, reason string) {
	disabled := false
	_ = s.store.UpdateState(a.ID, func(rs *RunState) {
		rs.LastStatus = StatusSkipped
		rs.LastError = reason
		rs.LastRunAt = nowFunc().Format(time.RFC3339)
		rs.ConsecutiveFails++
		if rs.ConsecutiveFails >= AutoDisableThreshold {
			disabled = true
		}
	})
	if disabled {
		_, _ = s.store.SetEnabled(a.ID, false)
	}
	if s.onSkip != nil {
		s.onSkip(a, reason)
	}
}

// ExecuteRun runs an automation through the runner and records terminal state.
// It blocks until completion. Shared by the scheduler (scheduled fires) and the
// manual ▶ path so state bookkeeping is identical. For scheduled runs, repeated
// errors increment ConsecutiveFails and auto-disable past the threshold.
func ExecuteRun(ctx context.Context, store *Store, runner Runner, a *Automation, kind string) (string, error) {
	_ = store.UpdateState(a.ID, func(rs *RunState) {
		rs.LastStatus = StatusRunning
		rs.LastError = ""
		rs.LastRunAt = nowFunc().Format(time.RFC3339)
	})

	sessionID, err := safeStartRun(ctx, runner, a, kind)

	disable := false
	_ = store.UpdateState(a.ID, func(rs *RunState) {
		rs.LastSessionID = sessionID
		if err != nil {
			rs.LastStatus = StatusError
			rs.LastError = truncate(err.Error(), 300)
			if kind == KindScheduled {
				rs.ConsecutiveFails++
				if rs.ConsecutiveFails >= AutoDisableThreshold {
					disable = true
				}
			}
		} else {
			rs.LastStatus = StatusSuccess
			rs.LastError = ""
			rs.ConsecutiveFails = 0
		}
	})
	if disable {
		_, _ = store.SetEnabled(a.ID, false)
	}
	return sessionID, err
}

// safeStartRun runs the runner with a recover guard so a panic in one
// automation run (the agent/engine stack is large and concurrent) becomes a
// recorded StatusError instead of crashing the whole host process — which would
// take down the web UI / TUI / scheduler with it.
func safeStartRun(ctx context.Context, runner Runner, a *Automation, kind string) (sessionID string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("automation run panicked: %v", r)
		}
	}()
	return runner.StartRun(ctx, a, kind)
}

func nextRunString(after time.Time, t Trigger) string {
	if n, ok := ComputeNextRun(after, t); ok {
		return n.Format(time.RFC3339)
	}
	return ""
}

func projectUsable(p string) bool {
	if !IsLocalPath(p) {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
