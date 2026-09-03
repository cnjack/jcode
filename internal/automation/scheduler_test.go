package automation

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	mu    sync.Mutex
	calls int
	sid   string
	err   error
}

func (f *fakeRunner) StartRun(_ context.Context, _ *Automation, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.sid, f.err
}

func (f *fakeRunner) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

func waitFor(t *testing.T, cond func() bool, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func TestExecuteRun_SuccessThenError_AutoDisable(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9}, Enabled: true})

	okRunner := &fakeRunner{sid: "sess1"}
	if _, err := ExecuteRun(context.Background(), s, okRunner, a, KindScheduled); err != nil {
		t.Fatal(err)
	}
	st := s.State(a.ID)
	if st.LastStatus != StatusSuccess || st.LastSessionID != "sess1" || st.ConsecutiveFails != 0 {
		t.Fatalf("success state wrong: %+v", st)
	}

	failRunner := &fakeRunner{err: errors.New("boom")}
	for i := 0; i < AutoDisableThreshold; i++ {
		_, _ = ExecuteRun(context.Background(), s, failRunner, a, KindScheduled)
	}
	st = s.State(a.ID)
	if st.LastStatus != StatusError || st.ConsecutiveFails < AutoDisableThreshold {
		t.Fatalf("error state wrong: %+v", st)
	}
	if s.Get(a.ID).Enabled {
		t.Fatal("expected auto-disable after repeated failures")
	}
}

// ExecuteRun must refuse to start a second run while one is already in progress
// (atomic claim), so a scheduled fire racing a manual "Run Now" can't launch two
// agent sessions against the same project.
func TestExecuteRun_RefusesConcurrent(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9}, Enabled: true})

	// Simulate a run already in flight.
	if ok, _ := s.TryMarkRunning(a.ID); !ok {
		t.Fatal("setup claim failed")
	}

	r := &fakeRunner{sid: "sess"}
	if _, err := ExecuteRun(context.Background(), s, r, a, KindScheduled); err == nil {
		t.Fatal("expected ExecuteRun to refuse a concurrent run")
	}
	if r.count() != 0 {
		t.Fatal("runner must not be invoked when a run is already in progress")
	}
	// The live run's status must be untouched (not clobbered to error).
	if s.State(a.ID).LastStatus != StatusRunning {
		t.Fatalf("refused run clobbered the live status: %s", s.State(a.ID).LastStatus)
	}
}

type panicRunner struct{}

func (panicRunner) StartRun(_ context.Context, _ *Automation, _ string) (string, error) {
	panic("boom")
}

func TestExecuteRun_RecoversPanic(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerManual}})
	// A panicking run must not crash the process — it becomes a recorded error.
	_, err := ExecuteRun(context.Background(), s, panicRunner{}, a, KindManual)
	if err == nil {
		t.Fatal("expected error from recovered panic")
	}
	if s.State(a.ID).LastStatus != StatusError {
		t.Fatalf("expected error status, got %q", s.State(a.ID).LastStatus)
	}
}

func TestSchedulerTick_SeedsThenFires(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9}, Enabled: true})
	r := &fakeRunner{sid: "sess1"}
	sch := NewScheduler(s, r)

	// First sight: seeds NextRunAt, does NOT fire.
	sch.tick(context.Background())
	if r.count() != 0 {
		t.Fatal("should not fire on first sight")
	}
	if s.State(a.ID).NextRunAt == "" {
		t.Fatal("NextRunAt not seeded")
	}

	// Make it due, then tick again → fires.
	if err := s.UpdateState(a.ID, func(rs *RunState) {
		rs.NextRunAt = time.Now().Add(-time.Minute).Format(time.RFC3339)
		rs.LastFiredSlot = ""
	}); err != nil {
		t.Fatal(err)
	}
	sch.tick(context.Background())
	waitFor(t, func() bool { return r.count() == 1 }, 2*time.Second)
	waitFor(t, func() bool { return s.State(a.ID).LastStatus == StatusSuccess }, 2*time.Second)
}

func TestSchedulerTick_SlotDedup(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9}, Enabled: true})
	r := &fakeRunner{}
	sch := NewScheduler(s, r)

	due := time.Now().Add(-time.Minute)
	_ = s.UpdateState(a.ID, func(rs *RunState) {
		rs.NextRunAt = due.Format(time.RFC3339)
		rs.LastFiredSlot = SlotKey(due) // already fired this slot
	})
	sch.tick(context.Background())
	time.Sleep(50 * time.Millisecond)
	if r.count() != 0 {
		t.Fatal("slot dedup failed: fired an already-fired slot")
	}
}

func TestSchedulerTick_SkipMissingProject(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: "/no/such/dir/really",
		Trigger: Trigger{Type: TriggerManual}})
	// Force it into a schedule that's due (bypass Create's manual trigger).
	_, _ = s.Update(a.ID, func(x *Automation) {
		x.Trigger = Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9}
		x.Enabled = true
	})
	r := &fakeRunner{}
	sch := NewScheduler(s, r)
	for i := 0; i < AutoDisableThreshold; i++ {
		_ = s.UpdateState(a.ID, func(rs *RunState) {
			rs.NextRunAt = time.Now().Add(-time.Minute).Format(time.RFC3339)
			rs.LastFiredSlot = ""
		})
		sch.tick(context.Background())
	}
	if r.count() != 0 {
		t.Fatal("should not run with a missing project")
	}
	if s.Get(a.ID).Enabled {
		t.Fatal("missing-project automation should auto-disable")
	}
}

func TestSchedulerTick_SkipWhenInflight(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9}, Enabled: true})
	r := &fakeRunner{}
	sch := NewScheduler(s, r)
	sch.inflight[a.ID] = true // pretend a prior run is still going

	_ = s.UpdateState(a.ID, func(rs *RunState) {
		rs.NextRunAt = time.Now().Add(-time.Minute).Format(time.RFC3339)
	})
	sch.tick(context.Background())
	time.Sleep(50 * time.Millisecond)
	if r.count() != 0 {
		t.Fatal("overlap guard failed: fired while a run was in flight")
	}
}

// A once trigger fires exactly once via the scheduler and is then
// auto-disarmed (Enabled=false) — the definition is kept for review.
func TestSchedulerTick_OnceFiresThenDisarms(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	// Create with a future pin, then age it to already-due (Create rejects
	// past times by design).
	at := time.Now().Add(-time.Minute)
	a, _ := s.Create(Automation{Name: "one-shot", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerOnce, At: time.Now().Add(time.Hour).Format(time.RFC3339)}, Enabled: true})
	if _, err := s.Update(a.ID, func(x *Automation) { x.Trigger.At = at.Format(time.RFC3339) }); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{sid: "sess1"}
	sch := NewScheduler(s, r)

	// Seed, then make it due.
	sch.tick(context.Background())
	_ = s.UpdateState(a.ID, func(rs *RunState) {
		rs.NextRunAt = at.Format(time.RFC3339)
		rs.LastFiredSlot = ""
	})
	sch.tick(context.Background())
	waitFor(t, func() bool { return r.count() == 1 }, 2*time.Second)
	waitFor(t, func() bool { return s.State(a.ID).LastStatus == StatusSuccess }, 2*time.Second)

	got := s.Get(a.ID)
	if got.Enabled {
		t.Fatal("fired once-trigger must be auto-disarmed")
	}

	// No further ticks fire it again.
	sch.tick(context.Background())
	time.Sleep(50 * time.Millisecond)
	if r.count() != 1 {
		t.Fatalf("once fired %d times, want 1", r.count())
	}
}

// A manual "Run Now" of a once automation is a preview: it must NOT consume
// the scheduled fire.
func TestExecuteRun_ManualRunDoesNotDisarmOnce(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	at := time.Now().Add(time.Hour)
	a, _ := s.Create(Automation{Name: "one-shot", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerOnce, At: at.Format(time.RFC3339)}, Enabled: true})

	if _, err := ExecuteRun(context.Background(), s, &fakeRunner{sid: "s"}, a, KindManual); err != nil {
		t.Fatal(err)
	}
	if !s.Get(a.ID).Enabled {
		t.Fatal("manual run must not disarm a once trigger")
	}
}

// Late delivery: a once whose pinned time passed while the scheduler wasn't
// looking (created for the current minute, scheduler downtime) still fires —
// exactly once — on the next tick, then is disarmed. Re-enabling the consumed
// definition must NOT fire it again.
func TestSchedulerTick_OnceLateDelivery(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	past := time.Now().Add(-time.Hour)
	a, _ := s.Create(Automation{Name: "late", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerOnce, At: time.Now().Add(time.Hour).Format(time.RFC3339)}, Enabled: true})
	// Simulate a pin that expired before any scheduler saw it.
	if _, err := s.Update(a.ID, func(x *Automation) { x.Trigger.At = past.Format(time.RFC3339) }); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{sid: "sess1"}
	sch := NewScheduler(s, r)

	sch.tick(context.Background()) // seeds NextRunAt=At (late delivery)
	if got := s.State(a.ID).NextRunAt; got != past.Format(time.RFC3339) {
		t.Fatalf("late-delivery seed = %q, want pinned At", got)
	}
	sch.tick(context.Background())
	waitFor(t, func() bool { return r.count() == 1 }, 2*time.Second)
	waitFor(t, func() bool { return s.State(a.ID).LastStatus == StatusSuccess }, 2*time.Second)
	if s.Get(a.ID).Enabled {
		t.Fatal("late-delivered once must be disarmed after firing")
	}

	// Re-enable the consumed definition: seeding must not re-arm it.
	if _, err := s.SetEnabled(a.ID, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		sch.tick(context.Background())
	}
	time.Sleep(50 * time.Millisecond)
	if r.count() != 1 {
		t.Fatalf("consumed once fired again (%d runs), want exactly 1", r.count())
	}
}

// A consumed once (LastFiredSlot set, still enabled — e.g. disarm failed) is
// inert: the scheduler neither fires nor rewrites its state.
func TestSchedulerTick_ConsumedOnce_NoFireNoWrites(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	hourAgo := time.Now().Add(-time.Hour)
	a, _ := s.Create(Automation{Name: "consumed", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerOnce, At: time.Now().Add(time.Hour).Format(time.RFC3339)}, Enabled: true})
	if _, err := s.Update(a.ID, func(x *Automation) {
		x.Trigger.At = hourAgo.Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	_ = s.UpdateState(a.ID, func(rs *RunState) { rs.LastFiredSlot = SlotKey(hourAgo) })

	sch := NewScheduler(s, &fakeRunner{sid: "s"})
	before := s.State(a.ID)
	for i := 0; i < 3; i++ {
		sch.tick(context.Background())
	}
	time.Sleep(30 * time.Millisecond)
	if s.State(a.ID) != before {
		t.Fatalf("consumed once state changed: %+v -> %+v", before, s.State(a.ID))
	}
}

// The cron cadence wires through the scheduler's NextRunAt advance math.
func TestSchedulerTick_CronFiresAndAdvances(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "cron", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerSchedule, Cadence: CadenceCron, Expr: "30 9 * * *"}, Enabled: true})
	r := &fakeRunner{sid: "sess1"}
	sch := NewScheduler(s, r)

	due := time.Now().Add(-time.Minute)
	_ = s.UpdateState(a.ID, func(rs *RunState) {
		rs.NextRunAt = due.Format(time.RFC3339)
		rs.LastFiredSlot = ""
	})
	sch.tick(context.Background())
	waitFor(t, func() bool { return r.count() == 1 }, 2*time.Second)

	next, ok := ComputeNextRun(due, a.Trigger)
	if !ok {
		t.Fatal("cron trigger must have a next fire")
	}
	if got := s.State(a.ID).NextRunAt; got != next.Format(time.RFC3339) {
		t.Fatalf("cron NextRunAt advanced to %q, want %q", got, next.Format(time.RFC3339))
	}
}

// The scheduled claim of a due once must not consume the trigger when it is
// refused (a manual run already claimed it): the slot stays re-fireable and a
// later tick runs it.
func TestExecuteRun_OnceRefusedClaimStaysFireable(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "raced", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerOnce, At: time.Now().Add(time.Hour).Format(time.RFC3339)}, Enabled: true})
	past := time.Now().Add(-time.Minute)
	if _, err := s.Update(a.ID, func(x *Automation) { x.Trigger.At = past.Format(time.RFC3339) }); err != nil {
		t.Fatal(err)
	}

	// A manual run holds the claim; the scheduled fire is refused.
	if ok, _ := s.TryMarkRunning(a.ID); !ok {
		t.Fatal("setup claim failed")
	}
	if _, err := ExecuteRun(context.Background(), s, &fakeRunner{sid: "sched"}, a, KindScheduled); err == nil {
		t.Fatal("expected refused scheduled claim")
	}
	// Nothing consumed: still armed, pinned time intact, slot not stamped.
	got := s.Get(a.ID)
	if !got.Enabled || got.Trigger.At == "" {
		t.Fatalf("refused claim must not consume the trigger: %+v", got)
	}
	if st := s.State(a.ID); st.LastFiredSlot != "" {
		t.Fatalf("refused claim stamped the slot: %+v", st)
	}

	// Once the manual run releases, the next scheduled claim fires.
	_ = s.UpdateState(a.ID, func(rs *RunState) { rs.LastStatus = StatusSuccess })
	if _, err := ExecuteRun(context.Background(), s, &fakeRunner{sid: "sched2"}, a, KindScheduled); err != nil {
		t.Fatal(err)
	}
	if s.Get(a.ID).Enabled {
		t.Fatal("successful scheduled fire must disarm")
	}
}

// A due once whose project vanished is skipped, NOT consumed: the pinned time
// stays and it retries on later ticks until the project is back or the
// consecutive-fail auto-disable trips.
func TestSchedulerTick_OnceMissingProjectRetries(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	missing := filepath.Join(t.TempDir(), "vanished")
	a, _ := s.Create(Automation{Name: "gone", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerOnce, At: time.Now().Add(time.Hour).Format(time.RFC3339)}, Enabled: true})
	past := time.Now().Add(-time.Minute)
	if _, err := s.Update(a.ID, func(x *Automation) {
		x.Trigger.At = past.Format(time.RFC3339)
		x.ProjectPath = missing
	}); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{}
	sch := NewScheduler(s, r)

	// Each retry cycle is two ticks (re-seed the pin, then skip it), so run
	// enough cycles for ConsecutiveFails to reach the auto-disable threshold.
	for i := 0; i < AutoDisableThreshold*2+1; i++ {
		sch.tick(context.Background())
	}
	if r.count() != 0 {
		t.Fatal("must never run with a missing project")
	}
	got := s.Get(a.ID)
	if got.Enabled {
		t.Fatal("repeated skips should trip the auto-disable")
	}
	if st := s.State(a.ID); st.LastFiredSlot != "" {
		t.Fatalf("skips must not consume the once slot: %+v", st)
	}
}

// A crashed scheduled once-run is a SCHEDULER zombie: reconcileStale resets it
// unconditionally on election, not with the 2-hour manual heuristic.
func TestReconcileStale_OnceIsScheduledClass(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "zombie once", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerOnce, At: time.Now().Add(time.Hour).Format(time.RFC3339)}, Enabled: true})
	_ = s.UpdateState(a.ID, func(rs *RunState) { rs.LastStatus = StatusRunning })

	NewScheduler(s, &fakeRunner{}).reconcileStale()

	if got := s.State(a.ID).LastStatus; got != StatusInterrupted {
		t.Fatalf("crashed once-run not reset: %s", got)
	}
}

// reconcileStale must reset scheduled zombies unconditionally, reset only
// clearly-stale manual zombies (older than manualRunStaleWindow), and leave a
// fresh manual run — which may be live in another process — untouched.
func TestReconcileStale_ManualHeuristic(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	sch := NewScheduler(s, &fakeRunner{})

	sched, _ := s.Create(Automation{Name: "sched", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9}, Enabled: true})
	staleManual, _ := s.Create(Automation{Name: "stale", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerManual}})
	freshManual, _ := s.Create(Automation{Name: "fresh", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerManual}})

	_ = s.UpdateState(sched.ID, func(rs *RunState) { rs.LastStatus = StatusRunning })
	_ = s.UpdateState(staleManual.ID, func(rs *RunState) {
		rs.LastStatus = StatusRunning
		rs.LastRunAt = time.Now().Add(-3 * time.Hour).Format(time.RFC3339)
	})
	_ = s.UpdateState(freshManual.ID, func(rs *RunState) {
		rs.LastStatus = StatusRunning
		rs.LastRunAt = time.Now().Format(time.RFC3339)
	})

	sch.reconcileStale()

	if got := s.State(sched.ID).LastStatus; got != StatusInterrupted {
		t.Fatalf("scheduled zombie not reset: %s", got)
	}
	if got := s.State(staleManual.ID).LastStatus; got != StatusInterrupted {
		t.Fatalf("stale manual zombie not reset: %s", got)
	}
	if got := s.State(freshManual.ID).LastStatus; got != StatusRunning {
		t.Fatalf("fresh manual run was reset (may be live in another process): %s", got)
	}
}
