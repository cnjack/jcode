package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
)

// manageModelItem represents a model in the manage models view with a toggle state.
type manageModelItem struct {
	provider     string
	providerName string
	model        string
	modelName    string
	enabled      bool
	recommended  bool
	isHeader     bool // provider group header
}

func (i manageModelItem) Title() string {
	if i.isHeader {
		return i.providerName
	}
	toggle := "  "
	if i.enabled {
		toggle = "✓ "
	}
	title := toggle + i.modelName
	if i.recommended {
		title += " [recommended]"
	}
	return title
}

func (i manageModelItem) Description() string {
	if i.isHeader {
		return ""
	}
	return ""
}

func (i manageModelItem) FilterValue() string {
	if i.isHeader {
		return ""
	}
	return i.modelName + " " + i.model
}

// openManageModels transitions to the manage models view.
func (m Model) openManageModels(cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	cfg, err := config.LoadConfig()
	if err != nil {
		m.lines = append(m.lines, textLine(toolErrorStyle.Render("✗ Failed to load config: "+err.Error())))
		return m, tea.Batch(cmds...)
	}

	configuredProviders := cfg.GetProviders()
	registry := model.NewModelRegistryWithConfig(cfg)
	modelState, _ := config.LoadModelState()

	var items []list.Item

	for _, rp := range registry.ListProviders() {
		// Only show providers that the user has configured.
		if _, configured := configuredProviders[rp.ID]; !configured {
			continue
		}

		models := registry.ListProviderModels(rp.ID, false)
		if len(models) == 0 {
			continue
		}

		// Add provider header
		items = append(items, manageModelItem{
			provider:     rp.ID,
			providerName: "━━━ " + strings.ToUpper(rp.Name) + " ━━━",
			model:        "",
			modelName:    "",
			enabled:      false,
			recommended:  false,
			isHeader:     true,
		})

		for _, rm := range models {
			ref := config.ModelRef{Provider: rp.ID, Model: rm.ID}
			enabled := modelState.IsModelEnabled(ref, rm.DefaultEnabled)

			items = append(items, manageModelItem{
				provider:     rp.ID,
				providerName: rp.Name,
				model:        rm.ID,
				modelName:    rm.Name,
				enabled:      enabled,
				recommended:  rm.Recommended,
				isHeader:     false,
			})
		}
	}

	del := list.NewDefaultDelegate()
	del.SetSpacing(0)
	m.manageModelsPicker = list.New(items, del, 60, 15)
	m.manageModelsPicker.Title = "Space toggle · Enter done · Esc cancel"
	m.manageModelsPicker.SetShowHelp(false)
	m.manageModelsPicker.SetShowStatusBar(true)
	m.manageModelsPicker.SetShowPagination(true)
	m.manageModelsPicker.SetFilteringEnabled(true)
	m.managingModels = true
	m.textarea.Blur()
	return m, tea.Batch(cmds...)
}

// handleManageModelsKey processes key input in the manage models view.
func (m Model) handleManageModelsKey(msg tea.KeyPressMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	// When the list is actively filtering, let all keys pass through to the list
	if m.manageModelsPicker.FilterState() == list.Filtering {
		var cmd tea.Cmd
		m.manageModelsPicker, cmd = m.manageModelsPicker.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}
	switch msg.String() {
	case " ", "space":
		// Toggle the selected model's visibility
		selected := m.manageModelsPicker.SelectedItem()
		if selected != nil {
			item := selected.(manageModelItem)
			// Skip headers
			if item.isHeader {
				return m, tea.Batch(cmds...)
			}
			newEnabled := !item.enabled

			// Update the model state
			modelState, _ := config.LoadModelState()
			ref := config.ModelRef{Provider: item.provider, Model: item.model}
			modelState.SetModelEnabled(ref, newEnabled)
			_ = config.SaveModelState(modelState)

			// Update the item in the list
			idx := m.manageModelsPicker.Index()
			items := m.manageModelsPicker.Items()
			if idx < len(items) {
				updatedItem := items[idx].(manageModelItem)
				updatedItem.enabled = newEnabled
				items[idx] = updatedItem
				// Force list to update by setting items again
				cmd := m.manageModelsPicker.SetItems(items)
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case "enter":
		// Done — close manage models and return to model picker
		m.manageModelsPicker.ResetFilter()
		m.managingModels = false
		return m.handleModelInput(cmds)

	case "ctrl+c", "esc":
		// Cancel — close manage models and return to model picker
		m.manageModelsPicker.ResetFilter()
		m.managingModels = false
		return m.handleModelInput(cmds)
	}

	var cmd tea.Cmd
	m.manageModelsPicker, cmd = m.manageModelsPicker.Update(msg)
	cmds = append(cmds, cmd)

	// Skip provider headers when navigating with arrow keys
	if msg.String() == "up" || msg.String() == "down" {
		if selected := m.manageModelsPicker.SelectedItem(); selected != nil {
			if item, ok := selected.(manageModelItem); ok && item.isHeader {
				// Item is a header, move again in the same direction
				m.manageModelsPicker, cmd = m.manageModelsPicker.Update(msg)
				cmds = append(cmds, cmd)
				// Check again in case there are consecutive headers
				if selected := m.manageModelsPicker.SelectedItem(); selected != nil {
					if item, ok := selected.(manageModelItem); ok && item.isHeader {
						m.manageModelsPicker, cmd = m.manageModelsPicker.Update(msg)
						cmds = append(cmds, cmd)
					}
				}
			}
		}
	}

	return m, tea.Batch(cmds...)
}

// manageModelsView renders the manage models screen.
func (m Model) manageModelsView() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	contentW := w - 12
	if contentW > 80 {
		contentW = 80
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
		Render("⚙  Manage Models")

	subtitle := lipgloss.NewStyle().Foreground(colorDimText).
		Render("Toggle which models appear in the model selector")

	m.manageModelsPicker.SetSize(contentW-4, listH)
	m.manageModelsPicker.SetShowHelp(false)
	m.manageModelsPicker.SetShowPagination(true)

	var contentParts []string
	contentParts = append(contentParts, headerText)
	contentParts = append(contentParts, subtitle)
	contentParts = append(contentParts, "")
	contentParts = append(contentParts, m.manageModelsPicker.View())

	// Footer with key hints
	footer := lipgloss.NewStyle().Foreground(colorDimText).
		Render(fmt.Sprintf("  %s toggle · %s done · %s cancel",
			strings.ToUpper("space"),
			strings.ToUpper("enter"),
			strings.ToUpper("esc")))
	contentParts = append(contentParts, footer)

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, boxStyle.Render(content))
}
