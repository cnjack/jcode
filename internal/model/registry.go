package model

//go:generate go run ../../script/generate_models.go

import (
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

// RegistryProvider represents a provider from models.dev API.
type RegistryProvider struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Env  []string `json:"env"`
	API  string   `json:"api"`
	Doc  string   `json:"doc,omitempty"`
	// AuthMethods declares the authentication choices the provider supports.
	// It is product metadata consumed by Setup/Settings; model transports still
	// validate and enforce the selected method independently.
	AuthMethods []string                  `json:"auth_methods,omitempty"`
	Models      map[string]*RegistryModel `json:"models"`
	// Custom is true for providers that exist only because the user configured
	// them (an OpenAI-compatible endpoint not in models.dev), as opposed to a
	// built-in registry brand. Set during MergeConfigProviders.
	Custom bool `json:"custom,omitempty"`
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
	// ReasoningOptions describes how this model exposes its thinking controls,
	// mirroring models.dev's reasoning_options. Empty ⇒ no reasoning controls.
	ReasoningOptions []ReasoningOption `json:"reasoning_options,omitempty"`
}

// ReasoningOption is one reasoning/thinking control a model supports, from
// models.dev's reasoning_options. Type is one of:
//   - "effort"        — Values lists the supported effort levels (e.g. low/medium/high/xhigh/max)
//   - "toggle"        — reasoning can be switched on/off, no extra parameters
//   - "budget_tokens" — a thinking token budget bounded by Min/Max (nil ⇒ open-ended)
type ReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values,omitempty"`
	Min    *int     `json:"min,omitempty"`
	Max    *int     `json:"max,omitempty"`
}

// intPtr returns a pointer to i. Used by the generated registry to carry
// nullable reasoning_options bounds (Min/Max).
func intPtr(i int) *int { return &i }

// ModelModalities describes input/output modalities.
type ModelModalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

// SupportsImageInput reports whether the model advertises "image" among its
// input modalities. Returns false when modalities are unknown (nil) — callers
// that need a permissive default for unknown models must handle nil
// Modalities themselves.
func (m *RegistryModel) SupportsImageInput() bool {
	if m == nil || m.Modalities == nil {
		return false
	}
	for _, mod := range m.Modalities.Input {
		if mod == "image" {
			return true
		}
	}
	return false
}

// lookupStaticModel finds a model in the static registry (generated data +
// init-time merges) without deep-copying it. Read-only lookups only. Custom
// providers/models merged at runtime are not visible here — callers should
// treat a miss as "unknown", not "absent".
func lookupStaticModel(providerID, modelID string) *RegistryModel {
	prov, ok := generatedProviders[providerID]
	if !ok {
		return nil
	}
	if m, ok := prov.Models[modelID]; ok {
		return m
	}
	// Mirror LookupModel's partial matching for prefixed model ids.
	for mid, m := range prov.Models {
		if strings.HasSuffix(mid, "/"+modelID) || strings.HasPrefix(mid, modelID) {
			return m
		}
	}
	return nil
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
	if src.AuthMethods != nil {
		cp.AuthMethods = append([]string(nil), src.AuthMethods...)
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
				Custom: true,
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
				Attachment:     cm.Attachment,
				DefaultEnabled: !cm.Managed,
			}
			// A custom model flagged as reasoning gets the standard OpenAI-compatible
			// effort levels, so the chat picker's effort control can render for it.
			// Custom models not flagged reasoning stay without reasoning_options —
			// the effort control is hidden for them, matching "not specified ⇒ none".
			// When EffortTiers is provided, it replaces the standard set so users can
			// configure exactly which effort levels (e.g. high/max) a model offers.
			if cm.Reasoning {
				if len(cm.EffortTiers) > 0 {
					rm.ReasoningOptions = []ReasoningOption{{Type: "effort", Values: cm.EffortTiers}}
				} else {
					rm.ReasoningOptions = standardEffortOptions()
				}
			}
			if cm.Context > 0 {
				rm.Limit = &ModelLimit{Context: cm.Context}
			}
			// Managed rows persist only what the live catalog last wrote. A newer
			// Grok/Codex ID often lands before the static registry lists it, so
			// omitted capabilities inherit from the closest baked-in sibling
			// (grok-4.6 ← grok-4.5) instead of rendering as text-only.
			if cm.Managed {
				applyRelatedManagedModelDefaults(rm, RelatedRegistryModel(prov, cm.ID))
			}
			prov.Models[cm.ID] = rm
		}
	}
}

// RelatedRegistryModel returns a baked-in sibling that shares a versioned
// family key with modelID. grok-4.6 matches grok-4.5; grok-imagine-image
// does not. Used when a live managed catalog lists an ID the static
// registry has not been updated to include yet.
//
// Candidates come from immutable generatedProviders, never from the live
// provider.Models map, so an earlier custom row cannot pollute inheritance.
func RelatedRegistryModel(provider *RegistryProvider, modelID string) *RegistryModel {
	if provider == nil {
		return nil
	}
	baked := generatedProviders[provider.ID]
	if baked == nil {
		return nil
	}
	return relatedBakedInModel(baked.Models, modelID)
}

func relatedBakedInModel(models map[string]*RegistryModel, modelID string) *RegistryModel {
	key := managedModelFamilyKey(modelID)
	if key == "" || models == nil {
		return nil
	}
	targetParts := managedModelVersionParts(modelID)
	var related *RegistryModel
	var relatedDist int
	for _, candidate := range models {
		if candidate == nil || candidate.ID == modelID || managedModelFamilyKey(candidate.ID) != key {
			continue
		}
		dist := managedModelVersionDistance(managedModelVersionParts(candidate.ID), targetParts)
		if !preferRelatedModel(related, relatedDist, candidate, dist) {
			continue
		}
		related = candidate
		relatedDist = dist
	}
	return related
}

func preferRelatedModel(current *RegistryModel, currentDist int, candidate *RegistryModel, candidateDist int) bool {
	if current == nil {
		return true
	}
	if candidateDist != currentDist {
		return candidateDist < currentDist
	}
	if candidate.DefaultEnabled != current.DefaultEnabled {
		return candidate.DefaultEnabled
	}
	return candidate.ID < current.ID
}

func managedModelFamilyKey(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	for i := 0; i < len(id); i++ {
		if id[i] != '-' || i+1 >= len(id) || id[i+1] < '0' || id[i+1] > '9' {
			continue
		}
		end := i + 1
		for end < len(id) && id[end] >= '0' && id[end] <= '9' {
			end++
		}
		return id[:end]
	}
	return ""
}

func managedModelVersionParts(id string) []int {
	id = strings.ToLower(strings.TrimSpace(id))
	i := 0
	for i < len(id) {
		if id[i] == '-' && i+1 < len(id) && id[i+1] >= '0' && id[i+1] <= '9' {
			i++
			break
		}
		i++
	}
	var parts []int
	for i < len(id) {
		if id[i] < '0' || id[i] > '9' {
			if id[i] == '.' || id[i] == '-' {
				i++
				continue
			}
			break
		}
		n := 0
		for i < len(id) && id[i] >= '0' && id[i] <= '9' {
			n = n*10 + int(id[i]-'0')
			i++
		}
		parts = append(parts, n)
	}
	return parts
}

func managedModelVersionDistance(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	dist := 0
	weights := [...]int{1_000_000, 1_000, 1}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		d := av - bv
		if d < 0 {
			d = -d
		}
		w := 1
		if i < len(weights) {
			w = weights[i]
		}
		dist += d * w
	}
	return dist
}

func applyRelatedManagedModelDefaults(dst *RegistryModel, related *RegistryModel) {
	if dst == nil || related == nil {
		return
	}
	if related.Attachment {
		dst.Attachment = true
		if dst.Modalities == nil && related.Modalities != nil {
			dst.Modalities = deepCopyModel(related).Modalities
		}
	}
	if !dst.Reasoning && related.Reasoning {
		dst.Reasoning = true
		if len(dst.ReasoningOptions) == 0 && len(related.ReasoningOptions) > 0 {
			dst.ReasoningOptions = append([]ReasoningOption(nil), related.ReasoningOptions...)
		}
	}
	if dst.Limit == nil && related.Limit != nil && related.Limit.Context > 0 {
		limitCopy := *related.Limit
		dst.Limit = &limitCopy
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

// GetModelCacheCost returns the cache-read and cache-write prices (USD per 1M
// tokens) for a model, or 0 when the registry has no cache pricing for it.
func (r *ModelRegistry) GetModelCacheCost(providerID, modelID string) (cacheReadPer1M, cacheWritePer1M float64) {
	_, m, ok := r.LookupModel(providerID, modelID)
	if !ok || m == nil || m.Cost == nil {
		return 0, 0
	}
	return m.Cost.CacheRead, m.Cost.CacheWrite
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

// PickDefaultModel returns the best default model id for a provider, used when
// setup completes without an explicit model selection (the wizard no longer
// forces a model pick). Selection order: first DefaultEnabled model, then the
// first Recommended model, then simply the first model. Returns "" when the
// provider is unknown or has no models (e.g. a custom OpenAI-compatible
// provider) — callers must then require an explicit model id.
func (r *ModelRegistry) PickDefaultModel(providerID string) string {
	models := r.ListProviderModels(providerID, false)
	for _, m := range models {
		if m.DefaultEnabled {
			return m.ID
		}
	}
	for _, m := range models {
		if m.Recommended {
			return m.ID
		}
	}
	if len(models) > 0 {
		return models[0].ID
	}
	return ""
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
// internal-doc/model-research.md for the per-provider context-window survey behind these
// picks. Model IDs must match the registry exactly or the flag is silently ignored.
var recommendedModels = map[string]map[string]bool{
	// GLM-5.2 (1M window) is Zhipu's newest flagship. GLM-5.1 stays selectable
	// but unstarred.
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
// models.dev-sourced value misstates the model's real window — either understating
// it (a conservative floor) or overstating it (bad upstream data). Applied at
// init() so corrections survive `go generate` regeneration of registry_generated.go.
// Key: provider ID → model ID → context window (tokens). See internal-doc/model-research.md.
var contextLimitOverrides = map[string]map[string]int{
	// MiniMax-M3 advertises a 1M-token window; models.dev records only the
	// 512K "guaranteed minimum". Use the advertised window for sizing.
	"minimax":             {"MiniMax-M3": 1_000_000},
	"minimax-coding-plan": {"MiniMax-M3": 1_000_000},
	// models.dev's openrouter records carry transposed digits for these two, which
	// would size the context above the real window and fail requests near the edge.
	// Both corrected values are what the same models report under their native
	// providers (google / alibaba-cn), and each is a power of two — 2^20 and 2^17 —
	// while the upstream 1048756 / 131702 are not. Fixed here rather than in the
	// generated file, which is overwritten by `make generate`.
	"openrouter": {
		"google/gemini-3.1-pro-preview-customtools": 1_048_576,
		"qwen/qwen3-14b": 131_072,
	},
}

func init() {
	applyStaticProviders()
	applyContextLimitOverrides()
	applyRecommendedModels()
}

// staticProviders defines providers that are not on models.dev but should be
// built into the registry. They are added to generatedProviders/generatedProviderOrder
// at init time so they behave identically to models.dev providers.
var staticProviders = map[string]*RegistryProvider{
	// xAI's official API supports both ordinary API keys and the managed OAuth
	// device flow used by Grok clients. OAuth runtime policy pins Responses at
	// api.x.ai; this registry entry supplies setup/catalog metadata only.
	"xai": {
		ID: "xai", Name: "xAI (Grok)", Env: []string{"XAI_API_KEY"},
		API: "https://api.x.ai/v1", AuthMethods: []string{"api_key", "xai_oauth"},
		Models: map[string]*RegistryModel{
			"grok-4.5": {
				ID: "grok-4.5", Name: "Grok 4.5", Family: "grok",
				Reasoning: true, ToolCall: true, Attachment: true,
				DefaultEnabled: true, Recommended: true,
				Modalities: &ModelModalities{Input: []string{"text", "image"}, Output: []string{"text"}},
				Limit:      &ModelLimit{Context: 256000}, ReasoningOptions: standardEffortOptions(),
			},
		},
	},
	// Copilot authentication is account-only. The live service may advertise a
	// wider catalog; this conservative baseline gives first-run setup a model
	// before live catalog refresh is available.
	"github-copilot": {
		ID: "github-copilot", Name: "GitHub Copilot",
		API: "https://api.githubcopilot.com", AuthMethods: []string{"github_copilot"},
		Models: map[string]*RegistryModel{
			"gpt-4.1": {
				ID: "gpt-4.1", Name: "GPT-4.1", Family: "gpt",
				ToolCall: true, Attachment: true, DefaultEnabled: true, Recommended: true,
				Modalities: &ModelModalities{Input: []string{"text", "image"}, Output: []string{"text"}},
				Limit:      &ModelLimit{Context: 128000},
			},
		},
	},
	// Kimi For Coding is Moonshot's subscription coding plan. models.dev carries a
	// "kimi-for-coding" record, but its model ids (k2p5/k2p6/k2p7/kimi-k2-thinking)
	// are undocumented aliases that the vendor's own /models endpoint does not
	// advertise, so the provider is hand-written here instead of pulled through
	// generate_models.go. The three ids below are the ones the official
	// OpenAI-compatible setup documents. Verified against the live endpoint
	// 2026-07-15: tool calls work, reasoning_effort is honored, and every model
	// rejects temperature != 1 (hence Temperature stays false — note models.dev
	// wrongly reports temperature:true for three of its four ids).
	"kimi-for-coding": {
		ID:   "kimi-for-coding",
		Name: "Kimi For Coding",
		Env:  []string{"KIMI_API_KEY"},
		API:  "https://api.kimi.com/coding/v1",
		Doc:  "https://www.kimi.com/code/docs/third-party-tools/other-coding-agents.html",
		Models: map[string]*RegistryModel{
			// K3 is Moonshot's newest flagship: a 1,048,576-token context (4x the
			// kimi-for-coding window) with deep reasoning on by default. Per the docs
			// its thinking depth is the only one configurable via reasoning_effort,
			// and only "max" is currently accepted (low/high are documented as
			// planned but not yet live), so ReasoningOptions offers "max" alone.
			// Requires a Moderato-or-above subscription (kimi-for-coding also works
			// on the lower Andante tier). Output limit is unstated in the docs; it
			// carries the family's 32768 default pending live confirmation.
			"k3": {
				ID: "k3", Name: "Kimi K3", Family: "kimi",
				Attachment: true, Reasoning: true, ToolCall: true,
				DefaultEnabled: true, Recommended: true,
				Modalities:       &ModelModalities{Input: []string{"text", "image", "video"}, Output: []string{"text"}},
				Limit:            &ModelLimit{Context: 1048576, Output: 32768},
				ReasoningOptions: []ReasoningOption{{Type: "effort", Values: []string{"max"}}},
			},
			"kimi-for-coding": {
				ID: "kimi-for-coding", Name: "Kimi For Coding", Family: "kimi",
				Attachment: true, Reasoning: true, ToolCall: true,
				DefaultEnabled:   true,
				Modalities:       &ModelModalities{Input: []string{"text", "image", "video"}, Output: []string{"text"}},
				Limit:            &ModelLimit{Context: 262144, Output: 32768},
				ReasoningOptions: standardEffortOptions(),
			},
			// High-speed tier: ~5-6x output speed at ~3x quota burn, and it needs an
			// Allegretto-or-above subscription, so it is selectable but not starred.
			"kimi-for-coding-highspeed": {
				ID: "kimi-for-coding-highspeed", Name: "Kimi For Coding (High-Speed)", Family: "kimi",
				Attachment: true, Reasoning: true, ToolCall: true,
				DefaultEnabled:   true,
				Modalities:       &ModelModalities{Input: []string{"text", "image", "video"}, Output: []string{"text"}},
				Limit:            &ModelLimit{Context: 262144, Output: 32768},
				ReasoningOptions: standardEffortOptions(),
			},
		},
	},
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
	"xai",
	"github-copilot",
	"kimi-for-coding",
	"tencent-tokenhub-ep",
}

// applyStaticProviders merges staticProviders into generatedProviders.
func applyStaticProviders() {
	for id, prov := range staticProviders {
		generatedProviders[id] = prov
	}
	generatedProviderOrder = append(generatedProviderOrder, staticProviderOrder...)
	// OpenAI keeps its generated model catalog while gaining the opt-in
	// ChatGPT/Codex account login alongside its existing API-key path.
	if openAI := generatedProviders["openai"]; openAI != nil {
		openAI.AuthMethods = []string{"api_key", "codex_oauth"}
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

// standardEffortOptions is the reasoning_options applied to custom models the
// user flags as reasoning-capable. These are the effort levels the
// OpenAI-compatible "reasoning_effort" parameter conventionally accepts.
func standardEffortOptions() []ReasoningOption {
	return []ReasoningOption{{
		Type:   "effort",
		Values: []string{"minimal", "low", "medium", "high"},
	}}
}
