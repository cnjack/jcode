package command

import (
	"context"
	"fmt"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/providerauth"
	"github.com/cnjack/jcode/internal/providertools"
)

// providerRuntimeConfigLoader is deliberately evaluated at dispatch time,
// after approval and before quota reservation, durable journaling, or any
// provider call. This closes the window where a credential or policy changes
// while a one-time approval is pending.
type providerRuntimeConfigLoader func(context.Context) (*config.Config, error)

func projectProviderRuntimeConfigLoader(pwd string) providerRuntimeConfigLoader {
	return func(ctx context.Context) (*config.Config, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cfg, err := config.LoadConfig()
		if err != nil {
			return nil, fmt.Errorf("reload provider configuration: %w", err)
		}
		config.ApplyProjectOverlay(cfg, pwd)
		return cfg, nil
	}
}

func envProviderRuntimeConfigLoader() providerRuntimeConfigLoader {
	return func(ctx context.Context) (*config.Config, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cfg, err := config.LoadConfig()
		if err != nil {
			return nil, fmt.Errorf("reload provider configuration: %w", err)
		}
		config.ApplyEnvOverlay(cfg)
		return cfg, nil
	}
}

// activeChatProviderRuntimeConfigLoader keeps dispatch-time verification bound
// to the chat model captured by the agent that requested approval. The base
// loader still reloads current credentials and provider policy; only the active
// provider/model selection is projected because a model switch or custom role
// can differ from the persisted default model.
func activeChatProviderRuntimeConfigLoader(
	loader providerRuntimeConfigLoader,
	provider, modelID string,
) providerRuntimeConfigLoader {
	return func(ctx context.Context) (*config.Config, error) {
		if loader == nil {
			return nil, fmt.Errorf("provider runtime config loader is unavailable")
		}
		cfg, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		projectActiveChatModel(cfg, provider, modelID)
		return cfg, nil
	}
}

func imageRuntimeVerifier(
	expected providertools.ImageRuntime,
	loader providerRuntimeConfigLoader,
) func(context.Context) error {
	return func(ctx context.Context) error {
		if loader == nil {
			return fmt.Errorf("image runtime verifier is unavailable")
		}
		cfg, err := loader(ctx)
		if err != nil {
			return err
		}
		current, err := providertools.ResolveImageRuntime(cfg)
		if err != nil {
			return fmt.Errorf("resolve current image runtime: %w", err)
		}
		if current.Provider != expected.Provider || current.Model != expected.Model ||
			current.CredentialFingerprint != expected.CredentialFingerprint ||
			current.ConfigEpoch != expected.ConfigEpoch {
			return fmt.Errorf("image runtime changed after approval")
		}
		if expected.AuthMethod != "" {
			manager, managerErr := providerauth.Default(config.ConfigDir())
			if managerErr != nil {
				return fmt.Errorf("validate managed image account: %w", managerErr)
			}
			if validateErr := manager.ValidateBinding(ctx, providerauth.Binding{
				Method: providerauth.Method(expected.AuthMethod), AccountID: expected.AccountID,
			}); validateErr != nil {
				return fmt.Errorf("validate managed image account: %w", validateErr)
			}
		}
		return nil
	}
}

func webSearchRuntimeVerifier(
	expected providertools.WebSearchRuntime,
	loader providerRuntimeConfigLoader,
) func(context.Context) error {
	return func(ctx context.Context) error {
		if loader == nil {
			return fmt.Errorf("web search runtime verifier is unavailable")
		}
		cfg, err := loader(ctx)
		if err != nil {
			return err
		}
		current, err := providertools.ResolveWebSearchRuntime(cfg)
		if err != nil {
			return fmt.Errorf("resolve current web search runtime: %w", err)
		}
		if current.ProviderProfileID != expected.ProviderProfileID ||
			current.ModelLabel != expected.ModelLabel ||
			current.CredentialFingerprint != expected.CredentialFingerprint ||
			current.ConfigEpoch != expected.ConfigEpoch {
			return fmt.Errorf("web search runtime changed after approval")
		}
		return nil
	}
}
