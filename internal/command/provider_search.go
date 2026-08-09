package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino/components/tool"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/providertools"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/toolpolicy"
	"github.com/cnjack/jcode/internal/tools"
)

func projectActiveChatModel(cfg *config.Config, provider, modelID string) {
	if cfg == nil {
		return
	}
	cfg.Model = strings.TrimSpace(provider) + "/" + strings.TrimSpace(modelID)
}

const (
	defaultProviderSearchCallsPerTurn    = 2
	defaultProviderSearchCallsPerSession = 10
)

// providerSearchMCPCatalog binds raw MCP endpoints to the provider config
// epoch used to start their transport. Web provider settings can rebuild task
// agents before the asynchronous MCP reconnect completes; retaining this epoch
// prevents a newly-approved request from reaching an endpoint that still holds
// the previous credential.
type providerSearchMCPCatalog struct {
	Tools       []tool.BaseTool
	ConfigEpoch string
}

// configuredProviderSearchMCPTools wraps every tool owned by the reserved
// BigModel search MCP preset with the same session ledger. Generic MCP tools
// remain unchanged. If runtime metadata or any wrapper cannot be constructed,
// every reserved endpoint is removed so a provider credential can never reach
// an unapproved/unmetered tool path.
func configuredProviderSearchMCPTools(
	ctx context.Context,
	cfg *config.Config,
	recorder *session.Recorder,
	ledger *toolpolicy.UsageLedger,
	candidates []tool.BaseTool,
	runtimeLoader providerRuntimeConfigLoader,
) ([]tool.BaseTool, error) {
	generic, providerSearch, identifyErr := splitProviderSearchMCPTools(ctx, candidates)
	if len(providerSearch) == 0 {
		return generic, identifyErr
	}
	if recorder == nil || ledger == nil {
		return generic, errors.Join(identifyErr, fmt.Errorf("provider search session ledger is unavailable"))
	}
	runtime, err := providertools.ResolveWebSearchRuntime(cfg)
	if err != nil {
		return generic, errors.Join(identifyErr, err)
	}
	if runtime.MaxCallsPerTurn <= 0 || runtime.MaxCallsPerSession <= 0 {
		return generic, errors.Join(identifyErr, fmt.Errorf("provider search usage limits are invalid"))
	}
	ledger.SetLimits(runtime.MaxCallsPerTurn, runtime.MaxCallsPerSession)

	wrapped := make([]tool.BaseTool, 0, len(providerSearch))
	for _, candidate := range providerSearch {
		billable, wrapErr := tools.WrapProviderManagedBillableTool(ctx, candidate, tools.ProviderManagedToolDeps{
			Recorder: recorder, Ledger: ledger,
			CapabilityKey:     toolpolicy.CapabilityWebSearch,
			ProviderProfileID: runtime.ProviderProfileID, ModelLabel: runtime.ModelLabel,
			CredentialFingerprint: runtime.CredentialFingerprint,
			ConfigEpoch:           runtime.ConfigEpoch,
			DispatchPolicy: session.DispatchPolicy{
				Tool: session.SessionToolWebSearch, MaxPerSession: runtime.MaxCallsPerSession,
			},
			VerifyRuntime: webSearchRuntimeVerifier(runtime, runtimeLoader),
		})
		if wrapErr != nil {
			return generic, errors.Join(identifyErr, fmt.Errorf("wrap provider search MCP tool: %w", wrapErr))
		}
		wrapped = append(wrapped, billable)
	}
	return append(generic, wrapped...), identifyErr
}

// configuredProviderSearchMCPCatalog applies the normal wrapper only when raw
// MCP endpoints were connected from the same provider config epoch. Generic
// MCP endpoints remain available while a provider reconnect is pending.
func configuredProviderSearchMCPCatalog(
	ctx context.Context,
	cfg *config.Config,
	recorder *session.Recorder,
	ledger *toolpolicy.UsageLedger,
	catalog *providerSearchMCPCatalog,
	runtimeLoader providerRuntimeConfigLoader,
) ([]tool.BaseTool, error) {
	if catalog == nil {
		return nil, nil
	}
	generic, providerSearch, identifyErr := splitProviderSearchMCPTools(ctx, catalog.Tools)
	if len(providerSearch) == 0 {
		return generic, identifyErr
	}
	runtime, err := providertools.ResolveWebSearchRuntime(cfg)
	if err != nil {
		return generic, errors.Join(identifyErr, err)
	}
	if catalog.ConfigEpoch == "" || catalog.ConfigEpoch != runtime.ConfigEpoch {
		return generic, errors.Join(
			identifyErr,
			fmt.Errorf("provider search MCP connection is stale for the current config epoch"),
		)
	}
	return configuredProviderSearchMCPTools(
		ctx, cfg, recorder, ledger, catalog.Tools, runtimeLoader,
	)
}

func newProviderSearchMCPCatalog(cfg *config.Config, candidates []tool.BaseTool) *providerSearchMCPCatalog {
	catalog := &providerSearchMCPCatalog{Tools: append([]tool.BaseTool(nil), candidates...)}
	if runtime, err := providertools.ResolveWebSearchTransportRuntime(cfg); err == nil {
		catalog.ConfigEpoch = runtime.ConfigEpoch
	}
	return catalog
}

// splitProviderSearchMCPTools identifies tools from their canonical MCP owner.
// A candidate whose ToolInfo cannot be read is dropped: preserving an unknown
// endpoint could accidentally retain a reserved provider tool unwrapped.
func splitProviderSearchMCPTools(
	ctx context.Context,
	candidates []tool.BaseTool,
) (generic, providerSearch []tool.BaseTool, resultErr error) {
	generic = make([]tool.BaseTool, 0, len(candidates))
	providerSearch = make([]tool.BaseTool, 0, len(candidates))
	for index, candidate := range candidates {
		if candidate == nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("MCP candidate %d is nil", index))
			continue
		}
		info, err := candidate.Info(ctx)
		if err != nil || info == nil || info.Name == "" {
			if err == nil {
				err = fmt.Errorf("empty ToolInfo")
			}
			resultErr = errors.Join(resultErr, fmt.Errorf("identify MCP candidate %d: %w", index, err))
			continue
		}
		server, reserved := tools.MCPServerForTool(info.Name)
		if reserved && providertools.IsProviderSearchMCPServer(server) {
			original, exact := tools.MCPOriginalToolName(info.Name)
			if !exact || original != providertools.BigModelSearchMCPToolName {
				resultErr = errors.Join(
					resultErr,
					fmt.Errorf("drop unverified provider search MCP tool %q", info.Name),
				)
				continue
			}
			providerSearch = append(providerSearch, candidate)
			continue
		}
		generic = append(generic, candidate)
	}
	return generic, providerSearch, resultErr
}

func newProviderSearchUsageLedger(recorder *session.Recorder) (*toolpolicy.UsageLedger, error) {
	if recorder == nil {
		return nil, fmt.Errorf("provider search session recorder is unavailable")
	}
	return tools.NewProviderToolUsageLedger(
		recorder.UUID(), toolpolicy.CapabilityWebSearch,
		providertools.BigModelCodingProvider,
		defaultProviderSearchCallsPerTurn, defaultProviderSearchCallsPerSession,
	)
}

func resetProviderSearchUsageLedger(
	ledger *toolpolicy.UsageLedger,
	recorder *session.Recorder,
) error {
	if ledger == nil || recorder == nil {
		return fmt.Errorf("provider search session ledger is unavailable")
	}
	snapshots, err := session.LoadProviderToolOperations(recorder.UUID())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load provider search usage ledger: %w", err)
	}
	dispatched := session.CountDispatchedProviderToolOperations(
		snapshots, toolpolicy.CapabilityWebSearch, providertools.BigModelCodingProvider,
	)
	ledger.ResetSession(dispatched)
	return nil
}
