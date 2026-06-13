package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/cnjack/jcode/internal/theme"
)

// Color palette. These are reassigned by ApplyTheme from the active theme, so
// every style and inline lipgloss.NewStyle() call across the package picks up
// the current theme's colors on the next render. They are declared without
// initializers and seeded by init() below.
// lipgloss.Color is a constructor (func(string) color.Color), so these hold
// color.Color values, mirroring the original `colorX = lipgloss.Color("…")`.
var (
	colorPrimary   color.Color // brand accent (orange by default)
	colorSecondary color.Color // secondary accent (cyan family)
	colorSuccess   color.Color
	colorError     color.Color
	colorWarning   color.Color
	colorMuted     color.Color // low-emphasis text + subtle rules
	colorText      color.Color // primary foreground
	colorDimText   color.Color // secondary/dim text
	colorPlanMode  color.Color // plan-mode indicator (soft purple)
	colorLogoJ     color.Color // orange J in [JCODE] logo (tracks primary)
	colorOnPrimary color.Color // text/icons on a primary fill
	colorSubagent  color.Color // subagent accent (purple)
	colorBorder    color.Color // neutral surface borders
)

// Theme-derived styles. All are rebuilt by ApplyTheme — lipgloss captures color
// at construction time, so reassigning the color vars alone is not enough; the
// styles (and the pre-rendered icon strings) must be rebuilt too.
var (
	// ─── Sidebar styles ───
	sidebarSectionTitleStyle    lipgloss.Style
	sidebarValueStyle           lipgloss.Style
	sidebarScrollIndicatorStyle lipgloss.Style

	// ─── Mode pills styles ───
	modePillPlanStyle  lipgloss.Style
	modePillAskStyle   lipgloss.Style
	modePillAutoStyle  lipgloss.Style
	modeSeparatorStyle lipgloss.Style
	modeFillStyle      lipgloss.Style

	// ─── Minimal status bar style ───
	minimalStatusBarStyle lipgloss.Style

	assistantLabelStyle lipgloss.Style
	toolLabelStyle      lipgloss.Style
	subagentLabelStyle  lipgloss.Style
	subagentBoxStyle    lipgloss.Style
	toolNameStyle       lipgloss.Style
	toolArgsStyle       lipgloss.Style
	toolResultStyle     lipgloss.Style
	toolSuccessStyle    lipgloss.Style
	toolErrorStyle      lipgloss.Style
	diffAddStyle        lipgloss.Style
	diffRemoveStyle     lipgloss.Style
	spinnerStyle        lipgloss.Style
	errorStyle          lipgloss.Style
	userLabelStyle      lipgloss.Style
	// userPromptStyle renders user input with the primary background (e.g. "> hi")
	userPromptStyle     lipgloss.Style
	dividerStyle        lipgloss.Style
	todoCompletedStyle  lipgloss.Style
	todoInProgressStyle lipgloss.Style
	todoPendingStyle    lipgloss.Style
	todoCancelledStyle  lipgloss.Style

	// --- Tool call status icons (pre-rendered; rebuilt on theme change) ---
	toolIconPending string
	toolIconRunning string
	toolIconSuccess string
	toolIconError   string

	// --- Tool / subagent output body styles ---
	toolBodyStyle     lipgloss.Style
	subagentBodyStyle lipgloss.Style

	// --- Button / dialog styles ---
	buttonFocusStyle lipgloss.Style
	buttonBlurStyle  lipgloss.Style
	dialogBoxStyle   lipgloss.Style
)

// currentTheme is the active theme. It also drives the markdown (glamour)
// style via currentTheme.GlamourStyle().
var currentTheme theme.Theme

func init() {
	ApplyTheme(theme.DefaultDark)
}

// ApplyTheme switches the active palette and rebuilds all derived styles in
// place. Package-level style identifiers keep the same names, so all call
// sites pick up the new colors on the next render. Callers that cache rendered
// output (viewport lines, footer, sidebar) must invalidate it afterwards.
func ApplyTheme(name string) {
	t := theme.Resolve(name)
	currentTheme = t

	colorPrimary = lipgloss.Color(t.Primary)
	colorSecondary = lipgloss.Color(t.Accent)
	colorSuccess = lipgloss.Color(t.Success)
	colorError = lipgloss.Color(t.Error)
	colorWarning = lipgloss.Color(t.Warning)
	colorMuted = lipgloss.Color(t.TextMuted)
	colorText = lipgloss.Color(t.Text)
	colorDimText = lipgloss.Color(t.TextDim)
	colorPlanMode = lipgloss.Color(t.PlanMode)
	colorLogoJ = lipgloss.Color(t.Primary)
	colorOnPrimary = lipgloss.Color(t.OnPrimary)
	colorSubagent = lipgloss.Color(t.Subagent)
	colorBorder = lipgloss.Color(t.Border)

	// ─── Sidebar ───
	sidebarSectionTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorMuted)
	sidebarValueStyle = lipgloss.NewStyle().Foreground(colorText).PaddingLeft(1)
	sidebarScrollIndicatorStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true).PaddingLeft(1)

	// ─── Mode pills ───
	modePillPlanStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPlanMode)
	modePillAskStyle = lipgloss.NewStyle().Bold(true).Foreground(colorMuted)
	modePillAutoStyle = lipgloss.NewStyle().Bold(true).Foreground(colorWarning)
	modeSeparatorStyle = lipgloss.NewStyle().Foreground(colorMuted)
	modeFillStyle = lipgloss.NewStyle().Foreground(colorMuted)

	minimalStatusBarStyle = lipgloss.NewStyle().Foreground(colorMuted)

	assistantLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	toolLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSecondary)

	subagentLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSubagent)
	subagentBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorSubagent).
		Foreground(colorText).
		Padding(0, 1).
		MarginLeft(3)

	toolNameStyle = lipgloss.NewStyle().Bold(true).Foreground(colorWarning)
	toolArgsStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	toolResultStyle = lipgloss.NewStyle().Foreground(colorText).PaddingLeft(4)
	toolSuccessStyle = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true)
	toolErrorStyle = lipgloss.NewStyle().Foreground(colorError).Bold(true)

	diffAddStyle = lipgloss.NewStyle().Foreground(colorSuccess)
	diffRemoveStyle = lipgloss.NewStyle().Foreground(colorError)

	spinnerStyle = lipgloss.NewStyle().Foreground(colorSecondary)
	errorStyle = lipgloss.NewStyle().Foreground(colorError).Bold(true)
	userLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSecondary)
	userPromptStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorOnPrimary).
		Background(colorPrimary).
		Padding(0, 1)

	dividerStyle = lipgloss.NewStyle().Foreground(colorMuted)

	todoCompletedStyle = lipgloss.NewStyle().Foreground(colorSuccess)
	todoInProgressStyle = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
	todoPendingStyle = lipgloss.NewStyle().Foreground(colorMuted)
	todoCancelledStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)

	// --- Tool call status icons ---
	toolIconPending = lipgloss.NewStyle().Foreground(colorWarning).Render("●")
	toolIconRunning = lipgloss.NewStyle().Foreground(colorSecondary).Render("●")
	toolIconSuccess = lipgloss.NewStyle().Foreground(colorSuccess).Render("✓")
	toolIconError = lipgloss.NewStyle().Foreground(colorError).Render("✗")

	// --- Tool output body style (indented with left border) ---
	toolBodyStyle = lipgloss.NewStyle().
		Border(lipgloss.Border{Left: "│"}).
		BorderForeground(colorMuted).
		PaddingLeft(1).
		MarginLeft(3)

	// --- Subagent output body style (indented with left border, purple accent) ---
	subagentBodyStyle = lipgloss.NewStyle().
		Border(lipgloss.Border{Left: "│"}).
		BorderForeground(colorSubagent).
		PaddingLeft(1).
		MarginLeft(3)

	// --- Button styles for dialogs ---
	buttonFocusStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorOnPrimary).
		Background(colorPrimary).
		Padding(0, 2)
	buttonBlurStyle = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 2)

	// --- Dialog box style ---
	dialogBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 2)

	// Team coordinator-panel styles live in team_view.go.
	applyTeamStyles()
}

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
