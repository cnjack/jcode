package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ─── /rename: session title suggestion + inline editing ───
//
// `/rename` opens an inline editor seeded with the current title while a
// conversation-based suggestion is generated asynchronously (small model).
// The suggestion lands only if the user has not typed anything by then; Enter
// saves (user title — sticky against later automatic upgrades), Esc cancels
// with the old title untouched. `/rename <title>` skips the editor and saves
// the given title directly. Everything is pure UI state on the Model: nothing
// blocks a running turn, and a late/failed suggestion only updates the hint
// line.

// TitleSuggestedMsg carries the outcome of an asynchronous /rename suggestion
// request. Title is empty on failure; Err carries the actionable reason.
type TitleSuggestedMsg struct {
	Title string
	Err   error
}

// renamePlaceholder is the textarea placeholder while the editor is open.
const renamePlaceholder = "Session title — edit, enter to save, esc to cancel"

// handleRenameInput dispatches `/rename` and `/rename <title>`.
func (m *Model) handleRenameInput(prompt string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	m.invalidateFooterCache()
	if m.titleCtl == nil || m.titleCtl.Save == nil {
		m.appendRenameNotice("unavailable in this session")
		return m, tea.Batch(cmds...)
	}

	arg := strings.TrimSpace(strings.TrimPrefix(prompt, "/rename"))
	if arg != "" {
		// Direct manual override — same contract as the web rename.
		return m.applyRenameSave(arg, cmds)
	}

	// Open the editor seeded with the current title; kick off the async
	// suggestion. It seeds the editor only while the user has not typed.
	m.renameActive = true
	m.renameEdited = false
	m.renameNotice = "suggesting from conversation…"
	current := ""
	if m.titleCtl.Current != nil {
		current = m.titleCtl.Current()
	}
	m.textarea.SetValue(current)
	m.textarea.Placeholder = renamePlaceholder
	m.textarea.Focus()
	m.refreshViewport()
	if m.titleCtl.Suggest != nil {
		m.renameSuggesting = true
		suggest := m.titleCtl.Suggest
		cmds = append(cmds, func() tea.Msg {
			// Background ctx: the suggestion owns its timeout and must not be
			// tied to a turn's cancellation. A late result is a no-op (the
			// handler drops it once the editor is closed).
			title, err := suggest(context.Background())
			return TitleSuggestedMsg{Title: title, Err: err}
		})
	}
	return m, tea.Batch(cmds...)
}

// handleTitleSuggested applies a late suggestion result to the open editor.
func (m *Model) handleTitleSuggested(msg TitleSuggestedMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	m.renameSuggesting = false
	m.invalidateFooterCache()
	if !m.renameActive {
		// Editor was closed (saved/cancelled) before the result landed.
		return m, tea.Batch(cmds...)
	}
	switch {
	case msg.Err != nil:
		// Keep the current title in the editor; surface why nothing arrived.
		m.renameNotice = "no suggestion (" + msg.Err.Error() + ") — edit or esc"
	case m.renameEdited:
		// The user is faster than the model: never clobber typed input.
		m.renameNotice = "suggestion dropped (you already edited)"
	default:
		m.textarea.SetValue(msg.Title)
		m.renameNotice = "suggested from conversation — edit or enter to save"
	}
	m.refreshViewport()
	return m, tea.Batch(cmds...)
}

// handleRenameKey processes keys while the rename editor is open. It owns the
// textarea: enter saves, esc cancels, everything else edits (and marks the
// editor user-edited so a late suggestion cannot overwrite).
func (m *Model) handleRenameKey(key string, msg tea.KeyPressMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	switch key {
	case "enter":
		return m.applyRenameSave(m.textarea.Value(), cmds)
	case "esc", "ctrl+c":
		m.closeRenameEditor()
		m.appendRenameNotice("cancelled — title unchanged")
		return m, tea.Batch(cmds...)
	default:
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
		if m.textarea.Value() != "" {
			m.renameEdited = true
		}
		m.renameNotice = ""
		if m.ready {
			m.textareaLines = m.recalcTextareaLines()
			m.textarea.SetHeight(m.textareaLines)
		}
		return m, tea.Batch(cmds...)
	}
}

// applyRenameSave sanitizes+persists the given title through the controller
// and reports the outcome. An empty input cancels cleanly instead of erroring.
func (m *Model) applyRenameSave(input string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	title := strings.TrimSpace(input)
	if title == "" {
		m.closeRenameEditor()
		m.appendRenameNotice("nothing entered — title unchanged")
		return m, tea.Batch(cmds...)
	}
	saved, err := m.titleCtl.Save(title)
	m.closeRenameEditor()
	if err != nil {
		m.appendRenameNotice("not saved — " + err.Error())
		return m, tea.Batch(cmds...)
	}
	m.appendRenameNotice("title set — " + saved)
	return m, tea.Batch(cmds...)
}

// closeRenameEditor restores the prompt textarea after save/cancel.
func (m *Model) closeRenameEditor() {
	m.renameActive = false
	m.renameSuggesting = false
	m.renameEdited = false
	m.renameNotice = ""
	m.textarea.Reset()
	m.textarea.SetHeight(1)
	m.textareaLines = 1
	m.textarea.Placeholder = "Type your prompt here..."
	m.textarea.Focus()
	m.invalidateFooterCache()
	m.refreshViewport()
}

// appendRenameNotice adds a one-line rename status notice to the transcript
// (same shape as CommandNoticeMsg rendering).
func (m *Model) appendRenameNotice(text string) {
	m.lines = append(m.lines, textLine("  "+toolLabelStyle.Render("Rename: ")+lipgloss.NewStyle().Foreground(colorMuted).Render(text)))
	m.refreshViewport()
}

// renameHintLine renders the status line shown above the textarea while the
// editor is open.
func (m *Model) renameHintLine() string {
	hint := "✏️  Rename session · enter save · esc cancel"
	if m.renameNotice != "" {
		hint += " · " + m.renameNotice
	}
	return lipgloss.NewStyle().Foreground(colorPrimary).Render(hint)
}
