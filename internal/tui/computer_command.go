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
// gate that is shut, because computer use has three independent ones (enabled /
// backend / macOS permission) and "it doesn't work" is otherwise unactionable.
func (m *Model) handleComputerInput(prompt string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	m.textarea.SetValue("")
	fields := strings.Fields(prompt)

	if m.computer == nil || m.computer.Status == nil {
		m.lines = append(m.lines, textLine("  Computer use is not available in this session."))
		m.refreshViewport()
		return m, tea.Batch(cmds...)
	}

	// /computer on | off
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
		default:
			m.lines = append(m.lines, textLine("  Usage: /computer [on|off]"))
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

	if st.Enabled {
		line("state  ", "enabled")
	} else {
		line("state  ", "disabled  (/computer on to enable)")
	}
	backend := st.Backend
	if backend == "" {
		backend = "auto"
	}
	if st.BackendKind != "" && st.BackendKind != backend {
		backend += " → " + st.BackendKind
	}
	line("backend", backend)

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
