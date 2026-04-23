package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/cnjack/jcode/internal/tools"
)

// SidebarState holds the data needed to render the right sidebar.
type SidebarState struct {
	Width             int
	Height            int
	TotalWidth        int // full width including borders
	EnvLabel          string
	ActiveProvider    string
	ActiveModel       string
	TotalTokens       int64
	ModelContextLimit int
	TodoItems         []tools.TodoItem
	TodoScrollOffset  int
	MCPStatuses       []MCPStatusItem
	TeammateCount     int
	BgRunning         int
}

// SidebarComponent renders the right-hand info panel.
type SidebarComponent struct{}

// NewSidebarComponent creates a new sidebar component.
func NewSidebarComponent() *SidebarComponent {
	return &SidebarComponent{}
}

// View renders the sidebar. Content is built line-by-line to ensure it never
// exceeds state.Height. Each section is appended until the height budget is
// exhausted.
func (s *SidebarComponent) View(state SidebarState) string {
	if state.Height < 3 {
		state.Height = 3
	}
	maxLines := state.Height

	var lines []string
	addLines := func(content string) {
		for _, line := range strings.Split(content, "\n") {
			if len(lines) >= maxLines {
				return
			}
			lines = append(lines, line)
		}
	}

	addLines(s.renderLogo())
	if state.ActiveProvider != "" && len(lines) < maxLines {
		addLines(s.renderModelSection(state))
	}
	if len(lines) < maxLines {
		addLines(s.renderEnvSection(state))
	}
	if (state.TotalTokens > 0 || state.ModelContextLimit > 0) && len(lines) < maxLines {
		addLines(s.renderUsageSection(state))
	}
	if len(state.TodoItems) > 0 && len(lines) < maxLines {
		addLines(s.renderTodoSection(state))
	}
	if len(state.MCPStatuses) > 0 && len(lines) < maxLines {
		addLines(s.renderMCPSection(state))
	}
	if state.BgRunning > 0 && len(lines) < maxLines {
		addLines(s.renderBgSection(state))
	}
	if state.TeammateCount > 0 && len(lines) < maxLines {
		addLines(s.renderTeamSection(state))
	}

	// Fill remaining lines with empty lines so lipgloss Height matches exactly
	for len(lines) < maxLines {
		lines = append(lines, "")
	}

	// Manually build the sidebar with left+right borders.
	// Each line is padded so the right "│" aligns perfectly.
	contentWidth := state.TotalWidth - 4 // "│ " and " │"
	if contentWidth < 10 {
		contentWidth = 10
	}

	var result strings.Builder
	for i, line := range lines {
		result.WriteString("│ ")
		result.WriteString(line)
		// Pad with spaces so right border aligns
		w := lipgloss.Width(line)
		pad := contentWidth - w
		if pad < 0 {
			pad = 0
		}
		result.WriteString(strings.Repeat(" ", pad))
		result.WriteString(" │")
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}
	return result.String()
}

func (s *SidebarComponent) renderLogo() string {
	bracketStyle := lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	jStyle := lipgloss.NewStyle().Foreground(colorLogoJ).Bold(true)
	codeStyle := lipgloss.NewStyle().Foreground(colorText).Bold(true)
	logo := lipgloss.JoinHorizontal(lipgloss.Left,
		bracketStyle.Render("["),
		jStyle.Render("J"),
		codeStyle.Render("CODE"),
		bracketStyle.Render("]"),
	)
	line := lipgloss.NewStyle().Foreground(colorMuted).Render("─────────")
	return lipgloss.JoinVertical(lipgloss.Left, logo, line)
}

func (s *SidebarComponent) renderModelSection(state SidebarState) string {
	title := sidebarSectionTitleStyle.Render("Model")
	modelName := state.ActiveProvider + " / " + state.ActiveModel
	value := sidebarValueStyle.Render(truncateString(modelName, state.Width-3))
	return lipgloss.JoinVertical(lipgloss.Left, title, value)
}

func (s *SidebarComponent) renderEnvSection(state SidebarState) string {
	title := sidebarSectionTitleStyle.Render("Env")
	var envText string
	if state.EnvLabel == "Local" || state.EnvLabel == "local" || state.EnvLabel == "" {
		envText = "🖥️  Local"
	} else {
		envText = "🔗 " + state.EnvLabel
	}
	value := sidebarValueStyle.Render(envText)
	return lipgloss.JoinVertical(lipgloss.Left, title, value)
}

func (s *SidebarComponent) renderUsageSection(state SidebarState) string {
	title := sidebarSectionTitleStyle.Render("Usage")
	var usageLine string
	if state.ModelContextLimit > 0 {
		pct := float64(state.TotalTokens) / float64(state.ModelContextLimit) * 100
		bar := renderProgressBar(pct, 8)
		usageLine = fmt.Sprintf("%s %.0f%%", bar, pct)
	} else {
		usageLine = fmt.Sprintf("%d tokens", state.TotalTokens)
	}
	value := sidebarValueStyle.Render(usageLine)
	return lipgloss.JoinVertical(lipgloss.Left, title, value)
}

func (s *SidebarComponent) renderTodoSection(state SidebarState) string {
	if len(state.TodoItems) == 0 {
		return ""
	}

	completed, total := countTodos(state.TodoItems)
	title := sidebarSectionTitleStyle.Render(fmt.Sprintf("📋 Todo (%d/%d)", completed, total))

	// Calculate available lines for todo items
	// We need to estimate how many lines other sections use
	otherLines := s.countFixedSectionLines(state)
	available := state.Height - otherLines - 4 // reserve space for title + padding
	if available < 2 {
		available = 2
	}

	items := state.TodoItems
	start := state.TodoScrollOffset
	if start < 0 {
		start = 0
	}
	if start > len(items) {
		start = len(items)
	}

	var lines []string
	lines = append(lines, title)

	// Top scroll indicator
	if start > 0 {
		lines = append(lines, sidebarScrollIndicatorStyle.Render(fmt.Sprintf("▲ %d more", start)))
	}

	// Visible items
	end := start + available
	if end > len(items) {
		end = len(items)
	}
	for i := start; i < end && i < len(items); i++ {
		lines = append(lines, s.renderTodoItem(items[i]))
	}

	// Bottom scroll indicator
	if end < len(items) {
		lines = append(lines, sidebarScrollIndicatorStyle.Render(fmt.Sprintf("▼ %d more", len(items)-end)))
	}

	return strings.Join(lines, "\n")
}

func (s *SidebarComponent) renderTodoItem(item tools.TodoItem) string {
	var icon, text string
	switch item.Status {
	case tools.TodoCompleted:
		icon = todoCompletedStyle.Render("✓")
		text = todoCompletedStyle.Render(truncateString(item.Title, 20))
	case tools.TodoInProgress:
		icon = todoInProgressStyle.Render("⏳")
		text = todoInProgressStyle.Render(truncateString(item.Title, 20))
	case tools.TodoCancelled:
		icon = todoCancelledStyle.Render("✗")
		text = todoCancelledStyle.Render(truncateString(item.Title, 20))
	default:
		icon = todoPendingStyle.Render("○")
		text = todoPendingStyle.Render(truncateString(item.Title, 20))
	}
	return fmt.Sprintf("  %s %s", icon, text)
}

func (s *SidebarComponent) renderMCPSection(state SidebarState) string {
	title := sidebarSectionTitleStyle.Render("MCP")
	var lines []string
	lines = append(lines, title)

	activeServers := 0
	loadedTools := 0
	for _, st := range state.MCPStatuses {
		if st.Running {
			activeServers++
			loadedTools += st.ToolCount
		}
	}

	if activeServers > 0 {
		status := sidebarValueStyle.Render(fmt.Sprintf("● %d server%s, %d tool%s",
			activeServers, plural(activeServers), loadedTools, plural(loadedTools)))
		lines = append(lines, status)
	} else {
		lines = append(lines, sidebarValueStyle.Render("○ inactive"))
	}

	return strings.Join(lines, "\n")
}

func (s *SidebarComponent) renderBgSection(state SidebarState) string {
	title := sidebarSectionTitleStyle.Render("Background")
	value := sidebarValueStyle.Render(fmt.Sprintf("⏳ %d running", state.BgRunning))
	return lipgloss.JoinVertical(lipgloss.Left, title, value)
}

func (s *SidebarComponent) renderTeamSection(state SidebarState) string {
	title := sidebarSectionTitleStyle.Render("Team")
	value := sidebarValueStyle.Render(fmt.Sprintf("👥 %d teammate%s", state.TeammateCount, plural(state.TeammateCount)))
	return lipgloss.JoinVertical(lipgloss.Left, title, value)
}

// countFixedSectionLines estimates lines used by all sections except todo.
func (s *SidebarComponent) countFixedSectionLines(state SidebarState) int {
	lines := 5 // logo + padding
	if state.ActiveProvider != "" {
		lines += 3 // model section
	}
	lines += 3 // env section
	if state.TotalTokens > 0 || state.ModelContextLimit > 0 {
		lines += 3 // usage section
	}
	if len(state.MCPStatuses) > 0 {
		lines += 3 // mcp section
	}
	if state.BgRunning > 0 {
		lines += 3 // bg section
	}
	if state.TeammateCount > 0 {
		lines += 3 // team section
	}
	return lines
}

// Helper functions.

func countTodos(items []tools.TodoItem) (completed, total int) {
	total = len(items)
	for _, item := range items {
		if item.Status == tools.TodoCompleted {
			completed++
		}
	}
	return
}

func truncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	w := lipgloss.Width(s)
	if w <= maxLen {
		return s
	}
	// Simple truncation: cut by runes
	runes := []rune(s)
	truncated := string(runes[:maxLen-1])
	return truncated + "…"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
