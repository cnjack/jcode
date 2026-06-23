package automation

import (
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
