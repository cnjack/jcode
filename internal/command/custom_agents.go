package command

import (
	"fmt"
	"strings"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
)

// resolveCustomAgentModel applies a role's optional model over the caller's
// current selection. The "small" alias inherits when it is not configured,
// matching delegated-agent model routing.
func resolveCustomAgentModel(
	role config.AgentRoleConfig,
	cfg *config.Config,
	currentProvider, currentModel string,
) (string, string, error) {
	ref := strings.TrimSpace(role.Model)
	if ref == "" {
		return currentProvider, currentModel, nil
	}
	if ref == internalmodel.SmallModelAlias {
		if cfg == nil || strings.TrimSpace(cfg.SmallModel) == "" {
			return currentProvider, currentModel, nil
		}
		ref = strings.TrimSpace(cfg.SmallModel)
	}
	provider, modelName, err := internalmodel.ParseProviderModel(ref)
	if err != nil {
		return "", "", fmt.Errorf("custom agent model: %w", err)
	}
	return provider, modelName, nil
}

func loadCustomAgentRole(pwd, roleName string) (config.AgentRoleConfig, error) {
	role, ok := config.LoadAgentRoles(pwd)[roleName]
	if !ok {
		return config.AgentRoleConfig{}, fmt.Errorf("unknown custom agent %q", roleName)
	}
	return role, nil
}

func optionalCustomAgentRole(pwd, roleName string) (config.AgentRoleConfig, error) {
	if roleName == "" {
		return config.AgentRoleConfig{}, nil
	}
	return loadCustomAgentRole(pwd, roleName)
}

func withCustomAgentPrompt(
	base, roleName string,
	role config.AgentRoleConfig,
) string {
	if roleName == "" {
		return base
	}
	return base + "\n\n## Custom agent: " + roleName +
		"\nDescription: " + role.Description +
		"\n\n" + role.Instructions
}

func withLoadedCustomAgentPrompt(base, pwd, roleName string) string {
	role, err := optionalCustomAgentRole(pwd, roleName)
	if err != nil {
		return base
	}
	return withCustomAgentPrompt(base, roleName, role)
}

func resolveWebCustomAgentSelection(
	pwd, roleName, currentProvider, currentModel string,
) (config.AgentRoleConfig, string, string, error) {
	if roleName == "" {
		return config.AgentRoleConfig{}, currentProvider, currentModel, nil
	}
	role, err := loadCustomAgentRole(pwd, roleName)
	if err != nil {
		return config.AgentRoleConfig{}, "", "", err
	}
	if role.Model == "" {
		return role, currentProvider, currentModel, nil
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return config.AgentRoleConfig{}, "", "", fmt.Errorf(
			"load config for custom agent model: %w", err,
		)
	}
	provider, modelName, err := resolveCustomAgentModel(
		role, cfg, currentProvider, currentModel,
	)
	if err != nil {
		return config.AgentRoleConfig{}, "", "", fmt.Errorf(
			"custom agent %q: %w", roleName, err,
		)
	}
	return role, provider, modelName, nil
}
