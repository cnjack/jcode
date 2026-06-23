package automation

import (
	"context"
	"errors"
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
