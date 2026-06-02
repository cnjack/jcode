package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
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

// formatDefaultOutput renders tool output with left border, truncating if too many lines.
func formatDefaultOutput(output string, termWidth int) []string {
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return nil
	}

	const maxLines = 6
	rawLines := strings.Split(output, "\n")

	shown := rawLines
	hidden := 0
	if len(rawLines) > maxLines {
		shown = rawLines[:maxLines]
		hidden = len(rawLines) - maxLines
	}

	var boxContent strings.Builder
	for i, line := range shown {
		boxContent.WriteString(line)
		if i < len(shown)-1 {
			boxContent.WriteString("\n")
		}
	}
	if hidden > 0 {
		boxContent.WriteString("\n")
		boxContent.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
			Render(fmt.Sprintf("… %d more lines", hidden)))
	}

	boxWidth := termWidth - 8
	if boxWidth < 30 {
		boxWidth = 30
	}

	box := toolBodyStyle.Width(boxWidth).Render(boxContent.String())
	return []string{box}
}

// formatExecuteOutput shows the last 5 lines of command output with left border.
func formatExecuteOutput(output string, termWidth int) []string {
	const tailLines = 5
	rawLines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	// Take last N lines
	start := 0
	if len(rawLines) > tailLines {
		start = len(rawLines) - tailLines
	}
	tail := rawLines[start:]

	var boxContent strings.Builder
	if start > 0 {
		boxContent.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
			Render(fmt.Sprintf("… %d lines hidden", start)))
		boxContent.WriteString("\n")
	}
	for i, line := range tail {
		boxContent.WriteString(line)
		if i < len(tail)-1 {
			boxContent.WriteString("\n")
		}
	}

	boxWidth := termWidth - 8
	if boxWidth < 30 {
		boxWidth = 30
	}

	box := toolBodyStyle.Width(boxWidth).Render(boxContent.String())
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

// formatTodoWriteOutput renders todowrite result as a compact single line.
// The full state is visible in the todo bar, so just show the summary line.
func formatTodoWriteOutput(output string) []string {
	summary := strings.SplitN(output, "\n", 2)[0]
	if summary == "" {
		summary = "updated"
	}
	return []string{
		fmt.Sprintf("   %s %s", toolSuccessStyle.Render("✓"), toolArgsStyle.Render(summary)),
	}
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
