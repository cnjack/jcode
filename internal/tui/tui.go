package tui

import (
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
	"github.com/cnjack/jcode/internal/mode"
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

	lines       []contentLine
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
	goalStore *tools.GoalStore

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
	approvalSelected    int                      // 0=Approve, 1=ApproveAll, 2=Reject
	approvalQueue       []ToolApprovalRequestMsg // queued requests when dialog is already active

	envLabel  string
	agentMode AgentMode
	bgRunning int // count of running background tasks

	// lastAssistantRawText stores the raw (unrendered) text of the last
	// assistant response, used by Ctrl+Y to copy to clipboard without
	// picking up structural elements like dividers.
	lastAssistantRawText string

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

	// OnApprovalModeChange is called when the user promotes to auto-approve via the
	// approval dialog's "Approve All". It updates the backend ApprovalState's
	// approval axis directly (no agent rebuild), which is the correct fast path for
	// a mid-run approval-only change. The unified Shift+Tab selector instead flows
	// through modeSelectCh so it can also swap tools/prompt.
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
	teamLeaderLines []contentLine
	teamLeaderText  string

	// ─── Sidebar state ───
	showSidebar         bool // whether sidebar is currently visible
	sidebarScrollOffset int  // scroll offset for todo list in sidebar
	sidebarComp         *SidebarComponent

	// ─── Manage models state ───
	managingModels     bool
	manageModelsPicker list.Model

	// ─── Theme picker state ───
	pickingTheme       bool
	themePicker        list.Model
	themeBeforePreview string // theme name to restore if the picker is cancelled
	themePersisted     bool   // true when a theme was explicitly chosen/loaded
	//   (suppresses terminal-background auto-detection)

	// ─── Render performance cache ───
	contentDirty      bool   // true when lines/currentText/thinking changed since last render
	renderedContent   string // cached output of renderContent()
	renderedLineWidth int    // the contentWidth() used for the cached render

	// sidebarCache caches the rendered sidebar string between frames.
	sidebarCache      string
	sidebarCacheDirty bool // true when sidebar-affecting state changed
	sidebarCacheH     int  // viewport height when cache was built

	// renderPending tracks whether a batched stream render is scheduled.
	renderPending bool

	// footerCache caches the rendered input area (mode pills + textarea + status bar).
	// It's invalidated when textarea content, mode, bg count, or width changes.
	footerCache  string
	footerCacheW int // textarea width when cache was built
	footerCacheH int // height in lines

	// subagentBoxCache caches the rendered subagent progress box.
	subagentBoxCache      string
	subagentBoxCacheLen   int // len(m.subagentProgress) when cached
	subagentBoxCacheWidth int // content width when cached

	renderPerf tuiRenderPerf
}

func NewModel(hasPrompt bool, pwd string, todoStore *tools.TodoStore) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	md, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle(currentTheme.GlamourStyle()),
		glamour.WithWordWrap(96), // default, recreated on first WindowSizeMsg
	)

	mode := ModeAgent
	thinking := false
	var initialLines []contentLine
	if hasPrompt {
		thinking = true
	} else {
		initialLines = []contentLine{
			textLine(lipgloss.NewStyle().Foreground(colorMuted).Render("Welcome to JCODE. How can I help you today?")),
			textLine(""),
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
	ml.SetFilteringEnabled(true)

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
		mode:              mode,
		spinner:           s,
		thinking:          thinking,
		mdRenderer:        md,
		textarea:          newTextarea(),
		textareaLines:     1,
		currentText:       &strings.Builder{},
		sidebarComp:       NewSidebarComponent(),
		dirList:           l,
		modelPicker:       ml,
		settingMenu:       sl,
		sshAliasPicker:    sal,
		sessionPicker:     sesl,
		channelMenu:       chl,
		channelStates:     make(map[string]string),
		pwd:               pwd,
		history:           loadHistory(),
		todoStore:         todoStore,
		pasteStore:        NewPasteStore(),
		lines:             initialLines,
		envLabel:          "Local",
		approvalMode:      ModeManual, // Default to manual approval mode
		contentDirty:      true,
		sidebarCacheDirty: true,
		renderPerf:        newTUIRenderPerf(),
	}
	m.historyIndex = len(m.history)

	if cfg, err := config.LoadConfig(); err == nil {
		m.activeProvider, m.activeModel = cfg.GetProviderModel()
		if cfg.AutoApprove {
			m.approvalMode = ModeAuto
		}
	}

	m.logRenderPerf("start")

	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		textarea.Blink,
		// Ask the terminal for its background color so we can auto-pick a light
		// or dark default theme when the user hasn't chosen one explicitly.
		tea.RequestBackgroundColor,
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
	m.cancelSelected = 1 // default to "Wait" (non-destructive, matches Quit dialog's safe "No")
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
	return (m.mode == ModeAgent || m.sshStep > 0 || m.sshSavePrompt) && !m.pickingModel && !m.managingModels && !m.pickingTheme && !m.showingSetting && !m.showingHelp && !m.pickingSSHAlias && !m.pickingSession && !m.approvalPending && !m.planReviewActive && !m.askUserActive
}

// ModelOption configures a Model before the BubbleTea program starts.
type ModelOption func(*Model)

// WithApprovalModeChange sets the callback invoked when the user promotes to
// auto-approve via the approval dialog's "Approve All". The callback directly
// updates the backend ApprovalState atomically, bypassing the event loop.
func WithApprovalModeChange(fn func(bool)) ModelOption {
	return func(m *Model) {
		m.OnApprovalModeChange = fn
	}
}

// WithStartupMode seeds the initial selector mode so the mode pill reflects the
// resolved startup mode (config DefaultMode / legacy AutoApprove / --unsafe).
func WithStartupMode(sm mode.SessionMode) ModelOption {
	return func(m *Model) {
		m.applySelectorMode(sm)
	}
}

// selectorMode derives the unified selector mode from the two low-level TUI
// fields (tool axis + approval axis) for display in the mode pill.
func (m Model) selectorMode() mode.SessionMode {
	if m.agentMode == ModePlanning {
		return mode.Plan
	}
	if m.approvalMode == ModeAuto {
		return mode.Autopilot
	}
	return mode.Ask
}

// applySelectorMode sets the two low-level TUI fields to match a unified mode.
// Plan leaves the approval field untouched (read-only tools make it moot).
func (m *Model) applySelectorMode(sm mode.SessionMode) {
	switch sm {
	case mode.Plan:
		m.agentMode = ModePlanning
	case mode.Autopilot:
		m.agentMode = ModeNormal
		m.approvalMode = ModeAuto
	default: // Ask
		m.agentMode = ModeNormal
		m.approvalMode = ModeManual
	}
}

// WithVersion sets the version string displayed in the bottom hint bar.
func WithVersion(v string) ModelOption {
	return func(m *Model) {
		m.version = v
	}
}

// WithGoalStore wires the session goal store so the TUI can render the active
// goal indicator. The store is read directly when rendering.
func WithGoalStore(gs *tools.GoalStore) ModelOption {
	return func(m *Model) {
		m.goalStore = gs
	}
}

// WithTheme applies the persisted color theme at startup. A non-empty name
// marks the theme as explicit, suppressing terminal-background auto-detection.
func WithTheme(name string) ModelOption {
	return func(m *Model) {
		if name == "" {
			return
		}
		ApplyTheme(name)
		m.themePersisted = true
		// Rebuild the markdown renderer so it matches the applied theme.
		m.recreateMDRenderer()
	}
}

func RunTUI(hasPrompt bool, pwd string, todoStore *tools.TodoStore, opts ...ModelOption) (*tea.Program, Model) {
	m := NewModel(hasPrompt, pwd, todoStore)
	for _, opt := range opts {
		opt(&m)
	}
	p := tea.NewProgram(&m)
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
