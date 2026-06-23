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

// manualRunStaleWindow bounds how long a manual run may sit in "running" before
// reconciliation treats it as a zombie. Manual runs are uncapped and bypass the
// scheduler election, so on restart we can't prove one is dead the way we can a
// scheduled run; we only reset clearly-stale ones (older than this window).
const manualRunStaleWindow = 2 * time.Hour

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

// reconcileStale resets run-state left in "running" by a crashed owner so
// skip-if-running and the UI recover. Scheduled runs are reset unconditionally:
// winning the election lock proves the prior SCHEDULER owner is gone. Manual
// runs need a time heuristic instead — one may be executing right now in a
// DIFFERENT process (manual runs bypass the election), so resetting a fresh one
// would briefly show a bogus "interrupted" for a live cross-process run; only
// runs older than manualRunStaleWindow are treated as zombies.
func (s *Scheduler) reconcileStale() {
	now := nowFunc()
	for _, a := range s.store.List() {
		st := s.store.State(a.ID)
		if st.LastStatus != StatusRunning {
			continue
		}
		if a.Trigger.Type != TriggerSchedule {
			// Manual (or non-scheduled) run: a live one in another process has a
			// recent, valid LastRunAt (ExecuteRun stamps it atomically when it
			// claims the run). Leave those alone; reset only runs older than the
			// window — and also an empty/garbled LastRunAt, which can't be a live
			// run and would otherwise stay stuck at "running" forever.
			if t, err := time.Parse(time.RFC3339, st.LastRunAt); err == nil && now.Sub(t) < manualRunStaleWindow {
				continue
			}
		}
		_ = s.store.UpdateState(a.ID, func(st *RunState) {
			st.LastStatus = StatusInterrupted
			st.LastError = "previous run interrupted (process restart)"
		})
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
		// Skip if a scheduled run is still in flight (s.inflight) OR a manual run
		// is currently executing (LastStatus), so a scheduled fire can't overlap a
		// manual "Run Now" that races it. A crashed-run zombie left at "running" is
		// cleared by reconcileStale on the next owner election.
		if busy || st.LastStatus == StatusRunning {
			continue
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
	// State mutation and the conditional auto-disable happen in one lock scope so
	// a concurrent successful run can't reset ConsecutiveFails between them.
	_, _ = s.store.UpdateStateAndMaybeDisable(a.ID, func(rs *RunState) {
		rs.LastStatus = StatusSkipped
		rs.LastError = reason
		rs.LastRunAt = nowFunc().Format(time.RFC3339)
		rs.ConsecutiveFails++
	})
	if s.onSkip != nil {
		s.onSkip(a, reason)
	}
}

// ExecuteRun runs an automation through the runner and records terminal state.
// It blocks until completion. Shared by the scheduler (scheduled fires) and the
// manual ▶ path so state bookkeeping is identical. For scheduled runs, repeated
// errors increment ConsecutiveFails and auto-disable past the threshold.
func ExecuteRun(ctx context.Context, store *Store, runner Runner, a *Automation, kind string) (string, error) {
	// Atomically claim the run. If a run for this automation is already in
	// progress (a scheduled fire racing a manual "Run Now", or another process),
	// refuse rather than start a second agent session against the same project.
	// Returning before writing any terminal state preserves the live run's
	// status.
	claimed, _ := store.TryMarkRunning(a.ID)
	if !claimed {
		return "", fmt.Errorf("a run is already in progress for automation %q", a.ID)
	}

	sessionID, err := safeStartRun(ctx, runner, a, kind)

	if err != nil && kind == KindScheduled {
		// Scheduled failure: increment ConsecutiveFails and auto-disable past the
		// threshold atomically (single lock scope) so a concurrent success can't
		// race the disable.
		_, _ = store.UpdateStateAndMaybeDisable(a.ID, func(rs *RunState) {
			rs.LastSessionID = sessionID
			rs.LastStatus = StatusError
			rs.LastError = truncate(err.Error(), 300)
			rs.ConsecutiveFails++
		})
		return sessionID, err
	}

	_ = store.UpdateState(a.ID, func(rs *RunState) {
		rs.LastSessionID = sessionID
		if err != nil {
			// Manual failure: record the error but never auto-disable (manual runs
			// don't increment the failure counter).
			rs.LastStatus = StatusError
			rs.LastError = truncate(err.Error(), 300)
		} else {
			rs.LastStatus = StatusSuccess
			rs.LastError = ""
			rs.ConsecutiveFails = 0
		}
	})
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
