package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/cloudwego/eino/components/tool"

	"github.com/cnjack/jcode/internal/artifact"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/imagegen"
	"github.com/cnjack/jcode/internal/providerauth"
	"github.com/cnjack/jcode/internal/providertools"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/toolpolicy"
	"github.com/cnjack/jcode/internal/tools"
)

func configuredGenerateImageTool(
	cfg *config.Config,
	service *artifact.Service,
	recorder *session.Recorder,
	ledger *toolpolicy.UsageLedger,
	runtimeLoader providerRuntimeConfigLoader,
	eventHandler handler.AgentEventHandler,
	emitArtifact func(artifact.Record),
) (tool.BaseTool, error) {
	if ledger == nil {
		return nil, fmt.Errorf("image usage ledger is unavailable")
	}
	runtime, err := providertools.ResolveImageRuntime(cfg)
	if err != nil {
		return nil, err
	}
	credentialKind := "api_key"
	var credential imagegen.CredentialFunc
	if runtime.AuthMethod != "" {
		method := providerauth.Method(runtime.AuthMethod)
		if method != providerauth.MethodXAIOAuth {
			return nil, fmt.Errorf("unsupported managed image authentication method %q", runtime.AuthMethod)
		}
		manager, managerErr := providerauth.Default(config.ConfigDir())
		if managerErr != nil {
			return nil, fmt.Errorf("configure managed image credential: %w", managerErr)
		}
		binding := providerauth.Binding{Method: method, AccountID: runtime.AccountID}
		if validateErr := manager.ValidateBinding(context.Background(), binding); validateErr != nil {
			return nil, fmt.Errorf("configure managed image credential: %w", validateErr)
		}
		credential = func(ctx context.Context) (string, map[string]string, error) {
			resolved, resolveErr := manager.Credential(ctx, binding)
			if resolveErr != nil {
				return "", nil, resolveErr
			}
			if resolved.Protocol != providerauth.ProtocolResponses || resolved.BaseURL != runtime.BaseURL {
				return "", nil, fmt.Errorf("managed image runtime profile changed")
			}
			return resolved.Token, resolved.Headers, nil
		}
		credentialKind = "managed_account"
	}
	client, err := imagegen.NewGenerator(imagegen.ClientConfig{
		Protocol: runtime.Protocol, BaseURL: runtime.BaseURL, APIKey: runtime.APIKey,
		Headers: runtime.Headers, Credential: credential, Model: runtime.Model, AssetHosts: runtime.AssetHosts,
		MaxImageSize: 20 << 20,
	})
	if err != nil {
		return nil, fmt.Errorf("configure image client: %w", err)
	}
	epoch, err := strconv.ParseUint(runtime.ConfigEpoch, 16, 64)
	if err != nil {
		return nil, fmt.Errorf("configure image epoch: %w", err)
	}
	_ = epoch // parsed here so invalid epochs keep the tool out of the catalog
	sizes := imageModelSizes(cfg, runtime.Provider, runtime.Model)
	ledger.SetLimits(runtime.MaxCallsPerTurn, runtime.MaxCallsPerSession)
	return tools.NewGenerateImageTool(&tools.GenerateImageDeps{
		Generator: client, ArtifactService: service, Recorder: recorder, Ledger: ledger,
		Provider: runtime.Provider, Model: runtime.Model,
		EndpointProfile: "image:" + toolpolicy.StableID(string(runtime.Protocol), runtime.BaseURL),
		CredentialKind:  credentialKind, CredentialFingerprint: runtime.CredentialFingerprint,
		ConfigEpoch: runtime.ConfigEpoch,
		DispatchPolicy: session.DispatchPolicy{
			Tool: session.SessionToolImageGeneration, MaxPerSession: runtime.MaxCallsPerSession,
		},
		VerifyRuntime: imageRuntimeVerifier(runtime, runtimeLoader), SupportedSizes: sizes,
		Progress: func(event handler.ToolProgressEvent) {
			handler.EmitToolProgress(eventHandler, event)
		},
		EmitArtifact: emitArtifact,
	}), nil
}

func newImageUsageLedger(recorder *session.Recorder) (*toolpolicy.UsageLedger, error) {
	if recorder == nil {
		return nil, fmt.Errorf("image session recorder is unavailable")
	}
	dispatched, err := dispatchedImageOperationCount(recorder.UUID())
	if err != nil {
		return nil, err
	}
	return toolpolicy.NewUsageLedger(1, 20, dispatched), nil
}

func resetImageUsageLedger(ledger *toolpolicy.UsageLedger, recorder *session.Recorder) error {
	if ledger == nil || recorder == nil {
		return fmt.Errorf("image usage ledger is unavailable")
	}
	dispatched, err := dispatchedImageOperationCount(recorder.UUID())
	if err != nil {
		return err
	}
	ledger.ResetSession(dispatched)
	return nil
}

func dispatchedImageOperationCount(sessionID string) (int, error) {
	operations, loadErr := session.LoadGenerationOperations(sessionID)
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		return 0, fmt.Errorf("load image usage ledger: %w", loadErr)
	}
	dispatched := 0
	for _, operation := range operations {
		if operation.Dispatched {
			dispatched++
		}
	}
	return dispatched, nil
}

func imageModelSizes(cfg *config.Config, providerID, modelID string) []string {
	for _, model := range providertools.ImageModels(cfg) {
		if model.Provider == providerID && model.ID == modelID {
			return append([]string(nil), model.Sizes...)
		}
	}
	return nil
}
