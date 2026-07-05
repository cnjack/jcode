package runner

import "testing"

func TestContinuationSource(t *testing.T) {
	const cap = 3
	cases := []struct {
		name           string
		todoIncomplete bool
		todoUsed       int
		goalActive     bool
		stopBlock      bool
		want           string
	}{
		{"todo wins over goal and stop", true, 0, true, true, "todo"},
		{"todo within cap", true, 2, false, false, "todo"},
		{"todo exhausted falls to goal", true, cap, true, false, "goal"},
		{"todo exhausted falls to stop", true, cap, false, true, "stop"},
		{"goal beats stop", false, 0, true, true, "goal"},
		{"stop only", false, 0, false, true, "stop"},
		{"nothing continues", false, 0, false, false, ""},
		{"todo cap zero skips todo", true, 0, false, true, "stop"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			todoCap := cap
			if c.name == "todo cap zero skips todo" {
				todoCap = 0
			}
			got := continuationSource(c.todoIncomplete, c.todoUsed, todoCap, c.goalActive, c.stopBlock)
			if got != c.want {
				t.Errorf("continuationSource=%q want %q", got, c.want)
			}
		})
	}
}

// TestContinuationBounded proves the loop is bounded: repeatedly asking for the
// next source while incrementing todoUsed converges (todo eventually yields to
// the umbrella budget rather than looping forever).
func TestContinuationBounded(t *testing.T) {
	todoUsed := 0
	laps := 0
	for i := 0; i < 25; i++ { // umbrella budget
		src := continuationSource(true /*todo always incomplete*/, todoUsed, 3, false, false)
		if src == "" {
			break
		}
		if src == "todo" {
			todoUsed++
		}
		laps++
	}
	if laps != 3 {
		t.Errorf("todo-only continuation ran %d laps, want 3 (sub-cap), then stops", laps)
	}
}
