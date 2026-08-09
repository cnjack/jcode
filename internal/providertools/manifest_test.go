package providertools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/imagegen"
)

func TestResolveWebSearchRuntimeRequiresExactProfilePolicyAndCredential(t *testing.T) {
	base := &config.Config{Model: BigModelCodingProvider + "/glm-4.7", Providers: map[string]*config.ProviderConfig{
		BigModelCodingProvider: {
			APIKey: "credential-canary", BaseURL: bigModelCodingBaseURL,
		},
	}}
	if _, err := ResolveWebSearchRuntime(nil); err == nil {
		t.Fatal("nil config resolved")
	}
	if _, err := ResolveWebSearchRuntime(base); err == nil {
		t.Fatal("missing policy resolved")
	}
	base.Providers[BigModelCodingProvider].ProviderTools = map[string]config.ProviderToolPolicy{
		ToolWebSearch: {},
	}
	if _, err := ResolveWebSearchRuntime(base); err == nil {
		t.Fatal("disabled policy resolved")
	}
	base.Providers[BigModelCodingProvider].ProviderTools[ToolWebSearch] = config.ProviderToolPolicy{Enabled: true}
	base.Providers[BigModelCodingProvider].APIKey = ""
	if _, err := ResolveWebSearchRuntime(base); err == nil {
		t.Fatal("missing credential resolved")
	}
	base.Providers[BigModelCodingProvider].APIKey = "credential-canary"
	base.Providers[BigModelCodingProvider].BaseURL = "https://proxy.example/v4"
	if _, err := ResolveWebSearchRuntime(base); err == nil {
		t.Fatal("custom proxy inherited built-in web search")
	}

	base.Providers[BigModelCodingProvider].BaseURL = ""
	if _, err := ResolveWebSearchRuntime(base); err != nil {
		t.Fatalf("blank registry-default BaseURL did not resolve: %v", err)
	}

	base.Providers[BigModelCodingProvider].BaseURL = bigModelCodingBaseURL + "/"
	runtime, err := ResolveWebSearchRuntime(base)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ProviderProfileID != BigModelCodingProvider || runtime.ModelLabel != "web_search_prime" {
		t.Fatalf("runtime identity = %#v", runtime)
	}
	if runtime.MaxCallsPerTurn != 2 || runtime.MaxCallsPerSession != 10 {
		t.Fatalf("default limits = %d/%d", runtime.MaxCallsPerTurn, runtime.MaxCallsPerSession)
	}
}

func TestResolveWebSearchRuntimeFingerprintEpochLimitsAndSecretSafety(t *testing.T) {
	const secret = "credential-canary"
	provider := &config.ProviderConfig{
		APIKey: secret, BaseURL: bigModelCodingBaseURL,
		ProviderTools: map[string]config.ProviderToolPolicy{
			ToolWebSearch: {Enabled: true, MaxCallsPerTurn: 4, MaxCallsPerSession: 30},
		},
	}
	cfg := &config.Config{Model: BigModelCodingProvider + "/glm-4.7", Providers: map[string]*config.ProviderConfig{
		BigModelCodingProvider: provider,
	}}
	runtime, err := ResolveWebSearchRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.MaxCallsPerTurn != 4 || runtime.MaxCallsPerSession != 30 {
		t.Fatalf("configured limits = %d/%d", runtime.MaxCallsPerTurn, runtime.MaxCallsPerSession)
	}
	if runtime.CredentialFingerprint == "" || runtime.ConfigEpoch == "" ||
		runtime.CredentialFingerprint == secret {
		t.Fatalf("unsafe runtime metadata = %#v", runtime)
	}
	encoded, err := json.Marshal(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), bigModelSearchMCPURL) {
		t.Fatalf("metadata runtime exposed secret or endpoint: %s", encoded)
	}

	same, err := ResolveWebSearchRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if same.CredentialFingerprint != runtime.CredentialFingerprint || same.ConfigEpoch != runtime.ConfigEpoch {
		t.Fatalf("stable config produced unstable metadata: before=%#v after=%#v", runtime, same)
	}

	provider.APIKey = "rotated-credential"
	rotated, err := ResolveWebSearchRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.CredentialFingerprint == runtime.CredentialFingerprint || rotated.ConfigEpoch == runtime.ConfigEpoch {
		t.Fatalf("credential rotation did not change fingerprint and epoch: before=%#v after=%#v", runtime, rotated)
	}

	provider.ProviderTools[ToolWebSearch] = config.ProviderToolPolicy{
		Enabled: true, MaxCallsPerTurn: 5, MaxCallsPerSession: 30,
	}
	limitsChanged, err := ResolveWebSearchRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if limitsChanged.ConfigEpoch == rotated.ConfigEpoch {
		t.Fatalf("limit change did not advance config epoch: before=%#v after=%#v", rotated, limitsChanged)
	}
}

func TestWebSearchDoesNotCrossChatProviderBoundary(t *testing.T) {
	cfg := &config.Config{
		Model: "openai/gpt-5",
		Providers: map[string]*config.ProviderConfig{
			"openai": {APIKey: "chat-credential"},
			BigModelCodingProvider: {
				APIKey: "search-credential", BaseURL: bigModelCodingBaseURL,
				ProviderTools: map[string]config.ProviderToolPolicy{
					ToolWebSearch: {Enabled: true},
				},
			},
		},
	}
	if _, err := ResolveWebSearchRuntime(cfg); err == nil {
		t.Fatal("BigModel web search resolved for a different current chat provider")
	}
	if _, exists := EffectiveMCPServers(cfg)[bigModelSearchMCPName]; !exists {
		t.Fatal("trusted search transport was not kept ready for another task/provider switch")
	}
	cfg.Model = BigModelCodingProvider + "/glm-4.7"
	if _, err := ResolveWebSearchRuntime(cfg); err != nil {
		t.Fatalf("owner chat provider did not resolve search: %v", err)
	}
	if _, exists := EffectiveMCPServers(cfg)[bigModelSearchMCPName]; !exists {
		t.Fatal("owner chat provider did not receive search MCP")
	}
}

func TestProviderCapabilitiesExactCustomUnknownAndAbsent(t *testing.T) {
	const secret = "capability-secret-canary"
	cfg := &config.Config{
		ImageModel: "custom/canvas-2",
		Providers: map[string]*config.ProviderConfig{
			BigModelCodingProvider: {
				APIKey: secret,
				ProviderTools: map[string]config.ProviderToolPolicy{
					ToolWebSearch:       {Enabled: true},
					ToolImageGeneration: {MaxCallsPerTurn: 3, MaxCallsPerSession: 12},
				},
			},
			"custom": {
				APIKey: secret,
				ProviderTools: map[string]config.ProviderToolPolicy{
					ToolImageGeneration: {Enabled: true},
				},
				ImageEndpoint: &config.ImageEndpointConfig{
					Protocol: string(imagegen.ProtocolOpenAIImages), BaseURL: "https://images.example/v1",
					Models: []config.ImageModelConfig{{ID: "canvas-1"}, {ID: "canvas-2"}},
				},
			},
			"future": {
				APIKey: secret,
				ImageEndpoint: &config.ImageEndpointConfig{
					Protocol: "future_images", BaseURL: "https://future.example/v1",
					Models: []config.ImageModelConfig{{ID: "future-canvas"}},
				},
			},
			"plain": {APIKey: secret, BaseURL: "https://chat.example/v1"},
		},
	}

	builtin := ProviderCapabilities(cfg, BigModelCodingProvider)
	if len(builtin) != 1 {
		t.Fatalf("BigModel capabilities = %#v", builtin)
	}
	if builtin[0].ID != ToolWebSearch || builtin[0].Availability != "supported" ||
		builtin[0].Mechanism != "mcp_tool" || builtin[0].ModelLabel != "web_search_prime" ||
		!builtin[0].Enabled || builtin[0].MaxCallsPerTurn != 2 || builtin[0].MaxCallsPerSession != 10 {
		t.Fatalf("BigModel search capability = %#v", builtin[0])
	}

	custom := ProviderCapabilities(cfg, "custom")
	if len(custom) != 0 {
		t.Fatalf("image role leaked into custom provider capabilities = %#v", custom)
	}
	future := ProviderCapabilities(cfg, "future")
	if len(future) != 0 {
		t.Fatalf("image role leaked into future provider capabilities = %#v", future)
	}
	if absent := ProviderCapabilities(cfg, "plain"); len(absent) != 0 {
		t.Fatalf("plain provider gained capabilities: %#v", absent)
	}
	if absent := ProviderCapabilities(cfg, "missing"); len(absent) != 0 {
		t.Fatalf("missing provider gained capabilities: %#v", absent)
	}
	// Availability is manifest/config-shape metadata, not a credential test.
	// Removing or rotating a key must not alter this secret-free snapshot.
	cfg.Providers[BigModelCodingProvider].APIKey = ""
	withoutKey, err := json.Marshal(ProviderCapabilities(cfg, BigModelCodingProvider))
	if err != nil {
		t.Fatal(err)
	}
	withKey, err := json.Marshal(builtin)
	if err != nil {
		t.Fatal(err)
	}
	if string(withoutKey) != string(withKey) {
		t.Fatalf("credential changed manifest availability: with=%s without=%s", withKey, withoutKey)
	}

	encoded, err := json.Marshal(map[string]any{"builtin": builtin, "custom": custom, "future": future})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secret, bigModelCodingBaseURL, bigModelSearchMCPURL, "fingerprint"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("capability snapshot exposed %q: %s", forbidden, encoded)
		}
	}
}

func TestResolveImageRuntimeRequiresExplicitSelectionNotProviderPolicy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := &config.Config{Providers: map[string]*config.ProviderConfig{
		"custom": {
			APIKey: "credential-canary",
			ImageEndpoint: &config.ImageEndpointConfig{
				Protocol: string(imagegen.ProtocolOpenAIImages),
				BaseURL:  "https://images.example/v1",
				Models:   []config.ImageModelConfig{{ID: "paint-1"}},
			},
		},
	}}
	if _, err := ResolveImageRuntime(base); err == nil {
		t.Fatal("empty image_model resolved")
	}
	base.ImageModel = "custom/paint-1"
	base.Providers["custom"].ProviderTools = map[string]config.ProviderToolPolicy{
		ToolImageGeneration: {Enabled: false},
	}
	runtime, err := ResolveImageRuntime(base)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Provider != "custom" || runtime.Model != "paint-1" ||
		runtime.Protocol != imagegen.ProtocolOpenAIImages {
		t.Fatalf("runtime = %#v", runtime)
	}
	if runtime.CredentialFingerprint == "" || runtime.CredentialFingerprint == "credential-canary" {
		t.Fatalf("credential fingerprint = %q", runtime.CredentialFingerprint)
	}
}

func TestResolveImageRuntimeBindsCanonicalHeadersAndRequestConfig(t *testing.T) {
	const (
		apiKey       = "image-api-key-canary"
		headerSecret = "image-header-secret-canary"
	)
	provider := &config.ProviderConfig{
		APIKey: apiKey,
		Headers: map[string]string{
			"x-api-key": headerSecret,
			"X-Tenant":  "tenant-a",
		},
		ProviderTools: map[string]config.ProviderToolPolicy{
			ToolImageGeneration: {Enabled: true, MaxCallsPerTurn: 2, MaxCallsPerSession: 8},
		},
		ImageEndpoint: &config.ImageEndpointConfig{
			Protocol: string(imagegen.ProtocolOpenAIImages), BaseURL: "https://images.example/v1",
			Models:     []config.ImageModelConfig{{ID: "canvas-1"}, {ID: "canvas-2"}},
			AssetHosts: []string{"CDN.Example.", "*.media.example"},
		},
	}
	cfg := &config.Config{
		ImageModel: "custom/canvas-1",
		Providers:  map[string]*config.ProviderConfig{"custom": provider},
	}
	baseline, err := ResolveImageRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Headers["X-Api-Key"] != headerSecret || baseline.Headers["X-Tenant"] != "tenant-a" {
		t.Fatalf("canonical runtime headers = %#v", baseline.Headers)
	}
	for _, metadata := range []string{baseline.CredentialFingerprint, baseline.ConfigEpoch} {
		if metadata == "" || strings.Contains(metadata, apiKey) || strings.Contains(metadata, headerSecret) {
			t.Fatalf("runtime metadata exposed credential material: %q", metadata)
		}
	}

	// Header order and field-name casing do not affect an HTTP request and must
	// therefore produce the same credential/config identity.
	provider.Headers = map[string]string{
		"x-tenant":  "tenant-a",
		"X-API-KEY": headerSecret,
	}
	canonicalEquivalent, err := ResolveImageRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if canonicalEquivalent.CredentialFingerprint != baseline.CredentialFingerprint ||
		canonicalEquivalent.ConfigEpoch != baseline.ConfigEpoch {
		t.Fatalf("equivalent headers changed identity: before=%#v after=%#v", baseline, canonicalEquivalent)
	}

	provider.Headers["X-API-KEY"] = "rotated-header-secret"
	headerRotated, err := ResolveImageRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if headerRotated.CredentialFingerprint == baseline.CredentialFingerprint ||
		headerRotated.ConfigEpoch == baseline.ConfigEpoch {
		t.Fatalf("header rotation was not bound: before=%#v after=%#v", baseline, headerRotated)
	}

	// Endpoint, model, asset allowlist, and limits all affect dispatch or its
	// approval boundary and must each advance ConfigEpoch.
	provider.ImageEndpoint.BaseURL = "https://images.example/v2"
	endpointChanged, err := ResolveImageRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if endpointChanged.ConfigEpoch == headerRotated.ConfigEpoch {
		t.Fatal("endpoint change did not advance config epoch")
	}
	cfg.ImageModel = "custom/canvas-2"
	modelChanged, err := ResolveImageRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if modelChanged.ConfigEpoch == endpointChanged.ConfigEpoch {
		t.Fatal("model change did not advance config epoch")
	}
	provider.ImageEndpoint.AssetHosts = []string{"cdn.example", "assets.example"}
	assetsChanged, err := ResolveImageRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if assetsChanged.ConfigEpoch == modelChanged.ConfigEpoch {
		t.Fatal("asset-host change did not advance config epoch")
	}
	provider.ProviderTools[ToolImageGeneration] = config.ProviderToolPolicy{
		Enabled: true, MaxCallsPerTurn: 3, MaxCallsPerSession: 8,
	}
	limitsChanged, err := ResolveImageRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if limitsChanged.ConfigEpoch == assetsChanged.ConfigEpoch {
		t.Fatal("limit change did not advance config epoch")
	}

	provider.Headers["x-api-key"] = "conflicting-case-variant"
	if _, err := ResolveImageRuntime(cfg); err == nil || !strings.Contains(err.Error(), "conflicting image provider header") {
		t.Fatalf("conflicting canonical header did not fail closed: %v", err)
	}
}

func TestBigModelProfileIsExactAndPresetSecretIsDetached(t *testing.T) {
	cfg := &config.Config{Model: BigModelCodingProvider + "/glm-4.7", Providers: map[string]*config.ProviderConfig{
		BigModelCodingProvider: {
			APIKey: "credential-canary",
			ProviderTools: map[string]config.ProviderToolPolicy{
				ToolImageGeneration: {Enabled: true},
				ToolWebSearch:       {Enabled: true},
			},
		},
	}}
	cfg.ImageModel = BigModelCodingProvider + "/cogview-3-flash"
	runtime, err := ResolveImageRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.BaseURL != bigModelCodingBaseURL || len(runtime.AssetHosts) == 0 {
		t.Fatalf("blank registry-default BaseURL runtime = %#v", runtime)
	}
	servers := EffectiveMCPServers(cfg)
	preset := servers[bigModelSearchMCPName]
	if preset == nil || preset.Type != "http" || preset.URL != bigModelSearchMCPURL {
		t.Fatalf("preset = %#v", preset)
	}
	if preset.Headers["Authorization"] != "Bearer credential-canary" {
		t.Fatal("detached preset did not receive credential")
	}
	if len(cfg.MCPServers) != 0 {
		t.Fatalf("provider secret was copied into generic MCP config: %#v", cfg.MCPServers)
	}

	cfg.Providers[BigModelCodingProvider].BaseURL = "https://proxy.example/v4"
	if _, err := ResolveImageRuntime(cfg); err == nil {
		t.Fatal("brand-matching provider with non-profile base URL resolved")
	}
	if _, ok := EffectiveMCPServers(cfg)[bigModelSearchMCPName]; ok {
		t.Fatal("search preset enabled for non-profile base URL")
	}
}

func TestImageModelsMergesBuiltinAndExplicitWithoutDuplicates(t *testing.T) {
	cfg := &config.Config{Providers: map[string]*config.ProviderConfig{
		BigModelCodingProvider: {
			APIKey: "key", BaseURL: bigModelCodingBaseURL,
			ProviderTools: map[string]config.ProviderToolPolicy{
				ToolImageGeneration: {Enabled: true},
			},
			ImageEndpoint: &config.ImageEndpointConfig{
				Protocol: string(imagegen.ProtocolOpenAIImages), BaseURL: "https://images.example/v1",
				Models:     []config.ImageModelConfig{{ID: "cogview-3-flash"}},
				AssetHosts: []string{"assets.example"},
			},
		},
	}}
	cfg.ImageModel = BigModelCodingProvider + "/cogview-3-flash"
	models := ImageModels(cfg)
	if len(models) != 1 || models[0].ID != "cogview-3-flash" || !models[0].Supported || models[0].Builtin {
		t.Fatalf("models = %#v", models)
	}
	runtime, err := ResolveImageRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.BaseURL != "https://images.example/v1" ||
		len(runtime.AssetHosts) != 1 || runtime.AssetHosts[0] != "assets.example" {
		t.Fatalf("explicit endpoint did not override builtin model route: %#v", runtime)
	}
}
