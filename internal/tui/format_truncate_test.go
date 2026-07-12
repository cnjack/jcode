package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// numberedLines returns n lines "line01".."lineNN" joined by newlines.
func numberedLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%02d", i+1)
	}
	return strings.Join(lines, "\n")
}

// TestFormatDefaultOutputHeadTail asserts that long default output keeps both
// the head and the tail (tails often carry the error), with a logical-line
// hidden count in between.
func TestFormatDefaultOutputHeadTail(t *testing.T) {
	out := formatDefaultOutput(numberedLines(20), 80)
	if len(out) != 1 {
		t.Fatalf("expected a single box, got %d", len(out))
	}
	box := out[0]

	for _, want := range []string{"line01", "line03", "line18", "line20"} {
		if !strings.Contains(box, want) {
			t.Errorf("box missing %q:\n%s", want, box)
		}
	}
	// 20 lines − 3 head − 3 tail = 14 hidden, with the transcript pointer.
	if !strings.Contains(box, "+14 lines") || !strings.Contains(box, transcriptHint) {
		t.Errorf("box missing hidden-lines marker:\n%s", box)
	}
	if strings.Contains(box, "line10") {
		t.Errorf("middle line leaked into truncated box:\n%s", box)
	}
}

// TestFormatDefaultOutputNoTruncateWhenShort pins that content within the
// head+tail+1 row budget renders unmodified.
func TestFormatDefaultOutputNoTruncateWhenShort(t *testing.T) {
	box := formatDefaultOutput(numberedLines(7), 80)[0]
	for i := 1; i <= 7; i++ {
		if want := fmt.Sprintf("line%02d", i); !strings.Contains(box, want) {
			t.Errorf("short output missing %q", want)
		}
	}
	if strings.Contains(box, transcriptHint) {
		t.Errorf("short output must not carry a truncation marker:\n%s", box)
	}
}

// TestFormatDefaultOutputLongSingleLine asserts that one overlong line (which
// would soft-wrap into dozens of rows) is capped to the row budget instead of
// blowing up the viewport.
func TestFormatDefaultOutputLongSingleLine(t *testing.T) {
	long := strings.Repeat("x", 2000)
	box := formatDefaultOutput(long, 80)[0]

	// Capped head rows + 2 empty border rows from the box style. The uncapped
	// line would render ~29 rows at this width.
	if h := lipgloss.Height(box); h > 9 {
		t.Fatalf("overlong single line rendered %d rows, want <= 9:\n%s", h, box)
	}
	if !strings.Contains(box, "…") {
		t.Errorf("capped line should end with an ellipsis:\n%s", box)
	}
}

// TestFormatDefaultOutputMixedLongLine asserts head/tail budgeting is
// row-aware: a wrapped long line consumes several rows of the budget, and the
// last logical line still survives.
func TestFormatDefaultOutputMixedLongLine(t *testing.T) {
	lines := []string{
		strings.Repeat("a", 500), // wraps to ~8 rows at width 80
		"mid1", "mid2", "mid3", "mid4",
		"FINAL_ERROR_LINE",
	}
	box := formatDefaultOutput(strings.Join(lines, "\n"), 80)[0]

	if !strings.Contains(box, "FINAL_ERROR_LINE") {
		t.Errorf("tail line lost:\n%s", box)
	}
	// ≤ 2*3+1 content rows + 2 empty border rows from the box style.
	if h := lipgloss.Height(box); h > 9 {
		t.Fatalf("row-aware budget exceeded: %d rows:\n%s", h, box)
	}
}

// TestFormatExecuteOutputTail pins the execute tail strategy: last rows kept,
// hidden logical-line count on top, unified marker copy.
func TestFormatExecuteOutputTail(t *testing.T) {
	box := formatExecuteOutput(numberedLines(10), 80)[0]

	for _, want := range []string{"line06", "line10"} {
		if !strings.Contains(box, want) {
			t.Errorf("execute tail missing %q:\n%s", want, box)
		}
	}
	if strings.Contains(box, "line05") {
		t.Errorf("hidden head line leaked:\n%s", box)
	}
	if !strings.Contains(box, "+5 lines") || !strings.Contains(box, transcriptHint) {
		t.Errorf("execute box missing hidden-lines marker:\n%s", box)
	}
}

// TestFormatExecuteOutputLongLine asserts the execute tail is row-aware: an
// overlong line in the tail window is capped instead of flooding the box.
func TestFormatExecuteOutputLongLine(t *testing.T) {
	output := strings.Repeat("y", 2000) + "\ndone"
	box := formatExecuteOutput(output, 80)[0]

	if !strings.Contains(box, "done") {
		t.Errorf("final line lost:\n%s", box)
	}
	// 5 tail rows + 2 empty border rows from the box style.
	if h := lipgloss.Height(box); h > 7 {
		t.Fatalf("execute tail exceeded row budget: %d rows:\n%s", h, box)
	}
	if !strings.Contains(box, "…") {
		t.Errorf("capped line should carry an ellipsis:\n%s", box)
	}
}

// TestFormatExecuteOutputShort pins that short execute output is untouched.
func TestFormatExecuteOutputShort(t *testing.T) {
	box := formatExecuteOutput("one\ntwo", 80)[0]
	if !strings.Contains(box, "one") || !strings.Contains(box, "two") {
		t.Errorf("short execute output mangled:\n%s", box)
	}
	if strings.Contains(box, transcriptHint) {
		t.Errorf("short execute output must not carry a marker:\n%s", box)
	}
}

// TestTakeDisplayRows exercises the row-budget picker directly.
func TestTakeDisplayRows(t *testing.T) {
	lines := []string{"aa", "bb", "cc", "dd"}

	kept, taken := takeDisplayRows(lines, 2, 10, false)
	if taken != 2 || len(kept) != 2 || kept[0] != "aa" || kept[1] != "bb" {
		t.Fatalf("head take wrong: kept=%v taken=%d", kept, taken)
	}

	kept, taken = takeDisplayRows(lines, 2, 10, true)
	if taken != 2 || len(kept) != 2 || kept[0] != "cc" || kept[1] != "dd" {
		t.Fatalf("tail take wrong: kept=%v taken=%d", kept, taken)
	}

	// A line wrapping to 3 rows at width 4 against a 2-row budget gets capped.
	kept, taken = takeDisplayRows([]string{"abcdefghij"}, 2, 4, false)
	if taken != 1 || len(kept) != 1 {
		t.Fatalf("overflow take wrong: kept=%v taken=%d", kept, taken)
	}
	if rows := displayRows(kept[0], 4); rows > 2 {
		t.Fatalf("capped line still occupies %d rows: %q", rows, kept[0])
	}
	if !strings.HasSuffix(kept[0], "…") {
		t.Fatalf("capped line missing ellipsis: %q", kept[0])
	}
}
