package tui

import (
	"fmt"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/theme"
)

// openThemePicker opens the color-theme selector overlay. Arrow keys live-
// preview the highlighted theme; Enter applies and persists it; Esc reverts to
// whatever was active when the picker opened.
func (m *Model) openThemePicker(cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	m.themeBeforePreview = currentTheme.Name

	themes := theme.All()
	items := make([]list.Item, 0, len(themes))
	current := 0
	for idx, t := range themes {
		if t.Name == m.themeBeforePreview {
			current = idx
		}
		items = append(items, themeItem{
			name:        t.Name,
			displayName: t.DisplayName,
			appearance:  string(t.Appearance),
			isCurrent:   t.Name == m.themeBeforePreview,
		})
	}

	del := list.NewDefaultDelegate()
	del.SetSpacing(0)
	m.themePicker = list.New(items, del, 60, 15)
	m.themePicker.Title = "↑/↓ preview · Enter apply · Esc cancel"
	m.themePicker.SetShowHelp(false)
	m.themePicker.SetShowStatusBar(false)
	m.themePicker.SetShowPagination(true)
	m.themePicker.SetFilteringEnabled(true)
	m.themePicker.Select(current)
	m.pickingTheme = true
	m.textarea.Blur()
	return m, tea.Batch(cmds...)
}

// applyThemePreview switches to the named theme and repaints the whole UI so
// the change is visible immediately — chrome, transcript content, sidebar and
// markdown all re-render with the new palette.
func (m *Model) applyThemePreview(name string) {
	ApplyTheme(name)
	m.recreateMDRenderer()
	for i := range m.lines {
		m.lines[i].cachedRender = "" // force re-render with new colors
	}
	m.contentDirty = true
	m.invalidateSidebarCache()
	m.invalidateFooterCache()
	m.refreshViewport()
}

// selectedThemeName returns the highlighted theme's name, or "".
func (m *Model) selectedThemeName() string {
	if sel := m.themePicker.SelectedItem(); sel != nil {
		if it, ok := sel.(themeItem); ok {
			return it.name
		}
	}
	return ""
}

// handleThemePickerKey processes key input while the theme picker is open.
func (m *Model) handleThemePickerKey(msg tea.KeyPressMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	// While the list is actively filtering, forward all keys to it.
	if m.themePicker.FilterState() == list.Filtering {
		var cmd tea.Cmd
		m.themePicker, cmd = m.themePicker.Update(msg)
		cmds = append(cmds, cmd)
		// Filtering moves the selection, so keep the preview in sync.
		if name := m.selectedThemeName(); name != "" && name != currentTheme.Name {
			m.applyThemePreview(name)
		}
		return m, tea.Batch(cmds...)
	}

	switch msg.String() {
	case "enter":
		name := m.selectedThemeName()
		m.themePicker.ResetFilter()
		m.pickingTheme = false
		m.textarea.Focus()
		if name != "" {
			m.applyThemePreview(name) // make sure it is applied, then persist
			m.themePersisted = true
			if cfg, err := config.LoadConfig(); err == nil {
				cfg.Theme = name
				_ = config.SaveConfig(cfg)
			}
			disp := name
			if t, ok := theme.Get(name); ok {
				disp = t.DisplayName
			}
			m.lines = append(m.lines, textLine(fmt.Sprintf("  %s Theme: %s",
				toolSuccessStyle.Render("✓"), toolNameStyle.Render(disp))))
		}
		m.refreshViewport()
		return m, tea.Batch(cmds...)

	case "ctrl+c", "esc":
		// Revert the live preview to the theme active when the picker opened.
		m.themePicker.ResetFilter()
		m.pickingTheme = false
		m.applyThemePreview(m.themeBeforePreview)
		m.textarea.Focus()
		return m, tea.Batch(cmds...)
	}

	var cmd tea.Cmd
	m.themePicker, cmd = m.themePicker.Update(msg)
	cmds = append(cmds, cmd)

	// Live-preview whatever is highlighted after navigation.
	if name := m.selectedThemeName(); name != "" && name != currentTheme.Name {
		m.applyThemePreview(name)
	}

	return m, tea.Batch(cmds...)
}

// themePickerView renders the theme selector overlay.
func (m Model) themePickerView() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	contentW := w - 12
	if contentW > 70 {
		contentW = 70
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
		Render("🎨  Select Theme")
	subtitle := lipgloss.NewStyle().Foreground(colorDimText).
		Render("Preview as you move · applies to the terminal UI")

	m.themePicker.SetSize(contentW-4, listH)
	m.themePicker.SetShowHelp(false)
	m.themePicker.SetShowPagination(true)

	parts := []string{headerText, subtitle, "", m.themePicker.View()}

	footer := lipgloss.NewStyle().Foreground(colorDimText).
		Render("  ↑/↓ preview · Enter apply · Esc cancel · / filter")
	parts = append(parts, footer)

	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}
