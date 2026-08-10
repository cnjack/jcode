package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cnjack/jcode/internal/artifact"
	"github.com/cnjack/jcode/internal/imagegen"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/toolpolicy"
	"github.com/cnjack/jcode/internal/toolstate"
)

type stubImageGenerator struct {
	result imagegen.Result
	err    error
	calls  int
	input  imagegen.Request
}

func (g *stubImageGenerator) Generate(_ context.Context, input imagegen.Request) (imagegen.Result, error) {
	g.calls++
	g.input = input
	return g.result, g.err
}

func TestGenerateImagePersistsJournalArtifactAndProgress(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("generate an image")
	pixels := generatedPNG(t, 3, 2)
	generator := &stubImageGenerator{result: imagegen.Result{Images: []imagegen.Image{{
		Data: pixels, MIMEType: "image/png", Width: 3, Height: 2,
	}}}}
	var phases []toolstate.Phase
	service := artifact.NewServiceWithManagedRoot(
		session.LoadArtifactRecords, nil, filepath.Join(t.TempDir(), "managed"),
	)
	imageTool := NewGenerateImageTool(&GenerateImageDeps{
		Generator: generator, ArtifactService: service, Recorder: recorder,
		Ledger:   toolpolicy.NewUsageLedger(1, 20, 0),
		Provider: "provider-image", Model: "image-1", EndpointProfile: "endpoint-1",
		CredentialKind: "api_key", CredentialFingerprint: "credential-fingerprint",
		ConfigEpoch: "0000000000000001", SupportedSizes: []string{"1024x1024"},
		DispatchPolicy: imageDispatchPolicy(),
		VerifyRuntime:  allowImageRuntime,
		Progress:       func(event toolstate.ProgressEvent) { phases = append(phases, event.Phase) },
	})
	preparer := imageTool.(toolpolicy.BillableIntentPreparer)
	intent, err := preparer.PrepareBillableIntent(
		context.Background(), `{"prompt":"a quiet desk"}`, "call-image-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := toolpolicy.WithBillableIntent(context.Background(), intent)
	raw, err := imageTool.InvokableRun(ctx, `{"prompt":"a quiet desk"}`)
	if err != nil {
		t.Fatal(err)
	}
	output, ok := ParseGenerateImageOutput(raw)
	if !ok || output.Outcome != string(toolstate.OutcomeSucceeded) || output.Artifact == nil {
		t.Fatalf("output = %s", raw)
	}
	if output.Provider != "provider-image" || output.Model != "image-1" {
		t.Fatalf("output snapshot = %q/%q", output.Provider, output.Model)
	}
	if generator.calls != 1 {
		t.Fatalf("provider calls = %d", generator.calls)
	}
	wantPhases := []toolstate.Phase{
		toolstate.PhaseGenerating, toolstate.PhaseSaving, toolstate.PhaseSucceeded,
	}
	if len(phases) != len(wantPhases) {
		t.Fatalf("phases = %v", phases)
	}
	for i := range wantPhases {
		if phases[i] != wantPhases[i] {
			t.Fatalf("phases = %v", phases)
		}
	}
	operations, err := session.LoadGenerationOperations(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 1 || !operations[0].Dispatched ||
		operations[0].Latest.State != session.GenerationSucceeded ||
		len(operations[0].Latest.ArtifactIDs) != 1 {
		t.Fatalf("operations = %#v", operations)
	}
	if operations[0].Latest.ToolCallID != "call-image-1" ||
		operations[0].Latest.OperationID == operations[0].Latest.ToolCallID {
		t.Fatalf("host operation identity was not separated from model tool-call ID: %#v", operations[0])
	}
	records, err := service.List(context.Background(), recorder.UUID(), t.TempDir())
	if err != nil || len(records) != 1 || records[0].EffectiveStorageKind() != artifact.StorageManaged || records[0].Focus {
		t.Fatalf("records=%#v err=%v", records, err)
	}
}

func TestClassifyGenerationErrorKeepsTypedFailureCategories(t *testing.T) {
	tests := []struct {
		message string
		outcome toolstate.Outcome
		code    string
	}{
		{"image provider returned HTTP 401", toolstate.OutcomeFailed, "authentication_failed"},
		{"image provider returned HTTP 402", toolstate.OutcomeFailed, "quota_exceeded"},
		{"image provider returned HTTP 429", toolstate.OutcomeFailed, "rate_limited"},
		{"validate generated image 1: generated image URL host is not allowed", toolstate.OutcomeFailed, "asset_host_blocked"},
		{"validate generated image 1: generated image download failed", toolstate.OutcomeUncertain, "asset_download_failed"},
	}
	for _, test := range tests {
		outcome, _, code := classifyGenerationError(errors.New(test.message))
		if outcome != test.outcome || code != test.code {
			t.Errorf("%q => %s/%s, want %s/%s", test.message, outcome, code, test.outcome, test.code)
		}
	}
}

func TestGenerateImageRepeatedModelToolCallIDGetsUniqueHostOperations(t *testing.T) {
	recorder := &failingGenerationRecorder{}
	imageTool := NewGenerateImageTool(&GenerateImageDeps{
		Generator:       &stubImageGenerator{},
		ArtifactService: artifact.NewServiceWithManagedRoot(nil, nil, filepath.Join(t.TempDir(), "managed")),
		Recorder:        recorder, Provider: "p", Model: "m", EndpointProfile: "e",
		Ledger:         toolpolicy.NewUsageLedger(2, 20, 0),
		CredentialKind: "api_key", CredentialFingerprint: "f",
		ConfigEpoch: "0000000000000001", DispatchPolicy: imageDispatchPolicy(),
		VerifyRuntime: allowImageRuntime,
	})
	preparer := imageTool.(toolpolicy.BillableIntentPreparer)
	first, err := preparer.PrepareBillableIntent(context.Background(), `{"prompt":"desk"}`, "reused-call")
	if err != nil {
		t.Fatal(err)
	}
	second, err := preparer.PrepareBillableIntent(context.Background(), `{"prompt":"desk"}`, "reused-call")
	if err != nil {
		t.Fatal(err)
	}
	if first.OperationID == second.OperationID || first.ToolCallID != "reused-call" || second.ToolCallID != "reused-call" {
		t.Fatalf("operation identities first=%#v second=%#v", first, second)
	}
}

type failingGenerationRecorder struct {
	recordArtifactCalls int
	operationCalls      int
}

func (r *failingGenerationRecorder) UUID() string { return "session-1" }
func (r *failingGenerationRecorder) RecordArtifact(artifact.Record) error {
	r.recordArtifactCalls++
	return nil
}
func (r *failingGenerationRecorder) RecordGenerationOperation(session.GenerationOperation) error {
	r.operationCalls++
	return errors.New("disk unavailable")
}
func (r *failingGenerationRecorder) RecordGenerationDispatch(
	_ session.GenerationOperation,
	_ session.DispatchPolicy,
) error {
	r.operationCalls++
	return errors.New("disk unavailable")
}

func TestGenerateImageDoesNotDispatchWhenJournalStartFails(t *testing.T) {
	recorder := &failingGenerationRecorder{}
	generator := &stubImageGenerator{}
	imageTool := NewGenerateImageTool(&GenerateImageDeps{
		Generator:       generator,
		ArtifactService: artifact.NewServiceWithManagedRoot(nil, nil, filepath.Join(t.TempDir(), "managed")),
		Recorder:        recorder, Provider: "p", Model: "m", EndpointProfile: "e",
		Ledger:         toolpolicy.NewUsageLedger(1, 20, 0),
		CredentialKind: "api_key", CredentialFingerprint: "f",
		ConfigEpoch: "0000000000000001", DispatchPolicy: imageDispatchPolicy(),
		VerifyRuntime: allowImageRuntime,
	})
	intent, err := imageTool.(toolpolicy.BillableIntentPreparer).PrepareBillableIntent(
		context.Background(), `{"prompt":"desk"}`, "call-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := imageTool.InvokableRun(
		toolpolicy.WithBillableIntent(context.Background(), intent), `{"prompt":"desk"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	output, ok := ParseGenerateImageOutput(raw)
	if !ok || output.ErrorCode != "journal_persist_failed" {
		t.Fatalf("output = %s", raw)
	}
	if generator.calls != 0 || recorder.operationCalls != 1 || recorder.recordArtifactCalls != 0 {
		t.Fatalf("calls: provider=%d operation=%d artifact=%d", generator.calls, recorder.operationCalls, recorder.recordArtifactCalls)
	}
}

func TestGenerateImageIntentMismatchFailsClosed(t *testing.T) {
	recorder := &failingGenerationRecorder{}
	generator := &stubImageGenerator{}
	imageTool := NewGenerateImageTool(&GenerateImageDeps{
		Generator:       generator,
		ArtifactService: artifact.NewServiceWithManagedRoot(nil, nil, filepath.Join(t.TempDir(), "managed")),
		Recorder:        recorder, Provider: "p", Model: "m", EndpointProfile: "e",
		Ledger:         toolpolicy.NewUsageLedger(1, 20, 0),
		CredentialKind: "api_key", CredentialFingerprint: "f",
		ConfigEpoch: "0000000000000001", DispatchPolicy: imageDispatchPolicy(),
		VerifyRuntime: allowImageRuntime,
	})
	intent, err := imageTool.(toolpolicy.BillableIntentPreparer).PrepareBillableIntent(
		context.Background(), `{"prompt":"approved"}`, "call-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := imageTool.InvokableRun(
		toolpolicy.WithBillableIntent(context.Background(), intent), `{"prompt":"changed"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	output, ok := ParseGenerateImageOutput(raw)
	if !ok || output.ErrorCode != "intent_mismatch" || generator.calls != 0 || recorder.operationCalls != 0 {
		t.Fatalf("output=%s provider=%d journal=%d", raw, generator.calls, recorder.operationCalls)
	}
}

func TestGenerateImageIgnoresLegacyRevokedSessionPreference(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	override, err := recorder.CompareAndSwapSessionToolOverride(
		session.SessionToolImageGeneration, true, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	generator := &stubImageGenerator{}
	imageTool := NewGenerateImageTool(&GenerateImageDeps{
		Generator: generator,
		ArtifactService: artifact.NewServiceWithManagedRoot(
			session.LoadArtifactRecords, nil, filepath.Join(t.TempDir(), "managed"),
		),
		Recorder: recorder, Ledger: toolpolicy.NewUsageLedger(1, 20, 0),
		Provider: "provider-image", Model: "image-1", EndpointProfile: "endpoint-1",
		CredentialKind: "api_key", CredentialFingerprint: "credential-fingerprint",
		ConfigEpoch: "0000000000000001", DispatchPolicy: imageDispatchPolicy(),
		VerifyRuntime: allowImageRuntime,
	})
	intent, err := imageTool.(toolpolicy.BillableIntentPreparer).PrepareBillableIntent(
		context.Background(), `{"prompt":"desk"}`, "call-before-revoke",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.CompareAndSwapSessionToolOverride(
		session.SessionToolImageGeneration, false, override.Revision,
	); err != nil {
		t.Fatal(err)
	}
	raw, err := imageTool.InvokableRun(
		toolpolicy.WithBillableIntent(context.Background(), intent), `{"prompt":"desk"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	output, ok := ParseGenerateImageOutput(raw)
	if !ok || output.ErrorCode != "invalid_provider_output" {
		t.Fatalf("output = %s", raw)
	}
	if generator.calls != 1 {
		t.Fatalf("legacy image override blocked provider call: calls=%d", generator.calls)
	}
}

func TestGenerateImageRuntimeVerificationFailsBeforeReservationJournalAndProvider(t *testing.T) {
	recorder := &failingGenerationRecorder{}
	generator := &stubImageGenerator{}
	var allowed atomic.Bool
	imageTool := NewGenerateImageTool(&GenerateImageDeps{
		Generator:       generator,
		ArtifactService: artifact.NewServiceWithManagedRoot(nil, nil, filepath.Join(t.TempDir(), "managed")),
		Recorder:        recorder, Provider: "p", Model: "m", EndpointProfile: "e",
		Ledger:         toolpolicy.NewUsageLedger(1, 20, 0),
		CredentialKind: "api_key", CredentialFingerprint: "f",
		ConfigEpoch: "0000000000000001", DispatchPolicy: imageDispatchPolicy(),
		VerifyRuntime: func(context.Context) error {
			if !allowed.Load() {
				return errors.New("runtime changed")
			}
			return nil
		},
	})
	preparer := imageTool.(toolpolicy.BillableIntentPreparer)
	first, err := preparer.PrepareBillableIntent(
		context.Background(), `{"prompt":"first"}`, "call-runtime-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	firstCtx := toolpolicy.WithRunID(context.Background(), "same-turn")
	firstCtx = toolpolicy.WithBillableIntent(firstCtx, first)
	raw, err := imageTool.InvokableRun(firstCtx, `{"prompt":"first"}`)
	if err != nil {
		t.Fatal(err)
	}
	output, ok := ParseGenerateImageOutput(raw)
	if !ok || output.ErrorCode != "runtime_configuration_changed" ||
		generator.calls != 0 || recorder.operationCalls != 0 {
		t.Fatalf("output=%s provider=%d journal=%d", raw, generator.calls, recorder.operationCalls)
	}

	// A second operation in the same run must reach the journal after the
	// verifier succeeds, proving the failed verifier did not reserve quota.
	allowed.Store(true)
	second, err := preparer.PrepareBillableIntent(
		context.Background(), `{"prompt":"second"}`, "call-runtime-2",
	)
	if err != nil {
		t.Fatal(err)
	}
	secondCtx := toolpolicy.WithRunID(context.Background(), "same-turn")
	secondCtx = toolpolicy.WithBillableIntent(secondCtx, second)
	raw, err = imageTool.InvokableRun(secondCtx, `{"prompt":"second"}`)
	if err != nil {
		t.Fatal(err)
	}
	output, ok = ParseGenerateImageOutput(raw)
	if !ok || output.ErrorCode != "journal_persist_failed" || recorder.operationCalls != 1 {
		t.Fatalf("second output=%s journal=%d", raw, recorder.operationCalls)
	}
}

func TestGenerateImageSchemaHasNoCount(t *testing.T) {
	imageTool := NewGenerateImageTool(nil)
	info, err := imageTool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(info)
	if bytes.Contains(encoded, []byte(`"count"`)) {
		t.Fatalf("P0 schema exposes count: %s", encoded)
	}
}

func TestGenerateImageXAINativeSchemaAndLegacySizeNormalization(t *testing.T) {
	deps := &GenerateImageDeps{
		SupportedAspectRatios: []string{"1:1", "16:9", "9:16", "3:2", "2:3", "auto"},
		SupportedResolutions:  []string{"1k", "2k"},
	}
	imageTool := NewGenerateImageTool(deps)
	info, err := imageTool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"size"`)) ||
		!bytes.Contains(encoded, []byte(`"aspect_ratio"`)) ||
		!bytes.Contains(encoded, []byte(`"resolution"`)) {
		t.Fatalf("xAI schema = %s", encoded)
	}
	parsed, normalized, err := imageTool.(*generateImageTool).parseInput(
		`{"prompt":" portrait ","size":"1024x1792"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Prompt != "portrait" || parsed.Size != "" || parsed.AspectRatio != "9:16" ||
		parsed.Resolution != "1k" {
		t.Fatalf("parsed = %#v", parsed)
	}
	if normalized != `{"prompt":"portrait","aspect_ratio":"9:16","resolution":"1k"}` {
		t.Fatalf("normalized = %s", normalized)
	}
}

func TestGenerateImageRejectsAmbiguousOrUnsupportedNativeGeometry(t *testing.T) {
	imageTool := NewGenerateImageTool(&GenerateImageDeps{
		SupportedAspectRatios: []string{"1:1", "16:9"},
		SupportedResolutions:  []string{"1k", "2k"},
	}).(*generateImageTool)
	for _, raw := range []string{
		`{"prompt":"x","size":"1024x1024","aspect_ratio":"1:1"}`,
		`{"prompt":"x","aspect_ratio":"9:16"}`,
		`{"prompt":"x","resolution":"4k"}`,
		`{"prompt":"x","size":"800x600"}`,
	} {
		if _, _, err := imageTool.parseInput(raw); err == nil {
			t.Fatalf("parseInput(%s) succeeded", raw)
		}
	}
}

func TestGenerateImageLegacySizeDispatchesApprovedNativeGeometry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := session.NewRecorder(t.TempDir(), "xai", "grok-4.5")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("generate a portrait")
	generator := &stubImageGenerator{result: imagegen.Result{Images: []imagegen.Image{{
		Data: generatedPNG(t, 2, 3), MIMEType: "image/png", Width: 2, Height: 3,
	}}}}
	imageTool := NewGenerateImageTool(&GenerateImageDeps{
		Generator: generator,
		ArtifactService: artifact.NewServiceWithManagedRoot(
			session.LoadArtifactRecords, nil, filepath.Join(t.TempDir(), "managed"),
		),
		Recorder: recorder, Ledger: toolpolicy.NewUsageLedger(1, 20, 0),
		Provider: "xai", Model: "grok-imagine-image", EndpointProfile: "xai-images",
		CredentialKind: "managed_account", CredentialFingerprint: "account-1",
		ConfigEpoch: "0000000000000001", DispatchPolicy: imageDispatchPolicy(),
		VerifyRuntime:         allowImageRuntime,
		SupportedAspectRatios: []string{"1:1", "9:16"},
		SupportedResolutions:  []string{"1k", "2k"},
	})
	rawArgs := `{"prompt":"portrait","size":"1024x1792"}`
	intent, err := imageTool.(toolpolicy.BillableIntentPreparer).PrepareBillableIntent(
		context.Background(), rawArgs, "legacy-size-call",
	)
	if err != nil {
		t.Fatal(err)
	}
	if intent.NormalizedArgs != `{"prompt":"portrait","aspect_ratio":"9:16","resolution":"1k"}` {
		t.Fatalf("approved args = %s", intent.NormalizedArgs)
	}
	ctx := toolpolicy.WithBillableIntent(context.Background(), intent)
	if _, err := imageTool.InvokableRun(ctx, rawArgs); err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 || generator.input.Size != "" || generator.input.AspectRatio != "9:16" ||
		generator.input.Resolution != "1k" {
		t.Fatalf("provider request calls=%d input=%#v", generator.calls, generator.input)
	}
}

type atomicImageGenerator struct {
	result imagegen.Result
	calls  atomic.Int64
}

func (g *atomicImageGenerator) Generate(context.Context, imagegen.Request) (imagegen.Result, error) {
	g.calls.Add(1)
	return g.result, nil
}

func TestGenerateImageSharedLedgerAdmitsOnlyOneConcurrentCallPerRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("generate an image")
	generator := &atomicImageGenerator{result: imagegen.Result{Images: []imagegen.Image{{
		Data: generatedPNG(t, 2, 2), MIMEType: "image/png", Width: 2, Height: 2,
	}}}}
	imageTool := NewGenerateImageTool(&GenerateImageDeps{
		Generator: generator,
		ArtifactService: artifact.NewServiceWithManagedRoot(
			session.LoadArtifactRecords, nil, filepath.Join(t.TempDir(), "managed"),
		),
		Recorder: recorder, Ledger: toolpolicy.NewUsageLedger(1, 20, 0),
		Provider: "provider-image", Model: "image-1", EndpointProfile: "endpoint-1",
		CredentialKind: "api_key", CredentialFingerprint: "credential-fingerprint",
		ConfigEpoch: "0000000000000001", SupportedSizes: []string{"1024x1024"},
		DispatchPolicy: imageDispatchPolicy(),
		VerifyRuntime:  allowImageRuntime,
	})
	preparer := imageTool.(toolpolicy.BillableIntentPreparer)
	intents := make([]toolpolicy.BillableIntent, 2)
	for index, callID := range []string{"call-concurrent-1", "call-concurrent-2"} {
		intents[index], err = preparer.PrepareBillableIntent(
			context.Background(), `{"prompt":"a quiet desk"}`, callID,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	outputs := make(chan GenerateImageOutput, 2)
	var wg sync.WaitGroup
	for _, intent := range intents {
		wg.Add(1)
		go func(intent toolpolicy.BillableIntent) {
			defer wg.Done()
			<-start
			ctx := toolpolicy.WithRunID(context.Background(), "same-user-turn")
			ctx = toolpolicy.WithBillableIntent(ctx, intent)
			raw, runErr := imageTool.InvokableRun(ctx, `{"prompt":"a quiet desk"}`)
			if runErr != nil {
				t.Errorf("InvokableRun: %v", runErr)
				return
			}
			output, ok := ParseGenerateImageOutput(raw)
			if !ok {
				t.Errorf("invalid output: %s", raw)
				return
			}
			outputs <- output
		}(intent)
	}
	close(start)
	wg.Wait()
	close(outputs)

	succeeded, limited := 0, 0
	for output := range outputs {
		switch {
		case output.Outcome == string(toolstate.OutcomeSucceeded):
			succeeded++
		case output.ErrorCode == "provider_call_limit":
			limited++
		}
	}
	if succeeded != 1 || limited != 1 || generator.calls.Load() != 1 {
		t.Fatalf("succeeded=%d limited=%d provider_calls=%d", succeeded, limited, generator.calls.Load())
	}
}

func imageDispatchPolicy() session.DispatchPolicy {
	return session.DispatchPolicy{
		Tool: session.SessionToolImageGeneration, MaxPerSession: 20,
	}
}

func allowImageRuntime(context.Context) error { return nil }

func generatedPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 80, G: 140, B: 220, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
