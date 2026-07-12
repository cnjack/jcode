package tui

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

// TestFormatTodoWriteOutputCompact pins the collapsed todowrite timeline row:
// one muted "✓ Todos N/M · current" line, never the list body (the sidebar
// panel owns the full list).
func TestFormatTodoWriteOutputCompact(t *testing.T) {
	out := "3 todos (1 completed, 1 in_progress, 1 pending, 0 cancelled)\n" +
		`[{"id":1,"title":"done thing","status":"completed"},` +
		`{"id":2,"title":"current thing","status":"in_progress"},` +
		`{"id":3,"title":"later thing","status":"pending"}]`

	lines := formatTodoWriteOutput(out)
	if len(lines) != 1 {
		t.Fatalf("expected a single summary line, got %d", len(lines))
	}
	got := xansi.Strip(lines[0])
	if !strings.Contains(got, "Todos 1/3") {
		t.Errorf("missing count summary: %q", got)
	}
	if !strings.Contains(got, "current thing") {
		t.Errorf("missing in-progress task: %q", got)
	}
	for _, leaked := range []string{"done thing", "later thing"} {
		if strings.Contains(got, leaked) {
			t.Errorf("list body leaked into timeline (%q): %q", leaked, got)
		}
	}
}

// TestFormatTodoWriteOutputEnhanced covers the enhanced tool's summary
// wording ("Updated. … not_started … skipped") — same regex, same line.
func TestFormatTodoWriteOutputEnhanced(t *testing.T) {
	out := "Updated. 2 todos (2 completed, 0 in_progress, 0 not_started, 0 skipped)\n[]"
	got := xansi.Strip(formatTodoWriteOutput(out)[0])
	if !strings.Contains(got, "Todos 2/2") {
		t.Errorf("enhanced summary not parsed: %q", got)
	}
}

// TestFormatTodoWriteOutputFallback keeps unknown formats on the legacy
// first-line rendering instead of dropping them.
func TestFormatTodoWriteOutputFallback(t *testing.T) {
	got := xansi.Strip(formatTodoWriteOutput("something unrecognized")[0])
	if !strings.Contains(got, "something unrecognized") {
		t.Errorf("fallback lost the summary: %q", got)
	}
	if got2 := xansi.Strip(formatTodoWriteOutput("")[0]); !strings.Contains(got2, "updated") {
		t.Errorf("empty output fallback wrong: %q", got2)
	}
}
