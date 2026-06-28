package tui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
)

type SetupDoneMsg struct{}

// ProviderProfile holds the minimal info needed for the setup wizard flow.
type ProviderProfile struct {
	ID           string
	Name         string
	BaseURL      string
	NeedURL      bool // if true, prompt for custom URL
	NeedKey      bool // if true, prompt for API Key
	FromRegistry bool // true if provider exists in models.dev
}

// ollamaLocalProvider is a special-case provider not in models.dev.
var ollamaLocalProvider = ProviderProfile{
	ID:      "ollama",
	Name:    "Ollama (Local)",
	BaseURL: "http://localhost:11434/v1",
	NeedKey: false,
}

type providerItem struct {
	profile    ProviderProfile
	configured bool // this exact provider has an API key in config
}

func (i providerItem) Title() string { return i.profile.Name }
func (i providerItem) Description() string {
	if i.configured {
		return "✓ Configured · " + i.profile.BaseURL
	}
	if i.profile.BaseURL != "" {
		return i.profile.BaseURL
	}
	return i.profile.ID
}
func (i providerItem) FilterValue() string { return i.profile.Name + " " + i.profile.ID }

type SetupState int

const (
	StateProvider    SetupState = iota
	StateCustomModel            // only for custom/OpenAI-compatible providers (model id required)
	StateURL
	StateAPIKey
)

type SetupModel struct {
	state         SetupState
	providerList  list.Model
	customModelIn textinput.Model
	urlIn         textinput.Model
	keyIn         textinput.Model

	registry         *model.ModelRegistry
	selectedProvider *ProviderProfile
	selectedModel    string // only set for custom providers
	finalURL         string
	finalKey         string

	width  int
	height int
	err    string
	done   bool
}

func NewSetupModel() SetupModel {
	// Load existing config for custom model merging
	var cfg *config.Config
	if loadedCfg, err := config.LoadConfig(); err == nil {
		cfg = loadedCfg
	}

	m := SetupModel{
		state:    StateProvider,
		registry: model.NewModelRegistryWithConfig(cfg),
	}

	// Build a set of configured providers (from existing config)
	configuredProviders := make(map[string]bool)
	if cfg, err := config.LoadConfig(); err == nil && cfg != nil {
		for provID, pCfg := range cfg.GetProviders() {
			if pCfg.APIKey != "" {
				configuredProviders[provID] = true
			}
		}
	}

	var items []list.Item

	// Show all providers from the generated registry in curated order
	for _, rp := range m.registry.ListProviders() {
		// Providers need a key if they declare environment variable names.
		// Custom providers added via MergeConfigProviders always get a derived
		// Env entry, so this single check covers all cases.
		needKey := len(rp.Env) > 0
		items = append(items, providerItem{
			profile: ProviderProfile{
				ID:           rp.ID,
				Name:         rp.Name,
				BaseURL:      rp.API,
				NeedKey:      needKey,
				FromRegistry: true,
			},
			configured: configuredProviders[rp.ID],
		})
	}

	// Add Ollama (local) — not in models.dev
	items = append(items, providerItem{
		profile:    ollamaLocalProvider,
		configured: configuredProviders[ollamaLocalProvider.ID],
	})

	// Always add "OpenAI Compatible" as the last option
	items = append(items, providerItem{
		profile: ProviderProfile{
			ID:      "openai-compatible",
			Name:    "OpenAI Compatible",
			NeedURL: true,
			NeedKey: true,
		},
	})
	del := list.NewDefaultDelegate()
	del.SetSpacing(0)
	pl := list.New(items, del, 60, 15)
	pl.Title = "Select LLM Provider (↑/↓ to navigate, Enter to confirm)"
	pl.SetShowHelp(false)
	m.providerList = pl

	m.customModelIn = textinput.New()
	m.customModelIn.Placeholder = "Enter model id (e.g. gpt-4o)..."
	m.customModelIn.Prompt = "Model ID: "
	m.customModelIn.SetWidth(50)

	m.urlIn = textinput.New()
	m.urlIn.Placeholder = "https://your-base-url/v1"
	m.urlIn.Prompt = "Base URL: "
	m.urlIn.SetWidth(50)

	m.keyIn = textinput.New()
	m.keyIn.Placeholder = "sk-..."
	m.keyIn.Prompt = "API Key: "
	m.keyIn.SetWidth(50)

	return m
}

func (m SetupModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink)
}

func (m SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.PasteMsg:
		// Forward bracketed paste (Ctrl+Shift+V) to active textinput
		switch m.state {
		case StateCustomModel:
			var cmd tea.Cmd
			m.customModelIn, cmd = m.customModelIn.Update(msg)
			return m, cmd
		case StateURL:
			var cmd tea.Cmd
			m.urlIn, cmd = m.urlIn.Update(msg)
			return m, cmd
		case StateAPIKey:
			var cmd tea.Cmd
			m.keyIn, cmd = m.keyIn.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.ClipboardMsg:
		// OSC52 clipboard read result — convert to PasteMsg and forward to active textinput
		if msg.Content != "" {
			switch m.state {
			case StateCustomModel:
				var cmd tea.Cmd
				m.customModelIn, cmd = m.customModelIn.Update(tea.PasteMsg{Content: msg.Content})
				return m, cmd
			case StateURL:
				var cmd tea.Cmd
				m.urlIn, cmd = m.urlIn.Update(tea.PasteMsg{Content: msg.Content})
				return m, cmd
			case StateAPIKey:
				var cmd tea.Cmd
				m.keyIn, cmd = m.keyIn.Update(tea.PasteMsg{Content: msg.Content})
				return m, cmd
			}
		}
		return m, nil

	case tea.MouseMsg:
		// Right-click paste: request clipboard via OSC52
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseRight {
			// Only handle right-click when a textinput is active
			if m.state == StateCustomModel || m.state == StateURL || m.state == StateAPIKey {
				return m, tea.ReadClipboard
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		switch m.state {
		case StateProvider:
			// When filtering, let keys pass through to the list
			if m.providerList.FilterState() == list.Filtering {
				var cmd tea.Cmd
				m.providerList, cmd = m.providerList.Update(msg)
				cmds = append(cmds, cmd)
				return m, tea.Batch(cmds...)
			}
			if msg.String() == "enter" {
				sel := m.providerList.SelectedItem()
				if sel != nil {
					p := sel.(providerItem).profile
					m.selectedProvider = &p
					return m.advanceAfterProvider()
				}
			}
			var cmd tea.Cmd
			m.providerList, cmd = m.providerList.Update(msg)
			cmds = append(cmds, cmd)

		case StateCustomModel:
			if msg.String() == "enter" {
				val := strings.TrimSpace(m.customModelIn.Value())
				if val != "" {
					m.selectedModel = val
					// Custom providers always need a base URL next.
					m.state = StateURL
					m.urlIn.Focus()
					return m, nil
				}
			} else if msg.String() == "esc" {
				m.state = StateProvider
				return m, nil
			}
			var cmd tea.Cmd
			m.customModelIn, cmd = m.customModelIn.Update(msg)
			cmds = append(cmds, cmd)

		case StateURL:
			if msg.String() == "enter" {
				val := strings.TrimSpace(m.urlIn.Value())
				if val == "" && m.selectedProvider.BaseURL != "" {
					val = m.selectedProvider.BaseURL
				}
				if val != "" {
					m.finalURL = val
					return m.advanceAfterURL()
				}
			} else if msg.String() == "esc" {
				// Back to custom-model step if this is a custom provider, else
				// straight to the provider list.
				if m.selectedProvider.NeedURL {
					m.state = StateCustomModel
					m.customModelIn.Focus()
				} else {
					m.state = StateProvider
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.urlIn, cmd = m.urlIn.Update(msg)
			cmds = append(cmds, cmd)

		case StateAPIKey:
			if msg.String() == "enter" {
				val := strings.TrimSpace(m.keyIn.Value())
				if val != "" || !m.selectedProvider.NeedKey {
					m.finalKey = val
					return m.submit()
				} else if m.selectedProvider.NeedKey && val == "" {
					m.err = "API Key is required"
				}
			} else if msg.String() == "esc" {
				switch {
				case m.selectedProvider.NeedURL || m.selectedProvider.BaseURL == "":
					m.state = StateURL
					m.urlIn.Focus()
				default:
					m.state = StateProvider
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.keyIn, cmd = m.keyIn.Update(msg)
			cmds = append(cmds, cmd)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.providerList.SetSize(msg.Width-4, 15)
	}

	// Forward non-key/non-mouse messages (e.g. list.FilterMatchesMsg) to the
	// active list (only the provider list remains now).
	if _, isKey := msg.(tea.KeyPressMsg); !isKey {
		if _, isMouse := msg.(tea.MouseMsg); !isMouse {
			if m.state == StateProvider {
				var cmd tea.Cmd
				m.providerList, cmd = m.providerList.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

// advanceAfterProvider routes the wizard after a provider is picked. Registry
// providers don't need a model selection (a default is auto-picked on submit),
// so they go straight to URL/Key. Custom (OpenAI-compatible) providers need a
// model id and a base URL, so they start at the custom-model step.
func (m SetupModel) advanceAfterProvider() (tea.Model, tea.Cmd) {
	p := m.selectedProvider
	// Custom providers need a model id before anything else.
	if p.NeedURL && m.selectedModel == "" {
		m.state = StateCustomModel
		m.customModelIn.Focus()
		return m, nil
	}
	if p.NeedURL || p.BaseURL == "" {
		m.state = StateURL
		if p.BaseURL != "" {
			m.urlIn.Placeholder = p.BaseURL
		}
		m.urlIn.Focus()
		return m, nil
	}
	m.finalURL = p.BaseURL
	return m.advanceAfterURL()
}

func (m SetupModel) advanceAfterURL() (tea.Model, tea.Cmd) {
	if m.selectedProvider.NeedKey {
		// Check if this exact provider already has an API key in config
		if existingKey := m.findProviderAPIKey(); existingKey != "" {
			// Auto-use the existing key, skip the input step
			m.finalKey = existingKey
			return m.submit()
		}
		m.state = StateAPIKey
		m.keyIn.Focus()
		return m, nil
	}
	// skip key
	m.finalKey = ""
	return m.submit()
}

// findProviderAPIKey checks existing config and environment variables for an API key.
func (m SetupModel) findProviderAPIKey() string {
	// Check config first
	if cfg, err := config.LoadConfig(); err == nil && cfg != nil {
		providers := cfg.GetProviders()
		if pCfg, ok := providers[m.selectedProvider.ID]; ok && pCfg.APIKey != "" {
			return pCfg.APIKey
		}
	}
	// Check environment variables from registry
	if m.registry != nil {
		envVars := m.registry.GetProviderEnvVars(m.selectedProvider.ID)
		for _, envVar := range envVars {
			if val := os.Getenv(envVar); val != "" {
				return val
			}
		}
	}
	return ""
}

// containsCustomModel reports whether a model id is already registered on the
// provider config, used to avoid duplicate custom-model entries on re-setup.
func containsCustomModel(models []config.CustomModelConfig, id string) bool {
	for _, cm := range models {
		if cm.ID == id {
			return true
		}
	}
	return false
}

func (m SetupModel) submit() (tea.Model, tea.Cmd) {
	cfg, err := config.LoadConfig()
	if err != nil {
		// New config
		cfg = &config.Config{
			Providers:     make(map[string]*config.ProviderConfig),
			MaxIterations: 1000,
		}
	}

	pID := m.selectedProvider.ID

	if cfg.Providers == nil {
		cfg.Providers = make(map[string]*config.ProviderConfig)
		// Migrate legacy Models into Providers
		for k, v := range cfg.Models { //nolint:staticcheck
			cfg.Providers[k] = v
		}
	}

	pCfg, exists := cfg.Providers[pID]
	if !exists {
		pCfg = &config.ProviderConfig{}
		cfg.Providers[pID] = pCfg
	}

	pCfg.APIKey = m.finalKey
	pCfg.BaseURL = m.finalURL

	// Resolve the active model. For custom (OpenAI-compatible) providers the
	// user entered a model id; for registry providers a default is auto-picked.
	modelID := m.selectedModel
	if modelID == "" && m.registry != nil {
		modelID = m.registry.PickDefaultModel(pID)
	}
	if modelID == "" {
		m.err = "No default model available for " + pID + "; please select a model in Settings"
		return m, nil
	}
	// For custom providers, register the model so it's selectable afterwards.
	if m.selectedProvider.NeedURL {
		if pCfg.Name == "" {
			pCfg.Name = m.selectedProvider.Name
		}
		if !containsCustomModel(pCfg.CustomModels, modelID) {
			pCfg.CustomModels = append(pCfg.CustomModels, config.CustomModelConfig{
				ID:       modelID,
				Name:     modelID,
				ToolCall: true,
			})
		}
	}
	cfg.Model = pID + "/" + modelID

	if err := config.SaveConfig(cfg); err != nil {
		m.err = fmt.Sprintf("Failed to save config: %v", err)
		return m, nil
	}

	m.done = true
	return m, tea.Quit
}

func (m SetupModel) View() tea.View {
	w := m.width
	if w <= 0 {
		w = 80
	}

	logo := lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render("▓▒░ JCODE — Setup ░▒▓")
	subtitle := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).Render("Follow the wizard to configure your LLM")
	header := lipgloss.JoinVertical(lipgloss.Center, logo, subtitle)
	header = lipgloss.NewStyle().Width(w).Align(lipgloss.Center).PaddingTop(2).PaddingBottom(1).Render(header)

	var content string
	switch m.state {
	case StateProvider:
		content = m.providerList.View()
	case StateCustomModel:
		content = m.customModelIn.View()
		if m.selectedProvider != nil {
			content += "\n  " + lipgloss.NewStyle().Foreground(colorMuted).Render("Model id for "+m.selectedProvider.Name)
		}
	case StateURL:
		content = m.urlIn.View()
		if m.selectedProvider.BaseURL != "" {
			content += "\n  " + lipgloss.NewStyle().Foreground(colorMuted).Render("(Leave empty to use default: "+m.selectedProvider.BaseURL+")")
		}
	case StateAPIKey:
		content = m.keyIn.View()
	}

	if m.state != StateProvider {
		var helpText string
		if m.state == StateAPIKey {
			helpText = "  Press Enter to submit, Esc to go back. Paste: Ctrl+Shift+V (Win/Linux) or Cmd+V (Mac)"
		} else {
			helpText = "  Press Enter to submit, Esc to go back."
		}
		content = "\n" + content + "\n\n" + helpText
	}

	errLine := ""
	if m.err != "" {
		errLine = "\n" + lipgloss.NewStyle().PaddingLeft(2).Foreground(colorError).Bold(true).Render("  ⚠ "+m.err)
	}

	helpLine := lipgloss.NewStyle().Foreground(colorMuted).PaddingLeft(2).Render("  Ctrl+C quit")
	cfgPath := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).PaddingLeft(2).Render("  Config: " + config.ConfigPath())

	result := lipgloss.JoinVertical(lipgloss.Left,
		header,
		divider(w-4),
		"\n",
		lipgloss.NewStyle().PaddingLeft(2).Render(content),
		errLine,
		"\n",
		divider(w-4),
		helpLine,
		cfgPath,
	)
	v := tea.NewView(result)
	v.AltScreen = true
	return v
}

func (m SetupModel) IsDone() bool {
	return m.done
}

func RunSetupTUI() (bool, error) {
	m := NewSetupModel()
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return false, err
	}
	if sm, ok := finalModel.(SetupModel); ok {
		return sm.IsDone(), nil
	}
	return false, nil
}
