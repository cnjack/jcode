package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/providertools"
)

func TestStaleServerImageMutationPreservesNewerProviderCredentialAndPolicy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	initial := crossProcessProviderConfig()
	if err := config.SaveConfig(initial); err != nil {
		t.Fatal(err)
	}
	serverACfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	serverBCfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	serverA := &Server{cfg: serverACfg, registry: model.NewModelRegistryWithConfig(serverACfg), needsSetup: true}
	serverB := &Server{cfg: serverBCfg, registry: model.NewModelRegistryWithConfig(serverBCfg), needsSetup: true}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/providers/custom", strings.NewReader(`{
		"api_key":"rotated-provider-key",
		"provider_tools":{"image_generation":{"enabled":false}}
	}`))
	updateReq.SetPathValue("id", "custom")
	updateRec := httptest.NewRecorder()
	serverA.handleUpdateProvider(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("provider update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}

	imageRec := httptest.NewRecorder()
	serverB.handleSetImageModel(imageRec, httptest.NewRequest(
		http.MethodPost, "/api/image-model", strings.NewReader(`{"provider":"custom","model":"canvas-2"}`),
	))
	if imageRec.Code != http.StatusOK {
		t.Fatalf("image update status=%d body=%s", imageRec.Code, imageRec.Body.String())
	}

	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	provider := loaded.Providers["custom"]
	if provider.APIKey != "rotated-provider-key" || provider.ProviderTools[providertools.ToolImageGeneration].Enabled {
		t.Fatalf("stale Server revived provider credential/policy: %#v", provider)
	}
	if loaded.ImageModel != "custom/canvas-2" {
		t.Fatalf("image role mutation was lost: %q", loaded.ImageModel)
	}
	if serverBCfg.Providers["custom"].APIKey != "rotated-provider-key" {
		t.Fatalf("stale Server live snapshot was not refreshed: %#v", serverBCfg.Providers["custom"])
	}
}

func TestStaleServerCannotResurrectRevokedImageEndpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	initial := crossProcessProviderConfig()
	if err := config.SaveConfig(initial); err != nil {
		t.Fatal(err)
	}
	serverACfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	serverBCfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	serverA := &Server{cfg: serverACfg, registry: model.NewModelRegistryWithConfig(serverACfg), needsSetup: true}
	serverB := &Server{cfg: serverBCfg, registry: model.NewModelRegistryWithConfig(serverBCfg), needsSetup: true}

	revokeReq := httptest.NewRequest(http.MethodPut, "/api/providers/custom", strings.NewReader(`{"image_endpoint":null}`))
	revokeReq.SetPathValue("id", "custom")
	revokeRec := httptest.NewRecorder()
	serverA.handleUpdateProvider(revokeRec, revokeReq)
	if revokeRec.Code != http.StatusOK {
		t.Fatalf("endpoint revoke status=%d body=%s", revokeRec.Code, revokeRec.Body.String())
	}

	imageRec := httptest.NewRecorder()
	serverB.handleSetImageModel(imageRec, httptest.NewRequest(
		http.MethodPost, "/api/image-model", strings.NewReader(`{"provider":"custom","model":"canvas-2"}`),
	))
	if imageRec.Code != http.StatusBadRequest {
		t.Fatalf("stale image selection status=%d body=%s", imageRec.Code, imageRec.Body.String())
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Providers["custom"].ImageEndpoint != nil || loaded.ImageModel != "" {
		t.Fatalf("stale Server resurrected revoked image config: endpoint=%#v image_model=%q",
			loaded.Providers["custom"].ImageEndpoint, loaded.ImageModel)
	}
}

func crossProcessProviderConfig() *config.Config {
	return &config.Config{
		Model: "custom/chat", ImageModel: "custom/canvas-1",
		Providers: map[string]*config.ProviderConfig{
			"custom": {
				APIKey: "old-provider-key", BaseURL: "https://chat.example.test/v1",
				CustomModels: []config.CustomModelConfig{{ID: "chat", ToolCall: true}},
				ProviderTools: map[string]config.ProviderToolPolicy{
					providertools.ToolImageGeneration: {Enabled: true},
				},
				ImageEndpoint: &config.ImageEndpointConfig{
					Protocol: "openai_images", BaseURL: "https://images.example.test/v1",
					Models: []config.ImageModelConfig{{ID: "canvas-1"}, {ID: "canvas-2"}},
				},
			},
		},
	}
}
