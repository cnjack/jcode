package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
)

func (m Model) handleModelInput(cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	cfg, err := config.LoadConfig()
	if err != nil {
		m.lines = append(m.lines, toolErrorStyle.Render("✗ Failed to load config: "+err.Error()))
		return m, tea.Batch(cmds...)
	}

	currentProvider, currentModel := cfg.GetProviderModel()
	registry := model.NewModelRegistry()

	// Collect providers and sort them for stable ordering
	providers := cfg.GetProviders()
	providerNames := make([]string, 0, len(providers))
	for name := range providers {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)

	var items []list.Item

	for _, provider := range providerNames {
		models := registry.ListProviderModels(provider, false)
		if len(models) > 0 {
			for _, rm := range models {
				isCurrent := provider == currentProvider && rm.ID == currentModel

				// Build rich description with metadata
				var tags []string
				if rm.Limit != nil && rm.Limit.Context > 0 {
					tags = append(tags, fmt.Sprintf("%dk ctx", rm.Limit.Context/1000))
				}
				if rm.ToolCall {
					tags = append(tags, "tools")
				}
				if rm.Reasoning {
					tags = append(tags, "reasoning")
				}
				desc := provider
				if len(tags) > 0 {
					desc += " · " + strings.Join(tags, " · ")
				}

				title := rm.ID
				if isCurrent {
					title = "★ " + title
				}

				items = append(items, modelItem{
					provider:  provider,
					model:     rm.ID,
					title:     title,
					desc:      desc,
					isCurrent: isCurrent,
				})
			}
		} else if provider == currentProvider {
			// Provider not in registry — show configured model only
			items = append(items, modelItem{
				provider:  provider,
				model:     currentModel,
				title:     "★ " + currentModel,
				desc:      provider,
				isCurrent: true,
			})
		}
	}

	// Add "Add New Model" option at the end
	items = append(items, modelItem{
		title:    "➕ Add New Model…",
		desc:     "Configure a new provider and model",
		isAction: true,
	})

	m.modelPicker.SetItems(items)
	m.pickingModel = true
	m.textarea.Blur()
	return m, tea.Batch(cmds...)
}

// handleResumeInput parses the /resume command.
// /resume           — shows session picker for current project
// /resume <UUID>    — resumes specific session directly
func (m Model) handleResumeInput(input string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	arg := strings.TrimSpace(strings.TrimPrefix(input, "/resume"))

	if arg != "" {
		// Direct UUID resume
		m.lines = append(m.lines, toolLabelStyle.Render("📂 Loading session..."))
		m.thinking = true
		m.mode = ModeAgent
		m.agentDone = false
		uuid := arg
		cmds = append(cmds, m.spinner.Tick)
		cmds = append(cmds, func() tea.Msg {
			return ResumeRequestMsg{UUID: uuid}
		})
		return m, tea.Batch(cmds...)
	}

	// No UUID — show session picker
	metas, err := session.ListSessions(m.pwd)
	if err != nil || len(metas) == 0 {
		msg := "No sessions found for this project."
		if err != nil {
			msg = fmt.Sprintf("Error loading sessions: %v", err)
		}
		m.lines = append(m.lines, toolLabelStyle.Render("📂 Resume:")+" "+msg)
		m.refreshViewport()
		return m, tea.Batch(cmds...)
	}

	var items []list.Item
	// Show newest first
	for i := len(metas) - 1; i >= 0; i-- {
		items = append(items, sessionListItem{meta: metas[i]})
	}
	m.sessionPicker.SetItems(items)
	m.pickingSession = true
	m.textarea.Blur()
	return m, tea.Batch(cmds...)
}

func (m Model) settingMenuView() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	contentW := w - 12
	if contentW > 72 {
		contentW = 72
	}
	if contentW < 30 {
		contentW = 30
	}
	listH := h - 10
	if listH < 4 {
		listH = 4
	}

	boxStyle := dialogBoxStyle.Width(contentW)

	headerText := lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).
		Render("⚙  Settings")

	// Current model info
	modelInfo := ""
	if m.activeProvider != "" {
		modelInfo = lipgloss.NewStyle().Foreground(colorDimText).
			Render(fmt.Sprintf("Current: %s/%s", m.activeProvider, m.activeModel))
	}

	m.settingMenu.SetSize(contentW-4, listH)
	m.settingMenu.Title = "↑/↓ navigate · Enter confirm · Esc cancel"
	m.settingMenu.SetShowHelp(false)
	m.settingMenu.SetShowStatusBar(false)
	m.settingMenu.SetShowPagination(false)

	var contentParts []string
	contentParts = append(contentParts, headerText)
	if modelInfo != "" {
		contentParts = append(contentParts, modelInfo)
	}
	contentParts = append(contentParts, "")
	contentParts = append(contentParts, m.settingMenu.View())

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}

func (m Model) sshAliasPickerView() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	modW, modH := w-8, h-4
	if modW > 120 {
		modW = 120
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(0, 1).
		Width(modW)

	headerText := fmt.Sprintf(" %s ", toolNameStyle.Render("🔗 SSH Connections"))

	m.sshAliasPicker.SetSize(modW-6, modH-6)
	m.sshAliasPicker.Title = "Select SSH connection (↑/↓ to navigate, Enter to connect, Esc to cancel)"
	m.sshAliasPicker.SetShowHelp(false)
	m.sshAliasPicker.SetShowStatusBar(true)
	m.sshAliasPicker.SetShowPagination(false)

	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Padding(0, 1).Render(headerText),
		"",
		m.sshAliasPicker.View(),
	)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}

func (m Model) modelPickerView() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	contentW := w - 12
	if contentW > 72 {
		contentW = 72
	}
	if contentW < 30 {
		contentW = 30
	}
	listH := h - 10
	if listH < 4 {
		listH = 4
	}

	boxStyle := dialogBoxStyle.Width(contentW)

	headerText := lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).
		Render("🔀 Select Model")

	// Current model info
	modelInfo := ""
	if m.activeProvider != "" {
		modelInfo = lipgloss.NewStyle().Foreground(colorDimText).
			Render(fmt.Sprintf("Current: %s/%s", m.activeProvider, m.activeModel))
	}

	m.modelPicker.SetSize(contentW-4, listH)
	m.modelPicker.Title = "/ filter · ↑/↓ navigate · Enter confirm · Esc cancel"
	m.modelPicker.SetShowHelp(false)
	m.modelPicker.SetShowStatusBar(false)
	m.modelPicker.SetShowPagination(true)

	var contentParts []string
	contentParts = append(contentParts, headerText)
	if modelInfo != "" {
		contentParts = append(contentParts, modelInfo)
	}
	contentParts = append(contentParts, "")
	contentParts = append(contentParts, m.modelPicker.View())

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}

func (m Model) dirPickerView() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	modW, modH := w-8, h-4
	if modW > 120 {
		modW = 120
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(0, 1).
		Width(modW)

	headerText := fmt.Sprintf(" Open Folder: %s ", toolNameStyle.Render(m.sshAddr))

	pathBox := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorSecondary).
		Padding(0, 1).
		Foreground(colorText).
		Width(modW - 4).
		Render(m.sshPath)

	m.dirList.SetSize(modW-6, modH-10)
	m.dirList.Title = "↑/↓ navigate · Enter browse · Tab open folder · Esc cancel"
	m.dirList.SetShowHelp(false)
	m.dirList.SetShowStatusBar(true)
	m.dirList.SetShowPagination(false)

	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Padding(0, 1).Render(headerText),
		pathBox,
		"",
		m.dirList.View(),
	)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}

func (m Model) approvalDialogView() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	// Dialog content width (excluding border + padding)
	contentW := 60
	if contentW > w-12 {
		contentW = w - 12
	}
	if contentW < 30 {
		contentW = 30
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorWarning).
		Padding(1, 2).
		Width(contentW)

	// Different header based on whether this is external path access
	var headerText string
	switch {
	case m.approvalIsExternal:
		headerText = toolNameStyle.Render("⚠️  External Path Access")
	case m.approvalWorkerName != "":
		workerBadge := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(m.approvalWorkerColor)).
			Render("@" + m.approvalWorkerName)
		headerText = toolNameStyle.Render("⚠️  Teammate Approval: ") + workerBadge
	default:
		headerText = toolNameStyle.Render("⚠️  Permission Required")
	}

	argsDisplay := m.approvalToolArgs
	if len(argsDisplay) > 200 {
		argsDisplay = argsDisplay[:200] + "..."
	}

	// Tool info with icon
	toolLine := fmt.Sprintf("%s  %s", toolIconRunning, toolNameStyle.Render(m.approvalToolName))

	// Args in a subtle section
	argsBox := lipgloss.NewStyle().
		Foreground(colorDimText).
		Width(contentW - 4).
		Render(argsDisplay)

	// Button group — left/right navigation
	buttons := buttonGroup([]buttonOpts{
		{text: " Approve ", selected: m.approvalSelected == 0},
		{text: " Approve All ", selected: m.approvalSelected == 1},
		{text: " Reject ", selected: m.approvalSelected == 2},
	})

	// Keyboard hints
	hintText := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
		Render("←/→ switch  ·  Enter confirm  ·  y/a/n")

	content := lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.NewStyle().Bold(true).Render(headerText),
		"",
		toolLine,
		"",
		argsBox,
		"",
		buttons,
		"",
		hintText,
	)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}

func (m Model) exitDialogView() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	contentW := 48
	if contentW > w-12 {
		contentW = w - 12
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorWarning).
		Padding(1, 2).
		Width(contentW)

	headerText := lipgloss.NewStyle().Bold(true).Foreground(colorWarning).
		Render("⚠️  Quit?")

	var statusText string
	if m.thinking && !m.agentDone {
		statusText = lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
			Render("Agent is still running.")
	}

	buttons := buttonGroup([]buttonOpts{
		{text: " Yes ", selected: m.exitSelected == 0},
		{text: " No ", selected: m.exitSelected == 1},
	})

	hintText := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
		Render("←/→ switch  ·  Enter confirm  ·  y/n")

	var parts []string
	parts = append(parts, headerText)
	if statusText != "" {
		parts = append(parts, statusText)
	}
	parts = append(parts, "", buttons, "", hintText)

	content := lipgloss.JoinVertical(lipgloss.Center, parts...)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}

func (m Model) cancelDialogView() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	contentW := 48
	if contentW > w-12 {
		contentW = w - 12
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorWarning).
		Padding(1, 2).
		Width(contentW)

	headerText := lipgloss.NewStyle().Bold(true).Foreground(colorWarning).
		Render("⏹  Cancel agent?")

	statusText := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
		Render("Agent is still running.")

	buttons := buttonGroup([]buttonOpts{
		{text: " Cancel ", selected: m.cancelSelected == 0},
		{text: " Wait ", selected: m.cancelSelected == 1},
	})

	hintText := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
		Render("←/→ switch  ·  Enter confirm  ·  y/n")

	content := lipgloss.JoinVertical(lipgloss.Center,
		headerText, statusText, "", buttons, "", hintText)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}

func (m Model) sessionPickerView() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	modW, modH := w-8, h-4
	if modW > 120 {
		modW = 120
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(0, 1).
		Width(modW)

	headerText := fmt.Sprintf(" %s ", toolNameStyle.Render("📂 Resume Session"))

	m.sessionPicker.SetSize(modW-6, modH-6)
	m.sessionPicker.Title = "Select session (↑/↓ navigate · Enter resume · Esc cancel)"
	m.sessionPicker.SetShowHelp(false)
	m.sessionPicker.SetShowStatusBar(true)
	m.sessionPicker.SetShowPagination(false)

	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Padding(0, 1).Render(headerText),
		"",
		m.sessionPicker.View(),
	)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}

// bottomPromptView renders a compact prompt with selectable options for the input area.
func (m Model) bottomPromptView(title string, options []string, selected int, showTextInput bool, footer string) string {
	var lines []string

	titleLine := lipgloss.NewStyle().Bold(true).PaddingLeft(1).Foreground(colorPrimary).Render(title)
	lines = append(lines, titleLine)

	for i, opt := range options {
		prefix := "  ○ "
		style := lipgloss.NewStyle().Foreground(colorText)
		if i == selected {
			prefix = "  ● "
			style = style.Bold(true).Foreground(colorPrimary)
		}
		lines = append(lines, style.Render(prefix+opt))
	}

	if showTextInput {
		lines = append(lines, lipgloss.NewStyle().PaddingLeft(2).Render(
			strings.TrimRight(m.textarea.View(), "\n")))
	}

	footerLine := lipgloss.NewStyle().PaddingLeft(1).Foreground(colorMuted).Render(footer)
	lines = append(lines, footerLine)

	return strings.Join(lines, "\n")
}

// planReviewPromptView renders the plan review as a bottom prompt with options.
func (m Model) planReviewPromptView() string {
	options := []string{"Approve", "Reject with feedback", "Dismiss"}
	showTextInput := m.planRejectInput
	footer := "[↑/↓] Navigate  [Enter] Select  [y] Approve  [n] Reject  [Esc] Dismiss"
	if m.planRejectInput {
		footer = "Enter feedback, then press Enter to confirm  [Esc] Back"
	}
	title := "📐 Plan Review: " + m.planReviewTitle
	return m.bottomPromptView(title, options, m.planReviewSelected, showTextInput, footer)
}

// askUserPromptView renders the ask_user question as a bottom prompt with options.
func (m Model) askUserPromptView() string {
	title := "❓ " + m.askUserQuestion
	optCount := len(m.askUserOptions)
	if optCount == 0 {
		// No predefined options — just show text input
		footer := "[Enter] Submit  [Esc] Skip  [PgUp/PgDn] Scroll"
		return m.bottomPromptView(title, nil, -1, true, footer)
	}
	options := make([]string, optCount+1)
	copy(options, m.askUserOptions)
	options[optCount] = "Other (type below)"
	showTextInput := m.askUserSelected == optCount
	footer := "[↑/↓] Navigate  [Enter] Select/Submit  [Esc] Skip  [PgUp/PgDn] Scroll"
	return m.bottomPromptView(title, options, m.askUserSelected, showTextInput, footer)
}
