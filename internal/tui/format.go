package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

func formatToolArgs(argsJSON string) string {
	if argsJSON == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return truncate(argsJSON, 120)
	}
	parts := make([]string, 0, len(args))
	for k, v := range args {
		val := truncate(fmt.Sprintf("%v", v), 60)
		parts = append(parts, fmt.Sprintf("%s=%s", k, val))
	}
	return truncate(strings.Join(parts, " "), 200)
}

// ansiRe matches ANSI escape sequences (CSI, OSC, etc.).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x1b]*\x1b\\|\x1b[^\[\]]`)

// sanitize removes ANSI escape sequences and replaces control characters
// (except newline and tab) with their Unicode Control Pictures or a placeholder.
// This prevents special characters from corrupting the TUI layout.
func sanitize(s string) string {
	// Strip ANSI escape sequences
	s = ansiRe.ReplaceAllString(s, "")
	// Replace control characters
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
	s = sanitize(s)
	s = strings.ReplaceAll(s, "\n", "↲")
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// formatToolResult returns styled output lines depending on the tool name.
func formatToolResult(toolName, output string, termWidth int) []string {
	switch toolName {
	case "execute":
		return formatExecuteOutput(output, termWidth)
	case "edit":
		return formatEditOutput(output, termWidth)
	case "subagent":
		return formatSubagentOutput(output, termWidth)
	case "todowrite":
		return formatTodoWriteOutput(output)
	default:
		return formatDefaultOutput(toolName, output, termWidth)
	}
}

// formatDefaultOutput renders tool output in a bordered box, truncating if too many lines.
func formatDefaultOutput(toolName, output string, termWidth int) []string {
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return []string{fmt.Sprintf("   %s", toolSuccessStyle.Render("✓ Done"))}
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
			Render(fmt.Sprintf("... (%d more lines)", hidden)))
	}

	boxWidth := termWidth - 8
	if boxWidth < 30 {
		boxWidth = 30
	}

	box := outputBoxStyle.Width(boxWidth).Render(boxContent.String())
	return []string{
		fmt.Sprintf("   %s", toolSuccessStyle.Render("✓ Done:")),
		box,
	}
}

// formatExecuteOutput shows the last 5 lines of command output in a bordered box.
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
			Render(fmt.Sprintf("... (%d lines hidden)", start)))
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

	box := outputBoxStyle.Width(boxWidth).Render(boxContent.String())
	return []string{
		fmt.Sprintf("   %s", toolSuccessStyle.Render("✓ Output:")),
		box,
	}
}

// formatEditOutput renders the edit result with colored diff lines.
func formatEditOutput(output string, termWidth int) []string {
	// Split output into status line and diff block
	parts := strings.SplitN(output, "\n\n", 2)
	statusLine := parts[0]

	result := []string{
		fmt.Sprintf("   %s %s", toolSuccessStyle.Render("✓"), toolResultStyle.Render(statusLine)),
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
		if strings.HasPrefix(line, "+ ") {
			diffContent.WriteString(diffAddStyle.Render(line))
		} else if strings.HasPrefix(line, "- ") {
			diffContent.WriteString(diffRemoveStyle.Render(line))
		} else {
			diffContent.WriteString(line)
		}
		diffContent.WriteString("\n")
	}

	boxWidth := termWidth - 8
	if boxWidth < 30 {
		boxWidth = 30
	}

	diffBox := outputBoxStyle.Width(boxWidth).Render(strings.TrimRight(diffContent.String(), "\n"))
	result = append(result, diffBox)
	return result
}

// formatSubagentOutput shows the first few lines of subagent output in a bordered box.
func formatSubagentOutput(output string, termWidth int) []string {
	const tailLines = 8
	rawLines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	shown := rawLines
	hidden := 0
	if len(rawLines) > tailLines {
		shown = rawLines[:tailLines]
		hidden = len(rawLines) - tailLines
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
			Render(fmt.Sprintf("... (%d more lines)", hidden)))
	}

	boxWidth := termWidth - 8
	if boxWidth < 30 {
		boxWidth = 30
	}

	box := outputBoxStyle.Width(boxWidth).Render(boxContent.String())
	return []string{
		fmt.Sprintf("   %s", toolSuccessStyle.Render("✓ Subagent Result:")),
		box,
	}
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
