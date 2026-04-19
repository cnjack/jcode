package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/tools"
)

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
		default:
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

// commandSuggestion represents a single slash command suggestion item.
type commandSuggestion struct {
	cmd  string // e.g. "/setting"
	desc string // e.g. "Settings menu"
}

// getAllCommands returns all available slash commands (built-in + skill).
func (m Model) getAllCommands() []commandSuggestion {
	commands := []commandSuggestion{
		{"/setting", "Settings menu"},
		{"/model", "Switch model"},
		{"/ssh", "SSH connection"},
		{"/resume", "Resume a previous session"},
		{"/compact", "Compress conversation context"},
		{"/bg", "List background tasks"},
		{"/channel", "Manage channels (WeChat etc.)"},
	}
	for _, sc := range m.skillSlashCommands {
		commands = append(commands, commandSuggestion{sc.Slash, sc.Description})
	}
	return commands
}

// filterCommands returns commands that match the given prefix.
func filterCommands(commands []commandSuggestion, prefix string) []commandSuggestion {
	var matches []commandSuggestion
	for _, c := range commands {
		if strings.HasPrefix(c.cmd, prefix) {
			matches = append(matches, c)
		}
	}
	return matches
}

// updateSuggestions updates the command suggestion state based on current input.
func (m *Model) updateSuggestions() {
	val := m.textarea.Value()
	if !strings.HasPrefix(val, "/") {
		m.cmdSuggestionActive = false
		m.cmdSuggestions = nil
		m.cmdSuggestionIndex = 0
		return
	}
	all := m.getAllCommands()
	matches := filterCommands(all, val)
	if len(matches) == 0 || (len(matches) == 1 && matches[0].cmd == val) {
		m.cmdSuggestionActive = false
		m.cmdSuggestions = nil
		m.cmdSuggestionIndex = 0
		return
	}
	m.cmdSuggestions = matches
	m.cmdSuggestionActive = true
	if m.cmdSuggestionIndex >= len(matches) {
		m.cmdSuggestionIndex = 0
	}
}

func (m Model) inputAreaView() string {
	var parts []string

	// Show todo bar (compact) unless a bottom prompt needs the space
	if m.todoStore != nil && m.todoStore.HasItems() {
		todoLine := m.renderTodoBar()
		if todoLine != "" {
			parts = append(parts, todoLine)
		}
	}

	parts = append(parts, divider(m.width))

	switch {
	case m.planReviewActive:
		parts = append(parts, m.planReviewPromptView())
	case m.askUserActive:
		parts = append(parts, m.askUserPromptView())
	default:
		if m.cmdSuggestionActive && len(m.cmdSuggestions) > 0 {
			suggestionView := m.renderCommandSuggestions()
			if suggestionView != "" {
				parts = append(parts, suggestionView)
			}
		}
		parts = append(parts, lipgloss.NewStyle().PaddingLeft(1).PaddingRight(2).Render(strings.TrimRight(m.textarea.View(), "\n")))
	}

	parts = append(parts, divider(m.width))

	// Render StatusBar using StatusBarComponent
	sbComp := NewStatusBarComponent()
	teammateCount := len(m.teamState.Teammates)
	activeTokens := m.totalTokens
	if m.teamState.ViewingAgent != "" {
		activeTokens = m.teammateTokens[m.teamState.ViewingAgent]
	}
	statusLine := sbComp.View(StatusBarState{
		Width:             m.width,
		ActiveProvider:    m.activeProvider,
		ActiveModel:       m.activeModel,
		AutoApprove:       m.approvalMode == ModeAuto,
		TotalTokens:       activeTokens,
		ModelContextLimit: m.modelContextLimit,
		MCPStatuses:       m.mcpStatuses,
		Mode:              m.agentMode,
		BgRunning:         m.bgRunning,
		TeammateCount:     teammateCount,
		CopyNotice:        m.copyNotice,
	})
	parts = append(parts, statusLine)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderCommandSuggestions renders the interactive command suggestion list with
// the currently selected item highlighted. The user can navigate with ↑/↓ and
// select with Tab or Enter.
func (m Model) renderCommandSuggestions() string {
	maxVisible := 5
	suggestions := m.cmdSuggestions
	total := len(suggestions)
	if total == 0 {
		return ""
	}

	// Calculate the visible window around the selected index
	start := 0
	if m.cmdSuggestionIndex >= maxVisible {
		start = m.cmdSuggestionIndex - maxVisible + 1
	}
	if start+maxVisible > total {
		start = total - maxVisible
	}
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > total {
		end = total
	}

	var lines []string
	for i := start; i < end; i++ {
		s := suggestions[i]
		cmdText := s.cmd
		descText := s.desc
		if i == m.cmdSuggestionIndex {
			// Highlighted item
			cmdStyled := lipgloss.NewStyle().Bold(true).Foreground(colorBg).Background(colorPrimary).Render(cmdText)
			descStyled := lipgloss.NewStyle().Foreground(colorBg).Background(colorPrimary).Render(" " + descText)
			// Indicator
			indicator := lipgloss.NewStyle().Foreground(colorPrimary).Render("❯")
			lines = append(lines, fmt.Sprintf("  %s %s%s", indicator, cmdStyled, descStyled))
		} else {
			cmdStyled := lipgloss.NewStyle().Foreground(colorText).Render(cmdText)
			descStyled := lipgloss.NewStyle().Foreground(colorMuted).Render(" " + descText)
			indicator := lipgloss.NewStyle().Foreground(colorMuted).Render(" ")
			lines = append(lines, fmt.Sprintf("  %s %s%s", indicator, cmdStyled, descStyled))
		}
	}

	if total > maxVisible {
		remaining := total - end
		if remaining > 0 {
			lines = append(lines, lipgloss.NewStyle().PaddingLeft(3).Foreground(colorMuted).Italic(true).
				Render(fmt.Sprintf("  ... and %d more (↓ to scroll)", remaining)))
		}
	}

	return strings.Join(lines, "\n")
}

// --- Legacy hint function removed; replaced by interactive suggestion list.

// handleChannelInput shows the channel management panel.
func (m Model) handleChannelInput(cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	items := []list.Item{
		channelItem{title: "💬 WeChat", desc: m.channelStateDesc("wechat"), key: "wechat"},
	}
	m.channelMenu.SetItems(items)
	m.showingChannel = true
	m.textarea.Blur()
	return m, tea.Batch(cmds...)
}

// channelStateDesc returns a human-readable description for the channel state.
func (m Model) channelStateDesc(channelID string) string {
	switch m.channelStates[channelID] {
	case "enabled":
		return "Enabled"
	case "disabled":
		return "Disabled"
	default:
		return "Not connected"
	}
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
		m.viewport.SetHeight(m.calcViewportHeight(false))
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

// matchSkillSlash checks if the prompt matches a registered skill slash command.
// Returns a SkillSlashMsg if matched, nil otherwise.
func (m Model) matchSkillSlash(prompt string) *SkillSlashMsg {
	for _, sc := range m.skillSlashCommands {
		if prompt == sc.Slash || strings.HasPrefix(prompt, sc.Slash+" ") {
			userInput := ""
			if strings.HasPrefix(prompt, sc.Slash+" ") {
				userInput = strings.TrimSpace(prompt[len(sc.Slash):])
			}
			skillName := strings.TrimPrefix(sc.Slash, "/")
			return &SkillSlashMsg{
				SkillName: skillName,
				UserInput: userInput,
			}
		}
	}
	return nil
}

// handleSkillSlashInput handles a skill slash command by sending a prompt that
// loads the skill and follows its instructions.
func (m Model) handleSkillSlashInput(skillName, userInput string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	prompt := fmt.Sprintf("Use the load_skill tool with name=%q and follow its instructions.", skillName)
	if userInput != "" {
		prompt += fmt.Sprintf("\n\nAdditional context: %s", userInput)
	}

	displayLabel := "/" + skillName
	if userInput != "" {
		displayLabel += " " + userInput
	}

	m.mode = ModeAgent
	m.agentDone = false
	m.thinking = true
	m.lines = append(m.lines, fmt.Sprintf("%s %s",
		userLabelStyle.Render("🔧 Skill:"), displayLabel))
	if m.ready {
		m.viewport.SetHeight(m.calcViewportHeight(false))
		m.viewport.SetContent(m.renderContent())
		m.viewport.GotoBottom()
	}
	cmds = append(cmds, func() tea.Msg {
		return PromptSubmitMsg{Prompt: prompt}
	})
	cmds = append(cmds, m.spinner.Tick)
	return m, tea.Batch(cmds...)
}
