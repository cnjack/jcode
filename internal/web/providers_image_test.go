package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/providertools"
)

func TestValidateImageEndpoint(t *testing.T) {
	valid, err := validateImageEndpoint(&config.ImageEndpointConfig{
		Protocol: " openai_images ", BaseURL: "HTTPS://Images.Example.Test/v1/",
		Models:     []config.ImageModelConfig{{ID: " canvas-v1 ", Sizes: []string{"1024x1024", "1024x1024"}}},
		AssetHosts: []string{"CDN.Example.Test.", "*.media.example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if valid.Protocol != "openai_images" || valid.BaseURL != "https://images.example.test/v1" || valid.Models[0].ID != "canvas-v1" {
		t.Fatalf("normalized endpoint = %+v", valid)
	}
	if len(valid.Models[0].Sizes) != 1 || len(valid.AssetHosts) != 2 || valid.AssetHosts[0] != "cdn.example.test" {
		t.Fatalf("normalized endpoint lists = %+v", valid)
	}

	for name, endpoint := range map[string]*config.ImageEndpointConfig{
		"http":              {Protocol: "openai_images", BaseURL: "http://images.example.test/v1", Models: []config.ImageModelConfig{{ID: "x"}}},
		"userinfo":          {Protocol: "openai_images", BaseURL: "https://token@images.example.test/v1", Models: []config.ImageModelConfig{{ID: "x"}}},
		"unicode host":      {Protocol: "openai_images", BaseURL: "https://例子.测试/v1", Models: []config.ImageModelConfig{{ID: "x"}}},
		"empty models":      {Protocol: "openai_images", BaseURL: "https://images.example.test/v1"},
		"broad wildcard":    {Protocol: "openai_images", BaseURL: "https://images.example.test/v1", Models: []config.ImageModelConfig{{ID: "x"}}, AssetHosts: []string{"*.com"}},
		"single label host": {Protocol: "openai_images", BaseURL: "https://images.example.test/v1", Models: []config.ImageModelConfig{{ID: "x"}}, AssetHosts: []string{"localhost"}},
		"asset IP":          {Protocol: "openai_images", BaseURL: "https://images.example.test/v1", Models: []config.ImageModelConfig{{ID: "x"}}, AssetHosts: []string{"127.0.0.1"}},
		"unicode asset":     {Protocol: "openai_images", BaseURL: "https://images.example.test/v1", Models: []config.ImageModelConfig{{ID: "x"}}, AssetHosts: []string{"图片.例子"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validateImageEndpoint(endpoint); err == nil {
				t.Fatalf("invalid endpoint was accepted: %+v", endpoint)
			}
		})
	}
}

func TestAddImageOnlyCustomProviderExposesCatalogAndCanBeSelected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Model: "openai/gpt-5",
		Providers: map[string]*config.ProviderConfig{
			"openai": {APIKey: "chat-secret"},
		},
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg: cfg, registry: model.NewModelRegistryWithConfig(cfg),
		needsSetup: true, wsBroker: NewWSBroker(),
	}

	for name, body := range map[string]string{
		"empty shell": `{"id":"empty","api_key":"secret"}`,
		"chat model without route": `{
			"id":"chat-without-route","api_key":"secret","model":"chat",
			"image_endpoint":{"protocol":"openai_images","base_url":"https://images.example.test/v1","models":[{"id":"canvas"}]}
		}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.handleAddProvider(rec, httptest.NewRequest(http.MethodPost, "/api/providers", strings.NewReader(body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	addRec := httptest.NewRecorder()
	s.handleAddProvider(addRec, httptest.NewRequest(http.MethodPost, "/api/providers", strings.NewReader(`{
		"id":"image-only","api_key":"image-secret",
		"image_endpoint":{
			"protocol":"openai_images","base_url":"https://images.example.test/v1",
			"models":[{"id":"canvas","name":"Canvas","sizes":["1024x1024"]}]
		}
	}`)))
	if addRec.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", addRec.Code, addRec.Body.String())
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	imageProvider := loaded.Providers["image-only"]
	if imageProvider == nil || imageProvider.BaseURL != "" || imageProvider.ImageEndpoint == nil || len(imageProvider.CustomModels) != 0 {
		t.Fatalf("image-only provider = %#v", imageProvider)
	}

	providersRec := httptest.NewRecorder()
	s.handleListProviders(providersRec, httptest.NewRequest(http.MethodGet, "/api/providers", nil))
	if providersRec.Code != http.StatusOK {
		t.Fatalf("providers status=%d body=%s", providersRec.Code, providersRec.Body.String())
	}
	var providers []struct {
		ID           string                             `json:"id"`
		Custom       bool                               `json:"custom"`
		Capabilities []providertools.ProviderCapability `json:"capabilities"`
	}
	if err := json.Unmarshal(providersRec.Body.Bytes(), &providers); err != nil {
		t.Fatal(err)
	}
	var imageCapabilities []providertools.ProviderCapability
	for _, provider := range providers {
		if provider.ID == "image-only" {
			if !provider.Custom {
				t.Fatal("image-only provider was not marked custom")
			}
			imageCapabilities = provider.Capabilities
			break
		}
	}
	if len(imageCapabilities) != 0 {
		t.Fatalf("image role leaked into provider-managed capabilities = %#v", imageCapabilities)
	}

	modelsRec := httptest.NewRecorder()
	s.handleListModels(modelsRec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if modelsRec.Code != http.StatusOK {
		t.Fatalf("models status=%d body=%s", modelsRec.Code, modelsRec.Body.String())
	}
	var catalog struct {
		Providers []struct {
			ID     string `json:"id"`
			Models []struct {
				ID                     string   `json:"id"`
				OutputModalities       []string `json:"output_modalities"`
				CapabilityAvailability string   `json:"capability_availability"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(modelsRec.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	visible := false
	for _, provider := range catalog.Providers {
		if provider.ID != "image-only" || len(provider.Models) != 1 {
			continue
		}
		modelEntry := provider.Models[0]
		visible = modelEntry.ID == "canvas" && hasModality(modelEntry.OutputModalities, "image") &&
			modelEntry.CapabilityAvailability == "supported"
	}
	if !visible {
		t.Fatalf("image-only model missing from catalog: %s", modelsRec.Body.String())
	}

	selectRec := httptest.NewRecorder()
	s.handleSetImageModel(selectRec, httptest.NewRequest(
		http.MethodPost, "/api/image-model", strings.NewReader(`{"provider":"image-only","model":"canvas"}`),
	))
	if selectRec.Code != http.StatusOK {
		t.Fatalf("select status=%d body=%s", selectRec.Code, selectRec.Body.String())
	}
	selected, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if selected.ImageModel != "image-only/canvas" || selected.Model != "openai/gpt-5" {
		t.Fatalf("selected roles: chat=%q image=%q", selected.Model, selected.ImageModel)
	}
}

func TestUpdateProviderPersistsImageSettingsWithoutReplacingMaskedSecrets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		apiKey       = "provider-existing-secret"
		headerSecret = "header-existing-secret"
	)
	cfg := &config.Config{
		Model: "custom/chat", ImageModel: "custom/canvas",
		Providers: map[string]*config.ProviderConfig{
			"custom": {
				APIKey: apiKey, BaseURL: "https://chat.example.test/v1", Name: "Custom",
				Headers:         map[string]string{"Authorization": headerSecret},
				CustomModels:    []config.CustomModelConfig{{ID: "chat", ToolCall: true}},
				Vision:          boolTestPtr(true),
				Thinking:        boolTestPtr(true),
				ReasoningEffort: "high",
			},
		},
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg, registry: model.NewModelRegistryWithConfig(cfg), needsSetup: true}
	body := fmt.Sprintf(`{
		"api_key":%q,
		"headers":{"Authorization":%q},
		"protocol":"responses",
		"provider_tools":{"image_generation":{"enabled":true,"max_calls_per_turn":1,"max_calls_per_session":5}},
		"image_endpoint":{"protocol":"openai_images","base_url":"https://Images.Example.Test/v1/","models":[{"id":"canvas","sizes":["1024x1024"]}]}
	}`, maskSecret(apiKey), maskSecret(headerSecret))
	req := httptest.NewRequest(http.MethodPut, "/api/providers/custom", strings.NewReader(body))
	req.SetPathValue("id", "custom")
	rec := httptest.NewRecorder()
	s.handleUpdateProvider(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	pc := loaded.Providers["custom"]
	if pc.APIKey != apiKey || pc.Headers["Authorization"] != headerSecret {
		t.Fatalf("masked provider update replaced secrets: key=%q headers=%v", pc.APIKey, pc.Headers)
	}
	if pc.Protocol != "responses" || pc.ImageEndpoint == nil || pc.ImageEndpoint.BaseURL != "https://images.example.test/v1" {
		t.Fatalf("image settings were not normalized/persisted: %+v", pc)
	}
	if !pc.ProviderTools["image_generation"].Enabled {
		t.Fatal("image_generation policy did not persist")
	}
	if pc.Vision == nil || !*pc.Vision || pc.Thinking == nil || !*pc.Thinking || pc.ReasoningEffort != "high" {
		t.Fatalf("partial provider update changed omitted chat settings: %+v", pc)
	}

	clearReq := httptest.NewRequest(http.MethodPut, "/api/providers/custom", strings.NewReader(`{"image_endpoint":null}`))
	clearReq.SetPathValue("id", "custom")
	clearRec := httptest.NewRecorder()
	s.handleUpdateProvider(clearRec, clearReq)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", clearRec.Code, clearRec.Body.String())
	}
	cleared, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Providers["custom"].ImageEndpoint != nil {
		t.Fatalf("explicit image_endpoint:null did not clear endpoint: %+v", cleared.Providers["custom"].ImageEndpoint)
	}
	if cleared.ImageModel != "" {
		t.Fatalf("removing the selected endpoint left a stale image_model: %q", cleared.ImageModel)
	}
	if cleared.Providers["custom"].APIKey != apiKey || cleared.Providers["custom"].Headers["Authorization"] != headerSecret {
		t.Fatal("clearing image endpoint changed provider secrets")
	}
}

func TestUpdateBigModelNullBaseURLRestoresOfficialCapabilities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const secret = "bigmodel-existing-secret"
	cfg := &config.Config{
		Model: providertools.BigModelCodingProvider + "/glm-4.7",
		Providers: map[string]*config.ProviderConfig{
			providertools.BigModelCodingProvider: {
				APIKey: secret, BaseURL: "https://proxy.example.test/v4",
				ProviderTools: map[string]config.ProviderToolPolicy{
					providertools.ToolImageGeneration: {Enabled: true},
					providertools.ToolWebSearch:       {Enabled: true},
				},
			},
		},
	}
	if got := providertools.ProviderCapabilities(cfg, providertools.BigModelCodingProvider); len(got) != 0 {
		t.Fatalf("proxy unexpectedly inherited official capabilities: %#v", got)
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg: cfg, registry: model.NewModelRegistryWithConfig(cfg),
		needsSetup: true,
	}
	legacyReq := httptest.NewRequest(
		http.MethodPut,
		"/api/providers/"+providertools.BigModelCodingProvider,
		strings.NewReader(`{"base_url":""}`),
	)
	legacyReq.SetPathValue("id", providertools.BigModelCodingProvider)
	legacyRec := httptest.NewRecorder()
	s.handleUpdateProvider(legacyRec, legacyReq)
	if legacyRec.Code != http.StatusOK || s.cfg.Providers[providertools.BigModelCodingProvider].BaseURL != "https://proxy.example.test/v4" {
		t.Fatalf("legacy empty base_url did not preserve proxy: status=%d provider=%#v", legacyRec.Code, s.cfg.Providers[providertools.BigModelCodingProvider])
	}
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/providers/"+providertools.BigModelCodingProvider,
		strings.NewReader(`{"base_url":null,"api_key":""}`),
	)
	req.SetPathValue("id", providertools.BigModelCodingProvider)
	rec := httptest.NewRecorder()
	s.handleUpdateProvider(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	provider := loaded.Providers[providertools.BigModelCodingProvider]
	if provider.BaseURL != "" || provider.APIKey != secret {
		t.Fatalf("null base_url or empty api_key semantics = %#v", provider)
	}
	capabilities := providertools.ProviderCapabilities(loaded, providertools.BigModelCodingProvider)
	if len(capabilities) != 1 || capabilities[0].ID != providertools.ToolWebSearch ||
		capabilities[0].Availability != "supported" {
		t.Fatalf("restored capabilities = %#v", capabilities)
	}
	if _, err := providertools.ResolveWebSearchRuntime(loaded); err != nil {
		t.Fatalf("restored web-search runtime: %v", err)
	}
	foundImage := false
	for _, candidate := range providertools.ImageModels(loaded) {
		if candidate.Provider == providertools.BigModelCodingProvider && candidate.ID == "cogview-3-flash" {
			foundImage = true
		}
	}
	if !foundImage {
		t.Fatal("restored BigModel image catalog is missing cogview-3-flash")
	}
}

func TestUpdateCustomChatProviderCannotClearRequiredBaseURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Model: "custom/chat",
		Providers: map[string]*config.ProviderConfig{
			"custom": {
				APIKey: "secret", BaseURL: "https://chat.example.test/v1",
				CustomModels: []config.CustomModelConfig{{ID: "chat", ToolCall: true}},
				ImageEndpoint: &config.ImageEndpointConfig{
					Protocol: "openai_images", BaseURL: "https://images.example.test/v1",
					Models: []config.ImageModelConfig{{ID: "canvas"}},
				},
			},
		},
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg, registry: model.NewModelRegistryWithConfig(cfg), needsSetup: true}
	req := httptest.NewRequest(http.MethodPut, "/api/providers/custom", strings.NewReader(`{"base_url":null}`))
	req.SetPathValue("id", "custom")
	rec := httptest.NewRecorder()
	s.handleUpdateProvider(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Providers["custom"].BaseURL != "https://chat.example.test/v1" {
		t.Fatalf("rejected clear mutated config: %#v", loaded.Providers["custom"])
	}
}

func boolTestPtr(value bool) *bool { return &value }

func TestProviderListMasksSecretsAndReturnsImageSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		apiKey       = "provider-super-secret"
		headerSecret = "header-super-secret"
	)
	cfg := &config.Config{
		Model: "custom/chat",
		Providers: map[string]*config.ProviderConfig{
			"custom": {
				APIKey: apiKey, BaseURL: "https://chat.example.test/v1", Name: "Custom",
				Headers:       map[string]string{"Authorization": headerSecret},
				Protocol:      "responses",
				ProviderTools: map[string]config.ProviderToolPolicy{"image_generation": {Enabled: true, MaxCallsPerTurn: 1}},
				ImageEndpoint: &config.ImageEndpointConfig{
					Protocol: "openai_images", BaseURL: "https://images.example.test/v1",
					Models: []config.ImageModelConfig{{ID: "canvas"}},
				},
				CustomModels: []config.CustomModelConfig{{ID: "chat", ToolCall: true}},
			},
		},
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: model.NewModelRegistryWithConfig(cfg)}
	rec := httptest.NewRecorder()
	s.handleListProviders(rec, httptest.NewRequest(http.MethodGet, "/api/providers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, apiKey) || strings.Contains(body, headerSecret) {
		t.Fatalf("provider list leaked plaintext secret: %s", body)
	}
	var providers []struct {
		APIKey        string                               `json:"api_key"`
		Headers       map[string]string                    `json:"headers"`
		Protocol      string                               `json:"protocol"`
		ProviderTools map[string]config.ProviderToolPolicy `json:"provider_tools"`
		ImageEndpoint *config.ImageEndpointConfig          `json:"image_endpoint"`
		Capabilities  []providertools.ProviderCapability   `json:"capabilities"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providers); err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].APIKey != maskSecret(apiKey) || providers[0].Headers["Authorization"] != maskSecret(headerSecret) {
		t.Fatalf("safe provider view = %+v", providers)
	}
	if providers[0].Protocol != "responses" || providers[0].ImageEndpoint == nil || providers[0].ImageEndpoint.Models[0].ID != "canvas" {
		t.Fatalf("image provider settings missing: %+v", providers[0])
	}
	if !providers[0].ProviderTools["image_generation"].Enabled {
		t.Fatal("provider tool policy missing")
	}
	if len(providers[0].Capabilities) != 0 {
		t.Fatalf("image role leaked into provider capability snapshot = %+v", providers[0].Capabilities)
	}
}

func TestProviderListCapabilitiesComeFromManifestAndNeverExposeSecrets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		bigModelSecret = "bigmodel-capability-secret"
		customSecret   = "custom-capability-secret"
	)
	cfg := &config.Config{Providers: map[string]*config.ProviderConfig{
		providertools.BigModelCodingProvider: {
			APIKey: bigModelSecret, BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4",
			ProviderTools: map[string]config.ProviderToolPolicy{
				"image_generation": {Enabled: true},
				"web_search":       {Enabled: true},
			},
		},
		"custom-image": {
			APIKey: customSecret, BaseURL: "https://chat.example.test/v1",
			ImageEndpoint: &config.ImageEndpointConfig{
				Protocol: "openai_images", BaseURL: "https://images.example.test/v1",
				Models: []config.ImageModelConfig{{ID: "canvas"}},
			},
		},
		"plain": {APIKey: "plain-provider-secret", BaseURL: "https://chat.example.test/v1"},
	}}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: model.NewModelRegistryWithConfig(cfg)}
	rec := httptest.NewRecorder()
	s.handleListProviders(rec, httptest.NewRequest(http.MethodGet, "/api/providers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, secret := range []string{bigModelSecret, customSecret, "plain-provider-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("provider capability response leaked plaintext secret %q: %s", secret, body)
		}
	}
	var providers []struct {
		ID           string                             `json:"id"`
		Capabilities []providertools.ProviderCapability `json:"capabilities"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providers); err != nil {
		t.Fatal(err)
	}
	byID := make(map[string][]providertools.ProviderCapability, len(providers))
	for _, provider := range providers {
		byID[provider.ID] = provider.Capabilities
	}
	if len(byID[providertools.BigModelCodingProvider]) != 1 ||
		byID[providertools.BigModelCodingProvider][0].Mechanism != "mcp_tool" {
		t.Fatalf("BigModel capabilities = %#v", byID[providertools.BigModelCodingProvider])
	}
	if len(byID["custom-image"]) != 0 {
		t.Fatalf("custom capabilities = %#v", byID["custom-image"])
	}
	if capabilities, exists := byID["plain"]; !exists || len(capabilities) != 0 {
		t.Fatalf("plain provider capabilities = %#v, exists=%v", capabilities, exists)
	}
}
