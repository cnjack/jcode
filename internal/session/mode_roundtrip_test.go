package session

import (
	"testing"

	"github.com/cnjack/jcode/internal/mode"
)

// TestModeChangeRoundTrip verifies a recorded unified mode survives
// reconstruction and parses back to the same SessionMode — the persistence path
// the interactive/ACP resume logic relies on.
func TestModeChangeRoundTrip(t *testing.T) {
	cases := []struct {
		entries []string // sequence of recorded mode strings
		want    mode.SessionMode
	}{
		{[]string{"approval"}, mode.Approval},
		{[]string{"plan"}, mode.Plan},
		{[]string{"auto"}, mode.Auto},
		{[]string{"full_access"}, mode.FullAccess},
		{[]string{"approval", "plan", "auto", "full_access"}, mode.FullAccess}, // last wins
		{[]string{"plan", "approval"}, mode.Approval},
		{[]string{"auto", "approval"}, mode.Approval},
	}
	for _, c := range cases {
		entries := make([]Entry, 0, len(c.entries))
		for _, m := range c.entries {
			entries = append(entries, Entry{Type: EntryModeChange, Mode: m})
		}
		st := ReconstructState(entries)
		if got := mode.Parse(st.Mode); got != c.want {
			t.Errorf("entries=%v: reconstructed=%q parsed=%v, want %v", c.entries, st.Mode, got, c.want)
		}
	}
}

func TestStrictModeChangeSurvivesHistoryTruncation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("first turn")
	if err := recorder.RecordModeChangeStrict(mode.FullAccess.String()); err != nil {
		t.Fatal(err)
	}
	recorder.RecordUser("discarded turn")
	if err := recorder.TruncateAtUserMessage(0); err != nil {
		t.Fatal(err)
	}

	entries, err := LoadSession(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	if got := ReconstructState(entries).Mode; got != mode.FullAccess.String() {
		t.Fatalf("mode after truncate=%q, want %q", got, mode.FullAccess.String())
	}
}

func TestLoadSessionModeStrictRejectsAmbiguousCorruption(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	if err := recorder.RecordModeChangeStrict(mode.FullAccess.String()); err != nil {
		t.Fatal(err)
	}
	appendRawSessionLine(t, recorder.UUID(), `{"type":"mode_change"`)

	if _, err := LoadSessionModeStrict(recorder.UUID()); err == nil {
		t.Fatal("ambiguous malformed line was skipped during authorization restore")
	}
}
