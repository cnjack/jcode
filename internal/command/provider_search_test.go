package command

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/providertools"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/toolpolicy"
	internaltools "github.com/cnjack/jcode/internal/tools"
)

type providerSearchCommandTool struct {
	info   *schema.ToolInfo
	calls  atomic.Int32
	result string
}

func (t *providerSearchCommandTool) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *providerSearchCommandTool) InvokableRun(
	context.Context,
	string,
	...tool.Option,
) (string, error) {
	t.calls.Add(1)
	return t.result, nil
}

type providerSearchInfoErrorTool struct{}

func (providerSearchInfoErrorTool) Info(context.Context) (*schema.ToolInfo, error) {
	return nil, errors.New("info unavailable")
}

func TestConfiguredProviderSearchMCPToolsWrapsWithSharedSessionLedger(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := providerSearchCommandConfig(2, 1)
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("search")
	ledger, err := newProviderSearchUsageLedger(recorder)
	if err != nil {
		t.Fatal(err)
	}
	server := providerSearchServerName(t, cfg)
	const (
		searchName  = "mcp__provider_search_command__web_search_prime"
		genericName = "mcp__provider_search_command__generic"
	)
	internaltools.RegisterMCPToolIdentity(searchName, server, "web_search_prime")
	internaltools.RegisterMCPToolIdentity(genericName, "generic-server", "generic")
	searchEndpoint := &providerSearchCommandTool{
		info: &schema.ToolInfo{Name: searchName}, result: "search-result",
	}
	genericEndpoint := &providerSearchCommandTool{
		info: &schema.ToolInfo{Name: genericName}, result: "generic-result",
	}

	wrapped, err := configuredProviderSearchMCPTools(
		context.Background(), cfg, recorder, ledger,
		[]tool.BaseTool{searchEndpoint, genericEndpoint},
		testProviderRuntimeConfigLoader(cfg),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrapped) != 2 {
		t.Fatalf("wrapped candidates = %d", len(wrapped))
	}
	searchTool := commandToolByName(t, wrapped, searchName)
	if _, ok := searchTool.(toolpolicy.BillableIntentPreparer); !ok {
		t.Fatalf("provider search type %T lacks BillableIntentPreparer", searchTool)
	}
	if commandToolByName(t, wrapped, genericName) != genericEndpoint {
		t.Fatal("generic MCP endpoint was unexpectedly wrapped")
	}
	invokeProviderSearchCommandTool(t, searchTool, "turn-1", "call-1")

	// Simulate a Web model/config rebuild: create a fresh wrapper around the raw
	// MCP endpoint but retain this task's ledger. The session limit must not reset.
	rebuilt, err := configuredProviderSearchMCPTools(
		context.Background(), cfg, recorder, ledger,
		[]tool.BaseTool{searchEndpoint, genericEndpoint},
		testProviderRuntimeConfigLoader(cfg),
	)
	if err != nil {
		t.Fatal(err)
	}
	rebuiltSearch := commandToolByName(t, rebuilt, searchName)
	preparer := rebuiltSearch.(toolpolicy.BillableIntentPreparer)
	intent, err := preparer.PrepareBillableIntent(
		context.Background(), `{"query":"jcode"}`, "call-2",
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := toolpolicy.WithRunID(context.Background(), "turn-2")
	ctx = toolpolicy.WithBillableIntent(ctx, intent)
	if _, err := rebuiltSearch.(tool.InvokableTool).InvokableRun(ctx, `{"query":"jcode"}`); err == nil ||
		!strings.Contains(err.Error(), "session") {
		t.Fatalf("rebuilt wrapper reset session limit: %v", err)
	}
	if searchEndpoint.calls.Load() != 1 || genericEndpoint.calls.Load() != 0 {
		t.Fatalf("provider calls search=%d generic=%d", searchEndpoint.calls.Load(), genericEndpoint.calls.Load())
	}
	operations, err := session.LoadProviderToolOperations(recorder.UUID())
	if err != nil || len(operations) != 1 || !operations[0].Dispatched {
		t.Fatalf("operations=%#v err=%v", operations, err)
	}
}

func TestConfiguredProviderSearchMCPToolsFailsClosedAndPlanHides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := providerSearchCommandConfig(2, 10)
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := newProviderSearchUsageLedger(recorder)
	if err != nil {
		t.Fatal(err)
	}
	server := providerSearchServerName(t, cfg)
	const (
		searchName  = "mcp__provider_search_plan__web_search_prime"
		genericName = "mcp__provider_search_plan__generic"
	)
	internaltools.RegisterMCPToolIdentity(searchName, server, "web_search_prime")
	internaltools.RegisterMCPToolIdentity(genericName, "generic-plan-server", "generic")
	searchEndpoint := &providerSearchCommandTool{info: &schema.ToolInfo{Name: searchName}}
	genericEndpoint := &providerSearchCommandTool{info: &schema.ToolInfo{Name: genericName}}

	wrapped, err := configuredProviderSearchMCPTools(
		context.Background(), cfg, recorder, ledger,
		[]tool.BaseTool{searchEndpoint, genericEndpoint},
		testProviderRuntimeConfigLoader(cfg),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildCommandToolPlan(
		context.Background(), catalogTools("read"), wrapped,
		agent.ToolTransportWeb, agent.ToolModePlan,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.AllRuntimeTools()) != 1 || len(plan.Hidden) != 2 {
		t.Fatalf("plan runtime=%d hidden=%d", len(plan.AllRuntimeTools()), len(plan.Hidden))
	}

	// A hot reload that disables the provider policy must remove the reserved
	// endpoint while preserving unrelated MCP tools.
	cfg.Providers[providertools.BigModelCodingProvider].ProviderTools[providertools.ToolWebSearch] =
		config.ProviderToolPolicy{Enabled: false}
	filtered, err := configuredProviderSearchMCPTools(
		context.Background(), cfg, recorder, ledger,
		[]tool.BaseTool{searchEndpoint, genericEndpoint},
		testProviderRuntimeConfigLoader(cfg),
	)
	if err == nil {
		t.Fatal("disabled provider search runtime did not fail closed")
	}
	if len(filtered) != 1 || filtered[0] != genericEndpoint {
		t.Fatalf("fail-closed tools = %#v", filtered)
	}
}

func TestConfiguredProviderSearchMCPToolsDropsUnidentifiableCandidate(t *testing.T) {
	generic := &providerSearchCommandTool{info: &schema.ToolInfo{Name: "generic-no-owner"}}
	filtered, err := configuredProviderSearchMCPTools(
		context.Background(), nil, nil, nil,
		[]tool.BaseTool{providerSearchInfoErrorTool{}, generic},
		testProviderRuntimeConfigLoader(nil),
	)
	if err == nil || len(filtered) != 1 || filtered[0] != generic {
		t.Fatalf("filtered=%#v err=%v", filtered, err)
	}
}

func TestConfiguredProviderSearchMCPToolsDropsUnknownReservedServerTool(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := providerSearchCommandConfig(2, 10)
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("search")
	ledger, err := newProviderSearchUsageLedger(recorder)
	if err != nil {
		t.Fatal(err)
	}
	server := providerSearchServerName(t, cfg)
	const (
		unknownName = "mcp__provider_search_unknown__delete_everything"
		genericName = "mcp__provider_search_unknown__generic"
	)
	internaltools.RegisterMCPToolIdentity(unknownName, server, "delete_everything")
	internaltools.RegisterMCPToolIdentity(genericName, "generic-unknown-server", "generic")
	unknown := &providerSearchCommandTool{info: &schema.ToolInfo{Name: unknownName}}
	generic := &providerSearchCommandTool{info: &schema.ToolInfo{Name: genericName}}

	filtered, err := configuredProviderSearchMCPTools(
		context.Background(), cfg, recorder, ledger, []tool.BaseTool{unknown, generic},
		testProviderRuntimeConfigLoader(cfg),
	)
	if err == nil || !strings.Contains(err.Error(), "unverified provider search MCP tool") {
		t.Fatalf("error = %v", err)
	}
	if len(filtered) != 1 || filtered[0] != generic {
		t.Fatalf("filtered = %#v", filtered)
	}
}

func TestConfiguredProviderSearchMCPCatalogRejectsStaleCredentialEpoch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := providerSearchCommandConfig(2, 10)
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	ledger, err := newProviderSearchUsageLedger(recorder)
	if err != nil {
		t.Fatal(err)
	}
	server := providerSearchServerName(t, cfg)
	const (
		searchName  = "mcp__provider_search_epoch__web_search_prime"
		genericName = "mcp__provider_search_epoch__generic"
	)
	internaltools.RegisterMCPToolIdentity(searchName, server, "web_search_prime")
	internaltools.RegisterMCPToolIdentity(genericName, "generic-epoch-server", "generic")
	searchEndpoint := &providerSearchCommandTool{info: &schema.ToolInfo{Name: searchName}}
	genericEndpoint := &providerSearchCommandTool{info: &schema.ToolInfo{Name: genericName}}
	catalog := newProviderSearchMCPCatalog(
		cfg, []tool.BaseTool{searchEndpoint, genericEndpoint},
	)

	current, err := configuredProviderSearchMCPCatalog(
		context.Background(), cfg, recorder, ledger, catalog,
		testProviderRuntimeConfigLoader(cfg),
	)
	if err != nil || len(current) != 2 {
		t.Fatalf("current catalog=%#v err=%v", current, err)
	}

	// The raw endpoint still holds the old key until its MCP process reconnects.
	// A task rebuild against the rotated config must expose only generic tools.
	cfg.Providers[providertools.BigModelCodingProvider].APIKey = "rotated-credential-canary"
	filtered, err := configuredProviderSearchMCPCatalog(
		context.Background(), cfg, recorder, ledger, catalog,
		testProviderRuntimeConfigLoader(cfg),
	)
	if err == nil || len(filtered) != 1 || filtered[0] != genericEndpoint {
		t.Fatalf("stale catalog=%#v err=%v", filtered, err)
	}
}

func TestProviderSearchDispatchRejectsRuntimeRotationBeforeJournalOrEndpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := providerSearchCommandConfig(1, 10)
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("search")
	ledger, err := newProviderSearchUsageLedger(recorder)
	if err != nil {
		t.Fatal(err)
	}
	server := providerSearchServerName(t, cfg)
	const searchName = "mcp__provider_search_runtime__web_search_prime"
	internaltools.RegisterMCPToolIdentity(searchName, server, "web_search_prime")
	endpoint := &providerSearchCommandTool{
		info: &schema.ToolInfo{Name: searchName}, result: "search-result",
	}
	configured, err := configuredProviderSearchMCPTools(
		context.Background(), cfg, recorder, ledger, []tool.BaseTool{endpoint},
		testProviderRuntimeConfigLoader(cfg),
	)
	if err != nil || len(configured) != 1 {
		t.Fatalf("configured=%v err=%v", commandToolNames(t, configured), err)
	}
	search := configured[0]
	intent, err := search.(toolpolicy.BillableIntentPreparer).PrepareBillableIntent(
		context.Background(), `{"query":"pending approval"}`, "runtime-call-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Providers[providertools.BigModelCodingProvider].APIKey = "rotated-after-approval"
	ctx := toolpolicy.WithRunID(context.Background(), "runtime-turn")
	ctx = toolpolicy.WithBillableIntent(ctx, intent)
	if _, err := search.(tool.InvokableTool).InvokableRun(
		ctx, `{"query":"pending approval"}`,
	); err == nil || !strings.Contains(err.Error(), "runtime configuration") {
		t.Fatalf("runtime rotation error = %v", err)
	}
	if endpoint.calls.Load() != 0 {
		t.Fatalf("rotated runtime endpoint calls = %d", endpoint.calls.Load())
	}
	operations, loadErr := session.LoadProviderToolOperations(recorder.UUID())
	if loadErr != nil || len(operations) != 0 {
		t.Fatalf("rotated runtime operations=%#v err=%v", operations, loadErr)
	}
}

func TestProviderSearchDispatchIgnoresLegacySessionOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := providerSearchCommandConfig(1, 10)
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	override, err := recorder.CompareAndSwapSessionToolOverride(
		session.SessionToolWebSearch, false, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := newProviderSearchUsageLedger(recorder)
	if err != nil {
		t.Fatal(err)
	}
	server := providerSearchServerName(t, cfg)
	const searchName = "mcp__provider_search_revision__web_search_prime"
	internaltools.RegisterMCPToolIdentity(searchName, server, "web_search_prime")
	endpoint := &providerSearchCommandTool{
		info: &schema.ToolInfo{Name: searchName}, result: "search-result",
	}
	configured, err := configuredProviderSearchMCPTools(
		context.Background(), cfg, recorder, ledger, []tool.BaseTool{endpoint},
		testProviderRuntimeConfigLoader(cfg),
	)
	if err != nil || len(configured) != 1 {
		t.Fatalf("configured=%v err=%v", commandToolNames(t, configured), err)
	}
	search := configured[0]
	intent, err := search.(toolpolicy.BillableIntentPreparer).PrepareBillableIntent(
		context.Background(), `{"query":"pending approval"}`, "revision-call-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.CompareAndSwapSessionToolOverride(
		session.SessionToolWebSearch, true, override.Revision,
	); err != nil {
		t.Fatal(err)
	}
	ctx := toolpolicy.WithRunID(context.Background(), "revision-turn")
	ctx = toolpolicy.WithBillableIntent(ctx, intent)
	if result, err := search.(tool.InvokableTool).InvokableRun(
		ctx, `{"query":"pending approval"}`,
	); err != nil || result != "search-result" {
		t.Fatalf("legacy override gated provider search: result=%q err=%v", result, err)
	}
	if endpoint.calls.Load() != 1 {
		t.Fatalf("provider endpoint calls = %d", endpoint.calls.Load())
	}
	operations, loadErr := session.LoadProviderToolOperations(recorder.UUID())
	if loadErr != nil || len(operations) != 1 || !operations[0].Dispatched {
		t.Fatalf("operations=%#v err=%v", operations, loadErr)
	}
}

func TestProjectActiveChatModelControlsProviderSearchForSwitchesAndRoles(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		persistedModel              string
		activeProvider, activeModel string
		wantSearch                  bool
	}{
		{
			name: "switch into BigModel before config save", persistedModel: "openai/gpt-5",
			activeProvider: providertools.BigModelCodingProvider, activeModel: "glm-4.7", wantSearch: true,
		},
		{
			name:           "switch away from BigModel before config save",
			persistedModel: providertools.BigModelCodingProvider + "/glm-4.7",
			activeProvider: "openai", activeModel: "gpt-5", wantSearch: false,
		},
		{
			name: "BigModel custom role overrides other persisted model", persistedModel: "openai/gpt-5",
			activeProvider: providertools.BigModelCodingProvider, activeModel: "glm-role", wantSearch: true,
		},
		{
			name:           "other-provider custom role overrides persisted BigModel",
			persistedModel: providertools.BigModelCodingProvider + "/glm-4.7",
			activeProvider: "openai", activeModel: "gpt-role", wantSearch: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := providerSearchCommandConfig(2, 10)
			cfg.Model = tc.persistedModel
			cfg.Providers["openai"] = &config.ProviderConfig{APIKey: "openai-credential"}
			projectActiveChatModel(cfg, tc.activeProvider, tc.activeModel)
			_, err := providertools.ResolveWebSearchRuntime(cfg)
			if tc.wantSearch && err != nil {
				t.Fatalf("active BigModel search unavailable: %v", err)
			}
			if !tc.wantSearch && err == nil {
				t.Fatal("provider search followed stale persisted model instead of active chat provider")
			}
		})
	}
}

func TestActiveChatProviderRuntimeLoaderProjectsSelectionButReloadsPolicy(t *testing.T) {
	base := providerSearchCommandConfig(2, 10)
	base.Model = "openai/gpt-5"
	base.Providers["openai"] = &config.ProviderConfig{APIKey: "openai-credential"}
	loader := activeChatProviderRuntimeConfigLoader(
		cloningProviderRuntimeConfigLoader(base),
		providertools.BigModelCodingProvider, "glm-role",
	)

	projected, err := loader(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	provider, modelID := projected.GetProviderModel()
	if provider != providertools.BigModelCodingProvider || modelID != "glm-role" {
		t.Fatalf("projected chat model = %s/%s", provider, modelID)
	}
	expected, err := providertools.ResolveWebSearchRuntime(projected)
	if err != nil {
		t.Fatal(err)
	}
	if err := webSearchRuntimeVerifier(expected, loader)(context.Background()); err != nil {
		t.Fatalf("projected runtime rejected: %v", err)
	}

	// The projection must not freeze credentials or policy captured at build
	// time. Dispatch verification still reloads them after approval.
	base.Providers[providertools.BigModelCodingProvider].APIKey = "rotated-credential"
	if err := webSearchRuntimeVerifier(expected, loader)(context.Background()); err == nil {
		t.Fatal("credential rotation was hidden by active-model projection")
	}
}

func TestProviderSearchCatalogDoesNotLeakAcrossActiveChatProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bigModelCfg := providerSearchCommandConfig(2, 10)
	bigModelCfg.Providers["openai"] = &config.ProviderConfig{APIKey: "openai-credential"}
	server := providerSearchServerName(t, bigModelCfg)
	const (
		searchName  = "mcp__provider_search_no_leak__web_search_prime"
		genericName = "mcp__provider_search_no_leak__generic"
	)
	internaltools.RegisterMCPToolIdentity(searchName, server, "web_search_prime")
	internaltools.RegisterMCPToolIdentity(genericName, "generic-no-leak-server", "generic")
	searchEndpoint := &providerSearchCommandTool{info: &schema.ToolInfo{Name: searchName}}
	genericEndpoint := &providerSearchCommandTool{info: &schema.ToolInfo{Name: genericName}}
	catalog := newProviderSearchMCPCatalog(
		bigModelCfg, []tool.BaseTool{searchEndpoint, genericEndpoint},
	)

	// A custom role or model switch away from BigModel must remove the reserved
	// endpoint even if the process-wide catalog was connected for the persisted
	// BigModel default.
	activeOther := *bigModelCfg
	projectActiveChatModel(&activeOther, "openai", "gpt-role")
	filtered, err := configuredProviderSearchMCPCatalog(
		context.Background(), &activeOther, nil, nil, catalog,
		activeChatProviderRuntimeConfigLoader(
			cloningProviderRuntimeConfigLoader(bigModelCfg), "openai", "gpt-role",
		),
	)
	if err == nil {
		t.Fatal("cross-provider reserved search did not fail closed")
	}
	if len(filtered) != 1 || filtered[0] != genericEndpoint {
		t.Fatalf("cross-provider filtered tools = %#v", filtered)
	}

	// Conversely, the process catalog is connected from the exact enabled
	// provider profile even while the persisted/default chat model is another
	// provider. A BigModel task or role can therefore opt in without reconnecting
	// or disrupting other live tasks.
	otherCatalog := newProviderSearchMCPCatalog(
		&activeOther, []tool.BaseTool{searchEndpoint, genericEndpoint},
	)
	activeBigModel := activeOther
	projectActiveChatModel(&activeBigModel, providertools.BigModelCodingProvider, "glm-role")
	recorder, recordErr := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if recordErr != nil {
		t.Fatal(recordErr)
	}
	defer recorder.Close()
	ledger, ledgerErr := newProviderSearchUsageLedger(recorder)
	if ledgerErr != nil {
		t.Fatal(ledgerErr)
	}
	filtered, err = configuredProviderSearchMCPCatalog(
		context.Background(), &activeBigModel, recorder, ledger, otherCatalog,
		activeChatProviderRuntimeConfigLoader(
			cloningProviderRuntimeConfigLoader(bigModelCfg),
			providertools.BigModelCodingProvider, "glm-role",
		),
	)
	if err != nil || len(filtered) != 2 {
		t.Fatalf("switch-in catalog did not expose provider search: tools=%#v err=%v", filtered, err)
	}
	if commandToolByName(t, filtered, searchName) == searchEndpoint {
		t.Fatal("switch-in provider search endpoint was not policy-wrapped")
	}
}

func providerSearchCommandConfig(maxTurn, maxSession int) *config.Config {
	return &config.Config{
		Model: providertools.BigModelCodingProvider + "/glm-4.7",
		Providers: map[string]*config.ProviderConfig{
			providertools.BigModelCodingProvider: {
				APIKey:  "credential-canary",
				BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4",
				ProviderTools: map[string]config.ProviderToolPolicy{
					providertools.ToolWebSearch: {
						Enabled: true, MaxCallsPerTurn: maxTurn, MaxCallsPerSession: maxSession,
					},
				},
			},
		},
	}
}

func cloningProviderRuntimeConfigLoader(cfg *config.Config) providerRuntimeConfigLoader {
	return func(ctx context.Context) (*config.Config, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		clone := *cfg
		return &clone, nil
	}
}

func providerSearchServerName(t *testing.T, cfg *config.Config) string {
	t.Helper()
	for name := range providertools.EffectiveMCPServers(cfg) {
		if providertools.IsProviderSearchMCPServer(name) {
			return name
		}
	}
	t.Fatal("provider search MCP preset missing")
	return ""
}

func commandToolByName(t *testing.T, candidates []tool.BaseTool, name string) tool.BaseTool {
	t.Helper()
	for _, candidate := range candidates {
		info, err := candidate.Info(context.Background())
		if err == nil && info != nil && info.Name == name {
			return candidate
		}
	}
	t.Fatalf("tool %q missing", name)
	return nil
}

func invokeProviderSearchCommandTool(
	t *testing.T,
	candidate tool.BaseTool,
	runID, callID string,
) {
	t.Helper()
	preparer := candidate.(toolpolicy.BillableIntentPreparer)
	intent, err := preparer.PrepareBillableIntent(
		context.Background(), `{"query":"jcode"}`, callID,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := toolpolicy.WithRunID(context.Background(), runID)
	ctx = toolpolicy.WithBillableIntent(ctx, intent)
	if _, err := candidate.(tool.InvokableTool).InvokableRun(ctx, `{"query":"jcode"}`); err != nil {
		t.Fatal(err)
	}
}
