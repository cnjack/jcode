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
		if cl.tool != nil {
			// Tool results are multi-line boxes; estimate conservatively.
			n = maxRenderedLines(cl.tool, m.contentWidth())
		} else {
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

// replaceLastToolIcon replaces the status icon on the last tool call line.
func (m *Model) replaceLastToolIcon(newIcon string) {
	for i := len(m.lines) - 1; i >= 0; i-- {
		line := m.lines[i]
		if strings.Contains(line.text, toolIconRunning) {
			m.lines[i] = contentLine{text: strings.Replace(line.text, toolIconRunning, newIcon, 1)}
			return
		}
		if strings.Contains(line.text, toolIconPending) {
			m.lines[i] = contentLine{text: strings.Replace(line.text, toolIconPending, newIcon, 1)}
			return
		}
	}
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

// appendStatusLine writes the spinner / thinking status line into sb.
func (m *Model) appendStatusLine(sb *strings.Builder) {
	switch {
	case m.subagentActive && len(m.subagentProgress) > 0:
		sb.WriteString(m.renderSubagentBox())
		sb.WriteString("\n")
		tokenStr := ""
		if m.subagentTokens > 0 {
			if m.modelContextLimit > 0 {
				pct := float64(m.subagentTokens) / float64(m.modelContextLimit) * 100
				tokenStr = fmt.Sprintf(" %d tok / %.0f%%", m.subagentTokens, pct)
			} else {
				tokenStr = fmt.Sprintf(" %d tok", m.subagentTokens)
			}
		}
		fmt.Fprintf(sb, "  %s %s%s",
			m.spinner.View(),
			subagentLabelStyle.Render(fmt.Sprintf("Subagent [%d steps]...", m.subagentStepCount)),
			toolArgsStyle.Render(tokenStr),
		)
	case m.pendingTool != "":
		fmt.Fprintf(sb, "  %s Running %s...", m.spinner.View(), toolNameStyle.Render(m.pendingTool))
	default:
		fmt.Fprintf(sb, "  %s Thinking...", m.spinner.View())
	}
	sb.WriteString("\n")
}

// renderSubagentBox returns a bordered box showing live subagent tool calls.
// Results are cached until subagentProgress changes or width changes.
func (m *Model) renderSubagentBox() string {
	width := m.contentWidth()
	if m.subagentBoxCache != "" && m.subagentBoxCacheLen == len(m.subagentProgress) && m.subagentBoxCacheWidth == width {
		return m.subagentBoxCache
	}

	const maxVisible = 8
	lines := m.subagentProgress
	hidden := 0
	if len(lines) > maxVisible {
		hidden = len(lines) - maxVisible
		lines = lines[hidden:]
	}

	var content strings.Builder
	if hidden > 0 {
		content.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
			Render(fmt.Sprintf("... (%d earlier steps)", hidden)))
		content.WriteString("\n")
	}
	for i, line := range lines {
		content.WriteString(line)
		if i < len(lines)-1 {
			content.WriteString("\n")
		}
	}

	boxWidth := width - 8
	if boxWidth < 30 {
		boxWidth = 30
	}

	box := subagentBoxStyle.Width(boxWidth).Render(content.String())
	m.subagentBoxCache = box
	m.subagentBoxCacheLen = len(m.subagentProgress)
	m.subagentBoxCacheWidth = width
	return box
}
