package model

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

const (
	modelsDevURL      = "https://models.dev/api.json"
	registryCacheTTL  = 5 * time.Minute
	registryCacheDir  = "cache"
	registryCacheFile = "models_dev.json"
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
// It fetches the model database on first use and caches it locally.
type ModelRegistry struct {
	mu        sync.RWMutex
	providers map[string]*RegistryProvider
	loadedAt  time.Time
	cacheDir  string
}

// NewModelRegistry creates a new ModelRegistry.
func NewModelRegistry() *ModelRegistry {
	return &ModelRegistry{
		cacheDir: filepath.Join(config.ConfigDir(), registryCacheDir),
	}
}

// Load fetches or returns cached provider/model data.
func (r *ModelRegistry) Load() (map[string]*RegistryProvider, error) {
	r.mu.RLock()
	if r.providers != nil && time.Since(r.loadedAt) < registryCacheTTL {
		defer r.mu.RUnlock()
		return r.providers, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if r.providers != nil && time.Since(r.loadedAt) < registryCacheTTL {
		return r.providers, nil
	}

	providers, err := r.fetchOrLoadCache()
	if err != nil {
		return nil, err
	}
	r.providers = providers
	r.loadedAt = time.Now()
	return providers, nil
}

// LookupModel finds a model by "provider/model" identifier.
// Returns the provider info, model info, and whether it was found.
func (r *ModelRegistry) LookupModel(providerID, modelID string) (*RegistryProvider, *RegistryModel, bool) {
	providers, err := r.Load()
	if err != nil {
		config.Logger().Printf("[registry] load error: %v", err)
		return nil, nil, false
	}

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
	providers, err := r.Load()
	if err != nil {
		return ""
	}
	prov, ok := providers[providerID]
	if !ok {
		return ""
	}
	return prov.API
}

// GetProviderEnvVars returns the environment variable names for a provider.
func (r *ModelRegistry) GetProviderEnvVars(providerID string) []string {
	providers, err := r.Load()
	if err != nil {
		return nil
	}
	prov, ok := providers[providerID]
	if !ok {
		return nil
	}
	return prov.Env
}

// GetProvider returns provider info by ID, or nil if not found.
func (r *ModelRegistry) GetProvider(providerID string) *RegistryProvider {
	providers, err := r.Load()
	if err != nil {
		return nil
	}
	return providers[providerID]
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
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})
	return models
}

// HasProvider returns whether the given provider ID exists in the registry.
func (r *ModelRegistry) HasProvider(providerID string) bool {
	return r.GetProvider(providerID) != nil
}

func (r *ModelRegistry) fetchOrLoadCache() (map[string]*RegistryProvider, error) {
	// Try fetching from remote first
	providers, err := r.fetchRemote()
	if err == nil {
		// Write to cache on success
		if writeErr := r.writeCache(providers); writeErr != nil {
			config.Logger().Printf("[registry] cache write error: %v", writeErr)
		}
		return providers, nil
	}
	config.Logger().Printf("[registry] remote fetch failed: %v, trying cache", err)

	// Fall back to local cache
	providers, cacheErr := r.readCache()
	if cacheErr != nil {
		return nil, fmt.Errorf("registry unavailable: remote=%v, cache=%v", err, cacheErr)
	}
	return providers, nil
}

func (r *ModelRegistry) fetchRemote() (map[string]*RegistryProvider, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(modelsDevURL)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models.dev returned status %d", resp.StatusCode)
	}

	var providers map[string]*RegistryProvider
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return nil, fmt.Errorf("decode models.dev response: %w", err)
	}
	config.Logger().Printf("[registry] fetched %d providers from models.dev", len(providers))
	return providers, nil
}

func (r *ModelRegistry) cachePath() string {
	return filepath.Join(r.cacheDir, registryCacheFile)
}

func (r *ModelRegistry) writeCache(providers map[string]*RegistryProvider) error {
	if err := os.MkdirAll(r.cacheDir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(providers)
	if err != nil {
		return err
	}
	return os.WriteFile(r.cachePath(), data, 0644)
}

func (r *ModelRegistry) readCache() (map[string]*RegistryProvider, error) {
	data, err := os.ReadFile(r.cachePath())
	if err != nil {
		return nil, err
	}
	var providers map[string]*RegistryProvider
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, err
	}
	config.Logger().Printf("[registry] loaded %d providers from cache", len(providers))
	return providers, nil
}
