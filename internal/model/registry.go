package model

//go:generate go run ../../script/generate_models.go

import (
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

// RegistryProvider represents a provider from models.dev API.
type RegistryProvider struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	Env    []string                  `json:"env"`
	API    string                    `json:"api"`
	Doc    string                    `json:"doc,omitempty"`
	Models map[string]*RegistryModel `json:"models"`
}

// RegistryModel represents a model from models.dev API.
type RegistryModel struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Family           string           `json:"family,omitempty"`
	Attachment       bool             `json:"attachment,omitempty"`
	Reasoning        bool             `json:"reasoning,omitempty"`
	ToolCall         bool             `json:"tool_call,omitempty"`
	StructuredOutput bool             `json:"structured_output,omitempty"`
	Temperature      bool             `json:"temperature,omitempty"`
	Knowledge        string           `json:"knowledge,omitempty"`
	ReleaseDate      string           `json:"release_date,omitempty"`
	LastUpdated      string           `json:"last_updated,omitempty"`
	Modalities       *ModelModalities `json:"modalities,omitempty"`
	OpenWeights      bool             `json:"open_weights,omitempty"`
	Cost             *ModelCost       `json:"cost,omitempty"`
	Limit            *ModelLimit      `json:"limit,omitempty"`
	Status           string           `json:"status,omitempty"`
	Recommended      bool             `json:"recommended,omitempty"`
	DefaultEnabled   bool             `json:"default_enabled,omitempty"`
}

// ModelModalities describes input/output modalities.
type ModelModalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

// ModelCost describes per-token costs in USD per 1M tokens.
type ModelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// ModelLimit describes context window and output limits.
type ModelLimit struct {
	Context int `json:"context"`
	Input   int `json:"input,omitempty"`
	Output  int `json:"output,omitempty"`
}

// ModelRegistry provides model metadata from models.dev and custom config.
// The base data is statically generated at build time via go:generate.
// Custom models from config are merged in at runtime.
type ModelRegistry struct {
	providers     map[string]*RegistryProvider
	providerOrder []string
}

// NewModelRegistry creates a new ModelRegistry with a deep copy of generated data.
// Each RegistryProvider and its Models map are copied so that merging custom models
// at runtime never mutates the shared generatedProviders.
func NewModelRegistry() *ModelRegistry {
	providers := make(map[string]*RegistryProvider, len(generatedProviders))
	for k, v := range generatedProviders {
		providers[k] = deepCopyProvider(v)
	}
	providerOrder := make([]string, len(generatedProviderOrder))
	copy(providerOrder, generatedProviderOrder)
	return &ModelRegistry{
		providers:     providers,
		providerOrder: providerOrder,
	}
}

// deepCopyProvider creates a deep copy of a RegistryProvider, including its Models map.
func deepCopyProvider(src *RegistryProvider) *RegistryProvider {
	if src == nil {
		return nil
	}
	cp := *src // shallow copy of value fields
	// Deep copy Env slice
	if src.Env != nil {
		cp.Env = make([]string, len(src.Env))
		copy(cp.Env, src.Env)
	}
	// Deep copy Models map
	if src.Models != nil {
		cp.Models = make(map[string]*RegistryModel, len(src.Models))
		for mk, mv := range src.Models {
			cp.Models[mk] = deepCopyModel(mv)
		}
	}
	return &cp
}

// deepCopyModel creates a deep copy of a RegistryModel, including pointer fields.
func deepCopyModel(src *RegistryModel) *RegistryModel {
	if src == nil {
		return nil
	}
	cp := *src
	if src.Modalities != nil {
		cp.Modalities = &ModelModalities{}
		if src.Modalities.Input != nil {
			cp.Modalities.Input = make([]string, len(src.Modalities.Input))
			copy(cp.Modalities.Input, src.Modalities.Input)
		}
		if src.Modalities.Output != nil {
			cp.Modalities.Output = make([]string, len(src.Modalities.Output))
			copy(cp.Modalities.Output, src.Modalities.Output)
		}
	}
	if src.Cost != nil {
		costCopy := *src.Cost
		cp.Cost = &costCopy
	}
	if src.Limit != nil {
		limitCopy := *src.Limit
		cp.Limit = &limitCopy
	}
	return &cp
}

// NewModelRegistryWithConfig creates a ModelRegistry and merges custom models from config.
func NewModelRegistryWithConfig(cfg *config.Config) *ModelRegistry {
	r := NewModelRegistry()
	if cfg != nil {
		r.MergeConfigProviders(cfg.GetProviders())
	}
	return r
}

// MergeConfigProviders merges custom models from config providers into the registry.
// For providers not in the registry, a new entry is created.
// For existing providers, custom models are added (existing models are not overridden).
func (r *ModelRegistry) MergeConfigProviders(providers map[string]*config.ProviderConfig) {
	for provID, provCfg := range providers {
		if len(provCfg.CustomModels) == 0 {
			continue
		}

		prov, exists := r.providers[provID]
		if !exists {
			name := provCfg.Name
			if name == "" {
				name = provID
			}
			// Derive an env-var name so that the setup wizard's len(rp.Env)>0 check
			// correctly prompts for an API key for this provider.
			envKey := strings.ToUpper(strings.ReplaceAll(provID, "-", "_")) + "_API_KEY"
			prov = &RegistryProvider{
				ID:     provID,
				Name:   name,
				API:    provCfg.BaseURL,
				Env:    []string{envKey},
				Models: make(map[string]*RegistryModel),
			}
			r.providers[provID] = prov
			r.providerOrder = append(r.providerOrder, provID)
		}

		for _, cm := range provCfg.CustomModels {
			if _, exists := prov.Models[cm.ID]; exists {
				continue
			}
			name := cm.Name
			if name == "" {
				name = cm.ID
			}
			rm := &RegistryModel{
				ID:             cm.ID,
				Name:           name,
				ToolCall:       cm.ToolCall,
				Reasoning:      cm.Reasoning,
				DefaultEnabled: true,
			}
			if cm.Context > 0 {
				rm.Limit = &ModelLimit{Context: cm.Context}
			}
			prov.Models[cm.ID] = rm
		}
	}
}

// Load returns the provider/model data.
func (r *ModelRegistry) Load() (map[string]*RegistryProvider, error) {
	return r.providers, nil
}

// LookupModel finds a model by "provider/model" identifier.
// Returns the provider info, model info, and whether it was found.
func (r *ModelRegistry) LookupModel(providerID, modelID string) (*RegistryProvider, *RegistryModel, bool) {
	prov, ok := r.providers[providerID]
	if !ok {
		return nil, nil, false
	}

	model, ok := prov.Models[modelID]
	if !ok {
		// Try partial match for model names with prefixes
		for mid, m := range prov.Models {
			if strings.HasSuffix(mid, "/"+modelID) || strings.HasPrefix(mid, modelID) {
				return prov, m, true
			}
		}
		return prov, nil, false
	}
	return prov, model, true
}

// GetModelContextLimit returns the context limit for a model looked up via registry.
func (r *ModelRegistry) GetModelContextLimit(providerID, modelID string) int {
	_, m, ok := r.LookupModel(providerID, modelID)
	if !ok || m == nil || m.Limit == nil {
		return 0
	}
	return m.Limit.Context
}

// GetModelCost returns pricing info for a model.
func (r *ModelRegistry) GetModelCost(providerID, modelID string) (inputPer1M, outputPer1M float64) {
	_, m, ok := r.LookupModel(providerID, modelID)
	if !ok || m == nil || m.Cost == nil {
		return 0, 0
	}
	return m.Cost.Input, m.Cost.Output
}

// GetProviderAPI returns the API base URL for a provider from the registry.
func (r *ModelRegistry) GetProviderAPI(providerID string) string {
	prov := r.GetProvider(providerID)
	if prov == nil {
		return ""
	}
	return prov.API
}

// GetProviderEnvVars returns the environment variable names for a provider.
func (r *ModelRegistry) GetProviderEnvVars(providerID string) []string {
	prov := r.GetProvider(providerID)
	if prov == nil {
		return nil
	}
	return prov.Env
}

// GetProvider returns provider info by ID, or nil if not found.
func (r *ModelRegistry) GetProvider(providerID string) *RegistryProvider {
	return r.providers[providerID]
}

// ListProviderModels returns models for a provider from the registry.
// If toolCallOnly is true, only models with tool_call support are returned.
// Models are sorted by ID.
func (r *ModelRegistry) ListProviderModels(providerID string, toolCallOnly bool) []*RegistryModel {
	prov := r.GetProvider(providerID)
	if prov == nil {
		return nil
	}
	models := make([]*RegistryModel, 0, len(prov.Models))
	for _, m := range prov.Models {
		if toolCallOnly && !m.ToolCall {
			continue
		}
		models = append(models, m)
	}
	// Sort: recommended first, then by ID
	sortModels(models)
	return models
}

// HasProvider returns whether the given provider ID exists in the registry.
func (r *ModelRegistry) HasProvider(providerID string) bool {
	return r.GetProvider(providerID) != nil
}

// ListProviders returns all providers in the curated display order.
func (r *ModelRegistry) ListProviders() []*RegistryProvider {
	result := make([]*RegistryProvider, 0, len(r.providerOrder))
	for _, id := range r.providerOrder {
		if prov, ok := r.providers[id]; ok {
			result = append(result, prov)
		}
	}
	return result
}

// sortModels sorts models: recommended first, then by ID.
func sortModels(models []*RegistryModel) {
	for i := 0; i < len(models); i++ {
		for j := i + 1; j < len(models); j++ {
			iRec := models[i].Recommended
			jRec := models[j].Recommended
			if (!iRec && jRec) || (iRec == jRec && models[i].ID > models[j].ID) {
				models[i], models[j] = models[j], models[i]
			}
		}
	}
}

// recommendedModels defines recommended and default-enabled models per provider.
// Key: provider ID, Value: map of model ID → true (recommended + default enabled).
//
// Curated 2026-06-13 toward the current long-context flagships. See
// docs/model-research.md for the per-provider context-window survey behind these
// picks. Model IDs must match the registry exactly or the flag is silently ignored.
var recommendedModels = map[string]map[string]bool{
	// GLM-5.2 (1M window) is Zhipu's newest, injected via additionalModels since
	// it predates its models.dev record. GLM-5.1 stays selectable but unstarred.
	"zhipuai": {
		"glm-5.2": true,
	},
	"zhipuai-coding-plan": {
		"glm-5.2": true,
	},
	"zai": {
		"glm-5.2": true,
	},
	"zai-coding-plan": {
		"glm-5.2": true,
	},
	// DeepSeek-V4-Pro — genuine 1M-token window.
	"deepseek": {
		"deepseek-v4-pro": true,
	},
	// Qwen 3.7 Plus/Max and DeepSeek-V4-Pro all carry 1M windows on Alibaba's
	// endpoint; Kimi-K2.6 (256K) rounds out the agentic options.
	"alibaba-cn": {
		"qwen3.7-plus":    true,
		"qwen3.7-max":     true,
		"deepseek-v4-pro": true,
		"kimi-k2.6":       true,
	},
	"alibaba-coding-plan-cn": {
		"qwen3.7-plus": true,
	},
	// Kimi-K2.7-Code is Moonshot's newest coding model (256K window).
	"moonshotai": {
		"kimi-k2.7-code": true,
	},
	// MiniMax-M3 — 1M-token window (corrected via contextLimitOverrides below).
	"minimax": {
		"MiniMax-M3": true,
	},
	"minimax-coding-plan": {
		"MiniMax-M3": true,
	},
	// GPT-5.5 carries a ~1.05M window.
	"openai": {
		"gpt-5.5": true,
	},
	// Claude Opus 4.8 and Sonnet 4.6 both expose 1M-token windows.
	"anthropic": {
		"claude-opus-4-8":   true,
		"claude-sonnet-4-6": true,
	},
	// Gemini 3.1 Pro — ~1.05M window.
	"google": {
		"gemini-3.1-pro-preview": true,
	},
}

// contextLimitOverrides corrects context windows for built-in models whose
// models.dev-sourced value understates the model's advertised window. Applied at
// init() so corrections survive `go generate` regeneration of registry_generated.go.
// Key: provider ID → model ID → context window (tokens). See docs/model-research.md.
var contextLimitOverrides = map[string]map[string]int{
	// MiniMax-M3 advertises a 1M-token window; models.dev records only the
	// 512K "guaranteed minimum". Use the advertised window for sizing.
	"minimax":             {"MiniMax-M3": 1_000_000},
	"minimax-coding-plan": {"MiniMax-M3": 1_000_000},
}

// glm52Model builds a fresh GLM-5.2 entry. Returns a new object per call so each
// provider gets its own instance (deep-copied again per ModelRegistry).
//
// GLM-5.2 shipped 2026-06-13 to GLM Coding Plan users but isn't on models.dev yet
// (the standalone API / open weights land later), so it can't come through
// registry_generated.go. Spec confirmed from the official Z.ai DevPack config
// ("contextWindow": 1000000, "maxTokens": 131072). The full 1M window requires the
// "glm-5.2[1m]" variant on the Coding Plan endpoint. See docs/model-research.md.
func glm52Model() *RegistryModel {
	return &RegistryModel{
		ID: "glm-5.2", Name: "GLM-5.2", Family: "glm",
		Reasoning: true, ToolCall: true, StructuredOutput: true, Temperature: true,
		OpenWeights: true,
		ReleaseDate: "2026-06-13", LastUpdated: "2026-06-13",
		Modalities:     &ModelModalities{Input: []string{"text"}, Output: []string{"text"}},
		Limit:          &ModelLimit{Context: 1_000_000, Output: 131_072},
		DefaultEnabled: true,
	}
}

// additionalModels injects models into EXISTING generated providers when a model is
// released before models.dev publishes it. Applied at init() and MERGED in — an
// entry is skipped if the provider already defines that model id, so once the
// official record lands in registry_generated.go it transparently takes over.
// Key: provider ID → model ID → model. See docs/model-research.md.
var additionalModels = map[string]map[string]*RegistryModel{
	"zhipuai":             {"glm-5.2": glm52Model()},
	"zhipuai-coding-plan": {"glm-5.2": glm52Model()},
	"zai":                 {"glm-5.2": glm52Model()},
	"zai-coding-plan":     {"glm-5.2": glm52Model()},
}

func init() {
	applyStaticProviders()
	applyAdditionalModels()
	applyContextLimitOverrides()
	applyRecommendedModels()
}

// staticProviders defines providers that are not on models.dev but should be
// built into the registry. They are added to generatedProviders/generatedProviderOrder
// at init time so they behave identically to models.dev providers.
var staticProviders = map[string]*RegistryProvider{
	"tencent-tokenhub-ep": {
		ID:   "tencent-tokenhub-ep",
		Name: "Tencent TokenHub Enterprise",
		Env:  []string{"TENCENT_TOKENHUB_EP_API_KEY"},
		API:  "https://tokenhub.tencentmaas.com/plan/v3",
		Models: map[string]*RegistryModel{
			"auto": {
				ID: "auto", Name: "Auto", Family: "auto",
				ToolCall: true, Temperature: true,
				DefaultEnabled: true,
				Modalities:     &ModelModalities{Input: []string{"text"}, Output: []string{"text"}},
			},
			"glm-5": {
				ID: "glm-5", Name: "GLM-5", Family: "glm",
				ToolCall: true, Temperature: true,
				DefaultEnabled: true,
				Modalities:     &ModelModalities{Input: []string{"text"}, Output: []string{"text"}},
			},
			"glm-5.1": {
				ID: "glm-5.1", Name: "GLM-5.1", Family: "glm",
				ToolCall: true, Temperature: true,
				DefaultEnabled: true, Recommended: true,
				Modalities: &ModelModalities{Input: []string{"text"}, Output: []string{"text"}},
			},
			"glm-5-turbo": {
				ID: "glm-5-turbo", Name: "GLM-5-Turbo", Family: "glm",
				ToolCall: true, Temperature: true,
				DefaultEnabled: true,
				Modalities:     &ModelModalities{Input: []string{"text"}, Output: []string{"text"}},
			},
			"kimi-k2.5": {
				ID: "kimi-k2.5", Name: "Kimi-K2.5", Family: "kimi",
				Reasoning: true, ToolCall: true, Temperature: true,
				DefaultEnabled: true,
				Modalities:     &ModelModalities{Input: []string{"text"}, Output: []string{"text"}},
			},
			"kimi-k2.6": {
				ID: "kimi-k2.6", Name: "Kimi-K2.6", Family: "kimi",
				Reasoning: true, ToolCall: true, Temperature: true,
				DefaultEnabled: true, Recommended: true,
				Modalities: &ModelModalities{Input: []string{"text"}, Output: []string{"text"}},
			},
			"minimax-m2.5": {
				ID: "minimax-m2.5", Name: "MiniMax-M2.5", Family: "minimax",
				Reasoning: true, ToolCall: true, Temperature: true,
				DefaultEnabled: true,
				Modalities:     &ModelModalities{Input: []string{"text"}, Output: []string{"text"}},
			},
			"minimax-m2.7": {
				ID: "minimax-m2.7", Name: "MiniMax-M2.7", Family: "minimax",
				Reasoning: true, ToolCall: true, Temperature: true,
				DefaultEnabled: true, Recommended: true,
				Modalities: &ModelModalities{Input: []string{"text"}, Output: []string{"text"}},
			},
			"deepseek-v4-flash": {
				ID: "deepseek-v4-flash", Name: "DeepSeek-V4-Flash", Family: "deepseek",
				Reasoning: true, ToolCall: true, Temperature: true,
				DefaultEnabled: true,
				Modalities:     &ModelModalities{Input: []string{"text"}, Output: []string{"text"}},
			},
			"deepseek-v4-pro": {
				ID: "deepseek-v4-pro", Name: "DeepSeek-V4-Pro", Family: "deepseek",
				Reasoning: true, ToolCall: true, Temperature: true,
				DefaultEnabled: true, Recommended: true,
				Modalities: &ModelModalities{Input: []string{"text"}, Output: []string{"text"}},
			},
		},
	},
}

// staticProviderOrder defines the display order for static providers.
// They are appended after the generated providers.
var staticProviderOrder = []string{
	"tencent-tokenhub-ep",
}

// applyStaticProviders merges staticProviders into generatedProviders.
func applyStaticProviders() {
	for id, prov := range staticProviders {
		generatedProviders[id] = prov
	}
	generatedProviderOrder = append(generatedProviderOrder, staticProviderOrder...)
}

// applyAdditionalModels merges hand-maintained models into existing generated
// providers, skipping any model id the provider already defines (so the official
// models.dev record wins once it lands).
func applyAdditionalModels() {
	for provID, models := range additionalModels {
		prov, ok := generatedProviders[provID]
		if !ok {
			continue
		}
		if prov.Models == nil {
			prov.Models = make(map[string]*RegistryModel, len(models))
		}
		for modelID, m := range models {
			if _, exists := prov.Models[modelID]; exists {
				continue
			}
			prov.Models[modelID] = m
		}
	}
}

// applyContextLimitOverrides patches context windows for built-in models whose
// generated value understates the model's real advertised window.
func applyContextLimitOverrides() {
	for provID, models := range contextLimitOverrides {
		prov, ok := generatedProviders[provID]
		if !ok {
			continue
		}
		for modelID, ctx := range models {
			m, ok := prov.Models[modelID]
			if !ok {
				continue
			}
			if m.Limit == nil {
				m.Limit = &ModelLimit{}
			}
			m.Limit.Context = ctx
		}
	}
}

// applyRecommendedModels sets Recommended and DefaultEnabled on models in the generated registry.
func applyRecommendedModels() {
	for provID, models := range recommendedModels {
		prov, ok := generatedProviders[provID]
		if !ok {
			continue
		}
		for modelID := range models {
			if m, ok := prov.Models[modelID]; ok {
				m.Recommended = true
				m.DefaultEnabled = true
			}
		}
	}
}
