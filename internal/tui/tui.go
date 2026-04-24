package tui

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/team"
	"github.com/cnjack/jcode/internal/tools"
)

// --- Model ---

type Mode int

const (
	ModeAgent Mode = iota
)

const (
	sidebarWidth       = 36
	minWidthForSidebar = 90
)

type Model struct {
	mode      Mode
	agentDone bool

	lines       []string
	currentText *strings.Builder

	viewport viewport.Model
	ready    bool

	spinner  spinner.Model
	thinking bool

	width  int
	height int

	mdRenderer  *glamour.TermRenderer
	pendingTool string
	textarea    textarea.Model
	mcpStatuses []MCPStatusItem

	sshStep int
	sshAddr string
	sshPath string
	dirList list.Model

	modelPicker  list.Model
	pickingModel bool

	settingMenu    list.Model
	showingSetting bool

	sshAliasPicker  list.Model
	pickingSSHAlias bool

	sshSavePrompt bool
	sshSaveAddr   string
	sshSavePath   string

	history      []string
	historyIndex int

	sessionPicker  list.Model
	pickingSession bool

	// Channel panel state
	channelMenu    list.Model
	showingChannel bool
	channelStates  map[string]string // channelID → state ("none", "disabled", "enabled")

	showingHelp bool // keyboard shortcuts help panel
	helpScroll  int  // scroll offset for help panel

	version string // version string displayed in bottom bar

	agentsMdFound bool

	pwd string

	activeProvider string
	activeModel    string
	textareaLines  int

	pasteStore *PasteStore

	todoStore *tools.TodoStore

	totalTokens       int64
	modelContextLimit int

	promptStartTime time.Time // when user submitted the current prompt

	pendingPrompts []string

	approvalPending     bool
	approvalToolName    string
	approvalToolArgs    string
	approvalRespChan    chan ToolApprovalResponse
	approvalIsExternal  bool   // Whether this is an external path access
	approvalWorkerName  string // Non-empty for teammate approval
	approvalWorkerColor string // Teammate color
	approvalMode        ApprovalMode
	approvalSelected    int // 0=Approve, 1=ApproveAll, 2=Reject

	envLabel  string
	agentMode AgentMode
	bgRunning int // count of running background tasks

	// Plan review state
	planReviewActive   bool
	planReviewTitle    string
	planRejectInput    bool // true when prompting for rejection feedback
	planReviewSelected int  // 0=Approve, 1=Reject, 2=Dismiss

	// Ask user state
	askUserActive   bool
	askUserQuestion string
	askUserOptions  []string
	askUserSelected int // currently highlighted option index

	// Skill slash commands from skill loader
	skillSlashCommands []SkillSlashInfo

	// Subagent progress tracking
	subagentActive    bool
	subagentName      string
	subagentType      string
	subagentStepCount int      // total tool calls so far
	subagentLastTool  string   // last tool name + args summary
	subagentProgress  []string // tool call progress lines for box display
	subagentTokens    int64    // cumulative tokens used by current subagent

	// Exit / cancel confirmation
	exitPending     bool      // true when quit dialog is showing
	exitWarningTime time.Time // when the warning was shown
	exitSelected    int       // 0=Yes, 1=No (default No for safety)

	// Cancel-agent confirmation
	cancelPending  bool // true when cancel-agent dialog is showing
	cancelSelected int  // 0=Cancel, 1=Wait

	// OnApprovalModeChange is called when the user toggles approval mode via Ctrl+A.
	// It directly updates the backend ApprovalState atomically, bypassing the event loop.
	OnApprovalModeChange func(enabled bool)

	// Command autocomplete suggestions
	cmdSuggestionActive bool
	cmdSuggestionIndex  int
	cmdSuggestions      []commandSuggestion

	// Mouse text selection state

	// Team state
	teamState      TeamViewState
	teammateTokens map[string]int64 // per-teammate token usage for status bar
	// teamLeaderLines stores the leader's viewport content when switching to teammate view
	teamLeaderLines []string
	teamLeaderText  string

	// ─── Sidebar state ───
	showSidebar         bool // whether sidebar is currently visible
	sidebarScrollOffset int  // scroll offset for todo list in sidebar
	sidebarComp         *SidebarComponent
}

// dirItem implements list.Item
type dirItem struct {
	title       string
	name        string
	desc        string
	isDirectory bool
	isSelectBtn bool
}

func (i dirItem) Title() string       { return i.title }
func (i dirItem) Description() string { return i.desc }
func (i dirItem) FilterValue() string { return i.title }

type modelItem struct {
	provider  string
	model     string
	title     string
	desc      string
	isCurrent bool // currently active model
	isAction  bool // action item (e.g. "Add New Model")
}

func (i modelItem) Title() string       { return i.title }
func (i modelItem) Description() string { return i.desc }
func (i modelItem) FilterValue() string { return i.provider + "/" + i.model + " " + i.title }

// settingItem is used for the /setting menu
type settingItem struct {
	title string
	desc  string
	key   string // action key
}

func (i settingItem) Title() string       { return i.title }
func (i settingItem) Description() string { return i.desc }
func (i settingItem) FilterValue() string { return i.title }

// sessionListItem implements list.Item for session picking.
type sessionListItem struct {
	meta session.SessionMeta
}

func (i sessionListItem) Title() string {
	ts := i.meta.StartTime
	if len(ts) >= 16 {
		ts = ts[:16]
	}
	if i.meta.Title != "" {
		return fmt.Sprintf("%s  %s", ts, i.meta.Title)
	}
	return fmt.Sprintf("%s  %s / %s", ts, i.meta.Provider, i.meta.Model)
}
func (i sessionListItem) Description() string {
	if i.meta.Title != "" {
		return fmt.Sprintf("%s / %s  %s", i.meta.Provider, i.meta.Model, i.meta.UUID[:8])
	}
	return i.meta.UUID
}
func (i sessionListItem) FilterValue() string { return i.meta.StartTime + i.meta.UUID }

// sshAliasItem for the SSH alias picker
type sshAliasItem struct {
	title string
	desc  string
	addr  string
	path  string
	isNew bool // "Connect new SSH" option
}

func (i sshAliasItem) Title() string       { return i.title }
func (i sshAliasItem) Description() string { return i.desc }
func (i sshAliasItem) FilterValue() string { return i.title }

// channelItem for the channel management picker
type channelItem struct {
	title string
	desc  string
	key   string // channel ID (e.g. "wechat")
}

func (i channelItem) Title() string       { return i.title }
func (i channelItem) Description() string { return i.desc }
func (i channelItem) FilterValue() string { return i.title }

func newTextarea() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Type your prompt here..."
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = defaultMaxTextareaLines
	ta.Prompt = "> "
	st := ta.Styles()
	st.Focused.CursorLine = lipgloss.NewStyle()
	st.Cursor.Shape = tea.CursorBlock
	st.Cursor.Color = colorPrimary
	st.Focused.Prompt = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(colorDimText)
	st.Blurred.Placeholder = lipgloss.NewStyle().Foreground(colorDimText)
	ta.SetStyles(st)
	ta.Focus()
	return ta
}

func NewModel(hasPrompt bool, pwd string, todoStore *tools.TodoStore) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	md, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(100),
	)

	mode := ModeAgent
	thinking := false
	var initialLines []string
	if hasPrompt {
		thinking = true
	} else {
		initialLines = []string{
			lipgloss.NewStyle().Foreground(colorMuted).Render("Welcome to JCODE. How can I help you today?"),
			"",
		}
	}

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Select Remote Directory"
	l.SetShowHelp(false)

	modelDel := list.NewDefaultDelegate()
	modelDel.SetSpacing(0)
	ml := list.New([]list.Item{}, modelDel, 0, 0)
	ml.Title = "Select Model"
	ml.SetShowHelp(false)

	// Setting menu list
	settingDel := list.NewDefaultDelegate()
	settingDel.SetSpacing(0)
	sl := list.New([]list.Item{}, settingDel, 0, 0)
	sl.Title = "Settings"
	sl.SetShowHelp(false)

	// SSH alias picker list
	sshAliasDel := list.NewDefaultDelegate()
	sshAliasDel.SetSpacing(0)
	sal := list.New([]list.Item{}, sshAliasDel, 0, 0)
	sal.Title = "SSH Connections"
	sal.SetShowHelp(false)

	// Session picker list
	sessionDel := list.NewDefaultDelegate()
	sessionDel.SetSpacing(0)
	sesl := list.New([]list.Item{}, sessionDel, 0, 0)
	sesl.Title = "Sessions"
	sesl.SetShowHelp(false)

	// Channel menu list
	channelDel := list.NewDefaultDelegate()
	channelDel.SetSpacing(0)
	chl := list.New([]list.Item{}, channelDel, 0, 0)
	chl.Title = "Channels"
	chl.SetShowHelp(false)

	m := Model{
		mode:           mode,
		spinner:        s,
		thinking:       thinking,
		mdRenderer:     md,
		textarea:       newTextarea(),
		textareaLines:  1,
		currentText:    &strings.Builder{},
		sidebarComp:    NewSidebarComponent(),
		dirList:        l,
		modelPicker:    ml,
		settingMenu:    sl,
		sshAliasPicker: sal,
		sessionPicker:  sesl,
		channelMenu:    chl,
		channelStates:  make(map[string]string),
		pwd:            pwd,
		history:        loadHistory(),
		todoStore:      todoStore,
		pasteStore:     NewPasteStore(),
		lines:          initialLines,
		envLabel:       "Local",
		approvalMode:   ModeManual, // Default to manual approval mode
	}
	m.historyIndex = len(m.history)

	if cfg, err := config.LoadConfig(); err == nil {
		m.activeProvider, m.activeModel = cfg.GetProviderModel()
		if cfg.AutoApprove {
			m.approvalMode = ModeAuto
		}
	}

	return m
}

func loadHistory() []string {
	hPath, err := config.HistoryFilePath()
	if err != nil {
		return nil
	}
	content, err := os.ReadFile(hPath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(content), "\n")
	var history []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			history = append(history, l)
		}
	}
	return history
}

func appendHistory(prompt string) {
	hPath, err := config.HistoryFilePath()
	if err != nil {
		return
	}
	f, err := os.OpenFile(hPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(prompt + "\n")
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		textarea.Blink,
		// Force BubbleTea to use GraphemeWidth mode and enable Unicode Core
		// mode (2027) on the terminal. This synchronizes the renderer's width
		// calculation with the terminal, fixing emoji border alignment.
		func() tea.Msg {
			return tea.ModeReportMsg{
				Mode:  ansi.ModeUnicodeCore,
				Value: ansi.ModeReset,
			}
		},
	)
}

// cancelAgent shows a confirmation dialog to cancel the running agent.
// The actual cancellation happens when the user confirms.
func (m *Model) requestCancelAgent() {
	if !m.thinking || m.agentDone {
		return
	}
	m.cancelPending = true
	m.cancelSelected = 0 // default to "Cancel"
	m.refreshViewport()
}

// confirmCancelAgent executes the agent cancellation after user confirms.
func (m *Model) confirmCancelAgent() {
	m.cancelPending = false
	select {
	case cancelAgentCh <- struct{}{}:
	default:
	}
}

func (m Model) inputActive() bool {
	return (m.mode == ModeAgent || m.sshStep > 0 || m.sshSavePrompt) && !m.pickingModel && !m.showingSetting && !m.showingHelp && !m.pickingSSHAlias && !m.pickingSession && !m.approvalPending && !m.planReviewActive && !m.askUserActive
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { //nolint:funlen
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.PasteMsg:
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
					m.approvalPending = false
					if m.approvalRespChan != nil {
						m.approvalRespChan <- ToolApprovalResponse{Approved: true, Mode: ModeManual}
					}
				case 1: // Approve all
					m.approvalPending = false
					m.approvalMode = ModeAuto
					if m.approvalRespChan != nil {
						m.approvalRespChan <- ToolApprovalResponse{Approved: true, Mode: ModeAuto}
					}
					if m.OnApprovalModeChange != nil {
						m.OnApprovalModeChange(true)
					}
				case 2: // Reject
					m.approvalPending = false
					if m.approvalRespChan != nil {
						m.approvalRespChan <- ToolApprovalResponse{Approved: false, Mode: m.approvalMode}
					}
					m.lines = append(m.lines, fmt.Sprintf("   %s %s — user denied this operation",
						toolErrorStyle.Render("⚠ Rejected:"),
						toolNameStyle.Render(m.approvalToolName)))
				}
				m.textarea.Focus()
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			case "y", "Y":
				// Event: ApproveOnce - approve current only, stay in MANUAL mode
				m.approvalPending = false
				if m.approvalRespChan != nil {
					m.approvalRespChan <- ToolApprovalResponse{Approved: true, Mode: ModeManual}
				}
				m.textarea.Focus()
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			case "a", "A":
				m.approvalPending = false
				m.approvalMode = ModeAuto
				if m.approvalRespChan != nil {
					m.approvalRespChan <- ToolApprovalResponse{Approved: true, Mode: ModeAuto}
				}
				if m.OnApprovalModeChange != nil {
					m.OnApprovalModeChange(true)
				}
				m.textarea.Focus()
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			case "n", "N", "esc":
				// Event: Reject - deny the operation
				m.approvalPending = false
				if m.approvalRespChan != nil {
					m.approvalRespChan <- ToolApprovalResponse{Approved: false, Mode: m.approvalMode}
				}
				// Show rejection notice in chat view
				m.lines = append(m.lines, fmt.Sprintf("   %s %s — user denied this operation",
					toolErrorStyle.Render("⚠ Rejected:"),
					toolNameStyle.Render(m.approvalToolName)))
				m.textarea.Focus()
				m.refreshViewport()
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
					m.lines = append(m.lines, fmt.Sprintf("   %s Plan rejected%s",
						toolErrorStyle.Render("✗"),
						func() string {
							if feedback != "" {
								return ": " + feedback
							}
							return ""
						}()))
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
				m.lines = append(m.lines, fmt.Sprintf("   %s Plan approved: %s",
					toolSuccessStyle.Render("✓"),
					toolNameStyle.Render(m.planReviewTitle)))
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
					m.lines = append(m.lines, fmt.Sprintf("   %s Plan approved: %s",
						toolSuccessStyle.Render("✓"),
						toolNameStyle.Render(m.planReviewTitle)))
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
					m.lines = append(m.lines, fmt.Sprintf("   %s Plan dismissed",
						toolErrorStyle.Render("✗")))
					m.textarea.Focus()
					m.refreshViewport()
				}
				return m, tea.Batch(cmds...)
			case "esc":
				m.planReviewActive = false
				planResponseCh <- PlanResponse{Approved: false, Feedback: ""}
				m.lines = append(m.lines, fmt.Sprintf("   %s Plan dismissed",
					toolErrorStyle.Render("✗")))
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
				m.lines = append(m.lines, fmt.Sprintf("   %s %s",
					userLabelStyle.Render("💬 Answer:"), answer))
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
				m.lines = append(m.lines, fmt.Sprintf("   %s Question dismissed",
					toolErrorStyle.Render("✗")))
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
					m.lines = append(m.lines, toolLabelStyle.Render("📂 Loading session..."))
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
						m.lines = append(m.lines, toolLabelStyle.Render("⚙ Settings:")+" Please edit "+config.ConfigPath())
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

		// F1 or ? to toggle help panel (when not typing)
		if msg.String() == "f1" {
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
						m.lines = append(m.lines, toolLabelStyle.Render("🔗 SSH Setup"))
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

		if m.pickingModel {
			switch msg.String() {
			case "enter":
				selected := m.modelPicker.SelectedItem()
				if selected != nil {
					selItem := selected.(modelItem)
					if selItem.isAction {
						// "Add New Model" — launch setup wizard
						m.pickingModel = false
						m.textarea.Focus()
						cmds = append(cmds, func() tea.Msg {
							return AddModelMsg{}
						})
						return m, tea.Batch(cmds...)
					}
					if selItem.isCurrent {
						// Already active — just close picker
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
						select {
						case configCh <- cfg:
						default:
						}
					}
					m.pickingModel = false
					m.lines = append(m.lines, fmt.Sprintf("  %s Switched to %s",
						toolSuccessStyle.Render("✓"),
						toolNameStyle.Render(selItem.provider+"/"+selItem.model)))
					m.textarea.Focus()
					m.refreshViewport()
					return m, tea.Batch(cmds...)
				}
			case "ctrl+c", "esc":
				m.pickingModel = false
				m.textarea.Focus()
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			}
			var cmd tea.Cmd
			m.modelPicker, cmd = m.modelPicker.Update(msg)
			cmds = append(cmds, cmd)
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
				m.lines = append(m.lines, toolLabelStyle.Render("🔗 SSH:")+" Cancelled.")
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
			case "ctrl+p":
				// Toggle agent mode: Agent <-> Plan
				if m.agentMode == ModeNormal {
					m.agentMode = ModePlanning
				} else {
					m.agentMode = ModeNormal
				}
				// Notify main goroutine to rebuild agent with different prompt/tools.
				select {
				case planModeCh <- m.agentMode:
				default:
				}
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			case "ctrl+a":
				if m.approvalMode == ModeManual {
					m.approvalMode = ModeAuto
				} else {
					m.approvalMode = ModeManual
				}
				if m.OnApprovalModeChange != nil {
					m.OnApprovalModeChange(m.approvalMode == ModeAuto)
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
					m.textareaLines = recalcLines(m.textarea.Value(), calcMaxTextareaLines(m.height))
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
			case "enter":
				// If command suggestion is active, accept it instead of submitting
				if m.cmdSuggestionActive && len(m.cmdSuggestions) > 0 && m.cmdSuggestionIndex < len(m.cmdSuggestions) {
					selected := m.cmdSuggestions[m.cmdSuggestionIndex]
					m.textarea.SetValue(selected.cmd)
					m.textarea.CursorEnd()
					m.cmdSuggestionActive = false
					m.cmdSuggestions = nil
					m.cmdSuggestionIndex = 0
					m.textareaLines = recalcLines(m.textarea.Value(), calcMaxTextareaLines(m.height))
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
						m.lines = append(m.lines, userPromptStyle.Render("> "+prompt))
						m.refreshViewport()
						return m, tea.Batch(cmds...)
					}

					if prompt == "/setting" {
						return m.handleSettingInput(cmds)
					}
					if prompt == "/model" {
						return m.handleModelInput(cmds)
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

					if prompt == "/channel" {
						return m.handleChannelInput(cmds)
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
						if strings.Contains(m.lines[0], "Welcome to JCODE") {
							m.lines = nil
						}
					}

					if !m.agentDone && m.thinking {
						m.pendingPrompts = append(m.pendingPrompts, actualPrompt)
						// prompt already contains compact references from paste-time
						m.lines = append(m.lines, userPromptStyle.Render("> "+prompt+" (queued)"))
						if m.ready {
							m.viewport.SetHeight(m.calcViewportHeight(true))
							m.viewport.SetContent(m.renderContent())
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
					m.lines = append(m.lines, "")
					m.lines = append(m.lines, userPromptStyle.Render(modePrefix+" "+prompt))
					if m.ready {
						m.viewport.SetHeight(m.calcViewportHeight(false))
						m.viewport.SetContent(m.renderContent())
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
				m.textareaLines = recalcLines(m.textarea.Value(), calcMaxTextareaLines(m.height))
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
					m.textareaLines = recalcLines(m.textarea.Value(), calcMaxTextareaLines(m.height))
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
				m.textareaLines = recalcLines(m.textarea.Value(), calcMaxTextareaLines(m.height))
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
			case "ctrl+y":
				// Copy last assistant message to clipboard
				text := m.getLastAssistantText()
				if text != "" {
					cmds = append(cmds, tea.SetClipboard(text))
				}
				return m, tea.Batch(cmds...)
			case "escape":
				// Team: exit teammate view, return to leader
				if m.teamState.ViewMode == TeamViewTeammate {
					m.exitTeammateView()
					m.refreshViewport()
					return m, tea.Batch(cmds...)
				}
			}
			// Forward other keys to textarea
			var cmd tea.Cmd
			m.textarea, cmd = m.textarea.Update(msg)
			cmds = append(cmds, cmd)
			m.textareaLines = recalcLines(m.textarea.Value(), calcMaxTextareaLines(m.height))
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
		m.textareaLines = recalcLines(m.textarea.Value(), newMaxHeight)
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
		m.viewport.SetContent(m.renderContent())

	case spinner.TickMsg:
		if m.thinking {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case PromptSubmitMsg:
		promptCh <- msg.Prompt

	case SSHConnectMsg:
		sshCh <- msg

	case SSHListDirReqMsg:
		sshCh <- msg

	case SSHCancelMsg:
		m.envLabel = "Local"
		sshCh <- msg

	case ConfigUpdatedMsg:
		m.activeProvider = msg.Provider
		m.activeModel = msg.Model
		if msg.Message != "" {
			m.lines = append(m.lines, msg.Message)
			m.refreshViewport()
		}

	case MCPStatusMsg:
		m.mcpStatuses = msg.Statuses
		m.refreshViewport()

	case ChannelStateMsg:
		m.channelStates[msg.ChannelID] = msg.State
		if msg.Message != "" {
			m.lines = append(m.lines, toolLabelStyle.Render("📡 Channel:")+" "+msg.Message)
			m.refreshViewport()
		}

	case ChannelQRCodeMsg:
		m.lines = append(m.lines, toolLabelStyle.Render("📡 "+msg.Message))
		if msg.QRCodeContent != "" {
			qrLines := renderQRCode(msg.QRCodeContent)
			m.lines = append(m.lines, qrLines...)
		}
		m.refreshViewport()

	case ChannelInboundMsg:
		m.lines = append(m.lines, lipgloss.NewStyle().Foreground(colorSecondary).
			Render(fmt.Sprintf("📱 [WeChat] %s", msg.Text)))
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
			m.textarea.SetValue(msg.Val)
			m.textarea.CursorEnd()
			m.textareaLines = recalcLines(m.textarea.Value(), calcMaxTextareaLines(m.height))
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

				if len(m.lines) > 0 && strings.Contains(m.lines[0], "Welcome to JCODE") {
					m.lines = nil
				}

				if !m.agentDone && m.thinking {
					m.pendingPrompts = append(m.pendingPrompts, actualPrompt)
					// prompt already contains compact references from paste-time
					m.lines = append(m.lines, userPromptStyle.Render("> "+prompt+" (queued)"))
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
				m.lines = append(m.lines, "")
				m.lines = append(m.lines, userPromptStyle.Render(modePrefix+" "+prompt))
				if m.ready {
					m.viewport.SetHeight(m.calcViewportHeight(false))
					m.viewport.SetContent(m.renderContent())
					m.viewport.GotoBottom()
				}
				cmds = append(cmds, func() tea.Msg {
					return PromptSubmitMsg{Prompt: actualPrompt}
				})
				cmds = append(cmds, m.spinner.Tick)
			}
		case "cancel":
			// Clear the input area
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
		m.lines = append(m.lines, toolLabelStyle.Render("📂 Session resumed: ")+msg.UUID)
		m.lines = append(m.lines, "")
		for _, e := range msg.Entries {
			switch e.Type {
			case string(session.EntryUser):
				m.lines = append(m.lines, "")
				displayContent := m.pasteStore.StoreAndFormat(NormalizeLineEndings(e.Content))
				m.lines = append(m.lines, userPromptStyle.Render("> "+displayContent))
			case string(session.EntryAssistant):
				if e.Content != "" {
					rendered := e.Content
					if m.mdRenderer != nil {
						if md, err := m.mdRenderer.Render(e.Content); err == nil {
							rendered = md
						}
					}
					m.lines = append(m.lines, "")
					m.lines = append(m.lines, rendered)
				}
			case string(session.EntryToolCall):
				// Tool calls always show running icon (they don't have error status yet)
				m.lines = append(m.lines, fmt.Sprintf("  %s %s %s",
					toolIconRunning,
					toolNameStyle.Render(e.Name),
					toolArgsStyle.Render(truncate(sanitize(e.Args), 100)),
				))
			case string(session.EntryToolResult):
				if e.Error != "" {
					m.lines = append(m.lines, formatToolResultBody(e.Name, "", fmt.Errorf("%s", e.Error), m.contentWidth())...)
				} else {
					m.lines = append(m.lines, formatToolResult(e.Name, e.Output, m.contentWidth())...)
				}
			case string(session.EntrySubagentStart):
				typeLabel := e.SubagentType
				if typeLabel == "" {
					typeLabel = "explore"
				}
				m.lines = append(m.lines, fmt.Sprintf("  %s %s %s",
					subagentLabelStyle.Render("🤖 Subagent:"),
					toolNameStyle.Render(e.SubagentName),
					toolArgsStyle.Render("("+typeLabel+")"),
				))
			case string(session.EntrySubagentResult):
				if e.Error != "" {
					m.lines = append(m.lines, fmt.Sprintf("   %s %s",
						toolErrorStyle.Render("✗ Subagent Error:"),
						toolResultStyle.Render(truncate(sanitize(e.Error), maxToolOutputLen))))
				} else {
					m.lines = append(m.lines, fmt.Sprintf("   %s %s",
						toolSuccessStyle.Render("✓ Subagent Done:"),
						toolResultStyle.Render(truncate(sanitize(e.Output), maxToolOutputLen))))
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
				m.lines = append(m.lines, fmt.Sprintf("  %s Plan %s: %s",
					toolLabelStyle.Render(statusIcon),
					toolNameStyle.Render(e.PlanStatus),
					toolArgsStyle.Render(e.PlanTitle)))
			case string(session.EntryTodoSnapshot):
				if len(e.Todos) > 0 {
					m.lines = append(m.lines, toolLabelStyle.Render("  📋 Todo List:"))
					for _, t := range e.Todos {
						statusIcon := "⬜"
						if t.Status == "completed" || t.Status == "done" {
							statusIcon = "✅"
						}
						m.lines = append(m.lines, fmt.Sprintf("     %s %d: %s",
							statusIcon, t.ID, t.Title))
					}
				}
			case string(session.EntryModeChange):
				m.lines = append(m.lines, fmt.Sprintf("  %s Mode changed to: %s",
					toolLabelStyle.Render("🔄"),
					toolNameStyle.Render(e.Mode)))
			case string(session.EntryCompact):
				m.lines = append(m.lines, fmt.Sprintf("  %s Context compacted: %d messages summarized",
					toolSuccessStyle.Render("✓"),
					e.CompactedN))
			case string(session.EntryBudgetWarning):
				m.lines = append(m.lines, toolErrorStyle.Render("  ⚠️ Budget warning"))
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
			m.lines = append(m.lines, "")
			m.lines = append(m.lines, line)
			m.lines = append(m.lines, "")
		}
		if m.ready {
			m.viewport.SetHeight(m.calcViewportHeight(true))
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoBottom()
		}
		m.textarea.Focus()

	case SSHDirResultsMsg:
		m.thinking = false
		if msg.Err != nil {
			m.lines = append(m.lines, fmt.Sprintf("   %s Failed to list directory: %v",
				toolErrorStyle.Render("✗ SSH Error:"), msg.Err))
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
			m.lines = append(m.lines, fmt.Sprintf("   %s Connected to %s",
				toolSuccessStyle.Render("✓"), toolNameStyle.Render(msg.Label)))
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
				m.lines = append(m.lines, toolLabelStyle.Render("⚙ SSH:")+" Save as alias? Enter alias name (or press Enter/type 'n' to skip)")
				m.textarea.Placeholder = "Enter alias name (e.g. my-server)..."
			}
		} else {
			m.lines = append(m.lines, fmt.Sprintf("   %s %s",
				toolErrorStyle.Render("✗ SSH Error:"),
				toolResultStyle.Render(msg.Err.Error())))
			m.sshSaveAddr = ""
			m.sshSavePath = ""
		}
		m.agentDone = true
		m.textarea.Focus()
		if m.ready {
			m.viewport.SetHeight(m.calcViewportHeight(true))
			m.viewport.SetContent(m.renderContent())
			m.viewport.GotoBottom()
		}

	case UserPromptMsg:
		m.lines = append(m.lines, "")
		displayPrompt := m.pasteStore.StoreAndFormat(NormalizeLineEndings(sanitize(msg.Prompt)))
		m.lines = append(m.lines, userPromptStyle.Render("> "+displayPrompt))
		m.refreshViewport()

	case AgentTextMsg:
		m.currentText.WriteString(sanitize(msg.Text))
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
		m.lines = append(m.lines, fmt.Sprintf("  %s %s%s",
			toolIconRunning,
			toolNameStyle.Render(displayLabel),
			subtitlePart,
		))
		m.refreshViewport()
		cmds = append(cmds, m.spinner.Tick)

	case ToolResultMsg:
		m.thinking = true
		m.pendingTool = ""
		if msg.Err != nil {
			// Replace the running icon with error icon on the last tool line
			m.replaceLastToolIcon(toolIconError)
			m.lines = append(m.lines, formatToolResultBody(msg.Name, "", msg.Err, m.contentWidth())...)
		} else {
			// Replace the running icon with success icon
			m.replaceLastToolIcon(toolIconSuccess)
			m.lines = append(m.lines, formatToolResultBody(msg.Name, sanitize(msg.Output), nil, m.contentWidth())...)
		}
		m.refreshViewport()
		cmds = append(cmds, m.spinner.Tick)

	case TokenUpdateMsg:
		m.totalTokens = msg.TotalTokens
		m.modelContextLimit = msg.ModelContextLimit

	case AgentDoneMsg:
		m.thinking = false
		m.flushText()
		if msg.Err != nil {
			if msg.Err.Error() == "context canceled" {
				// User-initiated cancellation — show a clean message, not an error.
				m.lines = append(m.lines, lipgloss.NewStyle().Foreground(colorMuted).Render("⏹  Agent cancelled."))
			} else {
				m.lines = append(m.lines, errorStyle.Render("Error: "+msg.Err.Error()))
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
			m.lines = append(m.lines, "")
			m.lines = append(m.lines, line)
			m.lines = append(m.lines, "")
		}
		m.lines = append(m.lines, "")
		m.agentDone = true
		m.textarea.Focus()
		if m.ready {
			m.viewport.SetHeight(m.calcViewportHeight(true))
			m.viewport.SetContent(m.renderContent())
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
		m.approvalPending = true
		m.approvalToolName = msg.Name
		m.approvalToolArgs = msg.Args
		m.approvalRespChan = msg.Resp
		m.approvalIsExternal = msg.IsExternal
		m.approvalWorkerName = msg.WorkerName
		m.approvalWorkerColor = msg.WorkerColor
		m.approvalSelected = 0 // Default to "Approve"
		m.textarea.Blur()
		m.refreshViewport()

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
		m.lines = append(m.lines, fmt.Sprintf("  %s %s %s",
			subagentLabelStyle.Render("🤖 Subagent:"),
			toolNameStyle.Render(msg.Name),
			toolArgsStyle.Render("("+typeLabel+")"),
		))
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
		m.refreshViewport()

	case SubagentDoneMsg:
		m.pendingTool = ""
		m.subagentActive = false
		m.subagentLastTool = ""
		m.subagentProgress = nil
		m.subagentTokens = 0
		if msg.Err != nil {
			m.lines = append(m.lines, fmt.Sprintf("   %s %s",
				toolErrorStyle.Render("✗ Subagent Error:"),
				toolResultStyle.Render(truncate(sanitize(msg.Err.Error()), maxToolOutputLen))))
		} else {
			m.lines = append(m.lines, fmt.Sprintf("   %s %s",
				toolSuccessStyle.Render("✓ Subagent Done:"),
				toolResultStyle.Render(truncate(sanitize(msg.Result), maxToolOutputLen))))
		}
		m.refreshViewport()
		cmds = append(cmds, m.spinner.Tick)

	case CompactDoneMsg:
		m.thinking = false
		if msg.Err != nil {
			m.lines = append(m.lines, fmt.Sprintf("  %s %s",
				toolErrorStyle.Render("✗ Compact Error:"),
				toolResultStyle.Render(msg.Err.Error())))
		} else {
			m.lines = append(m.lines, fmt.Sprintf("  %s Tokens: %d → %d",
				toolSuccessStyle.Render("✓ Context compacted."),
				msg.OldTokens, msg.NewTokens))
		}
		m.lines = append(m.lines, "")
		m.agentDone = true
		m.textarea.Focus()
		m.refreshViewport()

	case BgTaskDoneMsg:
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
			m.lines = append(m.lines, fmt.Sprintf("  %s Background task %s (%s): %s",
				statusIcon,
				toolNameStyle.Render(msg.TaskID),
				msg.Status,
				toolArgsStyle.Render(truncate(sanitize(msg.Command), 60))))
		}
		m.refreshViewport()

	// --- Team messages ---
	case team.SetTeamManagerMsg:
		m.teamState.Manager = msg.Manager

	case team.TeammateSpawnedMsg:
		m.teamState.RefreshTeammates()
		// Auto-show panel when first teammate spawns.
		if !m.teamState.PanelVisible {
			m.teamState.PanelVisible = true
		}
		nameStyled := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(msg.Color)).Render("@" + msg.Name)
		spawnLine := fmt.Sprintf("  %s Teammate %s spawned", toolSuccessStyle.Render("👥"), nameStyled)
		promptLine := fmt.Sprintf("    %s", lipgloss.NewStyle().Faint(true).Render(truncate(msg.Prompt, 80)))
		// Add to leader view.
		m.lines = append(m.lines, spawnLine, promptLine)
		// Initialize per-teammate display lines.
		m.teamState.AppendTeammateLine(msg.AgentID, spawnLine, promptLine, "")
		m.refreshViewport()

	case team.TeammateStatusMsg:
		m.teamState.RefreshTeammates()
		nameStyled := toolNameStyle.Render(msg.AgentID)
		icon := statusIcon(msg.Status)
		switch {
		case msg.Status == team.StatusRunning:
			m.lines = append(m.lines, fmt.Sprintf("  %s %s is working...", icon, nameStyled))
		case msg.Status.IsTerminal():
			errInfo := ""
			if msg.Error != "" {
				errInfo = ": " + msg.Error
			}
			m.lines = append(m.lines, fmt.Sprintf("  %s %s %s%s", icon, nameStyled, string(msg.Status), errInfo))
		case msg.Status == team.StatusIdle:
			m.lines = append(m.lines, fmt.Sprintf("  %s %s idle, waiting for messages", icon, nameStyled))
		}
		m.refreshViewport()

	case team.TeammateProgressMsg:
		// Update cached state, refresh panel if visible
		m.teamState.RefreshTeammates()
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
					formatToolResult(msg.ToolName, sanitize(msg.Content), m.contentWidth())...)
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
				m.lines = append(m.lines, RenderTeammateViewHeader(state.Identity))
				m.lines = append(m.lines, "")
				m.lines = append(m.lines, m.teamState.GetTeammateDisplayLines(msg.AgentID)...)
			}
			m.refreshViewport()
		}
		// Show brief notification in leader view for assistant messages.
		if m.teamState.ViewingAgent == "" && msg.Role == "assistant" {
			preview := truncate(msg.Content, 60)
			m.lines = append(m.lines, fmt.Sprintf("  💬 %s: %s",
				toolNameStyle.Render(msg.AgentID), lipgloss.NewStyle().Faint(true).Render(preview)))
			m.refreshViewport()
		}
	// --- End team messages ---

	case PlanApprovalMsg:
		m.planReviewActive = true
		m.planReviewTitle = msg.PlanPath
		m.planRejectInput = false
		m.planReviewSelected = 0
		m.textarea.Blur()
		m.refreshViewport()

	case AskUserQuestionMsg:
		m.askUserActive = true
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
		m.lines = append(m.lines, fmt.Sprintf("  %s  %s",
			lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("Quit?"),
			quitButtons))
		m.refreshViewport()
		return m, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
			return ExitTimeoutMsg{}
		})

	}

	if m.ready && m.mode == ModeAgent {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

const (
	defaultMaxTextareaLines = 5
	minTextareaLines        = 3
	maxTextareaLinesCap     = 20
)

// calcMaxTextareaLines dynamically computes the max textarea height based on
// terminal height. It returns a value between minTextareaLines and
// maxTextareaLinesCap, capped at 40% of the terminal height.
func calcMaxTextareaLines(termHeight int) int {
	if termHeight <= 0 {
		return defaultMaxTextareaLines
	}
	// Use up to 40% of terminal height for the input area, but keep within bounds.
	n := termHeight * 2 / 5
	if n < minTextareaLines {
		n = minTextareaLines
	}
	if n > maxTextareaLinesCap {
		n = maxTextareaLinesCap
	}
	return n
}

// handlePasteContent processes normalized paste content: stores long pastes
// as a reference in PasteStore, inserts the appropriate text into the textarea,
// and recalculates textarea/viewport height.
func (m Model) handlePasteContent(content string) (tea.Model, tea.Cmd) {
	display := m.pasteStore.StoreAndFormat(content)
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(tea.PasteMsg{Content: display})
	m.textareaLines = recalcLines(m.textarea.Value(), calcMaxTextareaLines(m.height))
	m.textarea.SetHeight(m.textareaLines)
	if m.ready {
		m.viewport.SetHeight(m.calcViewportHeight(m.inputActive()))
	}
	return m, cmd
}

func recalcLines(s string, maxLines int) int {
	n := strings.Count(s, "\n") + 1
	if n < 1 {
		n = 1
	}
	if n > maxLines {
		n = maxLines
	}
	return n
}

func (m Model) inputAreaHeight() int {
	// Dynamically compute by rendering the actual footer
	return lipgloss.Height(m.inputAreaView())
}

func (m Model) calcViewportHeight(_ ...bool) int {
	footerHeight := m.inputAreaHeight()
	teamPanelHeight := 0
	if m.teamState.HasTeam() && m.teamState.PanelVisible {
		teamPanelHeight = m.teamPanelHeight()
	}
	h := m.height - footerHeight - teamPanelHeight
	if h < 3 {
		h = 3
	}
	return h
}

// teamPanelHeight calculates the rendered height of the team coordinator panel.
func (m Model) teamPanelHeight() int {
	if !m.teamState.HasTeam() || !m.teamState.PanelVisible {
		return 0
	}
	panel := RenderCoordinatorPanel(&m.teamState, m.width)
	return lipgloss.Height(panel)
}

func (m Model) newView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) View() tea.View {
	if m.showingHelp {
		return m.newView(m.helpPanelView())
	}

	if m.showingSetting {
		return m.newView(m.settingMenuView())
	}

	if m.showingChannel {
		return m.newView(m.channelPanelView())
	}

	if m.pickingSSHAlias {
		return m.newView(m.sshAliasPickerView())
	}

	if m.pickingModel {
		return m.newView(m.modelPickerView())
	}

	if m.pickingSession {
		return m.newView(m.sessionPickerView())
	}

	if !m.ready {
		return m.newView("\n  Initializing...")
	}

	if m.sshStep == 3 {
		return m.newView(m.dirPickerView())
	}

	if m.approvalPending {
		return m.newView(m.approvalDialogView())
	}

	if m.cancelPending {
		return m.newView(m.cancelDialogView())
	}

	if m.exitPending {
		return m.newView(m.exitDialogView())
	}

	// ─── Determine sidebar visibility (local, don't modify Model in View) ───
	showSidebar := m.width >= minWidthForSidebar

	// ─── Footer (input area) — always full width ───
	footer := m.inputAreaView()
	footerHeight := lipgloss.Height(footer)

	// ─── Team coordinator panel ───
	teamPanel := ""
	teamPanelHeight := 0
	if m.teamState.HasTeam() && m.teamState.PanelVisible {
		teamPanel = RenderCoordinatorPanel(&m.teamState, m.width)
		teamPanelHeight = lipgloss.Height(teamPanel)
	}

	// ─── Calculate main content area dimensions ───
	mainWidth := m.width
	if showSidebar {
		mainWidth = m.width - sidebarWidth
	}

	// ─── Viewport ───
	vpH := m.height - footerHeight - teamPanelHeight
	if vpH < 3 {
		vpH = 3
	}
	if m.ready {
		m.viewport.SetWidth(mainWidth)
		m.viewport.SetHeight(vpH)
		m.viewport.SetContent(strings.TrimRight(m.renderContent(), "\n"))
	}

	vpView := m.viewport.View()

	// ─── Assemble layout ───
	if showSidebar {
		// Manual line-by-line join: viewport | divider | sidebar
		// This avoids JoinHorizontal's reliance on width calculation which
		// can misalign the │ when ANSI sequences or wide chars are present.
		sidebar := m.renderSidebar(vpH)
		contentRow := joinColumnsWithDivider(vpView, sidebar, mainWidth, vpH)
		parts := []string{contentRow}
		if teamPanel != "" {
			parts = append(parts, teamPanel)
		}
		parts = append(parts, footer)
		mainView := lipgloss.JoinVertical(lipgloss.Left, parts...)
		return m.newView(mainView)
	}

	// Single-column fallback
	parts := []string{vpView}
	if teamPanel != "" {
		parts = append(parts, teamPanel)
	}
	parts = append(parts, footer)
	mainView := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return m.newView(mainView)
}

// joinColumnsWithDivider manually joins the viewport and sidebar line-by-line
// with a "│ " divider. Each viewport line is padded with spaces to vpWidth
// using ansi.StringWidth (GraphemeWidth method), matching BubbleTea's internal
// cell buffer width calculation when Unicode Core mode is enabled.
func joinColumnsWithDivider(vpView, sidebar string, vpWidth, height int) string {
	vpLines := strings.Split(vpView, "\n")
	sbLines := strings.Split(sidebar, "\n")

	var buf strings.Builder
	for i := 0; i < height; i++ {
		var vl, sl string
		if i < len(vpLines) {
			vl = vpLines[i]
		}
		if i < len(sbLines) {
			sl = sbLines[i]
		}

		// Pad viewport line to fixed width using the same GraphemeWidth
		// method that BubbleTea's renderer uses when Unicode Core mode
		// 2027 is enabled. ansi.StringWidth handles ANSI stripping internally.
		visW := ansi.StringWidth(vl)
		buf.WriteString(vl)
		if pad := vpWidth - visW; pad > 0 {
			for j := 0; j < pad; j++ {
				buf.WriteByte(' ')
			}
		}
		buf.WriteString("│ ")
		buf.WriteString(sl)

		if i < height-1 {
			buf.WriteByte('\n')
		}
	}
	return buf.String()
}

// renderSidebar renders the right sidebar panel.
func (m Model) renderSidebar(height int) string {
	if m.sidebarComp == nil {
		m.sidebarComp = NewSidebarComponent()
	}
	var todos []tools.TodoItem
	if m.todoStore != nil {
		todos = m.todoStore.Items()
	}
	activeTokens := m.totalTokens
	if m.teamState.ViewingAgent != "" {
		activeTokens = m.teammateTokens[m.teamState.ViewingAgent]
	}
	return m.sidebarComp.View(SidebarState{
		Width:             sidebarWidth - 2, // content width: total - leftBorder - leftPad
		Height:            height,
		TotalWidth:        sidebarWidth,
		EnvLabel:          m.envLabel,
		ActiveProvider:    m.activeProvider,
		ActiveModel:       m.activeModel,
		TotalTokens:       activeTokens,
		ModelContextLimit: m.modelContextLimit,
		TodoItems:         todos,
		TodoScrollOffset:  m.sidebarScrollOffset,
		MCPStatuses:       m.mcpStatuses,
		TeammateCount:     len(m.teamState.Teammates),
		BgRunning:         m.bgRunning,
	})
}

// refreshViewport recalculates viewport height, updates content and scrolls to bottom.
func (m *Model) refreshViewport() {
	if m.ready {
		m.viewport.SetHeight(m.calcViewportHeight())
		m.viewport.SetContent(m.renderContent())
		m.viewport.GotoBottom()
	}
}

// --- Helpers ---

// replaceLastToolIcon replaces the status icon on the last tool call line.
func (m *Model) replaceLastToolIcon(newIcon string) {
	for i := len(m.lines) - 1; i >= 0; i-- {
		line := m.lines[i]
		if strings.Contains(line, toolIconRunning) {
			m.lines[i] = strings.Replace(line, toolIconRunning, newIcon, 1)
			return
		}
		if strings.Contains(line, toolIconPending) {
			m.lines[i] = strings.Replace(line, toolIconPending, newIcon, 1)
			return
		}
	}
}

// getLastAssistantText extracts the last assistant response text from lines.
func (m *Model) getLastAssistantText() string {
	// If we have streaming text, that's the latest
	if m.currentText.Len() > 0 {
		return m.currentText.String()
	}
	// Scan backwards from the end, collecting text until we hit a boundary
	// (user prompt, tool call, or other structural marker)
	var textLines []string
	for i := len(m.lines) - 1; i >= 0; i-- {
		line := m.lines[i]
		// Stop at user prompt (contains orange background ANSI), tool icons, or other boundaries
		if strings.Contains(line, toolIconRunning) ||
			strings.Contains(line, toolIconSuccess) ||
			strings.Contains(line, toolIconError) ||
			strings.Contains(line, "Session resumed:") ||
			strings.Contains(line, "Subagent:") {
			break
		}
		// Detect user prompt line (rendered with background color via userPromptStyle)
		if strings.Contains(line, "\x1b[") && strings.Contains(line, "> ") && strings.Contains(line, "48;2;") {
			break
		}
		if line != "" {
			textLines = append(textLines, line)
		}
	}
	// Reverse since we scanned backwards
	for i, j := 0, len(textLines)-1; i < j; i, j = i+1, j-1 {
		textLines[i], textLines[j] = textLines[j], textLines[i]
	}
	return ansi.Strip(strings.Join(textLines, "\n"))
}

func (m *Model) flushText() {
	text := m.currentText.String()
	if text == "" {
		return
	}
	m.currentText.Reset()
	rendered := text
	if m.mdRenderer != nil {
		if md, err := m.mdRenderer.Render(text); err == nil {
			rendered = md
		}
	}
	m.lines = append(m.lines, "")
	m.lines = append(m.lines, rendered)
}

// contentWidth returns the width available for the main content area,
// accounting for the sidebar when visible.
func (m *Model) contentWidth() int {
	if m.showSidebar {
		return m.width - sidebarWidth
	}
	return m.width
}

func (m *Model) renderContent() string {
	var sb strings.Builder
	for _, line := range m.lines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	if m.currentText.Len() > 0 {
		sb.WriteString("\n")
		sb.WriteString(m.currentText.String())
		sb.WriteString("\n")
	}
	if m.thinking && !m.agentDone {
		var statusLine string
		switch {
		case m.subagentActive && len(m.subagentProgress) > 0:
			sb.WriteString(m.renderSubagentBox())
			sb.WriteString("\n")
			tokenStr := ""
			if m.subagentTokens > 0 {
				if m.modelContextLimit > 0 {
					pct := float64(m.subagentTokens) / float64(m.modelContextLimit) * 100
					tokenStr = fmt.Sprintf(" %d tok / %.0f%%", m.subagentTokens, pct)
				} else {
					tokenStr = fmt.Sprintf(" %d tok", m.subagentTokens)
				}
			}
			statusLine = fmt.Sprintf("  %s %s%s",
				m.spinner.View(),
				subagentLabelStyle.Render(fmt.Sprintf("Subagent [%d steps]...", m.subagentStepCount)),
				toolArgsStyle.Render(tokenStr),
			)
		case m.pendingTool != "":
			statusLine = fmt.Sprintf("  %s Running %s...", m.spinner.View(), toolNameStyle.Render(m.pendingTool))
		default:
			statusLine = fmt.Sprintf("  %s Thinking...", m.spinner.View())
		}
		sb.WriteString(statusLine)
		sb.WriteString("\n")
	}
	return sb.String()
}

// renderSubagentBox returns a bordered box showing live subagent tool calls.
func (m *Model) renderSubagentBox() string {
	const maxVisible = 8
	lines := m.subagentProgress
	hidden := 0
	if len(lines) > maxVisible {
		hidden = len(lines) - maxVisible
		lines = lines[hidden:]
	}

	var content strings.Builder
	if hidden > 0 {
		content.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
			Render(fmt.Sprintf("... (%d earlier steps)", hidden)))
		content.WriteString("\n")
	}
	for i, line := range lines {
		content.WriteString(line)
		if i < len(lines)-1 {
			content.WriteString("\n")
		}
	}

	boxWidth := m.width - 8
	if boxWidth < 30 {
		boxWidth = 30
	}

	box := subagentBoxStyle.Width(boxWidth).Render(content.String())
	return box
}

// ModelOption configures a Model before the BubbleTea program starts.
type ModelOption func(*Model)

// WithApprovalModeChange sets the callback invoked when the user toggles
// approval mode via Ctrl+A or the approval dialog. The callback directly
// updates the backend ApprovalState atomically, bypassing the event loop.
func WithApprovalModeChange(fn func(bool)) ModelOption {
	return func(m *Model) {
		m.OnApprovalModeChange = fn
	}
}

// WithVersion sets the version string displayed in the bottom hint bar.
func WithVersion(v string) ModelOption {
	return func(m *Model) {
		m.version = v
	}
}

func RunTUI(hasPrompt bool, pwd string, todoStore *tools.TodoStore, opts ...ModelOption) (*tea.Program, Model) {
	m := NewModel(hasPrompt, pwd, todoStore)
	for _, opt := range opts {
		opt(&m)
	}
	p := tea.NewProgram(m)
	return p, m
}

func HeaderView() string {
	bracketStyle := lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	jStyle := lipgloss.NewStyle().Foreground(colorLogoJ).Bold(true)
	codeStyle := lipgloss.NewStyle().Foreground(colorText).Bold(true)
	return lipgloss.JoinHorizontal(lipgloss.Left,
		bracketStyle.Render("["),
		jStyle.Render("J"),
		codeStyle.Render("CODE"),
		bracketStyle.Render("]"),
	)
}
