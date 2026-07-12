package tui

import (
	"strings"
	"time"

	"charm.land/glamour/v2"
)

// --- Content line types for resize-aware rendering ---

// contentLine represents a line in the conversation display.
// Most lines are plain rendered text; tool results are stored as structured
// data so they can be re-rendered when the terminal width changes.
type contentLine struct {
	text  string             // plain rendered text (default)
	tool  *toolResultData    // non-nil for tool results that need dynamic rendering
	group *activityGroupData // non-nil for structured activity-group lines

	// cachedRender holds the last rendered output for this line.
	// It is invalidated when the terminal width changes (resize) and, for
	// group lines, when the group's revision moves (cachedGroupRev).
	cachedRender   string
	cachedWidth    int
	cachedGroupRev int
}

// toolResultData stores the raw data for a tool result, allowing
// re-rendering with the current terminal width on resize.
type toolResultData struct {
	name     string
	output   string
	err      error         // non-nil for error results
	expanded bool          // true when subagent output is expanded (full markdown)
	duration time.Duration // call→result latency; 0 when unknown (legacy/replay)
}

// textLine creates a plain text content line.
func textLine(s string) contentLine {
	return contentLine{text: s}
}

// toolResultContentLine creates a tool result content line from raw data.
func toolResultContentLine(name, output string, err error) contentLine {
	return contentLine{tool: &toolResultData{name: name, output: output, err: err}}
}

// render returns the rendered string for this content line, using the
// given width for tool result boxes. Plain text lines are returned as-is.
// Results are cached to avoid redundant lipgloss/glamour re-computation.
func (cl *contentLine) render(width int, mdRenderer *glamour.TermRenderer) string {
	// Activity-group lines re-render whenever the group's revision moved
	// (member added / result landed), so state flips need no string backfill.
	if cl.group != nil {
		if cl.cachedRender != "" && cl.cachedWidth == width && cl.cachedGroupRev == cl.group.rev {
			return cl.cachedRender
		}
		result := cl.group.render()
		cl.cachedRender = result
		cl.cachedWidth = width
		cl.cachedGroupRev = cl.group.rev
		return result
	}
	// Fast path: return cached result if width hasn't changed.
	if cl.cachedRender != "" && cl.cachedWidth == width {
		return cl.cachedRender
	}
	var result string
	if cl.tool != nil {
		lines := formatToolResultBody(cl.tool.name, cl.tool.output, cl.tool.err, width, cl.tool.expanded, mdRenderer)
		result = strings.Join(lines, "\n")
	} else {
		result = cl.text
	}
	cl.cachedRender = result
	cl.cachedWidth = width
	return result
}

// toContentLines converts a []string to []contentLine.
func toContentLines(ss []string) []contentLine {
	result := make([]contentLine, len(ss))
	for i, s := range ss {
		result[i] = contentLine{text: s}
	}
	return result
}
