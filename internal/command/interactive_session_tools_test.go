package command

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/providertools"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/toolpolicy"
	internaltools "github.com/cnjack/jcode/internal/tools"
)

func TestTUISessionToolEnvironmentPolicyKeepsEngineImageButDropsRemoteSearch(t *testing.T) {
	configurable := session.SupportedSessionTools()
	if len(configurable) != 0 {
		t.Fatalf("TUI configurable session tools = %#v", configurable)
	}
	if session.IsConfigurableSessionTool(session.SessionToolWebSearch) {
		t.Fatal("legacy web_search override remains configurable")
	}
	if !sessionToolAllowedInEnvironment(session.SessionToolImageGeneration, true) {
		t.Fatal("remote coding target incorrectly disabled local JCode image generation")
	}
	if sessionToolAllowedInEnvironment(session.SessionToolWebSearch, true) {
		t.Fatal("remote coding target exposed provider-managed web search")
	}
}

func TestTUIProviderCatalogFollowsActiveModelSwitchWithoutReconnect(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := providerSearchCommandConfig(2, 10)
	cfg.Providers["openai"] = &config.ProviderConfig{APIKey: "chat-credential"}
	cfg.Model = "openai/gpt-5"
	server := providerSearchServerName(t, cfg)
	const (
		searchName  = "mcp__provider_search_tui_switch__web_search_prime"
		genericName = "mcp__provider_search_tui_switch__generic"
	)
	internaltools.RegisterMCPToolIdentity(searchName, server, "web_search_prime")
	internaltools.RegisterMCPToolIdentity(genericName, "generic-tui-switch", "generic")
	searchEndpoint := &providerSearchCommandTool{info: &schema.ToolInfo{Name: searchName}}
	genericEndpoint := &providerSearchCommandTool{info: &schema.ToolInfo{Name: genericName}}
	catalog := newProviderSearchMCPCatalog(
		cfg, []tool.BaseTool{searchEndpoint, genericEndpoint},
	)
	recorder, err := session.NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	ledger, err := newProviderSearchUsageLedger(recorder)
	if err != nil {
		t.Fatal(err)
	}
	state := &interactiveState{
		ctx: context.Background(), cfg: cfg, rec: recorder,
		providerSearchLedger: ledger,
		rawMCPTools:          catalog.Tools,
		rawMCPConfigEpoch:    catalog.ConfigEpoch,
		activeProvider:       "openai",
		activeModel:          "gpt-5",
		pwd:                  t.TempDir(),
	}
	otherTools, otherErr := state.providerMCPToolsFor()
	if otherErr == nil || len(otherTools) != 1 || otherTools[0] != genericEndpoint {
		t.Fatalf("other-provider TUI tools=%#v err=%v", otherTools, otherErr)
	}

	state.activeProvider = providertools.BigModelCodingProvider
	state.activeModel = "glm-switched"
	bigModelTools, bigModelErr := state.providerMCPToolsFor()
	if bigModelErr != nil || len(bigModelTools) != 2 {
		t.Fatalf("BigModel TUI tools=%#v err=%v", bigModelTools, bigModelErr)
	}
	search := commandToolByName(t, bigModelTools, searchName)
	if _, ok := search.(toolpolicy.BillableIntentPreparer); !ok {
		t.Fatalf("BigModel TUI search type %T is not wrapped", search)
	}
}
