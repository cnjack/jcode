package model

//go:generate go run ../../script/generate_models.go

import (
	"strings"
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

// ModelRegistry provides model metadata from models.dev.
// The data is statically generated at build time via go:generate.
type ModelRegistry struct {
}

// NewModelRegistry creates a new ModelRegistry.
func NewModelRegistry() *ModelRegistry {
	return &ModelRegistry{}
}

// Load returns the statically generated provider/model data.
func (r *ModelRegistry) Load() (map[string]*RegistryProvider, error) {
	return generatedProviders, nil
}

// LookupModel finds a model by "provider/model" identifier.
// Returns the provider info, model info, and whether it was found.
func (r *ModelRegistry) LookupModel(providerID, modelID string) (*RegistryProvider, *RegistryModel, bool) {
	providers := generatedProviders

	prov, ok := providers[providerID]
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
	return generatedProviders[providerID]
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
	// Sort by ID for consistent ordering
	sortModelsByID(models)
	return models
}

// HasProvider returns whether the given provider ID exists in the registry.
func (r *ModelRegistry) HasProvider(providerID string) bool {
	return r.GetProvider(providerID) != nil
}

// ListProviders returns all providers in the curated display order.
func (r *ModelRegistry) ListProviders() []*RegistryProvider {
	result := make([]*RegistryProvider, 0, len(generatedProviderOrder))
	for _, id := range generatedProviderOrder {
		if prov, ok := generatedProviders[id]; ok {
			result = append(result, prov)
		}
	}
	return result
}

// sortModelsByID sorts a slice of RegistryModel by ID in-place.
func sortModelsByID(models []*RegistryModel) {
	// Simple bubble sort for small lists
	for i := 0; i < len(models); i++ {
		for j := i + 1; j < len(models); j++ {
			if models[i].ID > models[j].ID {
				models[i], models[j] = models[j], models[i]
			}
		}
	}
}
