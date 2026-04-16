package tui

import (
	"bytes"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	qrterminal "github.com/mdp/qrterminal/v3"
)

// handleChannelKeyPress handles keyboard input when the channel panel is active.
func (m Model) handleChannelKeyPress(msg tea.KeyPressMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		selected := m.channelMenu.SelectedItem()
		if selected == nil {
			return m, tea.Batch(cmds...)
		}

		switch item := selected.(type) {
		case channelItem:
			// Selected a channel — show its actions
			return m.showChannelActions(item.key, cmds)
		case channelActionItem:
			// Selected an action — dispatch it
			m.showingChannel = false
			m.textarea.Focus()

			select {
			case channelActionCh <- ChannelAction{
				ChannelID: item.channelID,
				Action:    item.action,
			}:
			default:
			}

			m.lines = append(m.lines, toolLabelStyle.Render(
				fmt.Sprintf("📡 %s: %s...", item.channelID, item.action)))
			m.refreshViewport()
			return m, tea.Batch(cmds...)
		}

	case "ctrl+c", "esc":
		m.showingChannel = false
		m.textarea.Focus()
		m.refreshViewport()
		return m, tea.Batch(cmds...)
	}

	var cmd tea.Cmd
	m.channelMenu, cmd = m.channelMenu.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

// showChannelActions replaces the channel list with available actions for the selected channel.
func (m Model) showChannelActions(channelID string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	state := m.channelStates[channelID]
	var items []list.Item

	switch state {
	case "enabled":
		items = []list.Item{
			channelActionItem{title: "Disable", desc: "Stop push notifications and message polling", action: "disable", channelID: channelID},
			channelActionItem{title: "Logout", desc: "Clear login credentials", action: "logout", channelID: channelID},
		}
	case "disabled":
		items = []list.Item{
			channelActionItem{title: "Enable", desc: "Start push notifications and message polling", action: "enable", channelID: channelID},
			channelActionItem{title: "Logout", desc: "Clear login credentials", action: "logout", channelID: channelID},
		}
	default: // "none"
		items = []list.Item{
			channelActionItem{title: "Login", desc: "Scan QR code to connect WeChat", action: "login", channelID: channelID},
		}
	}

	m.channelMenu.SetItems(items)
	m.channelMenu.Title = fmt.Sprintf("↑/↓ navigate · Enter confirm · Esc cancel")
	return m, tea.Batch(cmds...)
}

// channelActionItem represents an action within the channel detail view
type channelActionItem struct {
	title     string
	desc      string
	action    string // "login", "logout", "enable", "disable"
	channelID string
}

func (i channelActionItem) Title() string       { return i.title }
func (i channelActionItem) Description() string { return i.desc }
func (i channelActionItem) FilterValue() string { return i.title }

// channelPanelView renders the channel management panel.
func (m Model) channelPanelView() string {
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
		Render("📡 Channels")

	m.channelMenu.SetSize(contentW-4, listH)
	m.channelMenu.SetShowHelp(false)
	m.channelMenu.SetShowStatusBar(false)
	m.channelMenu.SetShowPagination(false)

	var contentParts []string
	contentParts = append(contentParts, headerText)
	contentParts = append(contentParts, "")
	contentParts = append(contentParts, m.channelMenu.View())

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}

// renderQRCode renders a QR code as terminal text lines.
// Uses half-block characters (▀▄█ ) so each terminal cell represents
// two vertical pixels, producing a square QR code.
func renderQRCode(content string) []string {
	var buf bytes.Buffer
	cfg := qrterminal.Config{
		Level:          qrterminal.L,
		Writer:         &buf,
		HalfBlocks:     true,
		BlackChar:      qrterminal.BLACK_BLACK,
		WhiteChar:      qrterminal.WHITE_WHITE,
		BlackWhiteChar: qrterminal.BLACK_WHITE,
		WhiteBlackChar: qrterminal.WHITE_BLACK,
		QuietZone:      1,
	}
	qrterminal.GenerateWithConfig(content, cfg)
	lines := strings.Split(buf.String(), "\n")
	var result []string
	for _, line := range lines {
		if line != "" {
			result = append(result, "   "+line)
		}
	}
	return result
}
