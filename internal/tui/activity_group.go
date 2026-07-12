package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// ─── Structured activity groups ───
//
// Adjacent tool calls coalesce into one structured timeline line (mirroring
// the web timeline's activity groups, packages/jcode-ui-core/src/timeline/
// groupActivity.ts). While any member is still running the line renders a
// live member list; once every member has completed it collapses to a single
// category-count summary ("✓ Ran 3 commands · read 2 files"). Member state
// lives in the group, so a tool result is a data update followed by a
// re-render — no string backfill.
//
// "Adjacent" means the group line is still the last timeline line when the
// next tool call arrives: any interleaved append (assistant text flush, plan
// or approval verdict lines, subagent labels, run dividers) naturally closes
// the group. Members that share a BatchID are additionally routed to their
// batch's group even after an interruption (e.g. an approval dialog for a
// sibling), matching the web's batch anchoring.
//
// Tool calls without a ToolCallID (legacy replays, old sessions) keep the
// pre-existing string-line path (see update.go).

// memberStatus is the lifecycle state of one activity-group member.
type memberStatus int

const (
	memberRunning memberStatus = iota
	memberSuccess
	memberFailed
	memberDenied      // user rejected at the approval gate — not a failure
	memberInterrupted // run ended (cancel/error) before a result arrived
)

// activityMember is one tool call inside an activity group. It stores the
// full output (same retention as the old toolResultData boxes) so the
// transcript overlay can render it untruncated.
type activityMember struct {
	toolCallID string
	name       string // raw tool name — classification into summary buckets
	title      string // display title ("Read", "Shell"); falls back to name
	subtitle   string // context info (file path, command description)
	status     memberStatus
	duration   time.Duration // call→result latency; 0 when unknown
	output     string        // full sanitized output (transcript rendering)
	err        error         // non-nil for failed members
	startedAt  time.Time
}

// activityGroupData is the structured payload of an activity-group content
// line. rev is bumped on every mutation and keys the per-line render cache
// (same scheme as the subagent box's subagentRev).
type activityGroupData struct {
	members []*activityMember
	rev     int
}

// groupMemberRef routes a tool result to its member and lets the update bump
// the owning group's revision.
type groupMemberRef struct {
	group  *activityGroupData
	member *activityMember
}

// activityGroupContentLine wraps a group as a timeline content line.
func activityGroupContentLine(g *activityGroupData) contentLine {
	return contentLine{group: g}
}

// live reports whether any member is still running.
func (g *activityGroupData) live() bool {
	for _, mem := range g.members {
		if mem.status == memberRunning {
			return true
		}
	}
	return false
}

// ─── Model-side event handling ───

// appendGroupToolCall adds a tool call with a ToolCallID to the open activity
// group, or opens a new one. The open group is the group that is still the
// last timeline line; members of an already-seen BatchID rejoin their batch's
// group even when something (approval verdict, rejection notice) has been
// appended in between.
func (m *Model) appendGroupToolCall(msg ToolCallMsg) {
	title := msg.Title
	if title == "" {
		title = msg.Name
	}
	subtitle := msg.Subtitle
	if subtitle == "" {
		subtitle = formatToolArgs(msg.Args)
	}
	mem := &activityMember{
		toolCallID: msg.ToolCallID,
		name:       msg.Name,
		title:      title,
		subtitle:   subtitle,
		status:     memberRunning,
		startedAt:  msg.StartedAt,
	}

	if m.groupMembers == nil {
		m.groupMembers = make(map[string]groupMemberRef)
	}
	if m.groupBatches == nil {
		m.groupBatches = make(map[string]*activityGroupData)
	}

	var g *activityGroupData
	if msg.BatchID != "" {
		g = m.groupBatches[msg.BatchID]
	}
	if g == nil && len(m.lines) > 0 {
		g = m.lines[len(m.lines)-1].group
	}
	if g == nil {
		g = &activityGroupData{}
		m.lines = append(m.lines, activityGroupContentLine(g))
	}
	if msg.BatchID != "" {
		m.groupBatches[msg.BatchID] = g
	}
	g.members = append(g.members, mem)
	g.rev++
	m.groupMembers[msg.ToolCallID] = groupMemberRef{group: g, member: mem}
}

// resolveGroupToolResult routes a tool result to its activity-group member.
// It returns false when the result does not belong to a group (no ID, or the
// call went down the legacy string path), in which case the caller falls back
// to the old icon-flip + result-box behavior.
func (m *Model) resolveGroupToolResult(msg ToolResultMsg) bool {
	if msg.ToolCallID == "" {
		return false
	}
	ref, ok := m.groupMembers[msg.ToolCallID]
	if !ok {
		return false
	}
	delete(m.groupMembers, msg.ToolCallID)
	mem := ref.member
	mem.duration = msg.Duration
	switch {
	case msg.Denied:
		// A denial is a user decision, not a failure; the fixed rejection
		// boilerplate is not worth keeping.
		mem.status = memberDenied
	case msg.Err != nil:
		mem.status = memberFailed
		mem.err = msg.Err
	default:
		mem.status = memberSuccess
		mem.output = sanitize(msg.Output)
	}
	ref.group.rev++
	return true
}

// finalizeActivityGroups marks members that never received a result (run
// cancelled or errored mid-flight) as interrupted so their groups can
// collapse, and drops all per-run routing state.
func (m *Model) finalizeActivityGroups() {
	for id, ref := range m.groupMembers {
		if ref.member.status == memberRunning {
			ref.member.status = memberInterrupted
			ref.group.rev++
		}
		delete(m.groupMembers, id)
	}
	for id := range m.groupBatches {
		delete(m.groupBatches, id)
	}
}

// ─── Rendering ───

// memberIcon returns the pre-rendered status icon for a member.
func memberIcon(mem *activityMember) string {
	switch mem.status {
	case memberRunning:
		return toolIconRunning
	case memberFailed:
		return toolIconError
	case memberDenied, memberInterrupted:
		return toolIconDenied
	default:
		return toolIconSuccess
	}
}

// memberSuffix returns the dim metadata suffix for a member line: "denied" /
// "interrupted" verdicts, or the duration (failures always, successes only
// when slow enough to be interesting — same rule as the old string lines).
func memberSuffix(mem *activityMember) string {
	switch mem.status {
	case memberDenied:
		return " " + toolArgsStyle.Render("denied")
	case memberInterrupted:
		return " " + toolArgsStyle.Render("interrupted")
	case memberFailed:
		if mem.duration > 0 {
			return " " + toolArgsStyle.Render(formatToolDuration(mem.duration))
		}
	case memberSuccess:
		if mem.duration > 2*time.Second {
			return " " + toolArgsStyle.Render(formatToolDuration(mem.duration))
		}
	}
	return ""
}

// memberLine renders one member row at the given indent.
func memberLine(mem *activityMember, indent string) string {
	line := indent + memberIcon(mem) + " " + toolNameStyle.Render(mem.title)
	if mem.subtitle != "" {
		line += " " + toolArgsStyle.Render(mem.subtitle)
	}
	return line + memberSuffix(mem)
}

// memberErrorSummary returns a 1–2 line error digest for a failed member,
// shown directly under its row; the full error stays in the transcript.
func memberErrorSummary(mem *activityMember) []string {
	if mem.err == nil {
		return nil
	}
	text := strings.TrimSpace(sanitize(mem.err.Error()))
	if text == "" {
		return nil
	}
	lines := strings.SplitN(text, "\n", 3)
	if len(lines) > 2 {
		lines = lines[:2]
	}
	out := make([]string, 0, len(lines))
	errStyle := lipgloss.NewStyle().Foreground(colorError)
	for i, l := range lines {
		l = truncate(strings.TrimSpace(l), 120)
		if i == 0 {
			l = toolErrorStyle.Render("Error:") + " " + errStyle.Render(l)
		} else {
			l = errStyle.Render(l)
		}
		out = append(out, l)
	}
	return out
}

// render produces the group's current form: a live member list while any
// member runs, a single collapsed summary line once every member completed.
func (g *activityGroupData) render() string {
	if len(g.members) == 0 {
		return ""
	}
	if g.live() {
		return g.renderLive()
	}
	return g.renderCollapsed()
}

// renderLive renders the in-flight form: an optional "Running N tools…"
// header (multi-member groups only) plus one row per member. Completed
// members show their final icon/duration; failed members carry a short error
// digest underneath.
func (g *activityGroupData) renderLive() string {
	var sb strings.Builder
	indent := "  "
	if len(g.members) > 1 {
		sb.WriteString(fmt.Sprintf("  %s %s",
			toolIconRunning,
			toolNameStyle.Render(fmt.Sprintf("Running %d tools…", len(g.members)))))
		indent = "    "
	}
	for i, mem := range g.members {
		if i > 0 || len(g.members) > 1 {
			sb.WriteString("\n")
		}
		sb.WriteString(memberLine(mem, indent))
		for _, el := range memberErrorSummary(mem) {
			sb.WriteString("\n")
			sb.WriteString(indent + "  " + el)
		}
	}
	return sb.String()
}

// renderCollapsed renders the completed form. A single-member group keeps
// today's one tool line (plus its error digest when it failed); multi-member
// groups collapse to one category-count summary row.
func (g *activityGroupData) renderCollapsed() string {
	if len(g.members) == 1 {
		mem := g.members[0]
		var sb strings.Builder
		sb.WriteString(memberLine(mem, "  "))
		for _, el := range memberErrorSummary(mem) {
			sb.WriteString("\n")
			sb.WriteString("    " + el)
		}
		return sb.String()
	}

	failed, denied, interrupted := 0, 0, 0
	for _, mem := range g.members {
		switch mem.status {
		case memberFailed:
			failed++
		case memberDenied:
			denied++
		case memberInterrupted:
			interrupted++
		}
	}
	icon := toolIconSuccess
	switch {
	case failed > 0:
		icon = toolIconError
	case interrupted > 0:
		icon = toolIconDenied
	}

	line := "  " + icon + " " + toolNameStyle.Render(summarizeActivityCounts(g.members))
	sep := toolArgsStyle.Render(" · ")
	if failed > 0 {
		line += sep + toolErrorStyle.Render(fmt.Sprintf("%d failed", failed))
	}
	if denied > 0 {
		line += sep + toolArgsStyle.Render(fmt.Sprintf("%d denied", denied))
	}
	if interrupted > 0 {
		line += sep + toolArgsStyle.Render(fmt.Sprintf("%d interrupted", interrupted))
	}
	return line
}

// ─── Collapsed-summary buckets (aligned with the web's groupActivity.ts) ───

type activityBucket int

const (
	bucketCommand activityBucket = iota
	bucketRead
	bucketSearch
	bucketList
	bucketEdit
	bucketAgent
	bucketOther
)

// bucketOf classifies one member. `execute` always counts as a command (a
// `git grep` is still a command, not "a search"), matching the web rules.
func bucketOf(name string) activityBucket {
	switch name {
	case "execute":
		return bucketCommand
	case "subagent", "task", "team_spawn":
		return bucketAgent
	case "edit", "multi_edit", "write":
		return bucketEdit
	case "read":
		return bucketRead
	case "grep":
		return bucketSearch
	case "glob", "list_dir":
		return bucketList
	default:
		return bucketOther
	}
}

func pluralize(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// summarizeActivityCounts buckets a group's members into the compact
// category-count header. Mixed groups use verb phrases ("Ran 3 commands ·
// read 2 files"); all-read-only groups use the Explored phrasing ("Explored
// 3 files read · 2 searches"). Reads and edits dedupe by subtitle (file
// path) so re-touching the same file counts once.
func summarizeActivityCounts(members []*activityMember) string {
	var counts [bucketOther + 1]int
	readFiles := make(map[string]bool)
	editFiles := make(map[string]bool)
	explorative := len(members) > 0

	for _, mem := range members {
		b := bucketOf(mem.name)
		counts[b]++
		if b != bucketRead && b != bucketSearch && b != bucketList {
			explorative = false
		}
		file := strings.TrimSpace(mem.subtitle)
		if file != "" {
			switch b {
			case bucketRead:
				readFiles[file] = true
			case bucketEdit:
				editFiles[file] = true
			}
		}
	}

	readCount := len(readFiles)
	if readCount == 0 {
		readCount = counts[bucketRead]
	}
	editCount := len(editFiles)
	if editCount == 0 {
		editCount = counts[bucketEdit]
	}

	var parts []string
	if explorative {
		// Noun-first Explored phrasing, matching the web's exploring card.
		if counts[bucketRead] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s read", readCount, pluralize(readCount, "file", "files")))
		}
		if counts[bucketSearch] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[bucketSearch], pluralize(counts[bucketSearch], "search", "searches")))
		}
		if counts[bucketList] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[bucketList], pluralize(counts[bucketList], "list", "lists")))
		}
		return "Explored " + strings.Join(parts, " · ")
	}

	if counts[bucketCommand] > 0 {
		parts = append(parts, fmt.Sprintf("ran %d %s", counts[bucketCommand], pluralize(counts[bucketCommand], "command", "commands")))
	}
	if counts[bucketRead] > 0 {
		parts = append(parts, fmt.Sprintf("read %d %s", readCount, pluralize(readCount, "file", "files")))
	}
	if counts[bucketSearch] > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", counts[bucketSearch], pluralize(counts[bucketSearch], "search", "searches")))
	}
	if counts[bucketList] > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", counts[bucketList], pluralize(counts[bucketList], "list", "lists")))
	}
	if counts[bucketEdit] > 0 {
		parts = append(parts, fmt.Sprintf("edited %d %s", editCount, pluralize(editCount, "file", "files")))
	}
	if counts[bucketAgent] > 0 {
		parts = append(parts, fmt.Sprintf("ran %d %s", counts[bucketAgent], pluralize(counts[bucketAgent], "agent", "agents")))
	}
	if counts[bucketOther] > 0 {
		parts = append(parts, fmt.Sprintf("%d other", counts[bucketOther]))
	}
	joined := strings.Join(parts, " · ")
	if joined == "" {
		return ""
	}
	return strings.ToUpper(joined[:1]) + joined[1:]
}

// ─── Transcript rendering (full expansion) ───

// renderTranscriptGroup renders one activity group fully expanded for the
// transcript overlay: every member with its full command line, untruncated
// output box, and duration metadata (reusing renderTranscriptTool).
func renderTranscriptGroup(g *activityGroupData, width int) string {
	var sb strings.Builder
	for i, mem := range g.members {
		if i > 0 {
			sb.WriteString("\n")
		}
		line := "  " + memberIcon(mem) + " " + toolNameStyle.Render(mem.title)
		if mem.subtitle != "" {
			line += " " + toolArgsStyle.Render(mem.subtitle)
		}
		switch mem.status {
		case memberDenied:
			line += " " + toolArgsStyle.Render("denied")
		case memberInterrupted:
			line += " " + toolArgsStyle.Render("interrupted")
		}
		sb.WriteString(line)
		if mem.status == memberSuccess || mem.status == memberFailed {
			sb.WriteString("\n")
			sb.WriteString(renderTranscriptTool(&toolResultData{
				name:     mem.name,
				output:   mem.output,
				err:      mem.err,
				duration: mem.duration,
			}, width))
		}
	}
	return sb.String()
}
