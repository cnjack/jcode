package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// StatusBarState holds the props supplied to the StatusBar component
type StatusBarState struct {
	Width             int
	ActiveProvider    string
	ActiveModel       string
	AutoApprove       bool
	TotalTokens       int64
	ModelContextLimit int
	MCPStatuses       []MCPStatusItem
	Mode              AgentMode
	BgRunning         int
	TeammateCount     int
}

// StatusBarComponent is a stateless-like component in Bubble Tea.
type StatusBarComponent struct {
	// Any internal state can be kept here
}

func NewStatusBarComponent() *StatusBarComponent {
	return &StatusBarComponent{}
}

// View returns the rendered status bar.
func (s *StatusBarComponent) View(state StatusBarState) string {
	// Note: Mode indicator (Agent/Plan) is now shown in mode pills above input.
	// This status bar is only used in narrow-screen fallback mode.

	leftTxt := "Model: "
	if state.ActiveProvider != "" {
		leftTxt += state.ActiveProvider + " / " + state.ActiveModel
	} else {
		leftTxt += "Not configured"
	}

	var rightParts []string

	if state.AutoApprove {
		rightParts = append(rightParts, "Approve: "+lipgloss.NewStyle().Foreground(colorWarning).Render("Auto"))
	} else {
		rightParts = append(rightParts, "Approve: "+lipgloss.NewStyle().Foreground(colorMuted).Render("Ask"))
	}

	if state.TotalTokens > 0 || state.ModelContextLimit > 0 {
		if state.ModelContextLimit > 0 {
			usagePercent := float64(state.TotalTokens) / float64(state.ModelContextLimit) * 100
			bar := renderProgressBar(usagePercent, 10)
			rightParts = append(rightParts, fmt.Sprintf("%s %.0f%%", bar, usagePercent))
		} else {
			rightParts = append(rightParts, fmt.Sprintf("Tokens: %d", state.TotalTokens))
		}
	}

	if state.BgRunning > 0 {
		rightParts = append(rightParts, lipgloss.NewStyle().Foreground(colorWarning).Render(fmt.Sprintf("Bg: %d running", state.BgRunning)))
	}

	if state.TeammateCount > 0 {
		rightParts = append(rightParts, RenderTeamStatusPill(state.TeammateCount))
	}

	if len(state.MCPStatuses) > 0 {
		activeServers := 0
		loadedTools := 0
		for _, st := range state.MCPStatuses {
			if st.Running {
				activeServers++
				loadedTools += st.ToolCount
			}
		}
		rightParts = append(rightParts, fmt.Sprintf("MCP: %d/%d", activeServers, loadedTools))
	}

	rightTxt := strings.Join(rightParts, " │ ") + "  "

	statusStyle := lipgloss.NewStyle().Foreground(colorMuted)
	leftW := lipgloss.Width(leftTxt)
	rightW := lipgloss.Width(rightTxt)

	space := state.Width - leftW - rightW
	if space < 1 {
		space = 1
	}

	statusLine := leftTxt + strings.Repeat(" ", space) + rightTxt
	return statusStyle.Render(statusLine)
}

// renderProgressBar renders a progress bar for token usage.
// percent: 0-100, width: number of cells in the bar
func renderProgressBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(percent / 100.0 * float64(width))
	if filled > width {
		filled = width
	}

	var bar strings.Builder
	bar.WriteString("[")

	// Choose color based on usage
	var barStyle lipgloss.Style
	switch {
	case percent >= 90:
		barStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	case percent >= 70:
		barStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Orange
	default:
		barStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
	}

	// Filled part
	for i := 0; i < filled; i++ {
		bar.WriteString("█")
	}
	// Empty part
	for i := filled; i < width; i++ {
		bar.WriteString("░")
	}

	bar.WriteString("]")
	return barStyle.Render(bar.String())
}
