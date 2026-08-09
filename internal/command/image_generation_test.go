package command

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"github.com/cnjack/jcode/internal/artifact"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/imagegen"
	"github.com/cnjack/jcode/internal/providertools"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/toolpolicy"
	internaltools "github.com/cnjack/jcode/internal/tools"
	"github.com/cnjack/jcode/internal/toolstate"
)

func TestConfiguredGenerateImageToolDependsOnlyOnIndependentImageRole(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	service := artifact.NewServiceWithManagedRoot(
		session.LoadArtifactRecords, nil, filepath.Join(t.TempDir(), "managed"),
	)
	ledger, err := newImageUsageLedger(recorder)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Model: "openai/gpt-5", Providers: map[string]*config.ProviderConfig{
		"openai": {APIKey: "chat-secret"},
		"custom": {
			APIKey: "configured-secret",
			ImageEndpoint: &config.ImageEndpointConfig{
				Protocol: string(imagegen.ProtocolOpenAIImages), BaseURL: "https://images.example/v1",
				Models: []config.ImageModelConfig{{ID: "paint-1", Sizes: []string{"1024x1024"}}},
			},
		},
	}}
	loader := testProviderRuntimeConfigLoader(cfg)
	if _, err := configuredGenerateImageTool(
		cfg, service, recorder, ledger, loader, nil, nil,
	); err == nil {
		t.Fatal("tool was registered without an independently selected image model")
	}
	cfg.ImageModel = "custom/paint-1"
	imageTool, err := configuredGenerateImageTool(
		cfg, service, recorder, ledger, loader, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	info, err := imageTool.Info(context.Background())
	if err != nil || info == nil || info.Name != "generate_image" {
		t.Fatalf("tool info=%#v err=%v", info, err)
	}
	cfg.Providers["custom"].ProviderTools = map[string]config.ProviderToolPolicy{
		providertools.ToolImageGeneration: {Enabled: false},
	}
	if _, err := configuredGenerateImageTool(
		cfg, service, recorder, ledger, loader, nil, nil,
	); err != nil {
		t.Fatalf("legacy provider policy disabled the independent image role: %v", err)
	}
}

func TestGenerateImageCatalogIsNormalModeOnlyAcrossTransports(t *testing.T) {
	for _, transport := range []string{"tui", "acp", "web"} {
		normal, err := buildCommandToolPlan(
			context.Background(), catalogTools("generate_image"), nil, transport, "normal",
		)
		if err != nil {
			t.Fatalf("%s normal: %v", transport, err)
		}
		assertCatalogDescriptorNames(t, transport+" normal direct", normal.Direct, []string{"generate_image"})
		planning, err := buildCommandToolPlan(
			context.Background(), catalogTools("generate_image"), nil, transport, "plan",
		)
		if err != nil {
			t.Fatalf("%s plan: %v", transport, err)
		}
		assertCatalogDescriptorNames(t, transport+" plan hidden", planning.Hidden, []string{"generate_image"})
	}
}

func TestConfiguredGenerateImageRejectsRuntimeRotationBeforeJournalOrProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("generate an image")
	ledger, err := newImageUsageLedger(recorder)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		ImageModel: "custom/paint-1",
		Providers: map[string]*config.ProviderConfig{
			"custom": {
				APIKey: "configured-secret", ProviderTools: map[string]config.ProviderToolPolicy{
					providertools.ToolImageGeneration: {Enabled: true},
				},
				ImageEndpoint: &config.ImageEndpointConfig{
					Protocol: string(imagegen.ProtocolOpenAIImages), BaseURL: "https://images.example/v1",
					Models: []config.ImageModelConfig{{ID: "paint-1", Sizes: []string{"1024x1024"}}},
				},
			},
		},
	}
	service := artifact.NewServiceWithManagedRoot(
		session.LoadArtifactRecords, nil, filepath.Join(t.TempDir(), "managed"),
	)
	imageTool, err := configuredGenerateImageTool(
		cfg, service, recorder, ledger, testProviderRuntimeConfigLoader(cfg), nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := imageTool.(toolpolicy.BillableIntentPreparer).PrepareBillableIntent(
		context.Background(), `{"prompt":"pending approval"}`, "image-runtime-call",
	)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Providers["custom"].APIKey = "rotated-after-approval"
	ctx := toolpolicy.WithRunID(context.Background(), "image-runtime-turn")
	ctx = toolpolicy.WithBillableIntent(ctx, intent)
	raw, err := imageTool.(interface {
		InvokableRun(context.Context, string, ...tool.Option) (string, error)
	}).InvokableRun(ctx, `{"prompt":"pending approval"}`)
	if err != nil {
		t.Fatal(err)
	}
	output, ok := internaltools.ParseGenerateImageOutput(raw)
	if !ok || output.Outcome != string(toolstate.OutcomeFailed) ||
		output.ErrorCode != "runtime_configuration_changed" {
		t.Fatalf("output = %s", raw)
	}
	operations, loadErr := session.LoadGenerationOperations(recorder.UUID())
	if loadErr != nil || len(operations) != 0 {
		t.Fatalf("rotated image operations=%#v err=%v", operations, loadErr)
	}
}
