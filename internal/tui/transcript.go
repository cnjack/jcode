package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ─── Full-screen transcript overlay ───
//
// ctrl+t (ctrl+o during team sessions, where ctrl+t keeps toggling the
// coordinator panel) opens the complete session timeline with tool output
// rendered untruncated. The content is a snapshot taken at open time — no
// live tail — which keeps the overlay a plain pager; reopen to refresh.

// openTranscript enters the transcript overlay, positioned at the bottom
// (most recent entries).
func (m *Model) openTranscript() {
	if m.showingTranscript || m.width <= 0 || m.height <= 0 {
		return
	}
	m.showingTranscript = true
	m.textarea.Blur()
	m.transcriptVP = viewport.New(
		viewport.WithWidth(m.width),
		viewport.WithHeight(m.transcriptViewportHeight()),
	)
	m.transcriptVP.SoftWrap = true
	m.transcriptVP.SetContent(m.renderTranscript(m.width))
	m.transcriptVP.GotoBottom()
}

// closeTranscript leaves the overlay and restores input focus.
func (m *Model) closeTranscript() {
	m.showingTranscript = false
	m.textarea.Focus()
}

// transcriptViewportHeight reserves one row each for the header and the key
// hint bar.
func (m *Model) transcriptViewportHeight() int {
	h := m.height - 2
	if h < 3 {
		h = 3
	}
	return h
}

// resizeTranscript re-fits the overlay after a terminal resize.
func (m *Model) resizeTranscript() {
	if !m.showingTranscript {
		return
	}
	atBottom := m.transcriptVP.AtBottom()
	m.transcriptVP.SetWidth(m.width)
	m.transcriptVP.SetHeight(m.transcriptViewportHeight())
	m.transcriptVP.SetContent(m.renderTranscript(m.width))
	if atBottom {
		m.transcriptVP.GotoBottom()
	}
}

// handleTranscriptKey processes all keys while the overlay is open.
func (m *Model) handleTranscriptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "ctrl+t", "ctrl+o", "ctrl+c":
		m.closeTranscript()
		return m, nil
	case "g", "home":
		m.transcriptVP.GotoTop()
		return m, nil
	case "G", "shift+g", "end":
		m.transcriptVP.GotoBottom()
		return m, nil
	default:
		// ↑/↓/j/k/PgUp/PgDn handled by the viewport's default keymap.
		var cmd tea.Cmd
		m.transcriptVP, cmd = m.transcriptVP.Update(msg)
		return m, cmd
	}
}

// transcriptView renders the full-screen overlay: header, pager, hint bar
// with scroll percentage.
func (m *Model) transcriptView() string {
	w := m.width
	if w <= 0 {
		w = 80
	}

	header := " " +
		lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render("Transcript") +
		toolArgsStyle.Render("  full tool output · snapshot at open")

	hints := " ↑/↓ pgup/pgdn scroll · g/G top/bottom · esc close"
	pct := fmt.Sprintf("%3.0f%% ", m.transcriptVP.ScrollPercent()*100)
	gap := w - lipgloss.Width(hints) - lipgloss.Width(pct)
	if gap < 1 {
		gap = 1
	}
	footer := lipgloss.NewStyle().Foreground(colorMuted).
		Render(hints + strings.Repeat(" ", gap) + pct)

	return header + "\n" + m.transcriptVP.View() + "\n" + footer
}

// renderTranscript renders the whole timeline. Text lines are reused as-is
// (they carry their own styling); tool results are re-rendered without any
// truncation, plus duration metadata when known.
func (m *Model) renderTranscript(width int) string {
	var sb strings.Builder
	for i := range m.lines {
		cl := &m.lines[i]
		switch {
		case cl.group != nil:
			// Activity groups always render fully expanded here: every
			// member with its complete output and duration.
			sb.WriteString(renderTranscriptGroup(cl.group, width))
		case cl.tool != nil:
			sb.WriteString(renderTranscriptTool(cl.tool, width))
		default:
			sb.WriteString(cl.text)
		}
		sb.WriteString("\n")
	}
	// Include any in-flight streaming text in the snapshot.
	if m.currentText.Len() > 0 {
		sb.WriteString("\n")
		sb.WriteString(m.currentText.String())
		sb.WriteString("\n")
	}
	return sb.String()
}

// renderTranscriptTool renders one tool result untruncated: full output box,
// full error text, and a dim duration row when the latency is known.
func renderTranscriptTool(tool *toolResultData, width int) string {
	boxWidth := width - 8
	if boxWidth < 30 {
		boxWidth = 30
	}

	meta := ""
	if tool.duration > 0 {
		meta = "\n    " + toolArgsStyle.Render("⏱ "+formatToolDuration(tool.duration))
	}

	if tool.err != nil {
		return fmt.Sprintf("    %s %s%s",
			toolErrorStyle.Render("Error:"),
			lipgloss.NewStyle().Foreground(colorError).Render(sanitize(tool.err.Error())),
			meta)
	}

	output := strings.TrimRight(sanitize(tool.output), "\n")
	if output == "" {
		return "    " + toolArgsStyle.Render("(no output)") + meta
	}
	style := toolBodyStyle
	if tool.name == "subagent" {
		style = subagentBodyStyle
	}
	return style.Width(boxWidth).Render(output) + meta
}
