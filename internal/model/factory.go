package model

import (
	"context"
	"fmt"
	"strings"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cnjack/jcode/internal/config"
)

// ModelFactory creates and caches ChatModel instances by "provider/model" identifier.
type ModelFactory struct {
	mu       sync.RWMutex
	cfg      *config.Config
	cache    map[string]einomodel.ToolCallingChatModel
	fallback einomodel.ToolCallingChatModel
	registry *ModelRegistry
}

// NewModelFactory creates a model factory with the given config, fallback model, and registry.
func NewModelFactory(cfg *config.Config, fallback einomodel.ToolCallingChatModel) *ModelFactory {
	return &ModelFactory{
		cfg:      cfg,
		cache:    make(map[string]einomodel.ToolCallingChatModel),
		fallback: fallback,
		registry: NewModelRegistryWithConfig(cfg),
	}
}

// Registry returns the underlying ModelRegistry for metadata lookups.
func (f *ModelFactory) Registry() *ModelRegistry {
	return f.registry
}

// GetModel returns a ChatModel for the given "provider/model" identifier.
// Empty string returns the fallback model.
func (f *ModelFactory) GetModel(ctx context.Context, providerModel string) (einomodel.ToolCallingChatModel, error) {
	if providerModel == "" {
		return f.fallback, nil
	}

	f.mu.RLock()
	if m, ok := f.cache[providerModel]; ok {
		f.mu.RUnlock()
		return m, nil
	}
	f.mu.RUnlock()

	provider, modelName, err := ParseProviderModel(providerModel)
	if err != nil {
		return nil, err
	}

	providers := f.cfg.GetProviders()
	providerCfg, ok := providers[provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q, available: %v", provider, f.availableProviders())
	}

	// Validate model: check registry if available, otherwise allow any model
	if f.registry != nil && f.registry.HasProvider(provider) {
		if _, rm, ok := f.registry.LookupModel(provider, modelName); !ok || rm == nil {
			config.Logger().Printf("[model-factory] model %q not found in registry for provider %q, proceeding anyway", modelName, provider)
		}
	}

	// Resolve base URL: config override → registry → empty (use default)
	baseURL := providerCfg.BaseURL
	if baseURL == "" {
		baseURL = f.registry.GetProviderAPI(provider)
	}

	m, err := NewChatModel(ctx, &ChatModelConfig{
		Model:   modelName,
		APIKey:  providerCfg.APIKey,
		BaseURL: baseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create model %q: %w", providerModel, err)
	}

	f.mu.Lock()
	f.cache[providerModel] = m
	f.mu.Unlock()

	config.Logger().Printf("[model-factory] created model %q", providerModel)
	return m, nil
}

// Fallback returns the default fallback model.
func (f *ModelFactory) Fallback() einomodel.ToolCallingChatModel {
	return f.fallback
}

// ParseProviderModel splits "provider/model" into its components.
func ParseProviderModel(s string) (provider, model string, err error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid provider/model format %q, expected 'provider/model'", s)
	}
	return parts[0], parts[1], nil
}

func (f *ModelFactory) availableProviders() []string {
	providers := f.cfg.GetProviders()
	result := make([]string, 0, len(providers))
	for k := range providers {
		result = append(result, k)
	}
	return result
}
