package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/cnjack/jcode/internal/team"
)

// --- Team TUI state fields (to be added to Model) ---

// TeamViewState tracks team-related TUI state.
type TeamViewState struct {
	Manager       *team.Manager
	ViewingAgent  string // agentID of currently viewed teammate ("" = leader)
	PanelVisible  bool   // show coordinator panel
	PanelSelected int    // selected index in coordinator panel
	ViewMode      TeamViewMode
	Teammates     []*team.TeammateState // cached snapshot

	// Per-teammate display lines, keyed by agentID.
	TeammateLines       map[string][]string
	TeammateCurrentText map[string]*strings.Builder
}

// TeamViewMode represents what the user is viewing.
type TeamViewMode int

const (
	TeamViewLeader   TeamViewMode = iota // viewing leader's conversation
	TeamViewTeammate                     // viewing a teammate's conversation
	TeamViewPanel                        // navigating coordinator panel
)

// HasTeam returns true if a team is active.
func (t *TeamViewState) HasTeam() bool {
	return t.Manager != nil && t.Manager.HasTeam()
}

// initTeammateLines ensures the per-teammate maps are initialised.
func (t *TeamViewState) initTeammateLines() {
	if t.TeammateLines == nil {
		t.TeammateLines = make(map[string][]string)
	}
	if t.TeammateCurrentText == nil {
		t.TeammateCurrentText = make(map[string]*strings.Builder)
	}
}

// AppendTeammateLine adds a formatted display line for a teammate.
func (t *TeamViewState) AppendTeammateLine(agentID string, lines ...string) {
	t.initTeammateLines()
	t.TeammateLines[agentID] = append(t.TeammateLines[agentID], lines...)
}

// FlushTeammateText flushes any buffered streaming text for a teammate into a line.
func (t *TeamViewState) FlushTeammateText(agentID string) {
	t.initTeammateLines()
	sb := t.TeammateCurrentText[agentID]
	if sb != nil && sb.Len() > 0 {
		t.TeammateLines[agentID] = append(t.TeammateLines[agentID], sb.String())
		sb.Reset()
	}
}

// AppendTeammateText appends streaming text for a teammate.
func (t *TeamViewState) AppendTeammateText(agentID, text string) {
	t.initTeammateLines()
	if t.TeammateCurrentText[agentID] == nil {
		t.TeammateCurrentText[agentID] = &strings.Builder{}
	}
	t.TeammateCurrentText[agentID].WriteString(text)
}

// GetTeammateDisplayLines returns the accumulated display lines for a teammate.
func (t *TeamViewState) GetTeammateDisplayLines(agentID string) []string {
	t.initTeammateLines()
	lines := t.TeammateLines[agentID]
	// Also include any unflushed streaming text.
	if sb := t.TeammateCurrentText[agentID]; sb != nil && sb.Len() > 0 {
		return append(lines, sb.String())
	}
	return lines
}

// RefreshTeammates updates the cached teammate list.
func (t *TeamViewState) RefreshTeammates() {
	if t.Manager != nil {
		t.Teammates = t.Manager.ListTeammates()
	}
}

// --- Coordinator Panel rendering ---

// agentNameStyle has no theme color of its own — the per-agent color is
// applied at render time from team.Identity.Color.
var agentNameStyle = lipgloss.NewStyle().Bold(true)

// Theme-derived coordinator-panel styles and status icons. Rebuilt by
// applyTeamStyles, which ApplyTheme calls after setting the palette.
var (
	panelBorderStyle lipgloss.Style
	panelTitleStyle  lipgloss.Style

	statusRunning   string
	statusIdle      string
	statusCompleted string
	statusFailed    string
	statusKilled    string
	statusPending   string
)

func applyTeamStyles() {
	panelBorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1)
	panelTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorText)

	statusRunning = lipgloss.NewStyle().Foreground(colorSecondary).Render("⟳")
	statusIdle = lipgloss.NewStyle().Foreground(colorWarning).Render("◇")
	statusCompleted = lipgloss.NewStyle().Foreground(colorSuccess).Render("✓")
	statusFailed = lipgloss.NewStyle().Foreground(colorError).Render("✗")
	statusKilled = lipgloss.NewStyle().Foreground(colorMuted).Render("⊘")
	statusPending = lipgloss.NewStyle().Foreground(colorDimText).Render("…")
}

func statusIcon(s team.TeammateStatus) string {
	switch s {
	case team.StatusRunning:
		return statusRunning
	case team.StatusIdle:
		return statusIdle
	case team.StatusCompleted:
		return statusCompleted
	case team.StatusFailed:
		return statusFailed
	case team.StatusKilled:
		return statusKilled
	default:
		return statusPending
	}
}

// RenderCoordinatorPanel renders the team status panel at the bottom of the screen.
func RenderCoordinatorPanel(ts *TeamViewState, width int) string {
	if !ts.HasTeam() {
		return ""
	}

	ts.RefreshTeammates()
	teammates := ts.Teammates

	var lines []string

	// Title line
	title := panelTitleStyle.Render(fmt.Sprintf("Team: %s (%d)", ts.Manager.TeamName(), len(teammates)))
	lines = append(lines, title)

	// Leader line
	leaderIndicator := "○"
	if ts.ViewingAgent == "" {
		leaderIndicator = "●"
	}
	leaderLine := fmt.Sprintf("  %s Main (leader)", leaderIndicator)
	lines = append(lines, leaderLine)

	// Teammate lines
	for i, t := range teammates {
		indicator := "○"
		if ts.ViewingAgent == t.Identity.AgentID {
			indicator = "●"
		}

		icon := statusIcon(t.Status)
		elapsed := time.Since(t.StartedAt).Truncate(time.Second)

		name := agentNameStyle.Foreground(lipgloss.Color(t.Identity.Color)).Render("@" + t.Identity.AgentName)

		progress := ""
		if t.Progress != nil && t.Progress.ToolCallCount > 0 {
			progress = fmt.Sprintf(" [%d tools]", t.Progress.ToolCallCount)
		}

		sel := "  "
		if ts.ViewMode == TeamViewPanel && ts.PanelSelected == i+1 { // +1 because leader is index 0
			sel = "> "
		}

		line := fmt.Sprintf("%s%s %s %s %s%s",
			sel, indicator, icon, name, elapsed, progress)
		lines = append(lines, line)
	}

	// Keybinding hints
	lines = append(lines, lipgloss.NewStyle().Faint(true).Render("  shift+↑/↓: switch agent | esc: back to leader | ctrl+t: toggle panel"))

	content := strings.Join(lines, "\n")
	styled := panelBorderStyle.Width(width - 4).Render(content)
	return styled
}

// RenderTeammateViewHeader renders the header when viewing a teammate's conversation.
func RenderTeammateViewHeader(identity team.TeammateIdentity) string {
	name := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(identity.Color)).
		Render("@" + identity.AgentName)

	return fmt.Sprintf("  Viewing %s · [esc] return to leader", name)
}

// RenderTeamStatusPill renders a small "N teammates" indicator for the status bar.
func RenderTeamStatusPill(count int) string {
	if count == 0 {
		return ""
	}
	style := lipgloss.NewStyle().
		Background(colorBorder).
		Foreground(colorText).
		Padding(0, 1)
	return style.Render(fmt.Sprintf("%d teammates", count))
}

// --- Model methods for team view switching ---

// switchTeamView switches the agent view by delta (-1 = prev, +1 = next).
// Order: leader (index 0) → teammate[0] → teammate[1] → ...
func (m *Model) switchTeamView(delta int) {
	m.teamState.RefreshTeammates()
	teammates := m.teamState.Teammates
	count := len(teammates) + 1 // +1 for leader

	// Find current index.
	currentIdx := 0 // leader
	if m.teamState.ViewingAgent != "" {
		for i, t := range teammates {
			if t.Identity.AgentID == m.teamState.ViewingAgent {
				currentIdx = i + 1
				break
			}
		}
	}

	// Calculate new index with wrapping.
	newIdx := (currentIdx + delta + count) % count

	if newIdx == 0 {
		// Switch to leader.
		m.exitTeammateView()
	} else {
		// Switch to teammate.
		t := teammates[newIdx-1]
		m.enterTeammateView(t.Identity.AgentID)
	}
}

// enterTeammateView switches the viewport to show a teammate's conversation.
func (m *Model) enterTeammateView(agentID string) {
	// Save leader's viewport state if coming from leader.
	if m.teamState.ViewMode != TeamViewTeammate {
		m.teamLeaderLines = make([]contentLine, len(m.lines))
		copy(m.teamLeaderLines, m.lines)
		m.teamLeaderText = m.currentText.String()
	}

	m.teamState.ViewingAgent = agentID
	m.teamState.ViewMode = TeamViewTeammate
	m.invalidateSidebarCache()
	m.invalidateFooterCache()

	// Load teammate's accumulated display lines into viewport.
	state := m.teamState.Manager.GetTeammateState(agentID)
	if state != nil {
		m.lines = nil
		m.lines = append(m.lines, textLine(RenderTeammateViewHeader(state.Identity)))
		m.lines = append(m.lines, textLine(""))
		m.lines = append(m.lines, toContentLines(m.teamState.GetTeammateDisplayLines(agentID))...)
	}
	m.currentText.Reset()
	m.refreshViewport()
}

// exitTeammateView returns to the leader's view.
func (m *Model) exitTeammateView() {
	m.teamState.ViewingAgent = ""
	m.teamState.ViewMode = TeamViewLeader
	m.invalidateSidebarCache()
	m.invalidateFooterCache()

	// Restore leader's viewport state.
	if m.teamLeaderLines != nil {
		m.lines = m.teamLeaderLines
		m.teamLeaderLines = nil
		m.currentText.Reset()
		m.currentText.WriteString(m.teamLeaderText)
		m.teamLeaderText = ""
	}
}

// SetTeamManager sets the team manager on the model for TUI integration.
func (m *Model) SetTeamManager(manager *team.Manager) {
	m.teamState.Manager = manager
}
