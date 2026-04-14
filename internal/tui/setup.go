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

// popularProviderIDs is a curated list of well-known provider IDs from models.dev.
var popularProviderIDs = []string{
	// International providers
	"openai", "anthropic", "google", "deepseek", "mistral",
	"openrouter", "groq", "together-ai", "fireworks-ai", "perplexity",
	// Chinese providers (using models.dev official IDs)
	"alibaba-cn", "alibaba-coding-plan-cn",
	"zhipuai", "zhipuai-coding-plan",
	"moonshotai", "moonshotai-cn",
	"minimax", "minimax-coding-plan",
	"siliconflow",
	"tencent-coding-plan",
}

// ExtraProvider represents a provider not in the models.dev registry.
type ExtraProvider struct {
	ID      string
	Name    string
	BaseURL string
	NoKey   bool // true for local providers like Ollama
	NeedURL bool // true if user must provide URL
}

// extraProviders are popular providers not available in models.dev.
var extraProviders = []ExtraProvider{
	{ID: "ollama", Name: "Ollama (Local)", BaseURL: "http://localhost:11434/v1", NoKey: true},
	{ID: "ollama-cloud", Name: "Ollama Cloud", NeedURL: true},
	// Chinese regional providers not in models.dev
	{ID: "z.ai", Name: "Z.AI", BaseURL: "https://api.z.ai/v1"},
	{ID: "z.ai-plan", Name: "Z.AI Plan", BaseURL: "https://api.z.ai/v1"},
	{ID: "magicark", Name: "魔力方舟", BaseURL: "https://api.gitee.com/v1"},
}

// fallbackProviders are shown when the registry is not available.
var fallbackProviders = []ProviderProfile{
	{ID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.com/v1", NeedKey: true},
	{ID: "anthropic", Name: "Anthropic", BaseURL: "https://api.anthropic.com/v1", NeedKey: true},
	{ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com", NeedKey: true},
	{ID: "zhipuai", Name: "Zhipu AI", BaseURL: "https://open.bigmodel.cn/api/paas/v4", NeedKey: true},
	{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", NeedKey: true},
	{ID: "groq", Name: "Groq", BaseURL: "https://api.groq.com/openai/v1", NeedKey: true},
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

type modelListItem struct {
	name string
	desc string
}

func (i modelListItem) Title() string       { return i.name }
func (i modelListItem) Description() string { return i.desc }
func (i modelListItem) FilterValue() string { return i.name }

type SetupState int

const (
	StateProvider SetupState = iota
	StateModel
	StateCustomModel
	StateURL
	StateAPIKey
)

type SetupModel struct {
	state         SetupState
	providerList  list.Model
	modelList     list.Model
	customModelIn textinput.Model
	urlIn         textinput.Model
	keyIn         textinput.Model

	registry         *model.ModelRegistry
	selectedProvider *ProviderProfile
	selectedModel    string
	finalURL         string
	finalKey         string

	width  int
	height int
	err    string
	done   bool
}

func NewSetupModel() SetupModel {
	m := SetupModel{
		state:    StateProvider,
		registry: model.NewModelRegistry(),
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

	// Try loading registry for provider metadata
	registryProviders, _ := m.registry.Load()

	var items []list.Item

	if len(registryProviders) > 0 {
		// Registry available: show popular providers from registry
		addedIDs := make(map[string]bool)
		for _, pid := range popularProviderIDs {
			rp, ok := registryProviders[pid]
			if !ok {
				continue
			}
			needKey := len(rp.Env) > 0
			items = append(items, providerItem{
				profile: ProviderProfile{
					ID:           pid,
					Name:         rp.Name,
					BaseURL:      rp.API,
					NeedKey:      needKey,
					FromRegistry: true,
				},
				configured: configuredProviders[pid],
			})
			addedIDs[pid] = true
		}

		// Add extra providers (skip if already in registry popular list)
		for _, ep := range extraProviders {
			if addedIDs[ep.ID] {
				continue
			}
			// Check if this extra provider actually exists in registry
			fromRegistry := false
			baseURL := ep.BaseURL
			needKey := !ep.NoKey
			if rp, ok := registryProviders[ep.ID]; ok {
				fromRegistry = true
				if baseURL == "" {
					baseURL = rp.API
				}
				needKey = len(rp.Env) > 0 || needKey
			}
			items = append(items, providerItem{
				profile: ProviderProfile{
					ID:           ep.ID,
					Name:         ep.Name,
					BaseURL:      baseURL,
					NeedURL:      ep.NeedURL,
					NeedKey:      needKey,
					FromRegistry: fromRegistry,
				},
				configured: configuredProviders[ep.ID],
			})
		}
	} else {
		// Registry unavailable: show fallback providers
		for _, fp := range fallbackProviders {
			items = append(items, providerItem{
				profile:    fp,
				configured: configuredProviders[fp.ID],
			})
		}
		// Also add extra providers
		for _, ep := range extraProviders {
			items = append(items, providerItem{
				profile: ProviderProfile{
					ID:      ep.ID,
					Name:    ep.Name,
					BaseURL: ep.BaseURL,
					NeedURL: ep.NeedURL,
					NeedKey: !ep.NoKey,
				},
				configured: configuredProviders[ep.ID],
			})
		}
	}

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

	ml := list.New([]list.Item{}, del, 60, 15)
	ml.SetShowHelp(false)
	m.modelList = ml

	m.customModelIn = textinput.New()
	m.customModelIn.Placeholder = "Enter custom model name..."
	m.customModelIn.Prompt = "Model Name: "
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
			if msg.String() == "enter" {
				sel := m.providerList.SelectedItem()
				if sel != nil {
					p := sel.(providerItem).profile
					m.selectedProvider = &p

					var mItems []list.Item
					// Always try to load models from registry, regardless of initial FromRegistry flag.
					// This handles cases where registry was temporarily unavailable during init.
					models := m.registry.ListProviderModels(p.ID, false)
					if len(models) > 0 {
						for _, rm := range models {
							desc := modelDescription(rm)
							mItems = append(mItems, modelListItem{name: rm.ID, desc: desc})
						}
					}
					// Always offer "Custom..." as the last option
					mItems = append(mItems, modelListItem{name: "Custom...", desc: "Enter a custom model name"})
					m.modelList.SetItems(mItems)
					m.modelList.Title = "Select Model (" + p.Name + ")"

					m.state = StateModel
					return m, nil
				}
			}
			var cmd tea.Cmd
			m.providerList, cmd = m.providerList.Update(msg)
			cmds = append(cmds, cmd)

		case StateModel:
			if msg.String() == "enter" {
				sel := m.modelList.SelectedItem()
				if sel != nil {
					name := sel.(modelListItem).name
					if name == "Custom..." {
						m.state = StateCustomModel
						m.customModelIn.Focus()
					} else {
						m.selectedModel = name
						return m.advanceAfterModel()
					}
					return m, nil
				}
			} else if msg.String() == "esc" {
				m.state = StateProvider
				return m, nil
			}
			var cmd tea.Cmd
			m.modelList, cmd = m.modelList.Update(msg)
			cmds = append(cmds, cmd)

		case StateCustomModel:
			if msg.String() == "enter" {
				val := strings.TrimSpace(m.customModelIn.Value())
				if val != "" {
					m.selectedModel = val
					return m.advanceAfterModel()
				}
			} else if msg.String() == "esc" {
				m.state = StateModel
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
				if m.selectedModel == "Custom..." {
					m.state = StateCustomModel
				} else {
					m.state = StateModel
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
				case m.selectedModel == "Custom...":
					m.state = StateCustomModel
					m.customModelIn.Focus()
				default:
					m.state = StateModel
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
		m.modelList.SetSize(msg.Width-4, 15)
	}

	return m, tea.Batch(cmds...)
}

func (m SetupModel) advanceAfterModel() (tea.Model, tea.Cmd) {
	if m.selectedProvider.NeedURL || m.selectedProvider.BaseURL == "" {
		m.state = StateURL
		if m.selectedProvider.BaseURL != "" {
			m.urlIn.Placeholder = m.selectedProvider.BaseURL
		}
		m.urlIn.Focus()
		return m, nil
	}
	m.finalURL = m.selectedProvider.BaseURL
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

	// Set model in "provider/model" format
	cfg.Model = pID + "/" + m.selectedModel

	if err := config.SaveConfig(cfg); err != nil {
		m.err = fmt.Sprintf("Failed to save config: %v", err)
		return m, nil
	}

	m.done = true
	return m, tea.Quit
}

// modelDescription builds a short description for a registry model.
func modelDescription(rm *model.RegistryModel) string {
	var parts []string
	if rm.Limit != nil && rm.Limit.Context > 0 {
		parts = append(parts, fmt.Sprintf("%dk ctx", rm.Limit.Context/1000))
	}
	if rm.ToolCall {
		parts = append(parts, "tool_call")
	}
	if rm.Reasoning {
		parts = append(parts, "reasoning")
	}
	if rm.Cost != nil && rm.Cost.Input > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f/1M in", rm.Cost.Input))
	}
	if len(parts) == 0 {
		return rm.ID
	}
	return strings.Join(parts, " · ")
}

func (m SetupModel) View() tea.View {
	w := m.width
	if w <= 0 {
		w = 80
	}

	logo := lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render("🚀 Little Jack — Setup")
	subtitle := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).Render("Follow the wizard to configure your LLM")
	header := lipgloss.JoinVertical(lipgloss.Center, logo, subtitle)
	header = lipgloss.NewStyle().Width(w).Align(lipgloss.Center).PaddingTop(2).PaddingBottom(1).Render(header)

	var content string
	switch m.state {
	case StateProvider:
		content = m.providerList.View()
	case StateModel:
		content = m.modelList.View()
	case StateCustomModel:
		content = m.customModelIn.View()
	case StateURL:
		content = m.urlIn.View()
		if m.selectedProvider.BaseURL != "" {
			content += "\n  " + lipgloss.NewStyle().Foreground(colorMuted).Render("(Leave empty to use default: "+m.selectedProvider.BaseURL+")")
		}
	case StateAPIKey:
		content = m.keyIn.View()
	}

	if m.state != StateProvider && m.state != StateModel {
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
