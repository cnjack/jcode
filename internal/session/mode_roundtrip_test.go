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
		{[]string{"full_access"}, mode.FullAccess},
		{[]string{"approval", "plan", "full_access"}, mode.FullAccess}, // last wins
		{[]string{"plan", "approval"}, mode.Approval},
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
