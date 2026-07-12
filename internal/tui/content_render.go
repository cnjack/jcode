package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
)

// --- Helpers ---

// maxRenderedLines estimates the number of rendered lines for a tool result.
// This is used to map viewport scroll position back to content line indices.
func maxRenderedLines(tool *toolResultData, termWidth int) int {
	if tool == nil {
		return 1
	}
	output := tool.output
	if tool.err != nil {
		output = tool.err.Error()
	}
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return 1
	}
	lines := strings.Count(output, "\n") + 1

	// Account for word-wrapping: estimate based on width.
	boxWidth := termWidth - 12 // account for border + margin + padding
	if boxWidth < 20 {
		boxWidth = 20
	}
	wrapped := 0
	for _, line := range strings.Split(output, "\n") {
		if len(line) > boxWidth {
			wrapped += (len(line) / boxWidth)
		}
	}
	return lines + wrapped
}

// toggleSubagentExpand finds the nearest subagent tool result to the viewport
// top and toggles its expanded state. It searches from the viewport top line
// downward to find the first subagent tool result.
func (m *Model) toggleSubagentExpand() {
	if !m.ready || len(m.lines) == 0 {
		return
	}

	// Estimate which content line index corresponds to the viewport top.
	// Each content line produces one or more rendered lines. For tool results,
	// the rendered output can be multi-line. We estimate by counting newlines
	// in the text content and using a simple heuristic for tool results.
	topRenderedLine := m.viewport.YOffset()
	lineCount := 0
	startIdx := 0
	for i, cl := range m.lines {
		var n int
		switch {
		case cl.tool != nil:
			// Tool results are multi-line boxes; estimate conservatively.
			n = maxRenderedLines(cl.tool, m.contentWidth())
		case cl.group != nil:
			// Live form: header + one row per member; collapsed: one row.
			// Overestimating slightly is fine for scroll mapping.
			n = len(cl.group.members) + 1
		default:
			n = strings.Count(cl.text, "\n") + 1
		}
		if lineCount+n > topRenderedLine {
			startIdx = i
			break
		}
		lineCount += n
		if i == len(m.lines)-1 {
			startIdx = i
		}
	}

	// Search from viewport top downward.
	for i := startIdx; i < len(m.lines); i++ {
		if m.lines[i].tool != nil && m.lines[i].tool.name == "subagent" {
			m.lines[i].tool.expanded = !m.lines[i].tool.expanded
			m.lines[i].cachedRender = "" // invalidate cache for this line
			m.refreshViewport()
			return
		}
	}

	// If not found forward, search from beginning to viewport top.
	for i := 0; i < startIdx; i++ {
		if m.lines[i].tool != nil && m.lines[i].tool.name == "subagent" {
			m.lines[i].tool.expanded = !m.lines[i].tool.expanded
			m.lines[i].cachedRender = "" // invalidate cache for this line
			m.refreshViewport()
			return
		}
	}
}

// toolLineRef locates the timeline line of an announced tool call so its
// result can flip the status icon precisely by toolCallID.
type toolLineRef struct {
	idx     int    // index into m.lines (append-only during a run)
	batchID string // non-empty when the call belongs to a BatchSize>1 batch
}

// batchLineState tracks a batch header line ("⏺ Running N tools") until all
// members of the batch have reported a result.
type batchLineState struct {
	headerIdx int // index of the header line in m.lines
	size      int
	done      int
	failed    int
}

// replaceToolIconAt flips the status icon on the tool line at idx and appends
// suffix (e.g. a dimmed duration) to the line. It returns false when idx no
// longer points at a running/pending tool line — cleared history, view swap —
// so the caller can fall back to replaceLastToolIcon.
func (m *Model) replaceToolIconAt(idx int, newIcon, suffix string) bool {
	if idx < 0 || idx >= len(m.lines) {
		return false
	}
	text := m.lines[idx].text
	var replaced string
	switch {
	case strings.Contains(text, toolIconRunning):
		replaced = strings.Replace(text, toolIconRunning, newIcon, 1)
	case strings.Contains(text, toolIconPending):
		replaced = strings.Replace(text, toolIconPending, newIcon, 1)
	default:
		return false
	}
	m.lines[idx] = contentLine{text: replaced + suffix}
	return true
}

// replaceLastToolIcon replaces the status icon on the last tool call line.
// Fallback for results without a toolCallID (legacy replays); results that
// carry an ID flip their exact line via replaceToolIconAt instead.
func (m *Model) replaceLastToolIcon(newIcon, suffix string) {
	for i := len(m.lines) - 1; i >= 0; i-- {
		if m.replaceToolIconAt(i, newIcon, suffix) {
			return
		}
	}
}

// completeBatchMember records one finished member of a tool-call batch and,
// once every member has reported, flips the batch header line in place:
// all succeeded → "✓ Ran N tools", any failure → "✗ Ran N tools".
func (m *Model) completeBatchMember(batchID string, failed bool) {
	if batchID == "" {
		return
	}
	st, ok := m.batchLines[batchID]
	if !ok {
		return
	}
	st.done++
	if failed {
		st.failed++
	}
	if st.done < st.size {
		return
	}
	delete(m.batchLines, batchID)
	icon := toolIconSuccess
	if st.failed > 0 {
		icon = toolIconError
	}
	// Rebuild the header in place, but only if the line still is the running
	// header (history may have been cleared or swapped mid-batch).
	if st.headerIdx < 0 || st.headerIdx >= len(m.lines) ||
		!strings.Contains(m.lines[st.headerIdx].text, toolIconRunning) {
		return
	}
	m.lines[st.headerIdx] = contentLine{text: fmt.Sprintf("  %s %s",
		icon,
		toolNameStyle.Render(fmt.Sprintf("Ran %d tools", st.size)),
	)}
}

// resetToolLineTracking drops all toolCallID→line, batch header, and
// activity-group tracking. Call it whenever m.lines is rebuilt wholesale
// (e.g. session resume) so stale references can never mutate an unrelated
// line or group.
func (m *Model) resetToolLineTracking() {
	m.toolLines = make(map[string]toolLineRef)
	m.batchLines = make(map[string]*batchLineState)
	m.groupMembers = make(map[string]groupMemberRef)
	m.groupBatches = make(map[string]*activityGroupData)
}

// formatToolDuration renders a tool's call→result latency for the timeline:
// "4.2s" under a minute, "1m05s" from there on.
func formatToolDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	mins := int(d / time.Minute)
	secs := int(d/time.Second) % 60
	return fmt.Sprintf("%dm%02ds", mins, secs)
}

// formatElapsed renders the run stopwatch for the status line:
// "5s", "1m 05s", "1h 02m".
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d / time.Second)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	default:
		return fmt.Sprintf("%dh %02dm", s/3600, (s%3600)/60)
	}
}

// runElapsed computes the stopwatch value at now for a run started at start.
// pausedTotal is subtracted, and a non-zero pauseStart (an approval dialog is
// open) freezes the clock at the pause boundary.
func runElapsed(start, now time.Time, pausedTotal time.Duration, pauseStart time.Time) time.Duration {
	if start.IsZero() {
		return 0
	}
	end := now
	if !pauseStart.IsZero() && pauseStart.Before(end) {
		end = pauseStart
	}
	e := end.Sub(start) - pausedTotal
	if e < 0 {
		e = 0
	}
	return e
}

// currentRunElapsed returns the approval-adjusted elapsed time of the active
// agent run.
func (m *Model) currentRunElapsed() time.Duration {
	return runElapsed(m.promptStartTime, time.Now(), m.runPausedTotal, m.runPauseStart)
}

// beginRunPause freezes the run stopwatch while an approval dialog is open.
// Idempotent: queued dialogs shown back-to-back keep one open pause.
func (m *Model) beginRunPause() {
	if m.runPauseStart.IsZero() {
		m.runPauseStart = time.Now()
	}
}

// endRunPause folds an open pause into runPausedTotal and resumes the clock.
func (m *Model) endRunPause() {
	if !m.runPauseStart.IsZero() {
		m.runPausedTotal += time.Since(m.runPauseStart)
		m.runPauseStart = time.Time{}
	}
}

// resetRunClock starts a fresh stopwatch for a new agent run.
func (m *Model) resetRunClock() {
	m.promptStartTime = time.Now()
	m.runPausedTotal = 0
	m.runPauseStart = time.Time{}
	m.runningTools = 0
}

func (m *Model) flushText() {
	text := m.currentText.String()
	if text == "" {
		return
	}
	m.currentText.Reset()
	m.lastAssistantRawText = text
	rendered := text
	if m.mdRenderer != nil {
		if md, err := m.mdRenderer.Render(text); err == nil {
			rendered = md
		}
	}
	m.lines = append(m.lines, textLine(""))
	m.lines = append(m.lines, textLine(rendered))
	m.contentDirty = true
}

// recreateMDRenderer rebuilds the glamour markdown renderer with the current
// content width, ensuring WordWrap adapts to terminal resize.
func (m *Model) recreateMDRenderer() {
	width := m.contentWidth()
	if width < 40 {
		width = 40
	}
	if r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(currentTheme.GlamourStyle()),
		glamour.WithWordWrap(width-4), // account for left margin/padding
	); err == nil {
		m.mdRenderer = r
	}
}

// contentWidth returns the width available for the main content area,
// accounting for the sidebar when visible.
func (m *Model) contentWidth() int {
	if m.showSidebar {
		return m.width - sidebarWidth
	}
	return m.width
}

func (m *Model) renderContent() string {
	start := time.Now()
	m.renderPerf.contentRenderCalls++
	width := m.contentWidth()

	// Fast path: if content hasn't changed and width is the same, return
	// the cached base content with live parts appended.
	// We use string concatenation to avoid copying renderedContent into
	// a strings.Builder (which would duplicate the full history every frame).
	if !m.contentDirty && m.renderedLineWidth == width {
		m.renderPerf.contentCacheHits++
		result := m.renderedContent
		// Append live streaming text (changes on every AgentTextMsg).
		if m.currentText.Len() > 0 {
			result += "\n" + m.currentText.String() + "\n"
		}
		// Append thinking status line (changes on every spinner tick).
		if m.thinking && !m.agentDone {
			var sb strings.Builder
			m.appendStatusLine(&sb)
			result += sb.String()
		}
		m.observeContentRender(time.Since(start), len(result))
		return result
	}
	m.renderPerf.contentCacheMisses++

	// Slow path: re-render lines that need it.
	// We rebuild the cached base content from scratch and cache it.
	var sb strings.Builder
	for i := range m.lines {
		sb.WriteString(m.lines[i].render(width, m.mdRenderer))
		sb.WriteString("\n")
	}
	m.renderedContent = sb.String()
	m.renderedLineWidth = width

	// Append live parts.
	if m.currentText.Len() > 0 {
		sb.WriteString("\n")
		sb.WriteString(m.currentText.String())
		sb.WriteString("\n")
	}
	if m.thinking && !m.agentDone {
		m.appendStatusLine(&sb)
	}

	m.contentDirty = false
	result := sb.String()
	m.observeContentRender(time.Since(start), len(result))
	return result
}

// appendStatusLine writes the structured status block into sb:
//
//	<spinner> Working (1m 05s · esc interrupt)
//	  └ Shell: git push origin main
//
// The stopwatch excludes time spent waiting in approval dialogs. The subagent
// progress box keeps its dedicated rendering.
func (m *Model) appendStatusLine(sb *strings.Builder) {
	if m.hasSubagentDisplay() {
		sb.WriteString(m.renderSubagentBox())
		sb.WriteString("\n")
		steps, tokens := m.subagentTotals()
		tokenStr := ""
		if tokens > 0 {
			if m.modelContextLimit > 0 {
				pct := float64(tokens) / float64(m.modelContextLimit) * 100
				tokenStr = fmt.Sprintf(" %d tok / %.0f%%", tokens, pct)
			} else {
				tokenStr = fmt.Sprintf(" %d tok", tokens)
			}
		}
		label := fmt.Sprintf("Subagent [%d steps]...", steps)
		if n := m.activeSubagentCount(); n > 1 {
			label = fmt.Sprintf("%d subagents [%d steps]...", n, steps)
		}
		fmt.Fprintf(sb, "  %s %s%s",
			m.spinner.View(),
			subagentLabelStyle.Render(label),
			toolArgsStyle.Render(tokenStr),
		)
		sb.WriteString("\n")
		return
	}

	meta := "esc interrupt"
	if !m.promptStartTime.IsZero() {
		meta = formatElapsed(m.currentRunElapsed()) + " · " + meta
	}
	fmt.Fprintf(sb, "  %s %s %s", m.spinner.View(),
		shimmerText(time.Now()),
		toolArgsStyle.Render("("+meta+")"))

	if detail := m.statusDetail(); detail != "" {
		fmt.Fprintf(sb, "\n  %s %s", lipgloss.NewStyle().Foreground(colorMuted).Render("└"), detail)
	}
	sb.WriteString("\n")
}

// statusDetail returns the one-line current-activity row under the status
// line: the running tool's title + subtitle, or the concurrent batch count.
func (m *Model) statusDetail() string {
	switch {
	case m.runningTools > 1:
		return toolNameStyle.Render(fmt.Sprintf("%d tools running", m.runningTools))
	case m.pendingTool != "":
		title := m.pendingToolTitle
		if title == "" {
			title = m.pendingTool
		}
		s := toolNameStyle.Render(title)
		if m.pendingToolSubtitle != "" {
			s += toolArgsStyle.Render(": " + truncate(m.pendingToolSubtitle, 80))
		}
		return s
	case m.runningTools == 1:
		return toolNameStyle.Render("1 tool running")
	}
	return ""
}
