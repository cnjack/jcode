package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{5 * time.Second, "5s"},
		{59 * time.Second, "59s"},
		{65 * time.Second, "1m 05s"},
		{59*time.Minute + 59*time.Second, "59m 59s"},
		{time.Hour + 2*time.Minute, "1h 02m"},
		{-3 * time.Second, "0s"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.d); got != c.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestRunElapsed(t *testing.T) {
	t0 := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)

	// Zero start → zero elapsed.
	if got := runElapsed(time.Time{}, t0, 0, time.Time{}); got != 0 {
		t.Errorf("zero start: got %v, want 0", got)
	}

	// Plain wall time, no pauses.
	if got := runElapsed(t0, t0.Add(42*time.Second), 0, time.Time{}); got != 42*time.Second {
		t.Errorf("no pause: got %v, want 42s", got)
	}

	// Accumulated pauses are subtracted.
	if got := runElapsed(t0, t0.Add(60*time.Second), 10*time.Second, time.Time{}); got != 50*time.Second {
		t.Errorf("paused total: got %v, want 50s", got)
	}

	// An open pause freezes the clock at the pause boundary: no matter how
	// long the approval dialog stays up, elapsed stays at 30s.
	pauseAt := t0.Add(30 * time.Second)
	for _, now := range []time.Time{t0.Add(31 * time.Second), t0.Add(10 * time.Minute)} {
		if got := runElapsed(t0, now, 0, pauseAt); got != 30*time.Second {
			t.Errorf("open pause at now=%v: got %v, want 30s", now, got)
		}
	}

	// Never negative.
	if got := runElapsed(t0, t0.Add(60*time.Second), 2*time.Minute, time.Time{}); got != 0 {
		t.Errorf("over-paused: got %v, want 0", got)
	}
}

// TestApprovalPausesRunClock drives the approval dialog lifecycle and asserts
// the pause bookkeeping: showApproval opens exactly one pause (idempotent for
// queued dialogs) and resolveApproval folds it into runPausedTotal.
func TestApprovalPausesRunClock(t *testing.T) {
	m := newToolTestModel()
	m.textarea = newTextarea()
	m.promptStartTime = time.Now().Add(-time.Minute)

	m.showApproval(ToolApprovalRequestMsg{Name: "execute"})
	if m.runPauseStart.IsZero() {
		t.Fatal("showApproval did not open a pause")
	}
	first := m.runPauseStart

	// A queued second dialog keeps the original pause boundary.
	m.beginRunPause()
	if !m.runPauseStart.Equal(first) {
		t.Fatal("beginRunPause not idempotent")
	}

	elapsedBefore := m.currentRunElapsed()
	time.Sleep(15 * time.Millisecond)
	if got := m.currentRunElapsed(); got != elapsedBefore {
		t.Fatalf("elapsed advanced during open pause: %v → %v", elapsedBefore, got)
	}

	m.resolveApproval(ToolApprovalResponse{Approved: true, Mode: ModeManual})
	if !m.runPauseStart.IsZero() {
		t.Fatal("resolveApproval left the pause open")
	}
	if m.runPausedTotal <= 0 {
		t.Fatalf("pause not folded into runPausedTotal: %v", m.runPausedTotal)
	}
}

// TestAppendStatusLineStructure asserts the structured status block: verb +
// (elapsed · esc interrupt) meta, tool detail row, and batch count.
func TestAppendStatusLineStructure(t *testing.T) {
	m := newToolTestModel()
	m.spinner = spinner.New()
	m.thinking = true
	m.promptStartTime = time.Now().Add(-5 * time.Second)
	m.pendingTool = "execute"
	m.pendingToolTitle = "Shell"
	m.pendingToolSubtitle = "git push origin main"
	m.runningTools = 1

	var sb strings.Builder
	m.appendStatusLine(&sb)
	// The shimmer colors "Working" per character; strip ANSI so the
	// structural assertions see plain text.
	out := xansi.Strip(sb.String())

	for _, want := range []string{"Working", "esc interrupt", "5s", "└", "Shell", "git push origin main"} {
		if !strings.Contains(out, want) {
			t.Errorf("status line missing %q:\n%s", want, out)
		}
	}

	// Concurrent batch: the detail row shows the in-flight count instead.
	m.runningTools = 3
	sb.Reset()
	m.appendStatusLine(&sb)
	if out := sb.String(); !strings.Contains(out, "3 tools running") {
		t.Errorf("batch status missing count:\n%s", out)
	}

	// No pending tool but one in flight (result raced ahead of the next
	// call): generic single-tool detail.
	m.runningTools = 1
	m.pendingTool = ""
	sb.Reset()
	m.appendStatusLine(&sb)
	if out := sb.String(); !strings.Contains(out, "1 tool running") {
		t.Errorf("single running tool detail missing:\n%s", out)
	}

	// Idle between tools: no detail row.
	m.runningTools = 0
	sb.Reset()
	m.appendStatusLine(&sb)
	if out := sb.String(); strings.Contains(out, "└") {
		t.Errorf("unexpected detail row when idle:\n%s", out)
	}
}
