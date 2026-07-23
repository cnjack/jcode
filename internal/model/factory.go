package model

import (
	"context"
	"fmt"
	"strings"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cnjack/jcode/internal/config"
)

// ExternalModel is a resolved non-local provider model. The factory owns the
// common OpenAI-compatible construction, while the command layer supplies
// authentication/catalog resolution without creating a model↔cloud import
// cycle.
type ExternalModel struct {
	Provider string
	Model    string
	BaseURL  string
	Config   *config.ProviderConfig
}

type ExternalModelResolver func(context.Context, string, string) (*ExternalModel, error)

// ModelFactory creates and caches ChatModel instances by "provider/model" identifier.
type ModelFactory struct {
	mu       sync.RWMutex
	cfg      *config.Config
	cache    map[string]einomodel.ToolCallingChatModel
	fallback einomodel.ToolCallingChatModel
	registry *ModelRegistry
	external ExternalModelResolver
}

func (f *ModelFactory) SetExternalModelResolver(resolver ExternalModelResolver) {
	f.mu.Lock()
	f.external = resolver
	f.mu.Unlock()
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

// SmallModelAlias is a special model ref accepted by the model params that
// resolve through this factory (subagent/flow/team) plus the automation
// model override. It resolves to cfg.SmallModel; when no small model is
// configured it degrades to the session default so callers never fail just
// because the alias is unset.
const SmallModelAlias = "small"

// SmallModelRef returns the configured small model in "provider/model" format,
// or "" when unset. Used by tool builders to decide whether to advertise the
// "small" alias.
func (f *ModelFactory) SmallModelRef() string {
	if f.cfg == nil {
		return ""
	}
	return f.cfg.SmallModel
}

// ResolveRef expands the SmallModelAlias to its concrete "provider/model" ref.
// Returns "" when the input is empty or the alias is unset (i.e. the caller
// will use the session default) — useful for attributing usage to the model
// that actually served the calls.
func (f *ModelFactory) ResolveRef(providerModel string) string {
	if providerModel == SmallModelAlias {
		return f.SmallModelRef()
	}
	return providerModel
}

// GetModel returns a ChatModel for the given "provider/model" identifier.
// Empty string returns the fallback model. The SmallModelAlias ("small")
// resolves to the configured small_model; when small_model is unset OR
// malformed the alias degrades to the session default instead of erroring —
// a config typo must not fail every delegated task. Direct (non-alias) refs
// keep hard errors so caller typos surface.
func (f *ModelFactory) GetModel(ctx context.Context, providerModel string) (einomodel.ToolCallingChatModel, error) {
	if providerModel == SmallModelAlias {
		resolved := f.ResolveRef(providerModel)
		if resolved == "" {
			config.Logger().Printf("[model-factory] %q alias requested but small_model is not configured; using session model", SmallModelAlias)
		} else if _, _, err := ParseProviderModel(resolved); err != nil {
			config.Logger().Printf("[model-factory] small_model %q is invalid (%v); using session model", resolved, err)
			resolved = ""
		}
		providerModel = resolved
	}
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

	f.mu.RLock()
	externalResolver := f.external
	f.mu.RUnlock()
	if externalResolver != nil {
		resolved, err := externalResolver(ctx, provider, modelName)
		if err != nil {
			return nil, err
		}
		if resolved != nil {
			m, err := NewChatModelFromProvider(
				ctx, resolved.Provider, resolved.Model, resolved.BaseURL, resolved.Config,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create external model %q: %w", providerModel, err)
			}
			f.mu.Lock()
			f.cache[providerModel] = m
			f.mu.Unlock()
			return m, nil
		}
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

	// Apply a per-model reasoning-effort override (set from the chat picker)
	// over the provider-level default before constructing the model.
	facEffortCfg := *providerCfg
	facEffortCfg.ReasoningEffort = config.ResolveEffort(provider, modelName, providerCfg.ReasoningEffort)
	m, err := NewChatModelFromProvider(ctx, provider, modelName, baseURL, &facEffortCfg)
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

// BareModelID returns the model portion of a "provider/model" ref; refs
// without a provider prefix pass through unchanged. Usage events store bare
// model ids (runner attribution uses Recorder.Model()) — route every usage
// writer through this so the same model never splits into two stat buckets.
func BareModelID(ref string) string {
	if _, m, err := ParseProviderModel(ref); err == nil {
		return m
	}
	return ref
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
