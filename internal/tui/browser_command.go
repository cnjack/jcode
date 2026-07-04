package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// handleBrowserInput implements the `/browser` slash command: show browser-use
// status, and `/browser on` / `/browser off` to toggle it.
func (m *Model) handleBrowserInput(prompt string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	m.textarea.SetValue("")
	fields := strings.Fields(prompt)

	if m.browser == nil || m.browser.Status == nil {
		m.lines = append(m.lines, textLine("  Browser use is not available in this session."))
		m.refreshViewport()
		return m, tea.Batch(cmds...)
	}

	// /browser on | off
	if len(fields) >= 2 {
		switch fields[1] {
		case "on", "off":
			enable := fields[1] == "on"
			if m.browser.SetEnabled == nil {
				m.lines = append(m.lines, textLine("  Cannot change browser setting here."))
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			}
			if err := m.browser.SetEnabled(enable); err != nil {
				m.lines = append(m.lines, textLine("  "+toolLabelStyle.Render("🌐 Browser:")+" failed: "+err.Error()))
			} else {
				state := "disabled"
				if enable {
					state = "enabled"
				}
				m.lines = append(m.lines, textLine("  "+toolLabelStyle.Render("🌐 Browser:")+" "+state+"."))
			}
			m.refreshViewport()
			return m, tea.Batch(cmds...)
		default:
			m.lines = append(m.lines, textLine("  Usage: /browser [on|off]"))
			m.refreshViewport()
			return m, tea.Batch(cmds...)
		}
	}

	// /browser — status.
	st := m.browser.Status()
	m.lines = append(m.lines, textLine(toolLabelStyle.Render("🌐 Browser use:")))

	yn := func(b bool, yes, no string) string {
		if b {
			return yes
		}
		return no
	}
	line := func(label, val string) {
		m.lines = append(m.lines, textLine(fmt.Sprintf("  %s  %s", toolNameStyle.Render(label), val)))
	}

	line("state    ", yn(st.Enabled, "enabled", "disabled  (/browser on to enable)"))
	backend := st.Backend
	if backend == "" {
		backend = "auto"
	}
	line("backend  ", backend)
	if st.ChromeFound {
		info := st.ChromeInfo
		if info == "" {
			info = "found"
		}
		line("chrome   ", "found · "+info)
	} else {
		line("chrome   ", "not found (set browser.chrome_path in config)")
	}
	line("extension", yn(st.ExtensionOnline, "connected", "not connected (open the extension → Auto-connect)"))
	line("dev mode ", yn(st.DevMode, "on (browser_eval / raw CDP allowed)", "off"))

	m.refreshViewport()
	return m, tea.Batch(cmds...)
}
