package automation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreCRUDRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStoreDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Create(Automation{
		Name: "Nightly", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.Mode != "full_access" || a.Source != SourceManual {
		t.Fatalf("defaults not applied: %+v", a)
	}
	if got := s.List(); len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if s.Get(a.ID) == nil {
		t.Fatal("Get returned nil")
	}

	upd, err := s.Update(a.ID, func(x *Automation) { x.Name = "Renamed" })
	if err != nil || upd.Name != "Renamed" {
		t.Fatalf("update failed: %v %+v", err, upd)
	}

	// Persistence across reopen.
	s2, err := NewStoreDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Get(a.ID); got == nil || got.Name != "Renamed" {
		t.Fatalf("not persisted: %+v", got)
	}

	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Fatal("delete did not remove")
	}
}

func TestStoreTwoFileSeparation(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStoreDir(dir)
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerManual}})
	if err := s.UpdateState(a.ID, func(rs *RunState) { rs.LastStatus = StatusSuccess }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, defsFile)); err != nil {
		t.Fatalf("defs file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, stateFile)); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
	// State survives reopen and lives apart from defs.
	s2, _ := NewStoreDir(dir)
	if s2.State(a.ID).LastStatus != StatusSuccess {
		t.Fatal("state not persisted separately")
	}
}

func TestStoreCorruptStateIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewStoreDir(dir) // must not fail on corrupt state
	if err != nil {
		t.Fatalf("corrupt state should not block open: %v", err)
	}
	if len(s.List()) != 0 {
		t.Fatal("expected empty store")
	}
}

func TestStoreCreateValidates(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	if _, err := s.Create(Automation{Name: "", Prompt: "p", ProjectPath: "/x"}); err == nil {
		t.Fatal("expected validation error for empty name")
	}
}

// Re-enabling a recovered automation must clear ConsecutiveFails so a single
// later failure doesn't immediately re-disable it (regression: permanent
// re-disable loop).
func TestSetEnabledResetsConsecutiveFails(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9}, Enabled: true})

	// Drive it to the auto-disable threshold.
	disabled, err := s.UpdateStateAndMaybeDisable(a.ID, func(rs *RunState) {
		rs.ConsecutiveFails = AutoDisableThreshold
	})
	if err != nil || !disabled {
		t.Fatalf("expected auto-disable: disabled=%v err=%v", disabled, err)
	}
	if s.Get(a.ID).Enabled {
		t.Fatal("expected definition disabled at threshold")
	}
	if got := s.State(a.ID).ConsecutiveFails; got < AutoDisableThreshold {
		t.Fatalf("ConsecutiveFails = %d, want >= %d", got, AutoDisableThreshold)
	}

	// User re-enables: the counter must reset so the next single failure doesn't
	// immediately re-disable.
	if _, err := s.SetEnabled(a.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := s.State(a.ID).ConsecutiveFails; got != 0 {
		t.Fatalf("ConsecutiveFails after re-enable = %d, want 0", got)
	}
}

// UpdateStateAndMaybeDisable must not disable below the threshold, and reports
// whether it disabled.
func TestUpdateStateAndMaybeDisableThreshold(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9}, Enabled: true})

	for i := 1; i < AutoDisableThreshold; i++ {
		disabled, _ := s.UpdateStateAndMaybeDisable(a.ID, func(rs *RunState) { rs.ConsecutiveFails++ })
		if disabled {
			t.Fatalf("disabled early at fail %d", i)
		}
		if !s.Get(a.ID).Enabled {
			t.Fatalf("definition disabled early at fail %d", i)
		}
	}
	disabled, _ := s.UpdateStateAndMaybeDisable(a.ID, func(rs *RunState) { rs.ConsecutiveFails++ })
	if !disabled || s.Get(a.ID).Enabled {
		t.Fatal("expected disable at threshold")
	}
}

// The web UI re-enables an auto-disabled automation through the partial-patch
// PUT, which routes via Store.Update (not SetEnabled). Update must therefore
// also clear ConsecutiveFails on the disabled->enabled transition, and must NOT
// clear it on an unrelated edit.
func TestUpdateReEnableResetsConsecutiveFails(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9}, Enabled: true})

	if _, err := s.UpdateStateAndMaybeDisable(a.ID, func(rs *RunState) {
		rs.ConsecutiveFails = AutoDisableThreshold
	}); err != nil {
		t.Fatal(err)
	}
	if s.Get(a.ID).Enabled {
		t.Fatal("expected auto-disable")
	}

	// An edit that does NOT re-enable must leave the counter intact.
	if _, err := s.Update(a.ID, func(x *Automation) { x.Name = "renamed" }); err != nil {
		t.Fatal(err)
	}
	if got := s.State(a.ID).ConsecutiveFails; got != AutoDisableThreshold {
		t.Fatalf("unrelated edit reset the counter: %d", got)
	}

	// Re-enabling via Update (the web PUT path) must reset the counter.
	if _, err := s.Update(a.ID, func(x *Automation) { x.Enabled = true }); err != nil {
		t.Fatal(err)
	}
	if got := s.State(a.ID).ConsecutiveFails; got != 0 {
		t.Fatalf("ConsecutiveFails after web re-enable = %d, want 0", got)
	}
}

// TryMarkRunning is the atomic overlap guard: it claims only when not already
// running, and a terminal status frees the next claim.
func TestTryMarkRunning(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	a, _ := s.Create(Automation{Name: "n", Prompt: "p", ProjectPath: t.TempDir(),
		Trigger: Trigger{Type: TriggerManual}})

	if ok, _ := s.TryMarkRunning(a.ID); !ok {
		t.Fatal("first claim should succeed")
	}
	if s.State(a.ID).LastStatus != StatusRunning {
		t.Fatal("claim did not mark running")
	}
	if ok, _ := s.TryMarkRunning(a.ID); ok {
		t.Fatal("second claim must fail while running")
	}
	// Terminal status frees the slot.
	_ = s.UpdateState(a.ID, func(rs *RunState) { rs.LastStatus = StatusSuccess })
	if ok, _ := s.TryMarkRunning(a.ID); !ok {
		t.Fatal("claim should succeed after a terminal status")
	}
}

// Mutating a missing automation must report ErrNotFound so HTTP handlers can map
// it to 404 rather than 400.
func TestStoreNotFoundSentinel(t *testing.T) {
	s, _ := NewStoreDir(t.TempDir())
	if _, err := s.Update("nope", func(*Automation) {}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update: want ErrNotFound, got %v", err)
	}
	if _, err := s.SetEnabled("nope", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetEnabled: want ErrNotFound, got %v", err)
	}
	if err := s.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete: want ErrNotFound, got %v", err)
	}
}
