package tui

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// newToolTestModel returns a minimal Model that can process ToolCallMsg /
// ToolResultMsg without a running BubbleTea program (ready=false skips all
// viewport work).
func newToolTestModel() Model {
	return Model{currentText: &strings.Builder{}, textarea: newTextarea()}
}

// lineAt returns the raw text of m.lines[idx], failing the test on overflow.
func lineAt(t *testing.T, m *Model, idx int) string {
	t.Helper()
	if idx < 0 || idx >= len(m.lines) {
		t.Fatalf("line index %d out of range (have %d lines)", idx, len(m.lines))
	}
	return m.lines[idx].text
}

// groupAt returns the activity group of m.lines[idx], failing the test when
// the line is not a group line.
func groupAt(t *testing.T, m *Model, idx int) *activityGroupData {
	t.Helper()
	if idx < 0 || idx >= len(m.lines) {
		t.Fatalf("line index %d out of range (have %d lines)", idx, len(m.lines))
	}
	g := m.lines[idx].group
	if g == nil {
		t.Fatalf("line %d is not an activity group line: %q", idx, m.lines[idx].text)
	}
	return g
}

// renderLineAt renders m.lines[idx] at a fixed width.
func renderLineAt(t *testing.T, m *Model, idx int) string {
	t.Helper()
	if idx < 0 || idx >= len(m.lines) {
		t.Fatalf("line index %d out of range (have %d lines)", idx, len(m.lines))
	}
	return m.lines[idx].render(100, nil)
}

// TestToolBatchOutOfOrderResults drives a 3-tool concurrent batch whose
// results return out of order and asserts the structured activity-group
// behavior: all members coalesce into ONE group line, each result flips its
// own member (by toolCallID) while the group stays in live form, and once
// the whole batch has completed the line collapses to a single category-count
// summary — ✗ with a failed count because one member failed.
func TestToolBatchOutOfOrderResults(t *testing.T) {
	m := newToolTestModel()

	for i, id := range []string{"c1", "c2", "c3"} {
		m.Update(ToolCallMsg{
			Name: "read", Args: "{}", Title: "Read",
			ToolCallID: id, BatchID: "b1", BatchIndex: i, BatchSize: 3,
			StartedAt: time.Now(),
		})
	}

	// All three calls live in one structured group line.
	if len(m.lines) != 1 {
		t.Fatalf("expected 1 group line, got %d lines", len(m.lines))
	}
	g := groupAt(t, &m, 0)
	if len(g.members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(g.members))
	}
	live := renderLineAt(t, &m, 0)
	if !strings.Contains(live, "Running 3 tools…") || !strings.Contains(live, toolIconRunning) {
		t.Fatalf("live header wrong: %q", live)
	}

	// c2 returns first (fast success): only its member flips.
	m.Update(ToolResultMsg{Name: "read", Output: "ok row c2", ToolCallID: "c2", Duration: 120 * time.Millisecond})
	if g.members[1].status != memberSuccess {
		t.Fatalf("c2 member not flipped to success: %v", g.members[1].status)
	}
	for _, idx := range []int{0, 2} {
		if g.members[idx].status != memberRunning {
			t.Fatalf("member %d flipped prematurely: %v", idx, g.members[idx].status)
		}
	}
	live = renderLineAt(t, &m, 0)
	if !strings.Contains(live, toolIconSuccess) || !strings.Contains(live, toolIconRunning) {
		t.Fatalf("live form should mix flipped and running members: %q", live)
	}
	if !strings.Contains(live, "Running 3 tools…") {
		t.Fatalf("group collapsed before batch completion: %q", live)
	}

	// c3 fails: its member flips to error, and the live form shows a short
	// error digest under the row (plus a duration, always on failures).
	m.Update(ToolResultMsg{Name: "read", ToolCallID: "c3", Err: errors.New("boom"), Duration: 3 * time.Second})
	live = renderLineAt(t, &m, 0)
	if !strings.Contains(live, toolIconError) || !strings.Contains(live, "3.0s") || !strings.Contains(live, "boom") {
		t.Fatalf("failed member missing error icon/duration/digest: %q", live)
	}

	// c1 completes last: the group collapses to one summary line. All three
	// are reads → Explored phrasing; the failure keeps ✗ and a failed count.
	m.Update(ToolResultMsg{Name: "read", Output: "ok row c1", ToolCallID: "c1", Duration: 5200 * time.Millisecond})
	collapsed := renderLineAt(t, &m, 0)
	if strings.Contains(collapsed, "Running") {
		t.Fatalf("group did not collapse after completion: %q", collapsed)
	}
	if !strings.Contains(collapsed, toolIconError) || !strings.Contains(collapsed, "Explored 3 files read") ||
		!strings.Contains(collapsed, "1 failed") {
		t.Fatalf("collapsed summary wrong: %q", collapsed)
	}
	if strings.Count(collapsed, "\n") != 0 {
		t.Fatalf("collapsed multi-member group must be a single line: %q", collapsed)
	}
	if len(m.groupMembers) != 0 {
		t.Fatalf("member tracking leaked: %d entries", len(m.groupMembers))
	}

	// The transcript renders the group fully expanded: every member's
	// complete output plus durations.
	tr := m.renderTranscript(100)
	for _, want := range []string{"ok row c1", "ok row c2", "boom", "5.2s", "3.0s"} {
		if !strings.Contains(tr, want) {
			t.Errorf("transcript missing %q", want)
		}
	}
}

// TestToolBatchAllSuccess asserts the collapsed summary uses ✓ when every
// member succeeds, with the Explored phrasing for all-read-only groups.
func TestToolBatchAllSuccess(t *testing.T) {
	m := newToolTestModel()
	for i, id := range []string{"x1", "x2"} {
		m.Update(ToolCallMsg{Name: "grep", ToolCallID: id, BatchID: "b2", BatchIndex: i, BatchSize: 2})
	}
	m.Update(ToolResultMsg{Name: "grep", Output: "ok", ToolCallID: "x2", Duration: 90 * time.Millisecond})
	m.Update(ToolResultMsg{Name: "grep", Output: "ok", ToolCallID: "x1", Duration: 80 * time.Millisecond})

	collapsed := renderLineAt(t, &m, 0)
	if !strings.Contains(collapsed, toolIconSuccess) || !strings.Contains(collapsed, "Explored 2 searches") {
		t.Fatalf("collapsed summary not success/Explored: %q", collapsed)
	}
	// Fast successes (<2s) must not surface a duration anywhere.
	if strings.Contains(collapsed, "0.1s") {
		t.Fatalf("fast success should not carry duration: %q", collapsed)
	}
}

// TestMixedGroupSummaryCounts pins the mixed (non-explorative) phrasing and
// the read/edit dedupe-by-file rule: commands always count, repeated reads of
// the same file count once.
func TestMixedGroupSummaryCounts(t *testing.T) {
	m := newToolTestModel()
	calls := []ToolCallMsg{
		{Name: "execute", Title: "Shell", Subtitle: "go test", ToolCallID: "m1"},
		{Name: "read", Title: "Read", Subtitle: "a.go", ToolCallID: "m2"},
		{Name: "read", Title: "Read", Subtitle: "a.go", ToolCallID: "m3"},
		{Name: "edit", Title: "Edit", Subtitle: "b.go", ToolCallID: "m4"},
	}
	for _, c := range calls {
		m.Update(c)
	}
	for _, id := range []string{"m1", "m2", "m3", "m4"} {
		m.Update(ToolResultMsg{Name: "x", Output: "ok", ToolCallID: id})
	}

	collapsed := renderLineAt(t, &m, 0)
	for _, want := range []string{"Ran 1 command", "read 1 file", "edited 1 file"} {
		if !strings.Contains(collapsed, want) {
			t.Fatalf("collapsed summary missing %q: %q", want, collapsed)
		}
	}
	if strings.Contains(collapsed, "read 2 files") {
		t.Fatalf("same-file reads must dedupe: %q", collapsed)
	}
}

// TestSingleToolGroupForms pins the single-member group's two forms: live
// with no header at the standard indent, collapsed to today's one tool line
// plus its duration.
func TestSingleToolGroupForms(t *testing.T) {
	m := newToolTestModel()
	m.Update(ToolCallMsg{Name: "read", Title: "Read", Subtitle: "a.go", ToolCallID: "s1", BatchID: "b3", BatchIndex: 0, BatchSize: 1})

	if len(m.lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(m.lines))
	}
	live := renderLineAt(t, &m, 0)
	if strings.Contains(live, "Running") {
		t.Fatalf("single-member group must not render a header: %q", live)
	}
	if strings.HasPrefix(live, "    ") || !strings.HasPrefix(live, "  ") {
		t.Fatalf("single tool should keep the standard indent: %q", live)
	}
	if !strings.Contains(live, toolIconRunning) || !strings.Contains(live, "Read") {
		t.Fatalf("live single line wrong: %q", live)
	}

	m.Update(ToolResultMsg{Name: "read", Output: "ok", ToolCallID: "s1", Duration: 2500 * time.Millisecond})
	collapsed := renderLineAt(t, &m, 0)
	if !strings.Contains(collapsed, toolIconSuccess) || !strings.Contains(collapsed, "Read") ||
		!strings.Contains(collapsed, "2.5s") {
		t.Fatalf("collapsed single line wrong: %q", collapsed)
	}
	if strings.Count(collapsed, "\n") != 0 {
		t.Fatalf("collapsed single-member group must be one line: %q", collapsed)
	}
}

// TestTextMessageClosesGroup asserts that flushed assistant text between
// tool calls closes the open group, so the next tool opens a new one.
func TestTextMessageClosesGroup(t *testing.T) {
	m := newToolTestModel()
	m.Update(ToolCallMsg{Name: "read", Title: "Read", ToolCallID: "a1"})
	m.Update(ToolResultMsg{Name: "read", Output: "ok", ToolCallID: "a1"})
	m.Update(AgentTextMsg{Text: "some analysis"})
	m.Update(ToolCallMsg{Name: "read", Title: "Read", ToolCallID: "a2"})

	first := groupAt(t, &m, 0)
	last := m.lines[len(m.lines)-1].group
	if last == nil {
		t.Fatalf("last line should be a new group line")
	}
	if last == first {
		t.Fatalf("text flush must close the group; got the same group")
	}
	if len(first.members) != 1 || len(last.members) != 1 {
		t.Fatalf("unexpected member counts: %d / %d", len(first.members), len(last.members))
	}
}

// TestBatchMemberRejoinsAfterInterruption pins the approval-era rule: a
// member of an already-seen batch still joins its group even when another
// line (e.g. a rejection notice) was appended in between.
func TestBatchMemberRejoinsAfterInterruption(t *testing.T) {
	m := newToolTestModel()
	m.Update(ToolCallMsg{Name: "execute", Title: "Shell", ToolCallID: "p1", BatchID: "b9", BatchSize: 2})
	// An approval verdict line interrupts the timeline.
	m.lines = append(m.lines, textLine("  ⚠ Rejected: execute — user denied this operation"))
	m.Update(ToolCallMsg{Name: "execute", Title: "Shell", ToolCallID: "p2", BatchID: "b9", BatchSize: 2})

	g := groupAt(t, &m, 0)
	if len(g.members) != 2 {
		t.Fatalf("same-batch member did not rejoin its group: %d members", len(g.members))
	}
	if m.lines[len(m.lines)-1].group != nil {
		t.Fatalf("no new group line should have been appended")
	}
}

// TestAgentDoneInterruptsRunningMembers asserts a run that ends (cancel /
// error) with unresolved members collapses the group and marks them
// interrupted rather than leaving a frozen live form.
func TestAgentDoneInterruptsRunningMembers(t *testing.T) {
	m := newToolTestModel()
	m.Update(ToolCallMsg{Name: "execute", Title: "Shell", ToolCallID: "z1"})
	m.Update(AgentDoneMsg{})

	g := groupAt(t, &m, 0)
	if g.members[0].status != memberInterrupted {
		t.Fatalf("member not marked interrupted: %v", g.members[0].status)
	}
	rendered := m.lines[0].render(100, nil)
	if !strings.Contains(rendered, toolIconDenied) || !strings.Contains(rendered, "interrupted") {
		t.Fatalf("interrupted member render wrong: %q", rendered)
	}
	if len(m.groupMembers) != 0 || len(m.groupBatches) != 0 {
		t.Fatalf("group tracking leaked past AgentDone: members=%d batches=%d",
			len(m.groupMembers), len(m.groupBatches))
	}
}

// TestToolResultWithoutIDFallsBack pins the legacy behavior: calls/results
// without a toolCallID keep the old string line and last-running-icon flip.
func TestToolResultWithoutIDFallsBack(t *testing.T) {
	m := newToolTestModel()
	m.Update(ToolCallMsg{Name: "read", Title: "Read"}) // legacy: no ID, no batch
	if m.lines[0].group != nil {
		t.Fatalf("legacy call must not open an activity group")
	}
	m.Update(ToolResultMsg{Name: "read", Output: "ok"})
	if line := lineAt(t, &m, 0); !strings.Contains(line, toolIconSuccess) {
		t.Fatalf("fallback flip failed: %q", line)
	}
}

func TestFormatToolDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{4200 * time.Millisecond, "4.2s"},
		{59900 * time.Millisecond, "59.9s"},
		{65 * time.Second, "1m05s"},
		{2*time.Minute + 3*time.Second, "2m03s"},
	}
	for _, c := range cases {
		if got := formatToolDuration(c.d); got != c.want {
			t.Errorf("formatToolDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// TestToolResultDenied pins the denied semantics in the structured group: the
// member flips to a muted ⊘ with a "denied" verdict (no error red, no
// duration), no output is kept for the rejection boilerplate, and the
// collapsed summary counts it as "1 denied" — not a failure, so the summary
// icon stays ✓.
func TestToolResultDenied(t *testing.T) {
	m := newToolTestModel()
	for i, id := range []string{"d1", "d2"} {
		m.Update(ToolCallMsg{Name: "execute", Title: "Shell", ToolCallID: id, BatchID: "b4", BatchIndex: i, BatchSize: 2})
	}
	m.Update(ToolResultMsg{Name: "execute", Output: "ok", ToolCallID: "d1", Duration: 100 * time.Millisecond})
	live := renderLineAt(t, &m, 0)
	if !strings.Contains(live, "Running 2 tools…") {
		t.Fatalf("group should still be live: %q", live)
	}

	m.Update(ToolResultMsg{
		Name: "execute", Output: "Tool execution was rejected by user.",
		ToolCallID: "d2", Denied: true, Duration: 8 * time.Second,
	})

	g := groupAt(t, &m, 0)
	if g.members[1].status != memberDenied {
		t.Fatalf("denied member wrong status: %v", g.members[1].status)
	}
	if g.members[1].output != "" {
		t.Fatalf("denied member must not keep the rejection boilerplate: %q", g.members[1].output)
	}
	collapsed := renderLineAt(t, &m, 0)
	if !strings.Contains(collapsed, toolIconSuccess) || strings.Contains(collapsed, toolIconError) {
		t.Fatalf("denial must not flip the summary to failed: %q", collapsed)
	}
	if !strings.Contains(collapsed, "Ran 2 commands") || !strings.Contains(collapsed, "1 denied") {
		t.Fatalf("collapsed summary missing command/denied counts: %q", collapsed)
	}
	if strings.Contains(collapsed, "8.0s") || strings.Contains(collapsed, "failed") {
		t.Fatalf("denied result must not show duration or count as failed: %q", collapsed)
	}
}
