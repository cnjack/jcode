package tui

import (
	"fmt"
	"path"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/team"
	"github.com/cnjack/jcode/internal/theme"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { //nolint:funlen
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.PasteMsg:
		m.invalidateFooterCache()
		if m.inputActive() {
			return m.handlePasteContent(NormalizeLineEndings(msg.Content))
		}
		return m, tea.Batch(cmds...)

	case tea.ClipboardMsg:
		if m.inputActive() && msg.Content != "" {
			return m.handlePasteContent(NormalizeLineEndings(msg.Content))
		}
		return m, tea.Batch(cmds...)

	case tea.MouseMsg:
		// Right-click paste: request clipboard via OSC52 terminal protocol
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseRight && m.inputActive() {
			cmds = append(cmds, tea.ReadClipboard)
			return m, tea.Batch(cmds...)
		}

		switch {
		case m.pickingSession:
			var cmd tea.Cmd
			m.sessionPicker, cmd = m.sessionPicker.Update(msg)
			cmds = append(cmds, cmd)
		case m.showingSetting:
			var cmd tea.Cmd
			m.settingMenu, cmd = m.settingMenu.Update(msg)
			cmds = append(cmds, cmd)
		case m.pickingSSHAlias:
			var cmd tea.Cmd
			m.sshAliasPicker, cmd = m.sshAliasPicker.Update(msg)
			cmds = append(cmds, cmd)
		case m.pickingModel:
			var cmd tea.Cmd
			m.modelPicker, cmd = m.modelPicker.Update(msg)
			cmds = append(cmds, cmd)
		case m.pickingTheme:
			var cmd tea.Cmd
			m.themePicker, cmd = m.themePicker.Update(msg)
			cmds = append(cmds, cmd)
		case m.sshStep == 3:
			var cmd tea.Cmd
			m.dirList, cmd = m.dirList.Update(msg)
			cmds = append(cmds, cmd)
		case m.ready:
			var vpCmd tea.Cmd
			m.viewport, vpCmd = m.viewport.Update(msg)
			cmds = append(cmds, vpCmd)
		}
		return m, tea.Batch(cmds...)

	case tea.KeyPressMsg:
		m.invalidateFooterCache() // textarea content may change
		// Tool approval dialog handling
		if m.approvalPending {
			switch msg.String() {
			case "left":
				if m.approvalSelected > 0 {
					m.approvalSelected--
				}
				return m, tea.Batch(cmds...)
			case "right", "tab":
				if m.approvalSelected < 2 {
					m.approvalSelected++
				}
				return m, tea.Batch(cmds...)
			case "enter", " ":
				switch m.approvalSelected {
				case 0: // Approve once
					m.resolveApproval(ToolApprovalResponse{Approved: true, Mode: ModeManual})
				case 1: // Approve all
					m.approvalMode = ModeAuto
					m.resolveApproval(ToolApprovalResponse{Approved: true, Mode: ModeAuto})
					if m.OnApprovalModeChange != nil {
						m.OnApprovalModeChange(true)
					}
				case 2: // Reject
					m.lines = append(m.lines, textLine(fmt.Sprintf("   %s %s — user denied this operation",
						toolErrorStyle.Render("⚠ Rejected:"),
						toolNameStyle.Render(m.approvalToolName))))
					m.resolveApproval(ToolApprovalResponse{Approved: false, Mode: m.approvalMode})
				}
				return m, tea.Batch(cmds...)
			case "y", "Y":
				// Event: ApproveOnce - approve current only, stay in MANUAL mode
				m.resolveApproval(ToolApprovalResponse{Approved: true, Mode: ModeManual})
				return m, tea.Batch(cmds...)
			case "a", "A":
				m.approvalMode = ModeAuto
				m.resolveApproval(ToolApprovalResponse{Approved: true, Mode: ModeAuto})
				if m.OnApprovalModeChange != nil {
					m.OnApprovalModeChange(true)
				}
				return m, tea.Batch(cmds...)
			case "n", "N", "esc":
				// Event: Reject - deny the operation
				m.lines = append(m.lines, textLine(fmt.Sprintf("   %s %s — user denied this operation",
					toolErrorStyle.Render("⚠ Rejected:"),
					toolNameStyle.Render(m.approvalToolName))))
				m.resolveApproval(ToolApprovalResponse{Approved: false, Mode: m.approvalMode})
				return m, tea.Batch(cmds...)
			}
			return m, tea.Batch(cmds...)
		}

		// Plan review handling (bottom prompt with 3 options)
		if m.planReviewActive {
			if m.planRejectInput {
				// Collecting rejection feedback
				switch msg.String() {
				case "enter":
					feedback := strings.TrimSpace(m.textarea.Value())
					m.textarea.Reset()
					m.textarea.SetHeight(1)
					m.textareaLines = 1
					m.planRejectInput = false
					m.planReviewActive = false
					planResponseCh <- PlanResponse{Approved: false, Feedback: feedback}
					m.lines = append(m.lines, textLine(fmt.Sprintf("   %s Plan rejected%s",
						toolErrorStyle.Render("✗"),
						func() string {
							if feedback != "" {
								return ": " + feedback
							}
							return ""
						}())))
					m.textarea.Focus()
					m.textarea.Placeholder = "Type your prompt here..."
					m.refreshViewport()
					return m, tea.Batch(cmds...)
				case "esc":
					m.planRejectInput = false
					m.textarea.Reset()
					m.textarea.SetHeight(1)
					m.textareaLines = 1
					m.textarea.Placeholder = "Type your prompt here..."
					return m, tea.Batch(cmds...)
				default:
					var cmd tea.Cmd
					m.textarea, cmd = m.textarea.Update(msg)
					cmds = append(cmds, cmd)
					return m, tea.Batch(cmds...)
				}
			}
			switch msg.String() {
			case "y", "Y":
				m.planReviewActive = false
				planResponseCh <- PlanResponse{Approved: true}
				m.lines = append(m.lines, textLine(fmt.Sprintf("   %s Plan approved: %s",
					toolSuccessStyle.Render("✓"),
					toolNameStyle.Render(m.planReviewTitle))))
				m.textarea.Focus()
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			case "n", "N":
				m.planReviewSelected = 1
				m.planRejectInput = true
				m.textarea.SetValue("")
				m.textarea.Focus()
				m.textarea.Placeholder = "Enter feedback (optional, then press Enter)..."
				return m, tea.Batch(cmds...)
			case "up":
				if m.planReviewSelected > 0 {
					m.planReviewSelected--
				}
				return m, tea.Batch(cmds...)
			case "down":
				if m.planReviewSelected < 2 {
					m.planReviewSelected++
				}
				return m, tea.Batch(cmds...)
			case "enter":
				switch m.planReviewSelected {
				case 0: // Approve
					m.planReviewActive = false
					planResponseCh <- PlanResponse{Approved: true}
					m.lines = append(m.lines, textLine(fmt.Sprintf("   %s Plan approved: %s",
						toolSuccessStyle.Render("✓"),
						toolNameStyle.Render(m.planReviewTitle))))
					m.textarea.Focus()
					m.refreshViewport()
				case 1: // Reject with feedback
					m.planRejectInput = true
					m.textarea.SetValue("")
					m.textarea.Focus()
					m.textarea.Placeholder = "Enter feedback (optional, then press Enter)..."
				case 2: // Dismiss
					m.planReviewActive = false
					planResponseCh <- PlanResponse{Approved: false, Feedback: ""}
					m.lines = append(m.lines, textLine(fmt.Sprintf("   %s Plan dismissed",
						toolErrorStyle.Render("✗"))))
					m.textarea.Focus()
					m.refreshViewport()
				}
				return m, tea.Batch(cmds...)
			case "esc":
				m.planReviewActive = false
				planResponseCh <- PlanResponse{Approved: false, Feedback: ""}
				m.lines = append(m.lines, textLine(fmt.Sprintf("   %s Plan dismissed",
					toolErrorStyle.Render("✗"))))
				m.textarea.Focus()
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			case "pgup", "pgdown":
				if m.ready {
					var cmd tea.Cmd
					m.viewport, cmd = m.viewport.Update(msg)
					cmds = append(cmds, cmd)
				}
				return m, tea.Batch(cmds...)
			}
			return m, tea.Batch(cmds...)
		}

		// Ask user question handling (bottom prompt with options)
		if m.askUserActive {
			optCount := len(m.askUserOptions)
			totalOpts := optCount // predefined options count
			if optCount > 0 {
				totalOpts = optCount + 1 // +1 for "Other (type below)"
			}
			switch msg.String() {
			case "up":
				if m.askUserSelected > 0 {
					m.askUserSelected--
				}
				// Focus/blur textarea based on selection
				if optCount > 0 && m.askUserSelected == optCount {
					m.textarea.Focus()
					m.textarea.Placeholder = "Type your answer..."
				} else if optCount > 0 {
					m.textarea.Blur()
				}
				return m, tea.Batch(cmds...)
			case "down":
				if m.askUserSelected < totalOpts-1 {
					m.askUserSelected++
				}
				if optCount > 0 && m.askUserSelected == optCount {
					m.textarea.Focus()
					m.textarea.Placeholder = "Type your answer..."
				} else if optCount > 0 {
					m.textarea.Blur()
				}
				return m, tea.Batch(cmds...)
			case "enter":
				var answer string
				if m.askUserSelected < optCount {
					// Selected a predefined option
					answer = m.askUserOptions[m.askUserSelected]
				} else {
					// Custom text input
					answer = strings.TrimSpace(m.textarea.Value())
					m.textarea.Reset()
					m.textarea.SetHeight(1)
					m.textareaLines = 1
				}
				m.askUserActive = false
				askUserResponseCh <- AskUserResponse{Answer: answer}
				m.lines = append(m.lines, textLine(fmt.Sprintf("   %s %s",
					userLabelStyle.Render("💬 Answer:"), answer)))
				m.textarea.Focus()
				m.textarea.Placeholder = "Type your prompt here..."
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			case "esc":
				m.askUserActive = false
				m.textarea.Reset()
				m.textarea.SetHeight(1)
				m.textareaLines = 1
				m.textarea.Placeholder = "Type your prompt here..."
				askUserResponseCh <- AskUserResponse{Answer: ""}
				m.lines = append(m.lines, textLine(fmt.Sprintf("   %s Question dismissed",
					toolErrorStyle.Render("✗"))))
				m.textarea.Focus()
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			case "pgup", "pgdown":
				if m.ready {
					var cmd tea.Cmd
					m.viewport, cmd = m.viewport.Update(msg)
					cmds = append(cmds, cmd)
				}
				return m, tea.Batch(cmds...)
			default:
				// If text input is selected (Other or no options), forward keys to textarea
				if optCount == 0 || m.askUserSelected == optCount {
					var cmd tea.Cmd
					m.textarea, cmd = m.textarea.Update(msg)
					cmds = append(cmds, cmd)
				}
				return m, tea.Batch(cmds...)
			}
		}

		// Session picker handling
		if m.pickingSession {
			switch msg.String() {
			case "enter":
				selected := m.sessionPicker.SelectedItem()
				if selected != nil {
					selItem := selected.(sessionListItem)
					m.pickingSession = false
					m.textarea.Focus()
					m.lines = append(m.lines, textLine(toolLabelStyle.Render("📂 Loading session...")))
					m.thinking = true
					m.mode = ModeAgent
					m.agentDone = false
					uuid := selItem.meta.UUID
					cmds = append(cmds, m.spinner.Tick)
					cmds = append(cmds, func() tea.Msg {
						return ResumeRequestMsg{UUID: uuid}
					})
					return m, tea.Batch(cmds...)
				}
			case "ctrl+c", "esc":
				m.pickingSession = false
				m.textarea.Focus()
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			}
			var cmd tea.Cmd
			m.sessionPicker, cmd = m.sessionPicker.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		// Setting menu handling
		if m.showingSetting {
			switch msg.String() {
			case "enter":
				selected := m.settingMenu.SelectedItem()
				if selected != nil {
					selItem := selected.(settingItem)
					switch selItem.key {
					case "switch_model":
						m.showingSetting = false
						return m.handleModelInput(cmds)
					case "manage_models":
						m.showingSetting = false
						return m.openManageModels(cmds)
					case "add_model":
						m.showingSetting = false
						m.textarea.Focus()
						// Signal to launch the setup TUI
						cmds = append(cmds, func() tea.Msg {
							return AddModelMsg{}
						})
						return m, tea.Batch(cmds...)
					case "edit_config":
						m.showingSetting = false
						m.textarea.Focus()
						m.lines = append(m.lines, textLine(toolLabelStyle.Render("⚙ Settings:")+" Please edit "+config.ConfigPath()))
						m.refreshViewport()
						return m, tea.Batch(cmds...)
					}
				}
			case "ctrl+c", "esc":
				m.showingSetting = false
				m.textarea.Focus()
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			}
			var cmd tea.Cmd
			m.settingMenu, cmd = m.settingMenu.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		// Channel panel handling
		if m.showingChannel {
			return m.handleChannelKeyPress(msg, cmds)
		}

		// Help panel handling
		if m.showingHelp {
			switch msg.String() {
			case "esc", "f1", "q", "?":
				m.showingHelp = false
				m.helpScroll = 0
				m.textarea.Focus()
				return m, tea.Batch(cmds...)
			case "up", "k":
				if m.helpScroll > 0 {
					m.helpScroll--
				}
				return m, tea.Batch(cmds...)
			case "down", "j":
				m.helpScroll++
				return m, tea.Batch(cmds...)
			case "pgup":
				m.helpScroll -= 5
				if m.helpScroll < 0 {
					m.helpScroll = 0
				}
				return m, tea.Batch(cmds...)
			case "pgdown":
				m.helpScroll += 5
				return m, tea.Batch(cmds...)
			case "home":
				m.helpScroll = 0
				return m, tea.Batch(cmds...)
			}
			return m, tea.Batch(cmds...)
		}

		// F1 (always) or ? (when the input is empty) opens the help panel.
		if msg.String() == "f1" || (msg.String() == "?" && strings.TrimSpace(m.textarea.Value()) == "") {
			m.showingHelp = true
			m.helpScroll = 0
			m.textarea.Blur()
			return m, tea.Batch(cmds...)
		}

		// SSH alias picker handling
		if m.pickingSSHAlias {
			switch msg.String() {
			case "enter":
				selected := m.sshAliasPicker.SelectedItem()
				if selected != nil {
					selItem := selected.(sshAliasItem)
					m.pickingSSHAlias = false
					m.textarea.Focus()
					if selItem.isNew {
						// Start new SSH connection wizard
						m.sshStep = 1
						m.lines = append(m.lines, textLine(toolLabelStyle.Render("🔗 SSH Setup")))
						m.textarea.Placeholder = "Enter SSH address (e.g. root@hostname)..."
						m.refreshViewport()
						return m, tea.Batch(cmds...)
					}
					// Connect using saved alias
					path := selItem.path
					if path == "" {
						path = "?"
					}
					return m.startSSHConnect(selItem.addr, path, cmds)
				}
			case "ctrl+c", "esc":
				m.pickingSSHAlias = false
				m.textarea.Focus()
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			}
			var cmd tea.Cmd
			m.sshAliasPicker, cmd = m.sshAliasPicker.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		if m.managingModels {
			return m.handleManageModelsKey(msg, cmds)
		}

		if m.pickingTheme {
			return m.handleThemePickerKey(msg, cmds)
		}

		if m.pickingModel {
			// When the list is actively filtering, let all keys pass through to the list
			if m.modelPicker.FilterState() == list.Filtering {
				var cmd tea.Cmd
				m.modelPicker, cmd = m.modelPicker.Update(msg)
				cmds = append(cmds, cmd)
				return m, tea.Batch(cmds...)
			}
			switch msg.String() {
			case "enter":
				selected := m.modelPicker.SelectedItem()
				if selected != nil {
					selItem := selected.(modelItem)
					// Skip provider headers
					if selItem.isProviderHeader {
						return m, tea.Batch(cmds...)
					}
					if selItem.isAction {
						m.modelPicker.ResetFilter()
						m.pickingModel = false
						m.textarea.Focus()
						switch selItem.actionID {
						case "manage_models":
							// Open model management view
							return m.openManageModels(cmds)
						default:
							// "Add New Provider" — launch setup wizard
							cmds = append(cmds, func() tea.Msg {
								return AddModelMsg{}
							})
						}
						return m, tea.Batch(cmds...)
					}
					if selItem.isCurrent {
						// Already active — just close picker
						m.modelPicker.ResetFilter()
						m.pickingModel = false
						m.textarea.Focus()
						m.refreshViewport()
						return m, tea.Batch(cmds...)
					}
					cfg, err := config.LoadConfig()
					if err == nil {
						cfg.Model = selItem.provider + "/" + selItem.model
						_ = config.SaveConfig(cfg)
						m.activeProvider = selItem.provider
						m.activeModel = selItem.model
						m.invalidateSidebarCache()
						m.invalidateFooterCache()
						// Track in recent models.
						if state, err := config.LoadModelState(); err == nil {
							state.AddRecent(config.ModelRef{Provider: selItem.provider, Model: selItem.model})
							_ = config.SaveModelState(state)
						}
						select {
						case configCh <- cfg:
						default:
						}
					}
					m.modelPicker.ResetFilter()
					m.pickingModel = false
					m.lines = append(m.lines, textLine(fmt.Sprintf("  %s Switched to %s",
						toolSuccessStyle.Render("✓"),
						toolNameStyle.Render(selItem.provider+"/"+selItem.model))))
					m.textarea.Focus()
					m.refreshViewport()
					return m, tea.Batch(cmds...)
				}
			case "ctrl+c", "esc":
				// Reset filter state before closing
				m.modelPicker.ResetFilter()
				m.pickingModel = false
				m.textarea.Focus()
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			}
			var cmd tea.Cmd
			m.modelPicker, cmd = m.modelPicker.Update(msg)
			cmds = append(cmds, cmd)

			// Skip provider headers when navigating with arrow keys
			if msg.String() == "up" || msg.String() == "down" {
				if selected := m.modelPicker.SelectedItem(); selected != nil {
					if selItem, ok := selected.(modelItem); ok && selItem.isProviderHeader {
						// Item is a header, move again in the same direction
						m.modelPicker, cmd = m.modelPicker.Update(msg)
						cmds = append(cmds, cmd)
						// Check again in case there are consecutive headers
						if selected := m.modelPicker.SelectedItem(); selected != nil {
							if selItem, ok := selected.(modelItem); ok && selItem.isProviderHeader {
								m.modelPicker, cmd = m.modelPicker.Update(msg)
								cmds = append(cmds, cmd)
							}
						}
					}
				}
			}

			return m, tea.Batch(cmds...)
		}

		if m.sshStep == 3 {
			switch msg.String() {
			case "tab":
				// Tab = confirm current directory (Open Folder)
				return m.startSSHConnect(m.sshAddr, m.sshPath, cmds)
			case "enter":
				selected := m.dirList.SelectedItem()
				if selected != nil {
					selItem := selected.(dirItem)
					if selItem.isSelectBtn {
						// Finalize dir selection
						return m.startSSHConnect(m.sshAddr, m.sshPath, cmds)
					}
					// Otherwise, list this new dir
					m.thinking = true
					m.sshPath = path.Join(m.sshPath, selItem.name)
					if m.sshPath == "" {
						m.sshPath = "/"
					}
					cmds = append(cmds, m.spinner.Tick)
					cmds = append(cmds, func() tea.Msg {
						return SSHListDirReqMsg{Path: m.sshPath}
					})
					return m, tea.Batch(cmds...)
				}
			case "ctrl+c", "esc":
				// Cancel SSH step — notify main to restore local env
				m.sshStep = 0
				m.sshPath = ""
				m.sshAddr = ""
				m.sshSaveAddr = ""
				m.sshSavePath = ""
				m.textarea.Placeholder = "Type your prompt here..."
				m.lines = append(m.lines, textLine(toolLabelStyle.Render("🔗 SSH:")+" Cancelled."))
				m.refreshViewport()
				cmds = append(cmds, func() tea.Msg {
					return SSHCancelMsg{}
				})
				return m, tea.Batch(cmds...)
			}

			// Update list
			var cmd tea.Cmd
			m.dirList, cmd = m.dirList.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		if m.inputActive() {
			// Cancel-agent dialog is an overlay — handle its keys first
			if m.cancelPending {
				switch msg.String() {
				case "ctrl+c":
					return m, tea.Quit
				case "left", "right", "tab":
					m.cancelSelected = 1 - m.cancelSelected
					return m, tea.Batch(cmds...)
				case "enter", " ":
					if m.cancelSelected == 0 {
						m.confirmCancelAgent()
					} else {
						m.cancelPending = false
					}
					return m, tea.Batch(cmds...)
				case "y", "Y":
					m.confirmCancelAgent()
					return m, tea.Batch(cmds...)
				case "n", "N", "esc":
					m.cancelPending = false
					return m, tea.Batch(cmds...)
				default:
					m.cancelPending = false
				}
			}

			// Exit dialog is an overlay — handle its keys first
			if m.exitPending && msg.String() != "ctrl+c" {
				switch msg.String() {
				case "left", "right", "tab":
					m.exitSelected = 1 - m.exitSelected // toggle 0/1
					return m, tea.Batch(cmds...)
				case "enter", " ":
					m.exitPending = false
					if m.exitSelected == 0 {
						return m, tea.Quit
					}
					return m, tea.Batch(cmds...)
				case "y", "Y":
					m.exitPending = false
					return m, tea.Quit
				case "n", "N", "esc":
					m.exitPending = false
					return m, tea.Batch(cmds...)
				default:
					m.exitPending = false
				}
			}

			switch msg.String() {
			case "ctrl+c":
				// If agent is running, show cancel dialog instead of quit dialog
				if m.thinking && !m.agentDone {
					m.requestCancelAgent()
					return m, tea.Batch(cmds...)
				}
				// Check if already pending (2nd Ctrl+C = force quit)
				if m.exitPending {
					return m, tea.Quit
				}
				// Show exit confirmation dialog overlay
				m.exitPending = true
				m.exitSelected = 1 // Default to "No" for safety
				m.exitWarningTime = time.Now()
				return m, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
					return ExitTimeoutMsg{}
				})
			case "shift+tab":
				// Cycle the unified session mode: Approval → Plan → Full access → Approval.
				next := m.selectorMode().Next()
				m.applySelectorMode(next)
				m.invalidateFooterCache()
				// Notify main goroutine to apply both axes (tools/prompt + approval).
				select {
				case modeSelectCh <- next:
				default:
				}
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			case "ctrl+l":
				// Quick model switch
				return m.handleModelInput(cmds)
			case "ctrl+up":
				// Scroll sidebar todo list up
				if m.showSidebar && m.sidebarScrollOffset > 0 {
					m.sidebarScrollOffset--
					m.invalidateSidebarCache()
					m.refreshViewport()
					return m, tea.Batch(cmds...)
				}
			case "ctrl+down":
				// Scroll sidebar todo list down
				if m.showSidebar && m.todoStore != nil {
					maxOffset := len(m.todoStore.Items()) - 3
					if maxOffset < 0 {
						maxOffset = 0
					}
					if m.sidebarScrollOffset < maxOffset {
						m.sidebarScrollOffset++
						m.invalidateSidebarCache()
						m.refreshViewport()
						return m, tea.Batch(cmds...)
					}
				}
			case "tab":
				// Accept command suggestion if active
				if m.cmdSuggestionActive && len(m.cmdSuggestions) > 0 && m.cmdSuggestionIndex < len(m.cmdSuggestions) {
					selected := m.cmdSuggestions[m.cmdSuggestionIndex]
					m.textarea.SetValue(selected.cmd)
					m.textarea.CursorEnd()
					m.cmdSuggestionActive = false
					m.cmdSuggestions = nil
					m.cmdSuggestionIndex = 0
					m.textareaLines = m.recalcTextareaLines()
					m.textarea.SetHeight(m.textareaLines)
					// Re-evaluate suggestions after setting value (may show new filtered list)
					m.updateSuggestions()
					if m.ready {
						m.viewport.SetHeight(m.calcViewportHeight(m.inputActive()))
					}
					return m, tea.Batch(cmds...)
				}
			case "esc":
				// Dismiss command suggestion if active
				if m.cmdSuggestionActive {
					m.cmdSuggestionActive = false
					m.cmdSuggestions = nil
					m.cmdSuggestionIndex = 0
					return m, tea.Batch(cmds...)
				}
				// Team: exit teammate view, return to leader
				if m.teamState.ViewMode == TeamViewTeammate {
					m.exitTeammateView()
					m.refreshViewport()
					return m, tea.Batch(cmds...)
				}
			case "enter":
				// If command suggestion is active, accept it instead of submitting
				if m.cmdSuggestionActive && len(m.cmdSuggestions) > 0 && m.cmdSuggestionIndex < len(m.cmdSuggestions) {
					selected := m.cmdSuggestions[m.cmdSuggestionIndex]
					m.textarea.SetValue(selected.cmd)
					m.textarea.CursorEnd()
					m.cmdSuggestionActive = false
					m.cmdSuggestions = nil
					m.cmdSuggestionIndex = 0
					m.textareaLines = m.recalcTextareaLines()
					m.textarea.SetHeight(m.textareaLines)
					// Re-evaluate: exact match clears suggestions, partial shows new list
					m.updateSuggestions()
					if m.ready {
						m.viewport.SetHeight(m.calcViewportHeight(m.inputActive()))
					}
					return m, tea.Batch(cmds...)
				}
				prompt := strings.TrimSpace(m.textarea.Value())
				if prompt != "" {
					// Expand paste references to full content for the agent
					actualPrompt := m.pasteStore.ExpandRefs(prompt)
					appendHistory(prompt)
					if len(m.history) == 0 || m.history[len(m.history)-1] != prompt {
						m.history = append(m.history, prompt)
					}
					m.historyIndex = len(m.history)

					m.textarea.Reset()
					m.textareaLines = 1
					m.textarea.SetHeight(1)
					// Clear any active suggestions
					m.cmdSuggestionActive = false
					m.cmdSuggestions = nil
					m.cmdSuggestionIndex = 0

					// Team: route input to viewed teammate
					if m.teamState.ViewMode == TeamViewTeammate && m.teamState.ViewingAgent != "" {
						m.teamState.Manager.EnqueueUserMessage(m.teamState.ViewingAgent, actualPrompt)
						// prompt already contains compact references from paste-time
						m.lines = append(m.lines, textLine(userPromptStyle.Render("> "+prompt)))
						m.refreshViewport()
						return m, tea.Batch(cmds...)
					}

					if prompt == "/setting" {
						return m.handleSettingInput(cmds)
					}
					if prompt == "/model" {
						return m.handleModelInput(cmds)
					}
					if prompt == "/theme" {
						return m.openThemePicker(cmds)
					}

					if prompt == "/ssh" || strings.HasPrefix(prompt, "/ssh ") {
						return m.handleSSHInput(prompt, cmds)
					}

					if prompt == "/resume" || strings.HasPrefix(prompt, "/resume ") {
						return m.handleResumeInput(prompt, cmds)
					}

					if strings.HasPrefix(prompt, "/bg") {
						return m.handleBgInput(cmds)
					}

					if prompt == "/compact" {
						return m.handleCompactInput(cmds)
					}

					if prompt == "/goal" || strings.HasPrefix(prompt, "/goal ") {
						return m.handleGoalInput(prompt, cmds)
					}

					if prompt == "/channel" {
						return m.handleChannelInput(cmds)
					}

					if prompt == "/mcp" || strings.HasPrefix(prompt, "/mcp ") {
						return m.handleMCPInput(prompt, cmds)
					}

					if prompt == "/help" {
						m.showingHelp = true
						m.helpScroll = 0
						m.textarea.Blur()
						return m, tea.Batch(cmds...)
					}

					// Check skill slash commands (e.g. /review-pr, /security-review)
					if strings.HasPrefix(prompt, "/") {
						if skillCmd := m.matchSkillSlash(prompt); skillCmd != nil {
							return m.handleSkillSlashInput(skillCmd.SkillName, skillCmd.UserInput, cmds)
						}
					}

					if m.sshSavePrompt {
						return m.handleSSHSaveAlias(prompt, cmds)
					}

					if m.sshStep > 0 {
						return m.handleSSHStep(prompt, cmds)
					}

					if len(m.lines) > 0 {
						// Check if the lines are the initial welcome message, we clear it.
						if strings.Contains(m.lines[0].text, "Welcome to JCODE") {
							m.lines = nil
						}
					}

					if !m.agentDone && m.thinking {
						m.pendingPrompts = append(m.pendingPrompts, actualPrompt)
						// prompt already contains compact references from paste-time
						m.lines = append(m.lines, textLine(userPromptStyle.Render("> "+prompt+" (queued)")))
						if m.ready {
							m.contentDirty = true
							m.viewport.SetHeight(m.calcViewportHeight(true))
							m.viewport.SetContent(m.renderViewportContent())
							m.viewport.GotoBottom()
						}
						return m, tea.Batch(cmds...)
					}

					m.mode = ModeAgent
					m.agentDone = false
					m.thinking = true
					m.promptStartTime = time.Now()

					// In Plan mode, send prompt directly (agent already has plan system prompt + read-only tools).
					modePrefix := ">"
					if m.agentMode == ModePlanning {
						modePrefix = "📐"
					}

					// prompt already contains compact references from paste-time
					m.lines = append(m.lines, textLine(""))
					m.lines = append(m.lines, textLine(userPromptStyle.Render(modePrefix+" "+prompt)))
					if m.ready {
						m.contentDirty = true
						m.viewport.SetHeight(m.calcViewportHeight(false))
						m.viewport.SetContent(m.renderViewportContent())
						m.viewport.GotoBottom()
					}
					cmds = append(cmds, func() tea.Msg {
						return PromptSubmitMsg{Prompt: actualPrompt}
					})
					cmds = append(cmds, m.spinner.Tick)
				}
				return m, tea.Batch(cmds...)
			case "shift+enter":
				// Insert newline into textarea by forwarding a plain enter key
				var cmd tea.Cmd
				m.textarea, cmd = m.textarea.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				cmds = append(cmds, cmd)
				m.textareaLines = m.recalcTextareaLines()
				m.textarea.SetHeight(m.textareaLines)
				if m.ready {
					m.viewport.SetHeight(m.calcViewportHeight(m.inputActive()))
				}
				return m, tea.Batch(cmds...)
			case "up":
				if m.cmdSuggestionActive && len(m.cmdSuggestions) > 0 {
					// Navigate suggestion list up
					if m.cmdSuggestionIndex > 0 {
						m.cmdSuggestionIndex--
					}
					return m, tea.Batch(cmds...)
				}
				// Smart navigation: move cursor up within textarea first;
				// only trigger history when already on the first line.
				if m.textarea.Line() > 0 {
					m.textarea.CursorUp()
					return m, tea.Batch(cmds...)
				}
				if m.historyIndex > 0 {
					m.historyIndex--
					m.textarea.SetValue(m.history[m.historyIndex])
					m.textarea.CursorEnd()
					m.textareaLines = m.recalcTextareaLines()
					m.textarea.SetHeight(m.textareaLines)
					m.updateSuggestions()
					if m.ready {
						m.viewport.SetHeight(m.calcViewportHeight(m.inputActive()))
					}
				}
				return m, tea.Batch(cmds...)
			case "down":
				if m.cmdSuggestionActive && len(m.cmdSuggestions) > 0 {
					// Navigate suggestion list down
					if m.cmdSuggestionIndex < len(m.cmdSuggestions)-1 {
						m.cmdSuggestionIndex++
					}
					return m, tea.Batch(cmds...)
				}
				// Smart navigation: move cursor down within textarea first;
				// only trigger history when already on the last line.
				if m.textarea.Line() < m.textarea.LineCount()-1 {
					m.textarea.CursorDown()
					return m, tea.Batch(cmds...)
				}
				if m.historyIndex < len(m.history)-1 {
					m.historyIndex++
					m.textarea.SetValue(m.history[m.historyIndex])
					m.textarea.CursorEnd()
				} else if m.historyIndex == len(m.history)-1 {
					m.historyIndex++
					m.textarea.SetValue("")
				}
				m.textareaLines = m.recalcTextareaLines()
				m.textarea.SetHeight(m.textareaLines)
				m.updateSuggestions()
				if m.ready {
					m.viewport.SetHeight(m.calcViewportHeight(m.inputActive()))
				}
				return m, tea.Batch(cmds...)
			case "pgup", "pgdown":
				if m.ready && m.mode == ModeAgent {
					var vpCmd tea.Cmd
					m.viewport, vpCmd = m.viewport.Update(msg)
					cmds = append(cmds, vpCmd)
				}
				return m, tea.Batch(cmds...)
			case "shift+up":
				// Team: switch to previous agent view
				if m.teamState.HasTeam() {
					m.switchTeamView(-1)
					m.refreshViewport()
					return m, tea.Batch(cmds...)
				}
			case "shift+down":
				// Team: switch to next agent view
				if m.teamState.HasTeam() {
					m.switchTeamView(1)
					m.refreshViewport()
					return m, tea.Batch(cmds...)
				}
			case "ctrl+t":
				// Team: toggle coordinator panel
				if m.teamState.HasTeam() {
					m.teamState.PanelVisible = !m.teamState.PanelVisible
					m.refreshViewport()
					return m, tea.Batch(cmds...)
				}
			case "ctrl+e":
				// Toggle expand/collapse of subagent output near viewport top
				m.toggleSubagentExpand()
				return m, tea.Batch(cmds...)
			case "ctrl+y":
				// Copy last assistant message to clipboard
				text := m.currentText.String()
				if text == "" {
					text = m.lastAssistantRawText
				}
				if text != "" {
					cmds = append(cmds, tea.SetClipboard(text))
				}
				return m, tea.Batch(cmds...)
			}
			// Forward other keys to textarea
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			cmds = append(cmds, cmd)
			m.textareaLines = m.recalcTextareaLines()
			m.textarea.SetHeight(m.textareaLines)
			m.updateSuggestions()
			if m.ready {
				m.viewport.SetHeight(m.calcViewportHeight(m.inputActive()))
			}
			return m, tea.Batch(cmds...)
		}
		// Agent running — handle cancel/exit dialogs or ctrl+c
		switch {
		case m.cancelPending:
			switch msg.String() {
			case "ctrl+c":
				// 2nd Ctrl+C while cancel dialog → just quit
				return m, tea.Quit
			case "left", "right", "tab":
				m.cancelSelected = 1 - m.cancelSelected
				return m, tea.Batch(cmds...)
			case "enter", " ":
				if m.cancelSelected == 0 {
					m.confirmCancelAgent()
				} else {
					m.cancelPending = false
				}
				return m, tea.Batch(cmds...)
			case "y", "Y":
				m.confirmCancelAgent()
				return m, tea.Batch(cmds...)
			case "n", "N", "esc":
				m.cancelPending = false
				return m, tea.Batch(cmds...)
			default:
				m.cancelPending = false
			}
		case m.exitPending:
			switch msg.String() {
			case "ctrl+c":
				// 2nd Ctrl+C while exit dialog is showing during agent run: force quit
				return m, tea.Quit
			case "left", "right", "tab":
				m.exitSelected = 1 - m.exitSelected
				return m, tea.Batch(cmds...)
			case "enter", " ":
				m.exitPending = false
				if m.exitSelected == 0 {
					return m, tea.Quit
				}
				return m, tea.Batch(cmds...)
			case "y", "Y":
				m.exitPending = false
				return m, tea.Quit
			case "n", "N", "esc":
				m.exitPending = false
				return m, tea.Batch(cmds...)
			default:
				m.exitPending = false
			}
		case msg.String() == "ctrl+c":
			// Show cancel-agent confirmation dialog
			m.requestCancelAgent()
			return m, tea.Batch(cmds...)
		}

	case tea.BackgroundColorMsg:
		// Auto-select a light/dark default theme from the terminal background,
		// but only when the user hasn't explicitly chosen or persisted one.
		if !m.themePersisted {
			want := theme.Default(theme.Dark)
			if !msg.IsDark() {
				want = theme.Default(theme.Light)
			}
			if want != currentTheme.Name {
				m.applyThemePreview(want)
			}
		}
		return m, tea.Batch(cmds...)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Determine if sidebar will be shown (width-only, consistent with View()).
		m.showSidebar = m.width >= minWidthForSidebar
		mainWidth := m.width
		if m.showSidebar {
			mainWidth = m.width - sidebarWidth
		}

		inputWidth := mainWidth - 6
		if inputWidth < 20 {
			inputWidth = 20
		}
		m.textarea.SetWidth(inputWidth)

		// Update textarea max height based on new terminal dimensions
		newMaxHeight := calcMaxTextareaLines(m.height)
		m.textarea.MaxHeight = newMaxHeight
		m.textareaLines = recalcLines(m.textarea.Value(), newMaxHeight, m.textarea.Width())
		m.textarea.SetHeight(m.textareaLines)

		vpH := m.calcViewportHeight(m.inputActive())
		if !m.ready {
			m.viewport = viewport.New(viewport.WithWidth(mainWidth), viewport.WithHeight(vpH))
			m.viewport.SoftWrap = true
			m.ready = true
		} else {
			m.viewport.SetWidth(mainWidth)
			m.viewport.SetHeight(vpH)
		}
		m.dirList.SetSize(msg.Width, vpH)
		m.settingMenu.SetSize(msg.Width, vpH)
		m.sshAliasPicker.SetSize(msg.Width, vpH)
		m.sessionPicker.SetSize(msg.Width, vpH)
		m.recreateMDRenderer()
		m.contentDirty = true
		m.invalidateSidebarCache()
		m.invalidateFooterCache()
		// Invalidate per-line render caches since width changed.
		// The cachedWidth check in contentLine.render() handles this naturally,
		// but we reset renderedLineWidth so renderContent() takes the slow path.
		m.renderedLineWidth = 0
		m.viewport.SetContent(m.renderViewportContent())

	case spinner.TickMsg:
		if m.thinking {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
			m.contentDirty = true
		}

	case PromptSubmitMsg:
		promptCh <- msg.Prompt

	case SSHConnectMsg:
		sshCh <- msg

	case SSHListDirReqMsg:
		sshCh <- msg

	case SSHCancelMsg:
		m.envLabel = "Local"
		m.invalidateSidebarCache()
		sshCh <- msg

	case ConfigUpdatedMsg:
		m.activeProvider = msg.Provider
		m.activeModel = msg.Model
		m.invalidateSidebarCache()
		m.invalidateFooterCache()
		if msg.Message != "" {
			m.lines = append(m.lines, textLine(msg.Message))
			m.refreshViewport()
		}

	case MCPStatusMsg:
		m.mcpStatuses = msg.Statuses
		m.invalidateSidebarCache()
		m.refreshViewport()

	case MCPNoticeMsg:
		m.lines = append(m.lines, textLine("  "+toolLabelStyle.Render("🔐 MCP:")+" "+msg.Text))
		m.refreshViewport()

	case ChannelStateMsg:
		m.channelStates[msg.ChannelID] = msg.State
		if msg.Message != "" {
			m.lines = append(m.lines, textLine(toolLabelStyle.Render("📡 Channel:")+" "+msg.Message))
			m.refreshViewport()
		}

	case ChannelQRCodeMsg:
		m.lines = append(m.lines, textLine(toolLabelStyle.Render("📡 "+msg.Message)))
		if msg.QRCodeContent != "" {
			qrLines := renderQRCode(msg.QRCodeContent)
			m.lines = append(m.lines, toContentLines(qrLines)...)
		}
		m.refreshViewport()

	case ChannelInboundMsg:
		m.lines = append(m.lines, textLine(lipgloss.NewStyle().Foreground(colorSecondary).
			Render(fmt.Sprintf("📱 [WeChat] %s", msg.Text))))
		m.refreshViewport()
		// Queue the inbound message as a pending prompt
		select {
		case pendingPromptCh <- msg.Text:
		default:
		}

	case BLECommandMsg:
		switch msg.Cmd {
		case "input":
			// Replace input area with the received text
			m.invalidateFooterCache()
			m.textarea.SetValue(msg.Val)
			m.textarea.CursorEnd()
			m.textareaLines = m.recalcTextareaLines()
			m.textarea.SetHeight(m.textareaLines)
			if m.ready {
				m.viewport.SetHeight(m.calcViewportHeight(m.inputActive()))
			}
		case "submit":
			// Submit current input content to the agent
			prompt := strings.TrimSpace(m.textarea.Value())
			if prompt != "" {
				// Expand paste references to full content for the agent
				actualPrompt := m.pasteStore.ExpandRefs(prompt)
				appendHistory(prompt)
				if len(m.history) == 0 || m.history[len(m.history)-1] != prompt {
					m.history = append(m.history, prompt)
				}
				m.historyIndex = len(m.history)

				m.textarea.Reset()
				m.textareaLines = 1
				m.textarea.SetHeight(1)
				m.cmdSuggestionActive = false
				m.cmdSuggestions = nil
				m.cmdSuggestionIndex = 0

				if len(m.lines) > 0 && strings.Contains(m.lines[0].text, "Welcome to JCODE") {
					m.lines = nil
				}

				if !m.agentDone && m.thinking {
					m.pendingPrompts = append(m.pendingPrompts, actualPrompt)
					// prompt already contains compact references from paste-time
					m.lines = append(m.lines, textLine(userPromptStyle.Render("> "+prompt+" (queued)")))
					m.refreshViewport()
					return m, tea.Batch(cmds...)
				}

				m.mode = ModeAgent
				m.agentDone = false
				m.thinking = true
				m.promptStartTime = time.Now()

				modePrefix := ">"
				if m.agentMode == ModePlanning {
					modePrefix = "📐"
				}

				// prompt already contains compact references from paste-time
				m.lines = append(m.lines, textLine(""))
				m.lines = append(m.lines, textLine(userPromptStyle.Render(modePrefix+" "+prompt)))
				if m.ready {
					m.contentDirty = true
					m.viewport.SetHeight(m.calcViewportHeight(false))
					m.viewport.SetContent(m.renderViewportContent())
					m.viewport.GotoBottom()
				}
				cmds = append(cmds, func() tea.Msg {
					return PromptSubmitMsg{Prompt: actualPrompt}
				})
				cmds = append(cmds, m.spinner.Tick)
			}
		case "cancel":
			// Clear the input area
			m.invalidateFooterCache()
			m.textarea.Reset()
			m.textareaLines = 1
			m.textarea.SetHeight(1)
			m.cmdSuggestionActive = false
			m.cmdSuggestions = nil
			m.cmdSuggestionIndex = 0
			if m.ready {
				m.viewport.SetHeight(m.calcViewportHeight(m.inputActive()))
			}
		}

	case TodoUpdateMsg:
		m.invalidateSidebarCache()
		m.refreshViewport()

	case ModeSelectedMsg:
		// Backend changed the session mode programmatically (resume restore,
		// plan-completion revert). Sync the pill's two underlying fields.
		m.applySelectorMode(msg.Mode)
		m.invalidateFooterCache()
		m.refreshViewport()

	case AddModelMsg:
		select {
		case addModelCh <- struct{}{}:
		default:
		}

	case ResumeRequestMsg:
		select {
		case resumeCh <- msg.UUID:
		default:
		}

	case AgentsMdMsg:
		m.agentsMdFound = msg.Found
		m.refreshViewport()

	case SkillsLoadedMsg:
		m.skillSlashCommands = msg.SlashCommands

	case SessionResumedMsg:
		m.approvalMode = ModeManual
		m.thinking = false
		m.mode = ModeAgent
		m.agentDone = true
		m.lines = nil
		m.currentText.Reset()
		// Clear todo and usage on resume
		if m.todoStore != nil {
			m.todoStore.Update(nil)
		}
		m.totalTokens = 0
		m.sidebarScrollOffset = 0
		m.invalidateSidebarCache()
		m.invalidateFooterCache()
		m.lines = append(m.lines, textLine(toolLabelStyle.Render("📂 Session resumed: ")+msg.UUID))
		m.lines = append(m.lines, textLine(""))
		for _, e := range msg.Entries {
			switch e.Type {
			case string(session.EntryUser):
				m.lines = append(m.lines, textLine(""))
				displayContent := m.pasteStore.StoreAndFormat(NormalizeLineEndings(e.Content))
				m.lines = append(m.lines, textLine(userPromptStyle.Render("> "+displayContent)))
			case string(session.EntryAssistant):
				if e.Content != "" {
					m.lastAssistantRawText = e.Content
					rendered := e.Content
					if m.mdRenderer != nil {
						if md, err := m.mdRenderer.Render(e.Content); err == nil {
							rendered = md
						}
					}
					m.lines = append(m.lines, textLine(""))
					m.lines = append(m.lines, textLine(rendered))
				}
			case string(session.EntryToolCall):
				// Tool calls always show running icon (they don't have error status yet)
				m.lines = append(m.lines, textLine(fmt.Sprintf("  %s %s %s",
					toolIconRunning,
					toolNameStyle.Render(e.Name),
					toolArgsStyle.Render(truncate(sanitize(e.Args), 100)),
				)))
			case string(session.EntryToolResult):
				if e.Error != "" {
					m.lines = append(m.lines, toolResultContentLine(e.Name, "", fmt.Errorf("%s", e.Error)))
				} else {
					m.lines = append(m.lines, toolResultContentLine(e.Name, e.Output, nil))
				}
			case string(session.EntrySubagentStart):
				typeLabel := e.SubagentType
				if typeLabel == "" {
					typeLabel = "explore"
				}
				m.lines = append(m.lines, textLine(fmt.Sprintf("  %s %s %s",
					subagentLabelStyle.Render("🤖 Subagent:"),
					toolNameStyle.Render(e.SubagentName),
					toolArgsStyle.Render("("+typeLabel+")"),
				)))
			case string(session.EntrySubagentResult):
				if e.Error != "" {
					m.lines = append(m.lines, textLine(fmt.Sprintf("   %s %s",
						toolErrorStyle.Render("✗ Subagent Error:"),
						toolResultStyle.Render(truncate(sanitize(e.Error), maxToolOutputLen)))))
				} else {
					m.lines = append(m.lines, textLine(fmt.Sprintf("   %s",
						toolSuccessStyle.Render("✓ Subagent Done"))))
					m.lines = append(m.lines, toolResultContentLine("subagent", sanitize(e.Output), nil))
				}
			case string(session.EntryPlanUpdate):
				statusIcon := "📝"
				switch e.PlanStatus {
				case "approved":
					statusIcon = "✅"
				case "rejected":
					statusIcon = "❌"
				case "submitted":
					statusIcon = "📤"
				}
				m.lines = append(m.lines, textLine(fmt.Sprintf("  %s Plan %s: %s",
					toolLabelStyle.Render(statusIcon),
					toolNameStyle.Render(e.PlanStatus),
					toolArgsStyle.Render(e.PlanTitle))))
			case string(session.EntryTodoSnapshot):
				if len(e.Todos) > 0 {
					m.lines = append(m.lines, textLine(toolLabelStyle.Render("  📋 Todo List:")))
					for _, t := range e.Todos {
						statusIcon := "⬜"
						if t.Status == "completed" || t.Status == "done" {
							statusIcon = "✅"
						}
						m.lines = append(m.lines, textLine(fmt.Sprintf("     %s %d: %s",
							statusIcon, t.ID, t.Title)))
					}
				}
			case string(session.EntryModeChange):
				m.lines = append(m.lines, textLine(fmt.Sprintf("  %s Mode changed to: %s",
					toolLabelStyle.Render("🔄"),
					toolNameStyle.Render(e.Mode))))
			case string(session.EntryCompact):
				m.lines = append(m.lines, textLine(fmt.Sprintf("  %s Context compacted: %d messages summarized",
					toolSuccessStyle.Render("✓"),
					e.CompactedN)))
			case string(session.EntryBudgetWarning):
				m.lines = append(m.lines, textLine(toolErrorStyle.Render("  ⚠️ Budget warning")))
			}
		}
		// Add a divider line after resumed content
		{
			contentW := m.contentWidth()
			leftMargin := 2
			rightMargin := 2
			dividerText := " ◇ session resumed "
			textW := lipgloss.Width(dividerText)
			fillW := contentW - leftMargin - rightMargin - textW
			if fillW < 0 {
				fillW = 0
			}
			line := strings.Repeat(" ", leftMargin) + lipgloss.NewStyle().Foreground(colorMuted).Render(
				dividerText+strings.Repeat("─", fillW))
			m.lines = append(m.lines, textLine(""))
			m.lines = append(m.lines, textLine(line))
			m.lines = append(m.lines, textLine(""))
		}
		if m.ready {
			m.contentDirty = true
			m.viewport.SetHeight(m.calcViewportHeight(true))
			m.viewport.SetContent(m.renderViewportContent())
			m.viewport.GotoBottom()
		}
		m.textarea.Focus()

	case SSHDirResultsMsg:
		m.thinking = false
		if msg.Err != nil {
			m.lines = append(m.lines, textLine(fmt.Sprintf("   %s Failed to list directory: %v",
				toolErrorStyle.Render("✗ SSH Error:"), msg.Err)))
			m.sshStep = 0
			m.textarea.Placeholder = "Type your prompt here..."
		} else {
			m.sshStep = 3
			m.sshPath = msg.Path
			m.dirList.Title = fmt.Sprintf("Dir: %s", msg.Path)

			// Build list items: .. first, then subdirs, then ✅ at the bottom
			var items []list.Item
			for _, name := range msg.Items {
				if name == ".." {
					items = append(items, dirItem{title: "📁 ..", name: "..", desc: "Parent directory", isDirectory: true})
				}
			}
			for _, name := range msg.Items {
				if name == ".." {
					continue
				}
				fullPath := path.Join(msg.Path, name)
				items = append(items, dirItem{title: "📁 " + fullPath, name: name, desc: "Folder", isDirectory: true})
			}
			items = append(items, dirItem{title: "✅ Use this directory (" + msg.Path + ")", desc: "Open folder here", isDirectory: true, isSelectBtn: true})
			m.dirList.SetItems(items)
		}
		m.refreshViewport()

	case SSHStatusMsg:
		m.thinking = false
		if msg.Success {
			m.envLabel = msg.Label
			m.invalidateSidebarCache()
			m.lines = append(m.lines, textLine(fmt.Sprintf("   %s Connected to %s",
				toolSuccessStyle.Render("✓"), toolNameStyle.Render(msg.Label))))
			// If this was a direct /ssh user@host connection, offer to save alias
			if m.sshSaveAddr != "" {
				// Update sshSavePath from the actual connected path in label
				if m.sshSavePath == "" || m.sshSavePath == "?" {
					// Extract path from label like "user@host (pwd: /path)"
					if idx := strings.Index(msg.Label, "pwd: "); idx >= 0 {
						end := strings.Index(msg.Label[idx:], ")")
						if end > 0 {
							m.sshSavePath = msg.Label[idx+5 : idx+end]
						}
					}
				}
				m.sshSavePrompt = true
				m.lines = append(m.lines, textLine(toolLabelStyle.Render("⚙ SSH:")+" Save as alias? Enter alias name (or press Enter/type 'n' to skip)"))
				m.textarea.Placeholder = "Enter alias name (e.g. my-server)..."
			}
		} else {
			m.lines = append(m.lines, textLine(fmt.Sprintf("   %s %s",
				toolErrorStyle.Render("✗ SSH Error:"),
				toolResultStyle.Render(msg.Err.Error()))))
			m.sshSaveAddr = ""
			m.sshSavePath = ""
		}
		m.agentDone = true
		m.textarea.Focus()
		if m.ready {
			m.contentDirty = true
			m.viewport.SetHeight(m.calcViewportHeight(true))
			m.viewport.SetContent(m.renderViewportContent())
			m.viewport.GotoBottom()
		}

	case UserPromptMsg:
		m.lines = append(m.lines, textLine(""))
		displayPrompt := m.pasteStore.StoreAndFormat(NormalizeLineEndings(sanitize(msg.Prompt)))
		m.lines = append(m.lines, textLine(userPromptStyle.Render("> "+displayPrompt)))
		m.refreshViewport()

	case AgentTextMsg:
		m.renderPerf.agentTextMsgs++
		m.currentText.WriteString(sanitize(msg.Text))
		// Debounce: schedule a batch render instead of rendering every token.
		// Do NOT set contentDirty here — View() must skip rendering until
		// BatchRenderMsg fires, otherwise every token triggers a full render.
		if !m.renderPending {
			m.renderPending = true
			cmds = append(cmds, tea.Tick(33*time.Millisecond, func(_ time.Time) tea.Msg {
				return BatchRenderMsg{}
			}))
		}

	case BatchRenderMsg:
		m.renderPerf.batchRenderMsgs++
		m.renderPending = false
		m.refreshViewport()

	case ToolCallMsg:
		m.thinking = true
		m.flushText()
		m.pendingTool = msg.Name
		// Use display info if available, fall back to raw args
		displayLabel := msg.Name
		if msg.Title != "" {
			displayLabel = msg.Title
		}
		subtitlePart := ""
		if msg.Subtitle != "" {
			subtitlePart = " " + toolArgsStyle.Render(msg.Subtitle)
		} else {
			argsDisplay := formatToolArgs(msg.Args)
			if argsDisplay != "" {
				subtitlePart = " " + toolArgsStyle.Render(argsDisplay)
			}
		}
		m.lines = append(m.lines, textLine(fmt.Sprintf("  %s %s%s",
			toolIconRunning,
			toolNameStyle.Render(displayLabel),
			subtitlePart,
		)))
		m.refreshViewport()
		cmds = append(cmds, m.spinner.Tick)

	case ToolResultMsg:
		m.thinking = true
		m.pendingTool = ""
		if msg.Err != nil {
			// Replace the running icon with error icon on the last tool line
			m.replaceLastToolIcon(toolIconError)
			m.lines = append(m.lines, toolResultContentLine(msg.Name, "", msg.Err))
		} else {
			// Replace the running icon with success icon
			m.replaceLastToolIcon(toolIconSuccess)
			m.lines = append(m.lines, toolResultContentLine(msg.Name, sanitize(msg.Output), nil))
		}
		m.refreshViewport()
		cmds = append(cmds, m.spinner.Tick)

	case TokenUpdateMsg:
		m.totalTokens = msg.TotalTokens
		m.modelContextLimit = msg.ModelContextLimit
		m.invalidateSidebarCache()
		m.invalidateFooterCache()

	case AgentDoneMsg:
		m.thinking = false
		m.renderPending = false // cancel any pending batch render
		m.flushText()
		if msg.Err != nil {
			if msg.Err.Error() == "context canceled" {
				// User-initiated cancellation — show a clean message, not an error.
				m.lines = append(m.lines, textLine(lipgloss.NewStyle().Foreground(colorMuted).Render("⏹  Agent cancelled.")))
			} else {
				m.lines = append(m.lines, textLine(errorStyle.Render("Error: "+msg.Err.Error())))
			}
		}
		// Model info line with full-width divider (styled like "── ◇ model via provider 5s ──")
		if m.activeModel != "" {
			duration := time.Since(m.promptStartTime)
			var durationStr string
			if duration < time.Minute {
				durationStr = fmt.Sprintf("%.0fs", duration.Seconds())
			} else {
				durationStr = fmt.Sprintf("%.0fm%.0fs", duration.Minutes(), duration.Seconds()-float64(int(duration.Minutes()))*60)
			}
			modelInfoText := fmt.Sprintf("◇ %s via %s %s ", m.activeModel, m.activeProvider, durationStr)
			textW := lipgloss.Width(modelInfoText)
			contentW := m.contentWidth()
			leftMargin := 4
			rightMargin := 4
			fillW := contentW - leftMargin - rightMargin - textW
			if fillW < 0 {
				fillW = 0
			}
			line := strings.Repeat(" ", leftMargin) + lipgloss.NewStyle().Foreground(colorPrimary).Render(
				modelInfoText+strings.Repeat("─", fillW))
			m.lines = append(m.lines, textLine(""))
			m.lines = append(m.lines, textLine(line))
			m.lines = append(m.lines, textLine(""))
		}
		m.lines = append(m.lines, textLine(""))
		m.agentDone = true
		m.textarea.Focus()
		if m.ready {
			m.contentDirty = true
			m.viewport.SetHeight(m.calcViewportHeight(true))
			m.viewport.SetContent(m.renderViewportContent())
			m.viewport.GotoBottom()
		}

		if len(m.pendingPrompts) > 0 {
			first := m.pendingPrompts[0]
			m.pendingPrompts = m.pendingPrompts[1:]
			select {
			case pendingPromptCh <- first:
			default:
			}
		}

	case ToolApprovalRequestMsg:
		if m.approvalPending {
			// Already showing a dialog — queue this request instead of overwriting.
			m.approvalQueue = append(m.approvalQueue, msg)
		} else {
			m.showApproval(msg)
		}

	case SubagentStartMsg:
		m.thinking = true
		m.flushText()
		typeLabel := msg.Type
		if typeLabel == "" {
			typeLabel = "explore"
		}
		m.pendingTool = "subagent"
		m.subagentActive = true
		m.subagentName = msg.Name
		m.subagentType = typeLabel
		m.subagentStepCount = 0
		m.subagentLastTool = ""
		m.subagentProgress = nil
		m.subagentTokens = 0
		m.lines = append(m.lines, textLine(fmt.Sprintf("  %s %s %s",
			subagentLabelStyle.Render("🤖 Subagent:"),
			toolNameStyle.Render(msg.Name),
			toolArgsStyle.Render("("+typeLabel+")"),
		)))
		m.refreshViewport()
		cmds = append(cmds, m.spinner.Tick)

	case SubagentProgressMsg:
		if m.subagentActive && msg.Event == "tool_call" {
			m.subagentStepCount++
			args := formatToolArgs(msg.Detail)
			if args != "" {
				m.subagentLastTool = msg.ToolName + " " + args
			} else {
				m.subagentLastTool = msg.ToolName
			}
			line := fmt.Sprintf("%s %s", toolNameStyle.Render(msg.ToolName), toolArgsStyle.Render(args))
			m.subagentProgress = append(m.subagentProgress, line)
			m.refreshViewport()
		}

	case SubagentTokenUpdateMsg:
		m.subagentTokens = msg.TotalTokens
		m.invalidateSidebarCache()
		m.refreshViewport()

	case SubagentDoneMsg:
		m.pendingTool = ""
		m.subagentActive = false
		m.subagentLastTool = ""
		m.subagentProgress = nil
		m.subagentTokens = 0
		if msg.Err != nil {
			m.lines = append(m.lines, textLine(fmt.Sprintf("   %s %s",
				toolErrorStyle.Render("✗ Subagent Error:"),
				toolResultStyle.Render(truncate(sanitize(msg.Err.Error()), maxToolOutputLen)))))
		} else {
			m.lines = append(m.lines, textLine(fmt.Sprintf("   %s",
				toolSuccessStyle.Render("✓ Subagent Done"))))
		}
		m.refreshViewport()
		cmds = append(cmds, m.spinner.Tick)

	case CompactDoneMsg:
		m.thinking = false
		if msg.Err != nil {
			m.lines = append(m.lines, textLine(fmt.Sprintf("  %s %s",
				toolErrorStyle.Render("✗ Compact Error:"),
				toolResultStyle.Render(msg.Err.Error()))))
		} else {
			m.lines = append(m.lines, textLine(fmt.Sprintf("  %s Tokens: %d → %d",
				toolSuccessStyle.Render("✓ Context compacted."),
				msg.OldTokens, msg.NewTokens)))
		}
		m.lines = append(m.lines, textLine(""))
		m.agentDone = true
		m.textarea.Focus()
		m.refreshViewport()

	case BgTaskDoneMsg:
		m.invalidateFooterCache()  // bgRunning count changes
		m.invalidateSidebarCache() // sidebar shows bg count
		if msg.Status == "running" {
			m.bgRunning++
		} else {
			if m.bgRunning > 0 {
				m.bgRunning--
			}
			statusIcon := toolSuccessStyle.Render("✓")
			if msg.Status == "failed" || msg.Status == "timeout" {
				statusIcon = toolErrorStyle.Render("✗")
			}
			m.lines = append(m.lines, textLine(fmt.Sprintf("  %s Background task %s (%s): %s",
				statusIcon,
				toolNameStyle.Render(msg.TaskID),
				msg.Status,
				toolArgsStyle.Render(truncate(sanitize(msg.Command), 60)))))
		}
		m.refreshViewport()

	// --- Team messages ---
	case team.SetTeamManagerMsg:
		m.teamState.Manager = msg.Manager
		m.invalidateSidebarCache()
		m.invalidateFooterCache()

	case team.TeammateSpawnedMsg:
		m.teamState.RefreshTeammates()
		m.invalidateSidebarCache()
		m.invalidateFooterCache()
		// Auto-show panel when first teammate spawns.
		if !m.teamState.PanelVisible {
			m.teamState.PanelVisible = true
		}
		nameStyled := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(msg.Color)).Render("@" + msg.Name)
		spawnLine := fmt.Sprintf("  %s Teammate %s spawned", toolSuccessStyle.Render("👥"), nameStyled)
		promptLine := fmt.Sprintf("    %s", lipgloss.NewStyle().Faint(true).Render(truncate(msg.Prompt, 80)))
		// Add to leader view.
		m.lines = append(m.lines, textLine(spawnLine), textLine(promptLine))
		// Initialize per-teammate display lines.
		m.teamState.AppendTeammateLine(msg.AgentID, spawnLine, promptLine, "")
		m.refreshViewport()

	case team.TeammateStatusMsg:
		m.teamState.RefreshTeammates()
		m.invalidateSidebarCache()
		m.invalidateFooterCache()
		nameStyled := toolNameStyle.Render(msg.AgentID)
		icon := statusIcon(msg.Status)
		switch {
		case msg.Status == team.StatusRunning:
			m.lines = append(m.lines, textLine(fmt.Sprintf("  %s %s is working...", icon, nameStyled)))
		case msg.Status.IsTerminal():
			errInfo := ""
			if msg.Error != "" {
				errInfo = ": " + msg.Error
			}
			m.lines = append(m.lines, textLine(fmt.Sprintf("  %s %s %s%s", icon, nameStyled, string(msg.Status), errInfo)))
		case msg.Status == team.StatusIdle:
			m.lines = append(m.lines, textLine(fmt.Sprintf("  %s %s idle, waiting for messages", icon, nameStyled)))
		}
		m.refreshViewport()

	case team.TeammateProgressMsg:
		// Update cached state, refresh panel if visible
		m.teamState.RefreshTeammates()
		m.invalidateSidebarCache()
		m.invalidateFooterCache()
		if m.teamState.PanelVisible {
			m.refreshViewport()
		}

	case team.TeammateTokenUpdateMsg:
		if m.teammateTokens == nil {
			m.teammateTokens = make(map[string]int64)
		}
		m.teammateTokens[msg.AgentID] = msg.TotalTokens
		if m.teamState.ViewingAgent == msg.AgentID {
			// If we're currently viewing this teammate, refresh the status bar
			m.invalidateSidebarCache()
			m.invalidateFooterCache()
			m.refreshViewport()
		}

	case team.TeammateMessageMsg:
		switch msg.Role {
		case "user":
			// Render incoming message like "👤 From @leader: message text"
			fromLabel := "user"
			if msg.From != "" {
				fromLabel = "@" + msg.From
			}
			line := fmt.Sprintf("%s %s",
				userLabelStyle.Render(fmt.Sprintf("📨 %s:", fromLabel)),
				sanitize(msg.Content))
			m.teamState.FlushTeammateText(msg.AgentID)
			m.teamState.AppendTeammateLine(msg.AgentID, line)
		case "tool_call":
			argsDisplay := formatToolArgs(msg.ToolArgs)
			line := fmt.Sprintf("%s %s %s",
				toolLabelStyle.Render("🔧 Tool:"),
				toolNameStyle.Render(msg.ToolName),
				toolArgsStyle.Render(argsDisplay))
			m.teamState.FlushTeammateText(msg.AgentID)
			m.teamState.AppendTeammateLine(msg.AgentID, line)
		case "tool_result":
			if msg.ToolErr != "" {
				m.teamState.AppendTeammateLine(msg.AgentID,
					fmt.Sprintf("   %s %s",
						toolErrorStyle.Render("✗ Error:"),
						toolResultStyle.Render(truncate(sanitize(msg.ToolErr), maxToolOutputLen))))
			} else {
				m.teamState.AppendTeammateLine(msg.AgentID,
					formatToolResult(msg.ToolName, sanitize(msg.Content), m.contentWidth(), false, nil)...)
			}
		case "assistant":
			m.teamState.FlushTeammateText(msg.AgentID)
			// Render assistant label with teammate name, like "🤖 @backend:"
			state := m.teamState.Manager.GetTeammateState(msg.AgentID)
			label := "🤖 Assistant:"
			if state != nil {
				nameStyled := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(state.Identity.Color)).Render("@" + state.Identity.AgentName)
				label = fmt.Sprintf("🤖 %s:", nameStyled)
			}
			rendered := sanitize(msg.Content)
			if m.mdRenderer != nil {
				if md, err := m.mdRenderer.Render(msg.Content); err == nil {
					rendered = md
				}
			}
			m.teamState.AppendTeammateLine(msg.AgentID, assistantLabelStyle.Render(label))
			m.teamState.AppendTeammateLine(msg.AgentID, rendered)
		}

		// If viewing this teammate, update the viewport with latest lines.
		if m.teamState.ViewingAgent == msg.AgentID {
			state := m.teamState.Manager.GetTeammateState(msg.AgentID)
			if state != nil {
				m.lines = nil
				m.lines = append(m.lines, textLine(RenderTeammateViewHeader(state.Identity)))
				m.lines = append(m.lines, textLine(""))
				m.lines = append(m.lines, toContentLines(m.teamState.GetTeammateDisplayLines(msg.AgentID))...)
			}
			m.refreshViewport()
		}
		// Show brief notification in leader view for assistant messages.
		if m.teamState.ViewingAgent == "" && msg.Role == "assistant" {
			preview := truncate(msg.Content, 60)
			m.lines = append(m.lines, textLine(fmt.Sprintf("  💬 %s: %s",
				toolNameStyle.Render(msg.AgentID), lipgloss.NewStyle().Faint(true).Render(preview))))
			m.refreshViewport()
		}
	// --- End team messages ---

	case PlanApprovalMsg:
		m.planReviewActive = true
		m.invalidateFooterCache()
		m.planReviewTitle = msg.PlanPath
		m.planRejectInput = false
		m.planReviewSelected = 0
		m.textarea.Blur()
		m.refreshViewport()

	case AskUserQuestionMsg:
		m.askUserActive = true
		m.invalidateFooterCache()
		m.askUserQuestion = msg.Question
		m.askUserOptions = msg.Options
		m.askUserSelected = 0
		m.textarea.SetValue("")
		if len(msg.Options) == 0 {
			m.textarea.Focus()
			m.textarea.Placeholder = "Type your answer..."
		} else {
			m.textarea.Blur()
			m.textarea.Placeholder = "Or type a custom answer..."
		}
		m.refreshViewport()

	case ExitTimeoutMsg:
		// Clear exit confirmation after 5s if still pending
		if m.exitPending && time.Since(m.exitWarningTime) >= 5*time.Second {
			m.exitPending = false
		}

	case tea.InterruptMsg:
		// Handle ctrl+c from non-TTY / signal handler path.
		if m.exitPending || m.cancelPending {
			return m, tea.Quit
		}
		// If agent is running, show cancel dialog instead of quit dialog.
		if m.thinking && !m.agentDone {
			m.requestCancelAgent()
			return m, tea.Batch(cmds...)
		}
		m.exitPending = true
		m.exitSelected = 1
		m.exitWarningTime = time.Now()
		quitButtons := buttonGroup([]buttonOpts{
			{text: " Yes ", selected: false},
			{text: " No ", selected: true},
		})
		m.lines = append(m.lines, textLine(fmt.Sprintf("  %s  %s",
			lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("Quit?"),
			quitButtons)))
		m.refreshViewport()
		return m, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
			return ExitTimeoutMsg{}
		})

	}

	// Forward non-key/non-mouse messages (e.g. list.FilterMatchesMsg) to active pickers
	if _, isKey := msg.(tea.KeyPressMsg); !isKey {
		if _, isMouse := msg.(tea.MouseMsg); !isMouse {
			switch {
			case m.managingModels:
				var cmd tea.Cmd
				m.manageModelsPicker, cmd = m.manageModelsPicker.Update(msg)
				cmds = append(cmds, cmd)
			case m.pickingModel:
				var cmd tea.Cmd
				m.modelPicker, cmd = m.modelPicker.Update(msg)
				cmds = append(cmds, cmd)
			case m.pickingSession:
				var cmd tea.Cmd
				m.sessionPicker, cmd = m.sessionPicker.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
	}

	if m.ready && m.mode == ModeAgent {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}
