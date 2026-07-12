package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
)

func formatToolArgs(argsJSON string) string {
	if argsJSON == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return sanitize(argsJSON)
	}
	parts := make([]string, 0, len(args))
	for k, v := range args {
		val := sanitize(fmt.Sprintf("%v", v))
		parts = append(parts, fmt.Sprintf("%s=%s", k, val))
	}
	return strings.Join(parts, " ")
}

// ansiRe matches ANSI escape sequences (CSI, OSC, etc.).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x1b]*\x1b\\|\x1b[^\[\]]`)

// Pre-compiled regexes for tool result formatting (avoid per-call Compile).
var (
	skillNameRe  = regexp.MustCompile(`name="([^"]+)"`)
	skillDescRe  = regexp.MustCompile(`description="([^"]*)"`)
	teamMemberRe = regexp.MustCompile(`@(\S+)\s+status=(\S+)\s+type=(\S*)`)
	teamNameRe   = regexp.MustCompile(`^Team: (.+?) \(`)
)

// sanitize removes ANSI escape sequences and replaces control characters
// (except newline and tab) with their Unicode Control Pictures or a placeholder.
// This prevents special characters from corrupting the TUI layout.
func sanitize(s string) string {
	// Fast path: most LLM output doesn't contain ANSI or control chars.
	// Check for ESC byte before running the regex.
	hasANSI := strings.ContainsRune(s, '\x1b')
	hasControl := false
	if !hasANSI {
		// Quick scan for control characters and Private Use Area runes.
		for _, r := range s {
			if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f || unicode.Is(unicode.Co, r) {
				hasControl = true
				break
			}
		}
		if !hasControl {
			return s // clean string, return as-is
		}
	}

	// Strip ANSI escape sequences (only if present)
	if hasANSI {
		s = ansiRe.ReplaceAllString(s, "")
	}
	// Replace control characters (only if present or if ANSI was stripped)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20: // C0 control characters
			// Map to Unicode Control Pictures block (U+2400)
			b.WriteRune(0x2400 + r)
		case r == 0x7f: // DEL
			b.WriteRune('␡')
		case unicode.Is(unicode.Co, r): // Private Use Area - could break rendering
			b.WriteRune('�')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", "↲")
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// formatToolResult returns styled output lines depending on the tool name.
func formatToolResult(toolName, output string, termWidth int, expanded bool, mdRenderer *glamour.TermRenderer) []string {
	return formatToolResultBody(toolName, output, nil, termWidth, expanded, mdRenderer)
}

// formatToolResultBody returns styled output lines for a tool result with optional error.
func formatToolResultBody(toolName, output string, err error, termWidth int, expanded bool, mdRenderer *glamour.TermRenderer) []string {
	if err != nil {
		errText := truncate(sanitize(err.Error()), maxToolOutputLen)
		return []string{
			fmt.Sprintf("    %s %s",
				toolErrorStyle.Render("Error:"),
				lipgloss.NewStyle().Foreground(colorError).Render(errText)),
		}
	}

	switch toolName {
	case "execute":
		return formatExecuteOutput(output, termWidth)
	case "edit":
		return formatEditOutput(output, termWidth)
	case "subagent":
		return formatSubagentOutput(output, termWidth, expanded, mdRenderer)
	case "todowrite":
		return formatTodoWriteOutput(output)
	case "load_skill":
		return formatLoadSkillOutput(output)
	case "team_list":
		return formatTeamListOutput(output)
	case "team_send_message", "team_create", "team_spawn", "team_delete":
		return formatTeamShortOutput(output)
	default:
		return formatDefaultOutput(output, termWidth)
	}
}

// ─── Row-aware head/tail truncation ───

// transcriptHint is the trailing pointer on hidden-lines markers, steering the
// user to the full-output transcript overlay. Keep in sync with the key
// bindings in update.go (ctrl+t; ctrl+o during team sessions).
const transcriptHint = "ctrl+t transcript"

// toolBoxWidths returns the outer box width and the approximate inner text
// wrap width for a tool output box at the given terminal width.
func toolBoxWidths(termWidth int) (boxWidth, wrapWidth int) {
	boxWidth = termWidth - 8
	if boxWidth < 30 {
		boxWidth = 30
	}
	wrapWidth = boxWidth - 2 // left border + padding
	if wrapWidth < 20 {
		wrapWidth = 20
	}
	return boxWidth, wrapWidth
}

// displayRows returns how many terminal rows s occupies when soft-wrapped at
// width columns. Zero/negative width counts as a single row.
func displayRows(s string, width int) int {
	if width <= 0 {
		return 1
	}
	w := xansi.StringWidth(s)
	if w <= width {
		return 1
	}
	return (w + width - 1) / width
}

// capLineToRows truncates one logical line so it wraps into at most rows
// display rows at width, appending "…" when content was cut. Grapheme
// clusters are never split.
func capLineToRows(line string, rows, width int) string {
	if rows < 1 {
		rows = 1
	}
	if width <= 0 || displayRows(line, width) <= rows {
		return line
	}
	return xansi.Truncate(line, rows*width, "…")
}

// takeDisplayRows collects logical lines from the front (fromEnd=false) or the
// back (fromEnd=true) of lines until budget display rows are used. A line that
// alone overflows the remaining budget is capped to fit (trailing "…") and
// ends the take, so a single huge line can never blow up the viewport.
func takeDisplayRows(lines []string, budget, width int, fromEnd bool) (kept []string, taken int) {
	remaining := budget
	for i := 0; i < len(lines) && remaining > 0; i++ {
		idx := i
		if fromEnd {
			idx = len(lines) - 1 - i
		}
		line := lines[idx]
		rows := displayRows(line, width)
		if rows > remaining {
			line = capLineToRows(line, remaining, width)
			rows = remaining
		}
		if fromEnd {
			kept = append([]string{line}, kept...)
		} else {
			kept = append(kept, line)
		}
		taken++
		remaining -= rows
	}
	return kept, taken
}

// hiddenLinesMarker renders the "… +K lines" separator row. K counts logical
// lines, so the copy stays stable across terminal widths.
func hiddenLinesMarker(k int) string {
	return lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
		Render(fmt.Sprintf("… +%d lines (%s)", k, transcriptHint))
}

// headTailTruncate keeps the first headRows and the last tailRows display rows
// of lines (as wrapped at width) and replaces the middle with a hidden-lines
// marker — output tails often carry the error message, so both ends matter.
// Content that already fits within headRows+tailRows+1 rows is returned as-is.
func headTailTruncate(lines []string, headRows, tailRows, width int) string {
	totalRows := 0
	for _, l := range lines {
		totalRows += displayRows(l, width)
	}
	if totalRows <= headRows+tailRows+1 {
		return strings.Join(lines, "\n")
	}

	head, headTaken := takeDisplayRows(lines, headRows, width, false)
	tail, tailTaken := takeDisplayRows(lines[headTaken:], tailRows, width, true)
	hidden := len(lines) - headTaken - tailTaken

	parts := make([]string, 0, len(head)+len(tail)+1)
	parts = append(parts, head...)
	if hidden > 0 {
		parts = append(parts, hiddenLinesMarker(hidden))
	}
	parts = append(parts, tail...)
	return strings.Join(parts, "\n")
}

// formatDefaultOutput renders tool output with left border. Long outputs keep
// the first and last few display rows with an "… +K lines" marker in between;
// budgets count wrapped rows, so overlong single lines are capped too.
func formatDefaultOutput(output string, termWidth int) []string {
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return nil
	}

	// Head/tail row budget. Total shown is ≤ 2*edgeRows+1 rows, close to the
	// old 6-line head-only budget.
	const edgeRows = 3

	boxWidth, wrapWidth := toolBoxWidths(termWidth)
	body := headTailTruncate(strings.Split(output, "\n"), edgeRows, edgeRows, wrapWidth)

	box := toolBodyStyle.Width(boxWidth).Render(body)
	return []string{box}
}

// formatExecuteOutput shows the tail of command output with left border — the
// end is where errors and summaries land. The tail budget counts wrapped
// display rows, so one overlong line cannot flood the viewport.
func formatExecuteOutput(output string, termWidth int) []string {
	const tailRows = 5

	boxWidth, wrapWidth := toolBoxWidths(termWidth)
	rawLines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	totalRows := 0
	for _, l := range rawLines {
		totalRows += displayRows(l, wrapWidth)
	}

	var parts []string
	if totalRows <= tailRows+1 {
		parts = rawLines
	} else {
		tail, taken := takeDisplayRows(rawLines, tailRows, wrapWidth, true)
		if hidden := len(rawLines) - taken; hidden > 0 {
			parts = append(parts, hiddenLinesMarker(hidden))
		}
		parts = append(parts, tail...)
	}

	box := toolBodyStyle.Width(boxWidth).Render(strings.Join(parts, "\n"))
	return []string{box}
}

// formatEditOutput renders the edit result with colored diff lines.
func formatEditOutput(output string, termWidth int) []string {
	// Split output into status line and diff block
	parts := strings.SplitN(output, "\n\n", 2)
	statusLine := parts[0]

	result := []string{
		fmt.Sprintf("    %s", lipgloss.NewStyle().Foreground(colorDimText).Render(statusLine)),
	}

	if len(parts) < 2 {
		return result
	}

	// Parse the diff block (```diff ... ```)
	diffBlock := parts[1]
	diffBlock = strings.TrimPrefix(diffBlock, "```diff\n")
	diffBlock = strings.TrimSuffix(diffBlock, "```")
	diffBlock = strings.TrimRight(diffBlock, "\n")

	if diffBlock == "" {
		return result
	}

	var diffContent strings.Builder
	for _, line := range strings.Split(diffBlock, "\n") {
		switch {
		case strings.HasPrefix(line, "+ "):
			diffContent.WriteString(diffAddStyle.Render(line))
		case strings.HasPrefix(line, "- "):
			diffContent.WriteString(diffRemoveStyle.Render(line))
		default:
			diffContent.WriteString(line)
		}
		diffContent.WriteString("\n")
	}

	boxWidth := termWidth - 8
	if boxWidth < 30 {
		boxWidth = 30
	}

	diffBox := toolBodyStyle.Width(boxWidth).Render(strings.TrimRight(diffContent.String(), "\n"))
	result = append(result, diffBox)
	return result
}

// formatSubagentOutput renders subagent output with markdown support.
// When collapsed, it shows a limited number of lines with a hint to expand.
// When expanded, it shows the full rendered markdown output.
func formatSubagentOutput(output string, termWidth int, expanded bool, mdRenderer *glamour.TermRenderer) []string {
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return nil
	}

	// Render markdown via glamour if available.
	rendered := output
	if mdRenderer != nil {
		if md, err := mdRenderer.Render(output); err == nil {
			rendered = strings.TrimRight(md, "\n")
		}
	}

	const collapsedLines = 12
	rawLines := strings.Split(rendered, "\n")

	boxWidth := termWidth - 8
	if boxWidth < 30 {
		boxWidth = 30
	}

	if expanded || len(rawLines) <= collapsedLines {
		// Show all content.
		var boxContent strings.Builder
		for i, line := range rawLines {
			boxContent.WriteString(line)
			if i < len(rawLines)-1 {
				boxContent.WriteString("\n")
			}
		}
		if expanded && len(rawLines) > collapsedLines {
			boxContent.WriteString("\n")
			boxContent.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
				Render("▲ ctrl+e collapse"))
		}
		box := subagentBodyStyle.Width(boxWidth).Render(boxContent.String())
		return []string{box}
	}

	// Collapsed: show limited lines.
	shown := rawLines[:collapsedLines]
	hidden := len(rawLines) - collapsedLines

	var boxContent strings.Builder
	for i, line := range shown {
		boxContent.WriteString(line)
		if i < len(shown)-1 {
			boxContent.WriteString("\n")
		}
	}
	boxContent.WriteString("\n")
	boxContent.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
		Render(fmt.Sprintf("… %d more lines (ctrl+e expand)", hidden)))

	box := subagentBodyStyle.Width(boxWidth).Render(boxContent.String())
	return []string{box}
}

// todoSummaryRe matches the summary sentence both todowrite variants emit:
// "8 todos (3 completed, 1 in_progress, …)". Capture groups: total, completed.
var todoSummaryRe = regexp.MustCompile(`(\d+) todos \((\d+) completed, (\d+) in_progress`)

// formatTodoWriteOutput collapses a todowrite result to one minimal, muted
// change-summary line ("✓ Todos 3/8 · <current task>") — the authoritative
// list lives in the sidebar todo panel, so the timeline stays quiet.
func formatTodoWriteOutput(output string) []string {
	if m := todoSummaryRe.FindStringSubmatch(output); m != nil {
		total, _ := strconv.Atoi(m[1])
		completed, _ := strconv.Atoi(m[2])
		return []string{todoSummaryLine(completed, total, todoInProgressTitle(output))}
	}
	// Unrecognized output (future formats): keep the old first-line summary.
	summary := strings.SplitN(output, "\n", 2)[0]
	if summary == "" {
		summary = "updated"
	}
	return []string{
		fmt.Sprintf("   %s %s", toolSuccessStyle.Render("✓"), toolArgsStyle.Render(summary)),
	}
}

// todoSummaryLine renders the shared "✓ Todos N/M · current" row used by both
// the live todowrite result and the session-replay todo snapshot.
func todoSummaryLine(completed, total int, current string) string {
	text := fmt.Sprintf("Todos %d/%d", completed, total)
	if current != "" {
		text += " · " + truncate(current, 40)
	}
	return fmt.Sprintf("   %s %s", toolSuccessStyle.Render("✓"), toolArgsStyle.Render(text))
}

// todoInProgressTitle extracts the in-progress item's title from the JSON
// payload that follows a todowrite result's summary line, "" when absent.
func todoInProgressTitle(output string) string {
	idx := strings.IndexByte(output, '\n')
	if idx < 0 {
		return ""
	}
	var items []struct {
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(output[idx+1:]), &items); err != nil {
		return ""
	}
	for _, it := range items {
		if it.Status == "in_progress" {
			return it.Title
		}
	}
	return ""
}

// formatLoadSkillOutput shows skill name + description, skipping the full markdown body.
func formatLoadSkillOutput(output string) []string {
	nameMatch := skillNameRe.FindStringSubmatch(output)
	descMatch := skillDescRe.FindStringSubmatch(output)
	name := ""
	desc := ""
	if len(nameMatch) > 1 {
		name = nameMatch[1]
	}
	if len(descMatch) > 1 {
		desc = descMatch[1]
	}
	if name == "" {
		return formatDefaultOutput(output, 80)
	}
	line := fmt.Sprintf("   %s %s",
		toolSuccessStyle.Render("✓"),
		lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(name))
	if desc != "" {
		line += "  " + lipgloss.NewStyle().Foreground(colorMuted).Italic(true).Render(desc)
	}
	return []string{line}
}

// formatTeamListOutput renders team_list output as a structured member list.
func formatTeamListOutput(output string) []string {
	var result []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if m := teamNameRe.FindStringSubmatch(line); len(m) > 1 {
			result = append(result, fmt.Sprintf("   %s  %s",
				lipgloss.NewStyle().Foreground(colorDimText).Render("team"),
				lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(m[1])))
		} else if m := teamMemberRe.FindStringSubmatch(line); len(m) > 2 {
			statusColor := colorMuted
			if m[2] == "running" || m[2] == "busy" {
				statusColor = colorPrimary
			}
			memberLine := fmt.Sprintf("   %s  %-16s  %s",
				lipgloss.NewStyle().Foreground(statusColor).Render("●"),
				lipgloss.NewStyle().Foreground(colorText).Render("@"+m[1]),
				lipgloss.NewStyle().Foreground(colorMuted).Render(m[2]))
			if len(m) > 3 && m[3] != "" {
				memberLine += "  " + lipgloss.NewStyle().Foreground(colorDimText).Render(m[3])
			}
			result = append(result, memberLine)
		}
	}
	if len(result) == 0 {
		return formatDefaultOutput(output, 80)
	}
	return result
}

// formatTeamShortOutput shows the first line of a team operation result as a compact status.
func formatTeamShortOutput(output string) []string {
	summary := strings.SplitN(strings.TrimRight(output, "\n"), "\n", 2)[0]
	if summary == "" {
		summary = "done"
	}
	return []string{
		fmt.Sprintf("   %s %s",
			toolSuccessStyle.Render("✓"),
			lipgloss.NewStyle().Foreground(colorDimText).Render(truncate(summary, 80))),
	}
}
