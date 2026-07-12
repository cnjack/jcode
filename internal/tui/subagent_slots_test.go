package tui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	xansi "github.com/charmbracelet/x/ansi"
)

func newSubagentTestModel() Model {
	m := newToolTestModel()
	m.spinner = spinner.New()
	return m
}

// TestParallelSubagentSlots drives two concurrent subagents and asserts that
// progress, tokens and completion all land on the right slot, that the live
// box shows one section per subagent, and that the timeline done line carries
// the per-subagent stats.
func TestParallelSubagentSlots(t *testing.T) {
	m := newSubagentTestModel()
	m.Update(SubagentStartMsg{Name: "alpha", Type: "explore"})
	m.Update(SubagentStartMsg{Name: "beta", Type: "general"})

	m.Update(SubagentProgressMsg{AgentName: "alpha", Event: "tool_call", ToolName: "read", Detail: `{"file_path":"a.go"}`})
	m.Update(SubagentProgressMsg{AgentName: "beta", Event: "tool_call", ToolName: "grep", Detail: `{"pattern":"foo"}`})
	m.Update(SubagentProgressMsg{AgentName: "beta", Event: "tool_call", ToolName: "read", Detail: `{"file_path":"b.go"}`})

	if len(m.subagentSlots) != 2 {
		t.Fatalf("expected 2 slots, got %d", len(m.subagentSlots))
	}
	if m.subagentSlots[0].steps != 1 || m.subagentSlots[1].steps != 2 {
		t.Fatalf("steps routed wrong: alpha=%d beta=%d",
			m.subagentSlots[0].steps, m.subagentSlots[1].steps)
	}

	box := xansi.Strip(m.renderSubagentBox())
	for _, want := range []string{"alpha", "(explore) [1 steps]", "beta", "(general) [2 steps]", "grep"} {
		if !strings.Contains(box, want) {
			t.Errorf("live box missing %q:\n%s", want, box)
		}
	}

	// Token updates route by subagent name.
	m.Update(SubagentTokenUpdateMsg{Name: "beta", TotalTokens: 1234})
	if m.subagentSlots[1].tokens != 1234 || m.subagentSlots[0].tokens != 0 {
		t.Fatalf("tokens routed wrong: alpha=%d beta=%d",
			m.subagentSlots[0].tokens, m.subagentSlots[1].tokens)
	}

	// First subagent finishes: it collapses to a ✓ summary inside the box
	// while its sibling keeps running; slots are not cleared yet.
	m.Update(SubagentDoneMsg{Name: "alpha"})
	if m.activeSubagentCount() != 1 || len(m.subagentSlots) != 2 {
		t.Fatalf("slot lifecycle wrong after first done: active=%d slots=%d",
			m.activeSubagentCount(), len(m.subagentSlots))
	}
	box = xansi.Strip(m.renderSubagentBox())
	if !strings.Contains(box, "✓ alpha · 1 steps") {
		t.Errorf("finished slot summary missing:\n%s", box)
	}
	doneLine := xansi.Strip(lineAt(t, &m, len(m.lines)-1))
	if !strings.Contains(doneLine, "✓ Subagent") || !strings.Contains(doneLine, "alpha · 1 steps") {
		t.Errorf("timeline done line missing stats: %q", doneLine)
	}

	// Status line aggregates while several ran; single active now.
	var sb strings.Builder
	m.thinking = true
	m.appendStatusLine(&sb)
	if out := xansi.Strip(sb.String()); !strings.Contains(out, "[3 steps]") {
		t.Errorf("status line missing aggregated steps:\n%s", out)
	}

	// A failing sibling finishes last: timeline error line, all slots drop.
	m.Update(SubagentDoneMsg{Name: "beta", Err: errors.New("boom")})
	if len(m.subagentSlots) != 0 {
		t.Fatalf("slots not cleared after last done: %d", len(m.subagentSlots))
	}
	errLine := xansi.Strip(lineAt(t, &m, len(m.lines)-1))
	if !strings.Contains(errLine, "✗ Subagent Error") {
		t.Errorf("timeline error line wrong: %q", errLine)
	}
}

// TestSingleSubagentBoxParity pins the single-subagent visual: the classic
// headerless tail of progress lines with an "earlier steps" marker whose
// count stays truthful past the stored-line cap.
func TestSingleSubagentBoxParity(t *testing.T) {
	m := newSubagentTestModel()
	m.Update(SubagentStartMsg{Name: "solo", Type: "explore"})
	for i := 0; i < 10; i++ {
		m.Update(SubagentProgressMsg{AgentName: "solo", Event: "tool_call", ToolName: "read", Detail: "{}"})
	}

	box := xansi.Strip(m.renderSubagentBox())
	if strings.Contains(box, "solo") {
		t.Errorf("single subagent box should keep the classic headerless layout:\n%s", box)
	}
	if !strings.Contains(box, "... (2 earlier steps)") {
		t.Errorf("earlier-steps marker missing/wrong:\n%s", box)
	}

	var sb strings.Builder
	m.thinking = true
	m.appendStatusLine(&sb)
	if out := xansi.Strip(sb.String()); !strings.Contains(out, "Subagent [10 steps]") {
		t.Errorf("single-subagent status label wrong:\n%s", out)
	}
}

// TestSubagentProgressFallsBackToSoleActive covers the defensive path: a
// progress event whose name matches no slot still lands on the only active
// slot (event source and display disagreeing on the name).
func TestSubagentProgressFallsBackToSoleActive(t *testing.T) {
	m := newSubagentTestModel()
	m.Update(SubagentStartMsg{Name: "real-name", Type: "explore"})
	m.Update(SubagentProgressMsg{AgentName: "other", Event: "tool_call", ToolName: "read", Detail: "{}"})
	if m.subagentSlots[0].steps != 1 {
		t.Fatalf("sole-active fallback did not record the step: %d", m.subagentSlots[0].steps)
	}

	// With two active slots the fallback must NOT guess.
	m.Update(SubagentStartMsg{Name: "second", Type: "explore"})
	m.Update(SubagentProgressMsg{AgentName: "unknown", Event: "tool_call", ToolName: "grep", Detail: "{}"})
	if got := m.subagentSlots[0].steps + m.subagentSlots[1].steps; got != 1 {
		t.Fatalf("ambiguous progress event was routed anyway: total steps %d", got)
	}
}

// TestAgentDoneClearsSubagentSlots asserts a cancelled run cannot leave a
// stale live box behind.
func TestAgentDoneClearsSubagentSlots(t *testing.T) {
	m := newSubagentTestModel()
	m.textarea = newTextarea()
	m.Update(SubagentStartMsg{Name: "alpha", Type: "explore"})
	m.Update(SubagentProgressMsg{AgentName: "alpha", Event: "tool_call", ToolName: "read", Detail: "{}"})
	m.Update(AgentDoneMsg{})
	if len(m.subagentSlots) != 0 {
		t.Fatalf("AgentDoneMsg left %d slots", len(m.subagentSlots))
	}
	if m.hasSubagentDisplay() {
		t.Fatal("subagent display still active after run end")
	}
}
