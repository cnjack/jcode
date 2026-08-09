package command

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/providerauth"
	"github.com/cnjack/jcode/internal/providertools"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/toolpolicy"
	internaltools "github.com/cnjack/jcode/internal/tools"
	"github.com/cnjack/jcode/internal/web"
)

func TestEvaluateWebSearchFollowsProviderAndIgnoresLegacySessionOverride(t *testing.T) {
	cfg, catalog := webSearchTestConfigAndCatalog(t)
	environment := webSessionToolEnvironment{Config: cfg, Catalog: catalog, BillableAllowed: true}

	for _, snapshot := range []map[session.SessionTool]session.SessionToolOverride{
		nil,
		{session.SessionToolWebSearch: {Tool: session.SessionToolWebSearch, Persisted: false, Revision: 9}},
		{session.SessionToolWebSearch: {Tool: session.SessionToolWebSearch, Persisted: true, Revision: 10}},
	} {
		evaluation := evaluateWebSessionTool(
			context.Background(), session.SessionToolWebSearch, environment,
		)
		if !evaluation.Available || !evaluation.Effective || evaluation.DisabledReason != "" {
			t.Fatalf("provider-owned search evaluation = %#v for snapshot %#v", evaluation, snapshot)
		}
	}

	plan := environment
	plan.PlanMode = true
	evaluation := evaluateWebSessionTool(
		context.Background(), session.SessionToolWebSearch, plan,
	)
	if evaluation.Available || evaluation.Effective || evaluation.DisabledReason != web.SessionToolDisabledPlanMode {
		t.Fatalf("plan evaluation = %#v", evaluation)
	}

	cfg.Model = "openai/gpt-5"
	cfg.Providers["openai"] = &config.ProviderConfig{APIKey: "chat-credential"}
	evaluation = evaluateWebSessionTool(
		context.Background(), session.SessionToolWebSearch, environment,
	)
	if evaluation.Available || evaluation.Effective || evaluation.DisabledReason != web.SessionToolDisabledUnsupported {
		t.Fatalf("cross-provider evaluation = %#v", evaluation)
	}
}

func TestConfiguredWebSearchCatalogIgnoresLegacyOverrideAndKeepsBoundaries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, catalog := webSearchTestConfigAndCatalog(t)
	recorder, err := session.NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	if _, err := recorder.CompareAndSwapSessionToolOverride(session.SessionToolWebSearch, false, 0); err != nil {
		t.Fatal(err)
	}
	ledger, err := newProviderSearchUsageLedger(recorder)
	if err != nil {
		t.Fatal(err)
	}
	legacyDisabled, err := recorder.SessionToolOverrides()
	if err != nil {
		t.Fatal(err)
	}
	if legacyDisabled[session.SessionToolWebSearch].Persisted {
		t.Fatal("legacy fixture was not disabled")
	}

	configured, err := configuredProviderMCPTools(
		context.Background(), cfg, recorder, ledger, catalog, false, true,
		testProviderRuntimeConfigLoader(cfg),
	)
	if err != nil || len(configured) != 2 {
		t.Fatalf("automatic tools=%v err=%v", commandToolNames(t, configured), err)
	}
	search := commandToolByName(t, configured, "mcp__provider_search_auto__web_search_prime")
	if _, ok := search.(toolpolicy.BillableIntentPreparer); !ok {
		t.Fatalf("automatic search type %T is not billable", search)
	}

	for _, tc := range []struct {
		name           string
		plan, billable bool
		want           int
	}{
		{name: "plan", plan: true, billable: true, want: 0},
		{name: "unattended-or-remote", billable: false, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, gotErr := configuredProviderMCPTools(
				context.Background(), cfg, recorder, ledger, catalog, tc.plan, tc.billable,
				testProviderRuntimeConfigLoader(cfg),
			)
			if gotErr != nil || len(got) != tc.want {
				t.Fatalf("tools=%v err=%v", commandToolNames(t, got), gotErr)
			}
		})
	}

	cfg.Providers[providertools.BigModelCodingProvider].ProviderTools[providertools.ToolWebSearch] =
		config.ProviderToolPolicy{Enabled: false}
	filtered, err := configuredProviderMCPTools(
		context.Background(), cfg, recorder, ledger, catalog, false, true,
		testProviderRuntimeConfigLoader(cfg),
	)
	if err == nil || len(filtered) != 1 || commandToolName(t, filtered[0]) != "mcp__provider_search_auto__generic" {
		t.Fatalf("disabled provider policy tools=%v err=%v", commandToolNames(t, filtered), err)
	}
}

func TestWebProviderCatalogSupportsConcurrentTaskProviderBoundaries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, initialCatalog := webSearchTestConfigAndCatalog(t)
	cfg.Providers["openai"] = &config.ProviderConfig{APIKey: "chat-credential"}
	cfg.Model = "openai/gpt-5"
	// The detached catalog is process-wide and must remain ready even when the
	// persisted/default task uses another provider.
	catalog := newProviderSearchMCPCatalog(cfg, initialCatalog.Tools)
	if catalog.ConfigEpoch == "" {
		t.Fatal("provider transport catalog was coupled to the default chat model")
	}
	recorder, err := session.NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	ledger, err := newProviderSearchUsageLedger(recorder)
	if err != nil {
		t.Fatal(err)
	}

	otherTaskTools, otherErr := configuredProviderMCPTools(
		context.Background(), cfg, recorder, ledger, catalog, false, true,
		activeChatProviderRuntimeConfigLoader(
			testProviderRuntimeConfigLoader(cfg), "openai", "gpt-5",
		),
	)
	if otherErr == nil || len(otherTaskTools) != 1 {
		t.Fatalf("other-provider task tools=%v err=%v", commandToolNames(t, otherTaskTools), otherErr)
	}

	bigModelTaskCfg := *cfg
	projectActiveChatModel(
		&bigModelTaskCfg, providertools.BigModelCodingProvider, "glm-task",
	)
	bigModelTaskTools, bigModelErr := configuredProviderMCPTools(
		context.Background(), &bigModelTaskCfg, recorder, ledger, catalog, false, true,
		activeChatProviderRuntimeConfigLoader(
			testProviderRuntimeConfigLoader(cfg),
			providertools.BigModelCodingProvider, "glm-task",
		),
	)
	if bigModelErr != nil || len(bigModelTaskTools) != 2 {
		t.Fatalf("BigModel task tools=%v err=%v", commandToolNames(t, bigModelTaskTools), bigModelErr)
	}
	search := commandToolByName(t, bigModelTaskTools, "mcp__provider_search_auto__web_search_prime")
	if _, ok := search.(toolpolicy.BillableIntentPreparer); !ok {
		t.Fatalf("BigModel task search type %T is not wrapped", search)
	}
}

func TestRemoteWebTaskAllowsOnlyLocalManagedImageGeneration(t *testing.T) {
	if !webTaskBillableAllowed(session.SessionToolImageGeneration, true, false) {
		t.Fatal("remote tasks must allow image generation through the local JCode provider runtime")
	}
	if webTaskBillableAllowed(session.SessionToolWebSearch, true, false) {
		t.Fatal("remote tasks must not expose provider-managed web search")
	}
	if webTaskBillableAllowed(session.SessionToolImageGeneration, false, true) {
		t.Fatal("unattended tasks must not expose billable image generation")
	}
}

func TestManagedXAIImageGenerationPassesAvailabilityGate(t *testing.T) {
	cfg := &config.Config{
		ImageModel: "xai/grok-imagine-image-quality",
		Providers: map[string]*config.ProviderConfig{
			"xai": {Auth: &config.ProviderAuthBinding{
				Method: string(providerauth.MethodXAIOAuth), AccountID: "account-1",
			}},
		},
	}
	available, reason := evaluateImageGenerationAvailability(cfg)
	if !available || reason != "" {
		t.Fatalf("managed xAI image availability = %v, %q", available, reason)
	}
	if !imageGenerationEnabled(cfg, false, true) {
		t.Fatal("managed xAI image model was filtered before tool construction")
	}
}

func webSearchTestConfigAndCatalog(t *testing.T) (*config.Config, *providerSearchMCPCatalog) {
	t.Helper()
	cfg := providerSearchCommandConfig(2, 10)
	server := providerSearchServerName(t, cfg)
	const (
		searchName  = "mcp__provider_search_auto__web_search_prime"
		genericName = "mcp__provider_search_auto__generic"
	)
	internaltools.RegisterMCPToolIdentity(searchName, server, "web_search_prime")
	internaltools.RegisterMCPToolIdentity(genericName, "generic-provider-search-auto", "generic")
	search := &providerSearchCommandTool{info: &schema.ToolInfo{Name: searchName}}
	generic := &providerSearchCommandTool{info: &schema.ToolInfo{Name: genericName}}
	return cfg, newProviderSearchMCPCatalog(cfg, []tool.BaseTool{search, generic})
}

func commandToolName(t *testing.T, candidate tool.BaseTool) string {
	t.Helper()
	info, err := candidate.Info(context.Background())
	if err != nil || info == nil {
		t.Fatalf("tool info = %#v, %v", info, err)
	}
	return info.Name
}

func commandToolNames(t *testing.T, candidates []tool.BaseTool) []string {
	t.Helper()
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, commandToolName(t, candidate))
	}
	return names
}

func testProviderRuntimeConfigLoader(cfg *config.Config) providerRuntimeConfigLoader {
	return func(ctx context.Context) (*config.Config, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return cfg, nil
	}
}
