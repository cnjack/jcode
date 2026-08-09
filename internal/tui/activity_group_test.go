package tui

import (
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

// findGroupLines returns the indices of all activity-group lines.
func findGroupLines(m *Model) []int {
	var out []int
	for i := range m.lines {
		if m.lines[i].group != nil {
			out = append(out, i)
		}
	}
	return out
}

// TestReplayRebuildsCollapsedGroups asserts that resumed sessions rebuild
// adjacent tool_call/tool_result entries (with IDs) into activity groups
// rendered directly in collapsed form, that a non-tool entry splits groups,
// and that the transcript still carries the full outputs.
func TestReplayRebuildsCollapsedGroups(t *testing.T) {
	m := newToolTestModel()
	tc, tr := string(session.EntryToolCall), string(session.EntryToolResult)
	m.Update(SessionResumedMsg{
		UUID: "u1",
		Entries: []SessionEntry{
			{Type: tc, Name: "execute", Args: `{"command":"go test"}`, ToolCallID: "r1"},
			{Type: tr, Name: "execute", Output: "all tests passed row42", ToolCallID: "r1"},
			{Type: tc, Name: "read", Args: `{"file_path":"a.go"}`, ToolCallID: "r2"},
			{Type: tr, Name: "read", Output: "package main", ToolCallID: "r2"},
			{Type: string(session.EntryAssistant), Content: "looks good"},
			{Type: tc, Name: "grep", Args: `{"pattern":"foo"}`, ToolCallID: "r3"},
			{Type: tr, Name: "grep", Error: "exit status 2", ToolCallID: "r3"},
		},
	})

	groups := findGroupLines(&m)
	if len(groups) != 2 {
		t.Fatalf("expected 2 rebuilt groups (assistant text splits), got %d", len(groups))
	}

	// First group: execute + read, all succeeded → collapsed mixed summary.
	first := m.lines[groups[0]].render(100, nil)
	if !strings.Contains(first, toolIconSuccess) ||
		!strings.Contains(first, "Ran 1 command") || !strings.Contains(first, "read 1 file") {
		t.Fatalf("first replay group summary wrong: %q", first)
	}
	if strings.Contains(first, "Running") {
		t.Fatalf("replayed group must render collapsed: %q", first)
	}

	// Second group: single failed grep → one line with ✗ plus error digest.
	second := m.lines[groups[1]].render(100, nil)
	if !strings.Contains(second, toolIconError) || !strings.Contains(second, "exit status 2") {
		t.Fatalf("second replay group wrong: %q", second)
	}

	// No result boxes were appended for grouped members.
	for i := range m.lines {
		if m.lines[i].tool != nil {
			t.Fatalf("replay appended a legacy result box at line %d", i)
		}
	}

	// Transcript keeps the full member outputs.
	tr2 := m.renderTranscript(100)
	for _, want := range []string{"all tests passed row42", "package main", "exit status 2"} {
		if !strings.Contains(tr2, want) {
			t.Errorf("transcript missing %q", want)
		}
	}
}

// TestReplayLegacyEntriesKeepStringPath pins the fallback: entries without a
// ToolCallID replay exactly as before — string tool lines plus result boxes,
// including batch header stacking.
func TestReplayLegacyEntriesKeepStringPath(t *testing.T) {
	m := newToolTestModel()
	tc, tr := string(session.EntryToolCall), string(session.EntryToolResult)
	m.Update(SessionResumedMsg{
		UUID: "u2",
		Entries: []SessionEntry{
			{Type: tc, Name: "read", Args: `{"file_path":"a.go"}`, BatchID: "lb1", BatchSize: 2},
			{Type: tc, Name: "read", Args: `{"file_path":"b.go"}`, BatchID: "lb1", BatchSize: 2},
			{Type: tr, Name: "read", Output: "legacy output"},
			{Type: tr, Name: "read", Output: "legacy output 2"},
		},
	})

	if got := findGroupLines(&m); len(got) != 0 {
		t.Fatalf("legacy entries must not build activity groups, got %d", len(got))
	}
	var sawHeader, sawBox bool
	for i := range m.lines {
		if strings.Contains(m.lines[i].text, "Running 2 tools") {
			sawHeader = true
		}
		if m.lines[i].tool != nil {
			sawBox = true
		}
	}
	if !sawHeader || !sawBox {
		t.Fatalf("legacy replay lost header/box: header=%v box=%v", sawHeader, sawBox)
	}
}

func TestGeneratedImageUsesStandaloneTimelineAndShowsProgress(t *testing.T) {
	m := newToolTestModel()
	m.Update(ToolCallMsg{
		Name: "generate_image", Title: "Generate image", ToolCallID: "img-1", Standalone: true,
	})
	if groups := findGroupLines(&m); len(groups) != 0 {
		t.Fatalf("standalone image call was folded into an activity group: %v", groups)
	}
	if _, ok := m.toolLines["img-1"]; !ok {
		t.Fatal("standalone image call was not tracked for terminal updates")
	}
	m.Update(ToolProgressMsg{Name: "generate_image", ToolCallID: "img-1", Phase: "generating"})
	visible := false
	for i := range m.lines {
		if strings.Contains(m.lines[i].text, "Generating image") {
			visible = true
		}
	}
	if !visible {
		t.Fatal("image generation progress was not rendered")
	}
	m.Update(ToolResultMsg{Name: "generate_image", ToolCallID: "img-1", Output: "JCode engine path: /tmp/image.png"})
	var resultVisible bool
	for i := range m.lines {
		if m.lines[i].tool != nil && strings.Contains(m.lines[i].tool.output, "JCode engine path") {
			resultVisible = true
		}
	}
	if !resultVisible {
		t.Fatal("standalone image result did not keep its engine path output")
	}
}

func TestReplayKeepsGeneratedImageOutsideActivityGroups(t *testing.T) {
	m := newToolTestModel()
	m.Update(SessionResumedMsg{UUID: "image-session", Entries: []SessionEntry{
		{Type: string(session.EntryToolCall), Name: "generate_image", Args: `{"prompt":"desk"}`, ToolCallID: "img-r1"},
		{Type: string(session.EntryToolResult), Name: "generate_image", Output: `{"outcome":"succeeded"}`, ToolCallID: "img-r1"},
	}})
	if groups := findGroupLines(&m); len(groups) != 0 {
		t.Fatalf("replayed generated image was folded into an activity group: %v", groups)
	}
	var resultBox bool
	for i := range m.lines {
		if m.lines[i].tool != nil && m.lines[i].tool.name == "generate_image" {
			resultBox = true
		}
	}
	if !resultBox {
		t.Fatal("replayed generated image lost its standalone result")
	}
}

// TestReplayUnresolvedMemberInterrupted asserts a recorded call that never
// got a result (session died mid-run) is frozen as interrupted so the group
// still collapses.
func TestReplayUnresolvedMemberInterrupted(t *testing.T) {
	m := newToolTestModel()
	tc := string(session.EntryToolCall)
	m.Update(SessionResumedMsg{
		UUID: "u3",
		Entries: []SessionEntry{
			{Type: tc, Name: "execute", Args: `{"command":"sleep 999"}`, ToolCallID: "h1"},
		},
	})

	groups := findGroupLines(&m)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g := m.lines[groups[0]].group
	if g.members[0].status != memberInterrupted {
		t.Fatalf("unresolved replay member not interrupted: %v", g.members[0].status)
	}
	rendered := m.lines[groups[0]].render(100, nil)
	if strings.Contains(rendered, "Running") || !strings.Contains(rendered, "interrupted") {
		t.Fatalf("unresolved replay member render wrong: %q", rendered)
	}
}

// TestSummarizeActivityCounts covers the bucket phrasing rules directly.
func TestSummarizeActivityCounts(t *testing.T) {
	mk := func(name, subtitle string) *activityMember {
		return &activityMember{name: name, subtitle: subtitle, status: memberSuccess}
	}
	cases := []struct {
		name    string
		members []*activityMember
		want    string
	}{
		{
			name:    "explored dedupes files",
			members: []*activityMember{mk("read", "a.go"), mk("read", "a.go"), mk("grep", "foo"), mk("glob", "*.go")},
			want:    "Explored 1 file read · 1 search · 1 list",
		},
		{
			name:    "mixed verb phrasing",
			members: []*activityMember{mk("execute", "go build"), mk("execute", "go vet"), mk("edit", "a.go"), mk("subagent", "explore")},
			want:    "Ran 2 commands · edited 1 file · ran 1 agent",
		},
		{
			name:    "execute is always a command",
			members: []*activityMember{mk("execute", "rg foo"), mk("read", "b.go")},
			want:    "Ran 1 command · read 1 file",
		},
		{
			name:    "other bucket",
			members: []*activityMember{mk("todowrite", ""), mk("write", "c.go")},
			want:    "Edited 1 file · 1 other",
		},
	}
	for _, c := range cases {
		if got := summarizeActivityCounts(c.members); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
