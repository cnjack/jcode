package command

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/components/tool"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/providerauth"
	"github.com/cnjack/jcode/internal/providertools"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/toolpolicy"
	"github.com/cnjack/jcode/internal/web"
)

type webSessionToolEnvironment struct {
	Config          *config.Config
	Catalog         *providerSearchMCPCatalog
	PlanMode        bool
	BillableAllowed bool
}

func evaluateWebSessionTool(
	ctx context.Context,
	name session.SessionTool,
	environment webSessionToolEnvironment,
) web.SessionToolEvaluation {
	if environment.PlanMode {
		return web.SessionToolEvaluation{DisabledReason: web.SessionToolDisabledPlanMode}
	}
	if !environment.BillableAllowed {
		return web.SessionToolEvaluation{DisabledReason: web.SessionToolDisabledUnsupported}
	}
	if environment.Config == nil {
		return web.SessionToolEvaluation{DisabledReason: web.SessionToolDisabledConnectionUnavailable}
	}

	var available bool
	var reason string
	switch name {
	case session.SessionToolImageGeneration:
		available, reason = evaluateImageGenerationAvailability(environment.Config)
	case session.SessionToolWebSearch:
		available, reason = evaluateWebSearchAvailability(ctx, environment.Config, environment.Catalog)
	default:
		return web.SessionToolEvaluation{DisabledReason: web.SessionToolDisabledUnsupported}
	}
	if available {
		reason = ""
	}
	return web.SessionToolEvaluation{
		Available: available, Effective: available, DisabledReason: reason,
	}
}

func imageGenerationEnabled(cfg *config.Config, planMode, billableAllowed bool) bool {
	if planMode || !billableAllowed || cfg == nil {
		return false
	}
	available, _ := evaluateImageGenerationAvailability(cfg)
	return available
}

// webTaskBillableAllowed keeps remote execution boundaries explicit. Image
// generation is a local provider call whose bytes are stored under JCode's
// local managed Artifact root, so it remains available for remote workspaces.
// Provider-managed web search stays local-workspace-only.
func webTaskBillableAllowed(name session.SessionTool, remote, excludeInteractive bool) bool {
	if excludeInteractive {
		return false
	}
	return name == session.SessionToolImageGeneration || !remote
}

func sessionToolAllowedInEnvironment(name session.SessionTool, remote bool) bool {
	return !remote || name == session.SessionToolImageGeneration
}

func evaluateImageGenerationAvailability(cfg *config.Config) (bool, string) {
	selected := strings.TrimSpace(cfg.ImageModel)
	if selected == "" {
		return false, web.SessionToolDisabledNoModel
	}
	providerID, modelID, valid := strings.Cut(selected, "/")
	if !valid || strings.TrimSpace(providerID) == "" || strings.TrimSpace(modelID) == "" {
		return false, web.SessionToolDisabledNoModel
	}
	provider := cfg.GetProviders()[providerID]
	managedXAI := providerID == "xai" && provider != nil && provider.Auth != nil &&
		provider.Auth.Method == string(providerauth.MethodXAIOAuth)
	if provider == nil || (strings.TrimSpace(provider.APIKey) == "" && !managedXAI) {
		return false, web.SessionToolDisabledProviderDisabled
	}
	if _, err := providertools.ResolveImageRuntime(cfg); err != nil {
		return false, web.SessionToolDisabledUnsupported
	}
	return true, ""
}

func evaluateWebSearchAvailability(
	ctx context.Context,
	cfg *config.Config,
	catalog *providerSearchMCPCatalog,
) (bool, string) {
	provider := cfg.GetProviders()[providertools.BigModelCodingProvider]
	if provider == nil || strings.TrimSpace(provider.APIKey) == "" {
		return false, web.SessionToolDisabledProviderDisabled
	}
	policy, configured := provider.ProviderTools[providertools.ToolWebSearch]
	if !configured || !policy.Enabled {
		return false, web.SessionToolDisabledProviderDisabled
	}
	runtime, err := providertools.ResolveWebSearchRuntime(cfg)
	if err != nil {
		return false, web.SessionToolDisabledUnsupported
	}
	if catalog == nil {
		return false, web.SessionToolDisabledConnectionUnavailable
	}
	_, reserved, identifyErr := splitProviderSearchMCPTools(ctx, catalog.Tools)
	if identifyErr != nil || len(reserved) == 0 || catalog.ConfigEpoch != runtime.ConfigEpoch {
		return false, web.SessionToolDisabledConnectionUnavailable
	}
	return true, ""
}

func configuredProviderMCPTools(
	ctx context.Context,
	cfg *config.Config,
	recorder *session.Recorder,
	ledger *toolpolicy.UsageLedger,
	catalog *providerSearchMCPCatalog,
	planMode, billableAllowed bool,
	runtimeLoader providerRuntimeConfigLoader,
) ([]tool.BaseTool, error) {
	if planMode || catalog == nil {
		return nil, nil
	}
	generic, _, identifyErr := splitProviderSearchMCPTools(ctx, catalog.Tools)
	if !billableAllowed {
		return generic, identifyErr
	}
	wrapped, err := configuredProviderSearchMCPCatalog(
		ctx, cfg, recorder, ledger, catalog, runtimeLoader,
	)
	if err != nil {
		return wrapped, err
	}
	return wrapped, identifyErr
}
