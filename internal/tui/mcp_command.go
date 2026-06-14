package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// handleMCPInput handles the /mcp slash command. Without arguments it lists
// configured MCP servers and their connection status; "/mcp login <name>"
// triggers an OAuth login flow on the main goroutine (which opens the browser).
func (m *Model) handleMCPInput(prompt string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	m.textarea.SetValue("")
	fields := strings.Fields(prompt)

	// /mcp login <name>
	if len(fields) >= 2 && fields[1] == "login" {
		if len(fields) < 3 {
			m.lines = append(m.lines, textLine("  Usage: /mcp login <server-name>"))
			m.refreshViewport()
			return m, tea.Batch(cmds...)
		}
		name := fields[2]
		RequestMCPLogin(name)
		m.lines = append(m.lines, textLine(fmt.Sprintf("  %s Starting OAuth login for %s — your browser will open…",
			toolLabelStyle.Render("🔐 MCP:"), toolNameStyle.Render(name))))
		m.refreshViewport()
		return m, tea.Batch(cmds...)
	}

	// /mcp — list servers + status.
	if len(m.mcpStatuses) == 0 {
		m.lines = append(m.lines, textLine("  No MCP servers connected. Add one with: jcode mcp add <name> <url>"))
		m.refreshViewport()
		return m, tea.Batch(cmds...)
	}

	m.lines = append(m.lines, textLine(toolLabelStyle.Render("🔌 MCP servers:")))
	for _, st := range m.mcpStatuses {
		var status string
		switch {
		case st.NeedsAuth:
			status = "needs login → /mcp login " + st.Name
		case st.Running:
			status = fmt.Sprintf("connected · %d tool(s)", st.ToolCount)
		case st.ErrMsg != "":
			status = "error: " + st.ErrMsg
		default:
			status = "configured"
		}
		m.lines = append(m.lines, textLine(fmt.Sprintf("  %s — %s", toolNameStyle.Render(st.Name), status)))
	}
	m.refreshViewport()
	return m, tea.Batch(cmds...)
}
