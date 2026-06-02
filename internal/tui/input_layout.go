package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

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
func (m *Model) handlePasteContent(content string) (tea.Model, tea.Cmd) {
	display := m.pasteStore.StoreAndFormat(content)
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(tea.PasteMsg{Content: display})
	m.textareaLines = m.recalcTextareaLines()
	m.textarea.SetHeight(m.textareaLines)
	if m.ready {
		m.viewport.SetHeight(m.calcViewportHeight(m.inputActive()))
	}
	return m, cmd
}

// recalcLines counts the visual (soft-wrapped) lines needed to display the text
// within the textarea width, clamped to maxLines. Each logical line may produce
// multiple visual lines if it exceeds the available width.
func recalcLines(s string, maxLines int, taWidth int) int {
	if s == "" {
		return 1
	}
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if taWidth > 0 {
			lineLen := uniseg.StringWidth(line)
			wrapped := (lineLen + taWidth - 1) / taWidth
			if wrapped < 1 {
				wrapped = 1
			}
			n += wrapped
		} else {
			n++
		}
	}
	if n < 1 {
		n = 1
	}
	if n > maxLines {
		n = maxLines
	}
	return n
}

// recalcTextareaLines is a convenience wrapper that computes the visual line
// count for the current textarea content, clamped to the terminal-appropriate
// maximum. It should be called after textarea content changes.
func (m Model) recalcTextareaLines() int {
	return recalcLines(m.textarea.Value(), calcMaxTextareaLines(m.height), m.textarea.Width())
}

// inputAreaHeight returns the cached height of the footer, computing it
// only when the cache is empty or the textarea width changed.
func (m *Model) inputAreaHeight() int {
	taW := m.textarea.Width()
	if m.footerCache != "" && m.footerCacheW == taW {
		m.renderPerf.footerCacheHits++
		return m.footerCacheH
	}
	m.renderPerf.footerCacheMisses++
	v := m.inputAreaView()
	h := lipgloss.Height(v)
	m.footerCache = v
	m.footerCacheW = taW
	m.footerCacheH = h
	return h
}

// cachedInputAreaView returns the cached footer, rebuilding only when dirty.
func (m *Model) cachedInputAreaView() string {
	taW := m.textarea.Width()
	if m.footerCache != "" && m.footerCacheW == taW {
		m.renderPerf.footerCacheHits++
		return m.footerCache
	}
	m.renderPerf.footerCacheMisses++
	v := m.inputAreaView()
	m.footerCache = v
	m.footerCacheW = taW
	m.footerCacheH = lipgloss.Height(v)
	return v
}

// invalidateFooterCache marks the footer cache as needing rebuild.
func (m *Model) invalidateFooterCache() {
	m.footerCache = ""
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
// It reuses the panel string rendered in View() via the teamPanel variable
// passed from the caller context. This avoids rendering the panel twice.
func (m Model) teamPanelHeight() int {
	if !m.teamState.HasTeam() || !m.teamState.PanelVisible {
		return 0
	}
	panel := RenderCoordinatorPanel(&m.teamState, m.width)
	return lipgloss.Height(panel)
}
