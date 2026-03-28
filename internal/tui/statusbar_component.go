package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
	// Mode indicator (leftmost)
	var modeStr string
	switch state.Mode {
	case ModePlanning:
		modeStr = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")).Render(" Plan")
	default:
		modeStr = lipgloss.NewStyle().Bold(true).Foreground(colorSecondary).Render(" Agent")
	}

	leftTxt := modeStr + "  │  Model: "
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
			rightParts = append(rightParts, fmt.Sprintf("Tokens: %d/%d (%.0f%%)", state.TotalTokens, state.ModelContextLimit, usagePercent))
		} else {
			rightParts = append(rightParts, fmt.Sprintf("Tokens: %d", state.TotalTokens))
		}
	}

	if state.BgRunning > 0 {
		rightParts = append(rightParts, lipgloss.NewStyle().Foreground(colorWarning).Render(fmt.Sprintf("Bg: %d running", state.BgRunning)))
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
