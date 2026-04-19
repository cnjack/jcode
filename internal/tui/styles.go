package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	// Color palette
	colorPrimary   = lipgloss.Color("#7C3AED") // violet
	colorSecondary = lipgloss.Color("#06B6D4") // cyan
	colorSuccess   = lipgloss.Color("#10B981") // green
	colorError     = lipgloss.Color("#EF4444") // red
	colorWarning   = lipgloss.Color("#F59E0B") // amber
	colorMuted     = lipgloss.Color("#6B7280") // gray
	colorText      = lipgloss.Color("#E5E7EB") // light gray
	colorBg        = lipgloss.Color("#111827") // dark bg
	colorDimText   = lipgloss.Color("#9CA3AF") // dim text for secondary info

	// Styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			PaddingLeft(1)

	assistantLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPrimary)

	toolLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSecondary)

	subagentLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("99"))

	subagentBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("99")).
				Foreground(colorText).
				Padding(0, 1).
				MarginLeft(3)

	toolNameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWarning)

	toolArgsStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	toolResultStyle = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingLeft(4)

	toolSuccessStyle = lipgloss.NewStyle().
				Foreground(colorSuccess).
				Bold(true)

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	diffAddStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	diffRemoveStyle = lipgloss.NewStyle().
			Foreground(colorError)

	spinnerStyle = lipgloss.NewStyle().
			Foreground(colorSecondary)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	userLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSecondary)

	dividerStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	todoLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSecondary)

	todoCompletedStyle = lipgloss.NewStyle().
				Foreground(colorSuccess)

	todoInProgressStyle = lipgloss.NewStyle().
				Foreground(colorWarning).
				Bold(true)

	todoPendingStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	todoCancelledStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Italic(true)

	// --- Tool call status icons ---
	toolIconPending = lipgloss.NewStyle().Foreground(colorWarning).Render("●")
	toolIconRunning = lipgloss.NewStyle().Foreground(colorSecondary).Render("●")
	toolIconSuccess = lipgloss.NewStyle().Foreground(colorSuccess).Render("✓")
	toolIconError   = lipgloss.NewStyle().Foreground(colorError).Render("✗")
	// --- Tool output body style (indented with left border) ---
	toolBodyStyle = lipgloss.NewStyle().
			Border(lipgloss.Border{Left: "│"}).
			BorderForeground(colorMuted).
			PaddingLeft(1).
			MarginLeft(3)

	// --- Button styles for dialogs ---
	buttonFocusStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBg).
				Background(colorPrimary).
				Padding(0, 2)

	buttonBlurStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 2)

	// --- Dialog box style ---
	dialogBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(1, 2)
)

func divider(width int) string {
	if width <= 0 {
		width = 80
	}
	return dividerStyle.Render(strings.Repeat("─", width))
}

// buttonGroup renders a row of selectable buttons.
func buttonGroup(buttons []buttonOpts) string {
	if len(buttons) == 0 {
		return ""
	}
	const spacing = "  "
	parts := make([]string, 0, len(buttons)*2-1)
	for i, b := range buttons {
		if i > 0 {
			parts = append(parts, spacing)
		}
		if b.selected {
			parts = append(parts, buttonFocusStyle.Render(b.text))
		} else {
			parts = append(parts, buttonBlurStyle.Render(b.text))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, parts...)
}

type buttonOpts struct {
	text     string
	selected bool
}
