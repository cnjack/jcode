package providertools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/imagegen"
)

func TestAlibabaTokenPlanCNExactImageManifestAndRuntime(t *testing.T) {
	const secret = "token-plan-secret-canary"
	cfg := &config.Config{
		ImageModel: AlibabaTokenPlanCNProvider + "/wan2.7-image-pro",
		Providers: map[string]*config.ProviderConfig{
			AlibabaTokenPlanCNProvider: {
				APIKey: secret,
				ProviderTools: map[string]config.ProviderToolPolicy{
					ToolImageGeneration: {Enabled: true, MaxCallsPerTurn: 1, MaxCallsPerSession: 9},
				},
			},
		},
	}

	capabilities := ProviderCapabilities(cfg, AlibabaTokenPlanCNProvider)
	if len(capabilities) != 0 {
		t.Fatalf("image role leaked into provider-managed capabilities = %#v", capabilities)
	}
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secret, alibabaTokenPlanCNBaseURL, alibabaTokenPlanCNImageURL, "verification"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("capability metadata exposed %q: %s", forbidden, encoded)
		}
	}

	models := ImageModels(cfg)
	if len(models) != 2 {
		t.Fatalf("models = %#v", models)
	}
	byID := make(map[string]ImageModel, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}
	standard := byID["wan2.7-image"]
	if !standard.Builtin || !standard.Supported || standard.Protocol != string(imagegen.ProtocolTokenPlanMultimodal) ||
		strings.Join(standard.Sizes, ",") != "1024x1024,720x1280,1280x720" {
		t.Fatalf("standard model = %#v", standard)
	}
	pro := byID["wan2.7-image-pro"]
	if !pro.Builtin || !pro.Supported ||
		strings.Join(pro.Sizes, ",") != "1024x1024,720x1280,1280x720,2048x2048,1440x2560,2560x1440" {
		t.Fatalf("pro model = %#v", pro)
	}
	runtime, err := ResolveImageRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Provider != AlibabaTokenPlanCNProvider || runtime.Model != "wan2.7-image-pro" ||
		runtime.Protocol != imagegen.ProtocolTokenPlanMultimodal || runtime.BaseURL != alibabaTokenPlanCNImageURL ||
		runtime.APIKey != secret || runtime.ConfigEpoch == "" || runtime.CredentialFingerprint == "" {
		t.Fatalf("runtime = %#v", runtime)
	}
	assetHosts := make(map[string]bool, len(runtime.AssetHosts))
	for _, host := range runtime.AssetHosts {
		assetHosts[host] = true
	}
	if !assetHosts["*.oss-accelerate.aliyuncs.com"] {
		t.Fatalf("Token Plan accelerated OSS host is not allowed: %#v", runtime.AssetHosts)
	}
	if strings.Contains(runtime.ConfigEpoch, secret) || strings.Contains(runtime.CredentialFingerprint, secret) {
		t.Fatalf("runtime metadata exposed credential: %#v", runtime)
	}
}

func TestAlibabaTokenPlanCNProfileRejectsProxyAndUnknownImageModel(t *testing.T) {
	provider := &config.ProviderConfig{
		APIKey:  "credential",
		BaseURL: "https://proxy.example/compatible-mode/v1",
		ProviderTools: map[string]config.ProviderToolPolicy{
			ToolImageGeneration: {Enabled: true},
		},
	}
	cfg := &config.Config{
		ImageModel: AlibabaTokenPlanCNProvider + "/wan2.7-image",
		Providers:  map[string]*config.ProviderConfig{AlibabaTokenPlanCNProvider: provider},
	}
	if got := ProviderCapabilities(cfg, AlibabaTokenPlanCNProvider); len(got) != 0 {
		t.Fatalf("proxy inherited capabilities: %#v", got)
	}
	if got := ImageModels(cfg); len(got) != 0 {
		t.Fatalf("proxy inherited image models: %#v", got)
	}
	if _, err := ResolveImageRuntime(cfg); err == nil {
		t.Fatal("proxy inherited Token Plan image runtime")
	}

	provider.BaseURL = alibabaTokenPlanCNBaseURL + "/"
	cfg.ImageModel = AlibabaTokenPlanCNProvider + "/wan2.8-image"
	if _, err := ResolveImageRuntime(cfg); err == nil {
		t.Fatal("unknown Wan model resolved")
	}
	provider.APIKey = ""
	cfg.ImageModel = AlibabaTokenPlanCNProvider + "/wan2.7-image"
	if _, err := ResolveImageRuntime(cfg); err == nil {
		t.Fatal("missing Token Plan credential resolved")
	}
}

func TestExplicitTokenPlanMultimodalEndpointIsRecognized(t *testing.T) {
	cfg := &config.Config{
		ImageModel: "custom/wan2.7-image",
		Providers: map[string]*config.ProviderConfig{
			"custom": {
				APIKey: "credential",
				ProviderTools: map[string]config.ProviderToolPolicy{
					ToolImageGeneration: {Enabled: true},
				},
				ImageEndpoint: &config.ImageEndpointConfig{
					Protocol: string(imagegen.ProtocolTokenPlanMultimodal),
					BaseURL:  "https://images.example" + "/api/v1/services/aigc/multimodal-generation/generation",
					Models: []config.ImageModelConfig{{
						ID: "wan2.7-image", Sizes: []string{"1024x1024"},
					}},
				},
			},
		},
	}
	models := ImageModels(cfg)
	if len(models) != 1 || !models[0].Supported || models[0].Builtin ||
		models[0].Protocol != string(imagegen.ProtocolTokenPlanMultimodal) {
		t.Fatalf("models = %#v", models)
	}
	runtime, err := ResolveImageRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Protocol != imagegen.ProtocolTokenPlanMultimodal ||
		runtime.BaseURL != "https://images.example/api/v1/services/aigc/multimodal-generation/generation" {
		t.Fatalf("runtime = %#v", runtime)
	}
}
