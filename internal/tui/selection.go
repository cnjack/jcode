package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"
)

// viewportOffsetY returns the Y offset of the viewport in screen coordinates.
// This accounts for the header + divider lines above the viewport.
func (m *Model) viewportOffsetY() int {
	// header title + divider line = 2 lines total
	return 2
}

// handleMouseClick processes a left-click to start text selection.
// Coordinates are stored as viewport-visible-relative (not content-absolute).
func (m *Model) handleMouseClick(x, y int) {
	vpOffsetY := m.viewportOffsetY()
	vpY := y - vpOffsetY

	// Only track selection inside the viewport area
	if vpY < 0 || vpY >= m.viewport.Height() {
		return
	}

	// Clear any previous selection
	m.hasSelection = false
	m.mouseSelecting = true

	m.mouseStartX = x
	m.mouseStartY = vpY
	m.mouseEndX = x
	m.mouseEndY = vpY
}

// handleMouseDrag updates the selection endpoint during dragging.
func (m *Model) handleMouseDrag(x, y int) {
	if !m.mouseSelecting {
		return
	}

	vpOffsetY := m.viewportOffsetY()
	vpY := y - vpOffsetY

	// Clamp to viewport bounds
	if vpY < 0 {
		vpY = 0
	}
	if vpY >= m.viewport.Height() {
		vpY = m.viewport.Height() - 1
	}
	if x < 0 {
		x = 0
	}

	m.mouseEndX = x
	m.mouseEndY = vpY

	// Mark as having a valid selection if start != end
	m.hasSelection = (m.mouseStartX != m.mouseEndX || m.mouseStartY != m.mouseEndY)
}

// handleMouseRelease finishes selection and copies text if applicable.
func (m *Model) handleMouseRelease(x, y int) tea.Cmd {
	if !m.mouseSelecting {
		return nil
	}

	m.mouseSelecting = false

	// Update final endpoint
	vpOffsetY := m.viewportOffsetY()
	vpY := y - vpOffsetY
	if vpY < 0 {
		vpY = 0
	}
	if vpY >= m.viewport.Height() {
		vpY = m.viewport.Height() - 1
	}
	m.mouseEndX = x
	m.mouseEndY = vpY

	m.hasSelection = (m.mouseStartX != m.mouseEndX || m.mouseStartY != m.mouseEndY)

	if !m.hasSelection {
		return nil
	}

	// Extract selected text from the rendered content
	text := m.extractSelectedText()
	if text == "" {
		m.hasSelection = false
		return nil
	}

	// Copy to clipboard: OSC 52 + native clipboard
	return tea.Sequence(
		tea.SetClipboard(text),
		func() tea.Msg {
			_ = clipboard.WriteAll(text)
			return CopyNoticeMsg{Message: "Selected text copied"}
		},
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return CopyNoticeTimeoutMsg{}
		}),
	)
}

// extractSelectedText extracts plain text from the currently visible
// viewport content between the selection start and end positions.
// Uses display-column (not byte) positions, properly handling multi-byte
// and wide characters (CJK, emoji).
// It detects soft-wrapped lines (produced by glamour word wrapping) and
// merges them into single logical lines instead of inserting false newlines.
func (m *Model) extractSelectedText() string {
	content := m.viewport.View()
	lines := strings.Split(content, "\n")

	// Selection coords are viewport-visible-relative, in display columns
	startY, startX := m.mouseStartY, m.mouseStartX
	endY, endX := m.mouseEndY, m.mouseEndX

	// Ensure start is before end
	if startY > endY || (startY == endY && startX > endX) {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}
	// endX is exclusive: the mouse points AT the last char, so +1 to include it.
	endX++

	// Clamp to available lines
	if startY < 0 {
		startY = 0
		startX = 0
	}
	if startY >= len(lines) {
		return ""
	}
	if endY >= len(lines) {
		endY = len(lines) - 1
	}

	// Determine the wrap width used by the renderer.
	// The viewport width is the effective display width.
	vpWidth := m.viewport.Width()
	if vpWidth <= 0 {
		vpWidth = 100
	}

	var sb strings.Builder

	for lineIdx := startY; lineIdx <= endY; lineIdx++ {
		// Strip ANSI escape codes to get plain text
		plain := ansi.Strip(lines[lineIdx])

		sCol := 0
		eCol := -1 // -1 means end of line

		if lineIdx == startY {
			sCol = startX
		}
		if lineIdx == endY {
			eCol = endX
		}

		extracted := sliceByDisplayCol(plain, sCol, eCol)
		sb.WriteString(extracted)

		if lineIdx < endY {
			// Decide whether to emit a real newline or merge (soft wrap).
			// A line is soft-wrapped if its visible text fills the full
			// viewport width. We measure the stripped text (no ANSI) with
			// trailing spaces trimmed, because lipgloss pads styled blocks
			// to the full width.
			plainWidth := ansi.StringWidth(strings.TrimRight(plain, " "))
			if plainWidth >= vpWidth {
				// Soft-wrapped line: don't add newline, the next line
				// is a continuation of this one.
			} else {
				sb.WriteString("\n")
			}
		}
	}

	return strings.TrimSpace(sb.String())
}

// sliceByDisplayCol extracts a substring from plain text (no ANSI) based on
// display column positions. Uses ansi.DecodeSequence for grapheme-cluster-aware
// width calculation, consistent with highlightLine.
// endCol == -1 means to end of string.
func sliceByDisplayCol(s string, startCol, endCol int) string {
	var sb strings.Builder
	var state byte
	p := ansi.NewParser()
	col := 0
	input := s

	for len(input) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(input, state, p)
		state = newState

		if width == 0 {
			// Control character in plain text — skip
			input = input[n:]
			continue
		}

		if endCol >= 0 && col >= endCol {
			break
		}
		if col+width > startCol {
			sb.WriteString(seq)
		}

		col += width
		input = input[n:]
	}
	return sb.String()
}

// clearSelection resets the mouse selection state.
func (m *Model) clearSelection() {
	m.mouseSelecting = false
	m.hasSelection = false
	m.mouseStartX = 0
	m.mouseStartY = 0
	m.mouseEndX = 0
	m.mouseEndY = 0
}

// applySelectionHighlight adds reverse-video highlighting to the viewport
// content for the current selection range.
func (m *Model) applySelectionHighlight(vpContent string) string {
	if !m.hasSelection && !m.mouseSelecting {
		return vpContent
	}

	lines := strings.Split(vpContent, "\n")

	// Get normalized selection range (start before end)
	startY, startX := m.mouseStartY, m.mouseStartX
	endY, endX := m.mouseEndY, m.mouseEndX
	if startY > endY || (startY == endY && startX > endX) {
		startY, endY = endY, startY
		startX, endX = endX, startX
	}
	// endX is exclusive: the mouse points AT the last char, so +1 to include it.
	// This prevents the common "last character not selected" issue.
	endX++

	for i := range lines {
		if i < startY || i > endY {
			continue
		}

		sCol := 0
		eCol := -1 // -1 means end of line

		if i == startY {
			sCol = startX
		}
		if i == endY {
			eCol = endX
		}

		lines[i] = highlightLine(lines[i], sCol, eCol)
	}

	return strings.Join(lines, "\n")
}

// highlightLine applies reverse-video ANSI escape to a portion of a line.
// It uses ansi.DecodeSequence to properly parse all ANSI sequences (CSI, OSC,
// hyperlinks, etc.) so that escape bytes are never miscounted as visible columns.
// It re-applies reverse video after any escape sequence within the highlighted
// region, because SGR resets (\x1b[0m) in styled content (code blocks, colored
// text) would otherwise kill the highlight.
// endCol == -1 means highlight to end of line.
func highlightLine(line string, startCol, endCol int) string {
	if startCol == endCol {
		return line
	}

	var result strings.Builder
	result.Grow(len(line) + 40)

	var state byte
	p := ansi.NewParser()
	visCol := 0
	highlighted := false
	input := line

	for len(input) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(input, state, p)
		state = newState

		if width == 0 {
			// Control or escape sequence — pass through without advancing visCol
			result.WriteString(seq)
			// Re-apply reverse video: the content may contain SGR resets
			// (\x1b[0m) that clear all attributes including our reverse video.
			if highlighted {
				result.WriteString("\x1b[7m")
			}
		} else {
			// Printable grapheme cluster with display width > 0
			// Check if we need to start highlighting.
			if !highlighted && visCol+width > startCol && visCol <= startCol {
				result.WriteString("\x1b[7m") // reverse video ON
				highlighted = true
			}
			// Check if we need to stop highlighting.
			if highlighted && endCol >= 0 && visCol >= endCol {
				result.WriteString("\x1b[27m") // reverse video OFF
				highlighted = false
			}
			result.WriteString(seq)
			visCol += width
		}

		input = input[n:]
	}

	// If highlight extends to end of line, close it
	if highlighted {
		result.WriteString("\x1b[27m")
	}

	return result.String()
}
