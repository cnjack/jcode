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
		{[]string{"ask"}, mode.Ask},
		{[]string{"plan"}, mode.Plan},
		{[]string{"autopilot"}, mode.Autopilot},
		{[]string{"ask", "plan", "autopilot"}, mode.Autopilot}, // last wins
		{[]string{"plan", "ask"}, mode.Ask},
		// Legacy tool-axis strings written before the unified selector existed.
		{[]string{"planning"}, mode.Plan},
		{[]string{"normal"}, mode.Ask},
		{[]string{"executing"}, mode.Ask},
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
