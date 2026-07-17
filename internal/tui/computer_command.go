package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// handleComputerInput implements the `/computer` slash command: show
// computer-use status, and `/computer on` / `/computer off` to toggle it.
//
// Mirrors handleBrowserInput. The status output leans hard on naming the one
// gate that is shut, because computer use has independent enablement, helper,
// Accessibility and Screen Recording gates.
func (m *Model) handleComputerInput(prompt string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	m.textarea.SetValue("")
	fields := strings.Fields(prompt)

	if m.computer == nil || m.computer.Status == nil {
		m.lines = append(m.lines, textLine("  Computer use is not available in this session."))
		m.refreshViewport()
		return m, tea.Batch(cmds...)
	}

	// /computer on | off | grant
	if len(fields) >= 2 {
		switch fields[1] {
		case "on", "off":
			enable := fields[1] == "on"
			if m.computer.SetEnabled == nil {
				m.lines = append(m.lines, textLine("  Cannot change computer setting here."))
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			}
			if err := m.computer.SetEnabled(enable); err != nil {
				m.lines = append(m.lines, textLine("  "+toolLabelStyle.Render("🖥 Computer:")+" failed: "+err.Error()))
			} else {
				state := "disabled"
				if enable {
					state = "enabled"
				}
				m.lines = append(m.lines, textLine("  "+toolLabelStyle.Render("🖥 Computer:")+" "+state+"."))
			}
			m.refreshViewport()
			return m, tea.Batch(cmds...)
		case "grant":
			// Surface the real macOS consent prompts without leaving the
			// terminal — the in-run answer to "Accessibility permission not
			// granted". The system dialog is answered by the user; /computer
			// re-checks the state afterwards.
			if m.computer.RequestPermissions == nil {
				m.lines = append(m.lines, textLine("  Cannot request permissions here."))
				m.refreshViewport()
				return m, tea.Batch(cmds...)
			}
			if err := m.computer.RequestPermissions(); err != nil {
				m.lines = append(m.lines, textLine("  "+toolLabelStyle.Render("🖥 Computer:")+" permission request failed: "+err.Error()))
			} else {
				m.lines = append(m.lines, textLine("  "+toolLabelStyle.Render("🖥 Computer:")+" permission window (or macOS consent prompt) shown for jcode Computer Use."))
				m.lines = append(m.lines, textLine("  Allow it (or enable \"jcode Computer Use\" under System Settings > Privacy & Security > Accessibility / Screen Recording), then run /computer to re-check."))
			}
			m.refreshViewport()
			return m, tea.Batch(cmds...)
		default:
			m.lines = append(m.lines, textLine("  Usage: /computer [on|off|grant]"))
			m.refreshViewport()
			return m, tea.Batch(cmds...)
		}
	}

	// /computer — status.
	st := m.computer.Status()
	m.lines = append(m.lines, textLine(toolLabelStyle.Render("🖥 Computer use:")))

	line := func(label, val string) {
		m.lines = append(m.lines, textLine(fmt.Sprintf("  %s  %s", toolNameStyle.Render(label), val)))
	}

	if !st.Supported {
		line("support", "unavailable on "+st.Platform)
		if st.Detail != "" {
			m.lines = append(m.lines, textLine("  "+st.Detail))
		}
		m.refreshViewport()
		return m, tea.Batch(cmds...)
	}

	if st.Enabled {
		line("state  ", "enabled")
	} else {
		line("state  ", "disabled  (/computer on to enable)")
	}
	helper := "not installed"
	if st.HelperInstalled {
		helper = "installed"
	}
	if st.HelperConnected {
		helper = "connected"
	}
	if st.HelperVersion != "" {
		helper += " (" + st.HelperVersion + ")"
	}
	line("helper ", helper)
	line("access ", permissionLabel(st.Accessibility, st.Enabled))
	line("screen ", permissionLabel(st.ScreenRecording, st.Enabled))

	if st.Available {
		line("ready  ", "yes")
	} else {
		line("ready  ", "no")
	}
	// The detail is the whole point: it names which gate is shut and what to do.
	if st.Detail != "" {
		m.lines = append(m.lines, textLine("  "+st.Detail))
	}

	m.refreshViewport()
	return m, tea.Batch(cmds...)
}

func permissionLabel(state string, enabled bool) string {
	switch state {
	case "granted":
		return "granted"
	case "denied":
		return "not granted — open System Settings > Privacy & Security"
	default:
		if !enabled {
			return "not checked (computer use is off)"
		}
		return "unknown — update or reinstall jcode, then check again"
	}
}
