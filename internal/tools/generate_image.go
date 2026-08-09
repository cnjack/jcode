package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/artifact"
	"github.com/cnjack/jcode/internal/imagegen"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/toolpolicy"
	"github.com/cnjack/jcode/internal/toolstate"
)

type GenerationRecorder interface {
	artifact.Recorder
	UUID() string
	RecordGenerationOperation(session.GenerationOperation) error
	RecordGenerationDispatch(session.GenerationOperation, session.DispatchPolicy) error
}

type GenerateImageDeps struct {
	Generator             imagegen.Generator
	ArtifactService       *artifact.Service
	Recorder              GenerationRecorder
	Ledger                *toolpolicy.UsageLedger
	Provider              string
	Model                 string
	EndpointProfile       string
	CredentialKind        string
	CredentialFingerprint string
	ConfigEpoch           string
	DispatchPolicy        session.DispatchPolicy
	VerifyRuntime         func(context.Context) error
	SupportedSizes        []string
	Progress              func(toolstate.ProgressEvent)
	EmitArtifact          func(artifact.Record)
}

type GenerateImageInput struct {
	Prompt string `json:"prompt"`
	Size   string `json:"size,omitempty"`
}

type GenerateImageOutput struct {
	Message     string        `json:"message"`
	OperationID string        `json:"operation_id"`
	Outcome     string        `json:"outcome"`
	ErrorCode   string        `json:"error_code,omitempty"`
	Provider    string        `json:"provider,omitempty"`
	Model       string        `json:"model,omitempty"`
	Artifact    *artifact.Ref `json:"artifact,omitempty"`
}

type generateImageTool struct {
	deps *GenerateImageDeps
	info *schema.ToolInfo
}

func NewGenerateImageTool(deps *GenerateImageDeps) tool.InvokableTool {
	return &generateImageTool{deps: deps, info: &schema.ToolInfo{
		Name: "generate_image",
		Desc: `Generate exactly one new image with the configured image model and save it as a managed JCode Artifact. This is an externally billable operation: Ask for approval and Auto require one-time approval, while Full access runs without a prompt. Do not use it to inspect or edit an existing image.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"prompt": {
				Type: schema.String, Required: true,
				Desc: "A concrete visual description of the single image to generate.",
			},
			"size": {
				Type: schema.String,
				Desc: "Optional supported image size such as 1024x1024. Defaults to the provider profile's first size.",
			},
		}),
	}}
}

func (t *generateImageTool) Info(context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *generateImageTool) PrepareBillableIntent(
	_ context.Context,
	argsJSON, toolCallID string,
) (toolpolicy.BillableIntent, error) {
	if err := t.validateDeps(); err != nil {
		return toolpolicy.BillableIntent{}, err
	}
	_, normalized, err := t.parseInput(argsJSON)
	if err != nil {
		return toolpolicy.BillableIntent{}, err
	}
	if strings.TrimSpace(toolCallID) == "" {
		return toolpolicy.BillableIntent{}, fmt.Errorf("tool call ID is required")
	}
	operationID, err := toolpolicy.NewOperationID()
	if err != nil {
		return toolpolicy.BillableIntent{}, err
	}
	return toolpolicy.BillableIntent{
		OperationID: operationID, ToolCallID: toolCallID,
		CapabilityKey: toolpolicy.CapabilityImageGenerate,
		Provider:      t.deps.Provider, Model: t.deps.Model,
		CredentialFingerprint: t.deps.CredentialFingerprint,
		ConfigEpoch:           t.deps.ConfigEpoch, NormalizedArgs: normalized, Count: 1,
		IdempotencyKey: toolpolicy.StableID(
			t.deps.Recorder.UUID(), operationID, toolCallID, t.deps.ConfigEpoch, normalized,
		),
	}, nil
}

func (t *generateImageTool) InvokableRun(
	ctx context.Context,
	argsJSON string,
	_ ...tool.Option,
) (string, error) {
	if err := t.validateDeps(); err != nil {
		return t.marshalOutput(GenerateImageOutput{
			Message: "Image generation is unavailable in this context.",
			Outcome: string(toolstate.OutcomeFailed), ErrorCode: "configuration_error",
		}), nil
	}
	input, normalized, err := t.parseInput(argsJSON)
	if err != nil {
		return t.marshalOutput(GenerateImageOutput{
			Message: "The image request is invalid.", Outcome: string(toolstate.OutcomeFailed),
			ErrorCode: "invalid_request",
		}), nil
	}
	intent, ok := toolpolicy.BillableIntentFromContext(ctx)
	if !ok || intent.CapabilityKey != toolpolicy.CapabilityImageGenerate ||
		intent.Provider != t.deps.Provider || intent.Model != t.deps.Model ||
		intent.CredentialFingerprint != t.deps.CredentialFingerprint ||
		intent.ConfigEpoch != t.deps.ConfigEpoch || intent.NormalizedArgs != normalized ||
		intent.Count != 1 || intent.OperationID == "" || intent.ToolCallID == "" ||
		intent.IdempotencyKey == "" {
		return t.marshalOutput(GenerateImageOutput{
			Message:     "The authorized image request no longer matches the current configuration.",
			OperationID: intent.OperationID,
			Outcome:     string(toolstate.OutcomeFailed), ErrorCode: "intent_mismatch",
		}), nil
	}
	if err := t.deps.VerifyRuntime(ctx); err != nil {
		return t.marshalOutput(GenerateImageOutput{
			Message:     "Image generation was not dispatched because its runtime configuration changed or is unavailable.",
			OperationID: intent.OperationID, Outcome: string(toolstate.OutcomeFailed),
			ErrorCode: "runtime_configuration_changed",
		}), nil
	}
	reservation, reserveErr := t.deps.Ledger.ReserveRun(
		toolpolicy.RunIDFromContext(ctx), intent.OperationID,
	)
	if reserveErr != nil {
		return t.marshalOutput(GenerateImageOutput{
			Message:     "Image generation was not dispatched because the configured provider-call limit was reached.",
			OperationID: intent.OperationID, Outcome: string(toolstate.OutcomeFailed),
			ErrorCode: "provider_call_limit",
		}), nil
	}

	operation, err := t.startOperation(intent)
	if err != nil {
		reservation.Release()
		message, code := generationDispatchFailure(err)
		return t.marshalOutput(GenerateImageOutput{
			Message:     message,
			OperationID: intent.OperationID, Outcome: string(toolstate.OutcomeFailed),
			ErrorCode: code,
		}), nil
	}
	reservation.Commit()
	t.progress(intent, toolstate.PhaseGenerating, "", nil)

	result, generateErr := t.deps.Generator.Generate(ctx, imagegen.Request{
		Prompt: input.Prompt, Size: input.Size, Count: 1,
	})
	if generateErr != nil {
		outcome, phase, code := classifyGenerationError(generateErr)
		terminal := operation
		terminal.State = session.GenerationFailed
		if outcome == toolstate.OutcomeUncertain {
			terminal.State = session.GenerationUncertain
		}
		terminal.ErrorCode = code
		terminal.UpdatedAt = time.Now().UTC()
		if err := t.appendTransition(operation, terminal); err != nil {
			outcome, phase, code = toolstate.OutcomeUncertain, toolstate.PhaseUncertain, "journal_persist_failed"
		}
		t.progress(intent, phase, code, nil)
		return t.marshalOutput(GenerateImageOutput{
			Message: safeGenerationFailureMessage(outcome, code), OperationID: intent.OperationID,
			Outcome: string(outcome), ErrorCode: code,
		}), nil
	}
	if len(result.Images) != 1 {
		return t.failAfterDispatch(operation, intent, "invalid_provider_output",
			"The provider did not return exactly one valid image.")
	}

	accepted := operation
	accepted.State = session.GenerationAccepted
	accepted.UpdatedAt = time.Now().UTC()
	if err := t.appendTransition(operation, accepted); err != nil {
		return t.uncertainJournalFailure(intent)
	}
	saving := accepted
	saving.State = session.GenerationSaving
	saving.UpdatedAt = time.Now().UTC()
	if err := t.appendTransition(accepted, saving); err != nil {
		return t.uncertainJournalFailure(intent)
	}
	t.progress(intent, toolstate.PhaseSaving, "", nil)

	image := result.Images[0]
	record, saveErr := t.deps.ArtifactService.CreateManagedImage(ctx, artifact.ManagedImageRequest{
		SessionID: t.deps.Recorder.UUID(), Title: "Generated image",
		Reader: bytes.NewReader(image.Data), ProviderID: t.deps.Provider, ModelID: t.deps.Model,
		OperationID: intent.OperationID, ToolCallID: intent.ToolCallID, Focus: false,
		ExpectedMediaType: image.MIMEType, ExpectedWidth: image.Width, ExpectedHeight: image.Height,
	}, t.deps.Recorder)
	if saveErr != nil {
		return t.failAfterDispatch(saving, intent, "artifact_persist_failed",
			"The provider returned an image, but JCode could not save it as an Artifact.")
	}

	succeeded := saving
	succeeded.State = session.GenerationSucceeded
	succeeded.ArtifactIDs = []string{record.ID}
	succeeded.UpdatedAt = time.Now().UTC()
	if err := t.appendTransition(saving, succeeded); err != nil {
		return t.uncertainJournalFailure(intent)
	}
	if t.deps.EmitArtifact != nil {
		t.deps.EmitArtifact(record)
	}
	ref := record.Ref()
	handlerRef := artifactRefForHandler(record)
	t.progress(intent, toolstate.PhaseSucceeded, "", []toolstate.ArtifactRef{handlerRef})
	return t.marshalOutput(GenerateImageOutput{
		Message:     "Generated image saved as a managed Artifact.",
		OperationID: intent.OperationID, Outcome: string(toolstate.OutcomeSucceeded),
		Artifact: &ref,
	}), nil
}

func (t *generateImageTool) validateDeps() error {
	if t == nil || t.deps == nil || t.deps.Generator == nil ||
		t.deps.ArtifactService == nil || t.deps.Recorder == nil || t.deps.Ledger == nil ||
		strings.TrimSpace(t.deps.Provider) == "" || strings.TrimSpace(t.deps.Model) == "" ||
		strings.TrimSpace(t.deps.EndpointProfile) == "" ||
		strings.TrimSpace(t.deps.CredentialFingerprint) == "" ||
		strings.TrimSpace(t.deps.ConfigEpoch) == "" ||
		t.deps.DispatchPolicy.Tool != session.SessionToolImageGeneration ||
		t.deps.DispatchPolicy.MaxPerSession <= 0 || t.deps.VerifyRuntime == nil {
		return fmt.Errorf("generate_image dependencies are incomplete")
	}
	return nil
}

func (t *generateImageTool) parseInput(raw string) (GenerateImageInput, string, error) {
	var input GenerateImageInput
	_, err := toolpolicy.CanonicalJSON(raw, &input)
	if err != nil {
		return input, "", err
	}
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Size = strings.TrimSpace(input.Size)
	if input.Prompt == "" {
		return input, "", fmt.Errorf("prompt is required")
	}
	if input.Size == "" && len(t.deps.SupportedSizes) > 0 {
		input.Size = t.deps.SupportedSizes[0]
	}
	if input.Size != "" && !containsString(t.deps.SupportedSizes, input.Size) &&
		len(t.deps.SupportedSizes) > 0 {
		return input, "", fmt.Errorf("unsupported image size %q", input.Size)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return input, "", err
	}
	return input, string(encoded), nil
}

func (t *generateImageTool) startOperation(intent toolpolicy.BillableIntent) (session.GenerationOperation, error) {
	epoch, err := strconv.ParseUint(intent.ConfigEpoch, 16, 64)
	if err != nil {
		return session.GenerationOperation{}, fmt.Errorf("invalid config epoch")
	}
	operation := session.GenerationOperation{
		OperationID: intent.OperationID, ToolCallID: intent.ToolCallID,
		State: session.GenerationDispatchAttempted,
		CapabilityKey: session.OperationCapabilityKey{
			ProviderProfileID: intent.Provider, CredentialKind: t.deps.CredentialKind,
			EndpointProfile: t.deps.EndpointProfile, ModelID: intent.Model,
		},
		CredentialFingerprint: intent.CredentialFingerprint, ConfigEpoch: epoch,
		IdempotencyKey: intent.IdempotencyKey, UpdatedAt: time.Now().UTC(),
	}
	if err := session.ValidateGenerationStart(operation); err != nil {
		return session.GenerationOperation{}, err
	}
	if err := t.deps.Recorder.RecordGenerationDispatch(operation, t.deps.DispatchPolicy); err != nil {
		return session.GenerationOperation{}, err
	}
	return operation, nil
}

func generationDispatchFailure(err error) (string, string) {
	switch {
	case errors.Is(err, session.ErrDispatchSessionLimit):
		return "Image generation was not dispatched because the configured provider-call limit was reached.",
			"provider_call_limit"
	default:
		return "Image generation was not dispatched because the operation journal could not be saved.",
			"journal_persist_failed"
	}
}

func (t *generateImageTool) appendTransition(previous, next session.GenerationOperation) error {
	if err := session.ValidateGenerationTransition(previous, next); err != nil {
		return err
	}
	return t.deps.Recorder.RecordGenerationOperation(next)
}

func (t *generateImageTool) failAfterDispatch(
	previous session.GenerationOperation,
	intent toolpolicy.BillableIntent,
	code, message string,
) (string, error) {
	failed := previous
	failed.State = session.GenerationFailed
	failed.ErrorCode = code
	failed.UpdatedAt = time.Now().UTC()
	if err := t.appendTransition(previous, failed); err != nil {
		return t.uncertainJournalFailure(intent)
	}
	t.progress(intent, toolstate.PhaseFailed, code, nil)
	return t.marshalOutput(GenerateImageOutput{
		Message: message, OperationID: intent.OperationID,
		Outcome: string(toolstate.OutcomeFailed), ErrorCode: code,
	}), nil
}

func (t *generateImageTool) uncertainJournalFailure(intent toolpolicy.BillableIntent) (string, error) {
	t.progress(intent, toolstate.PhaseUncertain, "journal_persist_failed", nil)
	return t.marshalOutput(GenerateImageOutput{
		Message:     "The provider operation may have completed, but JCode could not durably record its terminal state.",
		OperationID: intent.OperationID, Outcome: string(toolstate.OutcomeUncertain),
		ErrorCode: "journal_persist_failed",
	}), nil
}

func (t *generateImageTool) progress(
	intent toolpolicy.BillableIntent,
	phase toolstate.Phase,
	errorCode string,
	artifacts []toolstate.ArtifactRef,
) {
	if t.deps.Progress == nil {
		return
	}
	t.deps.Progress(toolstate.ProgressEvent{
		Name: "generate_image", ToolCallID: intent.ToolCallID,
		Surface: toolstate.SurfaceStandalone, Phase: phase,
		OperationID: intent.OperationID, ErrorCode: errorCode,
		Provider: intent.Provider, Model: intent.Model, Artifacts: artifacts,
	})
}

func classifyGenerationError(err error) (toolstate.Outcome, toolstate.Phase, string) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	switch {
	case imagegen.IsContextError(err), strings.Contains(message, "request failed"):
		return toolstate.OutcomeUncertain, toolstate.PhaseUncertain, "provider_outcome_uncertain"
	case strings.Contains(message, "host is not allowed"):
		return toolstate.OutcomeFailed, toolstate.PhaseFailed, "asset_host_blocked"
	case strings.Contains(message, "download generated image"),
		strings.Contains(message, "generated image download failed"),
		strings.Contains(message, "validate generated image"):
		return toolstate.OutcomeUncertain, toolstate.PhaseUncertain, "asset_download_failed"
	case strings.Contains(message, "HTTP 401"), strings.Contains(message, "HTTP 403"):
		return toolstate.OutcomeFailed, toolstate.PhaseFailed, "authentication_failed"
	case strings.Contains(message, "HTTP 402"):
		return toolstate.OutcomeFailed, toolstate.PhaseFailed, "quota_exceeded"
	case strings.Contains(message, "HTTP 429"):
		return toolstate.OutcomeFailed, toolstate.PhaseFailed, "rate_limited"
	default:
		return toolstate.OutcomeFailed, toolstate.PhaseFailed, "provider_error"
	}
}

func safeGenerationFailureMessage(outcome toolstate.Outcome, code string) string {
	if outcome == toolstate.OutcomeUncertain {
		return "The provider outcome is uncertain; JCode will not retry because the request may have been billed."
	}
	switch code {
	case "authentication_failed":
		return "The image provider rejected its configured credential."
	case "quota_exceeded":
		return "The image provider reported that no quota is available."
	case "asset_host_blocked":
		return "The provider returned an image URL outside the configured asset-host allowlist."
	default:
		return "The image provider could not complete the request."
	}
}

func artifactRefForHandler(record artifact.Record) toolstate.ArtifactRef {
	return toolstate.ArtifactRef{
		ID: record.ID, Storage: string(record.EffectiveStorageKind()), Key: record.RelativeKey,
		Title: record.Title, Kind: string(record.Kind), MediaType: record.MediaType,
		Size: record.Size, Width: record.Width, Height: record.Height,
		Provider: record.ProviderID, Model: record.ModelID, OperationID: record.OperationID,
		ToolCallID: record.ToolCallID, Shareable: record.Shareable,
	}
}

func ParseGenerateImageOutput(raw string) (GenerateImageOutput, bool) {
	var output GenerateImageOutput
	if json.Unmarshal([]byte(raw), &output) != nil || output.OperationID == "" || output.Outcome == "" {
		return GenerateImageOutput{}, false
	}
	return output, true
}

func (t *generateImageTool) marshalOutput(output GenerateImageOutput) string {
	if t != nil && t.deps != nil {
		output.Provider = t.deps.Provider
		output.Model = t.deps.Model
	}
	return marshalGenerateImageOutput(output)
}

func marshalGenerateImageOutput(output GenerateImageOutput) string {
	encoded, err := json.Marshal(output)
	if err != nil {
		return `{"message":"Image generation failed.","outcome":"failed","error_code":"result_encoding_failed"}`
	}
	return string(encoded)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
