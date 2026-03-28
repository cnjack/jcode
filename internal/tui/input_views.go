package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cnjack/coding/internal/config"
	"github.com/cnjack/coding/internal/tools"
)

// todoBarHeight returns the number of lines the todo bar occupies.
func (m Model) todoBarHeight() int {
	if m.todoStore == nil || !m.todoStore.HasItems() {
		return 0
	}
	items := m.todoStore.Items()
	// Count: label line + item lines (max 5 visible)
	n := len(items)
	if n > 5 {
		n = 5
	}
	return 1 + n // header + items
}

// renderTodoBar renders the todo items as a compact block above the input.
func (m Model) renderTodoBar() string {
	if m.todoStore == nil {
		return ""
	}
	items := m.todoStore.Items()
	if len(items) == 0 {
		return ""
	}

	var completed, total int
	total = len(items)
	for _, item := range items {
		if item.Status == tools.TodoCompleted {
			completed++
		}
	}

	header := todoLabelStyle.Render(fmt.Sprintf("📋 Todo (%d/%d)", completed, total))

	var lines []string
	lines = append(lines, "  "+header)

	shown := items
	if len(shown) > 5 {
		shown = shown[:5]
	}
	for _, item := range shown {
		var icon, text string
		switch item.Status {
		case tools.TodoCompleted:
			icon = todoCompletedStyle.Render("✓")
			text = todoCompletedStyle.Render(item.Title)
		case tools.TodoInProgress:
			icon = todoInProgressStyle.Render("⏳")
			text = todoInProgressStyle.Render(item.Title)
		case tools.TodoCancelled:
			icon = todoCancelledStyle.Render("✗")
			text = todoCancelledStyle.Render(item.Title)
		default: // pending
			icon = todoPendingStyle.Render("○")
			text = todoPendingStyle.Render(item.Title)
		}
		lines = append(lines, fmt.Sprintf("    %s %s", icon, text))
	}
	if len(items) > 5 {
		more := todoPendingStyle.Render(fmt.Sprintf("    ... and %d more", len(items)-5))
		lines = append(lines, more)
	}
	return strings.Join(lines, "\n")
}

func (m Model) inputAreaView() string {
	var parts []string

	if m.todoStore != nil && m.todoStore.HasItems() {
		todoLine := m.renderTodoBar()
		if todoLine != "" {
			parts = append(parts, todoLine)
		}
	}

	parts = append(parts, divider(m.width))

	val := m.textarea.Value()
	if strings.HasPrefix(val, "/") {
		hints := m.getCommandHints(val)
		if hints != "" {
			hintStyle := lipgloss.NewStyle().PaddingLeft(2).Foreground(colorMuted).Italic(true)
			parts = append(parts, hintStyle.Render(hints))
		}
	}

	parts = append(parts, lipgloss.NewStyle().PaddingLeft(1).PaddingRight(2).Render(strings.TrimRight(m.textarea.View(), "\n")))
	parts = append(parts, divider(m.width))

	// Render StatusBar using StatusBarComponent
	sbComp := NewStatusBarComponent()
	statusLine := sbComp.View(StatusBarState{
		Width:             m.width,
		ActiveProvider:    m.activeProvider,
		ActiveModel:       m.activeModel,
		AutoApprove:       m.approvalMode == ModeAuto,
		TotalTokens:       m.totalTokens,
		ModelContextLimit: m.modelContextLimit,
		MCPStatuses:       m.mcpStatuses,
		Mode:              m.agentMode,
		BgRunning:         m.bgRunning,
	})
	parts = append(parts, statusLine)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) getCommandHints(input string) string {
	type cmdHint struct {
		cmd  string
		desc string
	}
	commands := []cmdHint{
		{"/setting", "Settings menu"},
		{"/model", "Switch model"},
		{"/ssh", "SSH connection"},
		{"/resume", "Resume a previous session"},
		{"/compact", "Compress conversation context"},
		{"/bg", "List background tasks"},
	}

	var matches []string
	for _, c := range commands {
		if strings.HasPrefix(c.cmd, input) {
			matches = append(matches, c.cmd+" "+c.desc)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	return "  " + strings.Join(matches, "  │  ")
}

// handleSettingInput shows the setting menu.
func (m Model) handleSettingInput(cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	items := []list.Item{
		settingItem{title: "🔀 Switch Model", desc: "Switch to a different configured model", key: "switch_model"},
		settingItem{title: "➕ Add New Model", desc: "Add a new model provider via setup wizard", key: "add_model"},
		settingItem{title: "📝 Edit Config File", desc: "Manually edit " + config.ConfigPath(), key: "edit_config"},
	}
	m.settingMenu.SetItems(items)
	m.showingSetting = true
	m.textarea.Blur()
	return m, tea.Batch(cmds...)
}

// handleBgInput handles `/bg` command to show background task status.
func (m Model) handleBgInput(cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	prompt := "Use the check_background tool to list all background tasks and report their status."
	m.mode = ModeAgent
	m.agentDone = false
	m.thinking = true
	m.lines = append(m.lines, fmt.Sprintf("%s /bg",
		userLabelStyle.Render("👤 You:")))
	if m.ready {
		m.viewport.Height = m.calcViewportHeight(false)
		m.viewport.SetContent(m.renderContent())
		m.viewport.GotoBottom()
	}
	cmds = append(cmds, func() tea.Msg {
		return PromptSubmitMsg{Prompt: prompt}
	})
	cmds = append(cmds, m.spinner.Tick)
	return m, tea.Batch(cmds...)
}

// handleCompactInput handles `/compact` by sending a compact request to the main goroutine.
func (m Model) handleCompactInput(cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	m.lines = append(m.lines, toolLabelStyle.Render("  ⏳ Compacting context..."))
	m.thinking = true
	m.agentDone = false
	if m.ready {
		m.viewport.SetContent(m.renderContent())
		m.viewport.GotoBottom()
	}

	select {
	case compactCh <- struct{}{}:
	default:
	}

	cmds = append(cmds, m.spinner.Tick)
	return m, tea.Batch(cmds...)
}
