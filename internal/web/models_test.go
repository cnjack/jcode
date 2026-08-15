package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/providerauth"
	"github.com/cnjack/jcode/internal/providertools"
)

func TestWebSwitchModelSameValueIsNoOp(t *testing.T) {
	s := &Server{
		Engine:   &Engine{providerName: "openai", modelName: "gpt-5"},
		wsBroker: NewWSBroker(),
	}
	rec := httptest.NewRecorder()
	s.handleSwitchModel(rec, httptest.NewRequest(http.MethodPost, "/api/model",
		strings.NewReader(`{"provider":"openai","model":"gpt-5"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("same model: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestProviderCatalogUsesPersistedModelVisibility(t *testing.T) {
	const (
		providerID      = "kimi-for-coding"
		enabledModelID  = "kimi-for-coding-highspeed"
		disabledModelID = "k3"
	)
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".jcode")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(configDir, "config.json"),
		[]byte(`{"providers":{"kimi-for-coding":{"api_key":"test"}}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	state := &config.ModelState{
		EnabledModels: []config.ModelRef{{
			Provider: providerID,
			Model:    enabledModelID,
		}},
		DisabledModels: []config.ModelRef{{
			Provider: providerID,
			Model:    disabledModelID,
		}},
	}
	if err := config.SaveModelState(state); err != nil {
		t.Fatal(err)
	}

	registry := model.NewModelRegistry()
	_, enabledModel, ok := registry.LookupModel(providerID, enabledModelID)
	if !ok {
		t.Fatalf("static model %s/%s is missing", providerID, enabledModelID)
	}
	// Make the enabled override observable instead of relying on the static
	// provider's current default visibility.
	enabledModel.DefaultEnabled = false
	s := &Server{registry: registry}
	req := httptest.NewRequest(http.MethodGet, "/api/providers/"+providerID+"/models", nil)
	req.SetPathValue("id", providerID)
	rec := httptest.NewRecorder()
	s.handleProviderCatalog(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog: code=%d body=%q", rec.Code, rec.Body.String())
	}

	var got []struct {
		ID    string `json:"id"`
		Added bool   `json:"added"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	addedByID := make(map[string]bool, len(got))
	for _, item := range got {
		addedByID[item.ID] = item.Added
	}
	if !addedByID[enabledModelID] {
		t.Errorf("explicitly enabled %s was reported disabled", enabledModelID)
	}
	if addedByID[disabledModelID] {
		t.Errorf("explicitly disabled default %s was reported enabled", disabledModelID)
	}
}

func TestCustomProviderCatalogUsesPersistedLiveModelVisibility(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": []map[string]string{{"id": "Qwen3.8-27B-MLX-8bit"}},
		})
	}))
	defer live.Close()

	const providerID = "Local"
	if err := config.SaveConfig(&config.Config{Providers: map[string]*config.ProviderConfig{
		providerID: {APIKey: "test", BaseURL: live.URL + "/v1", Name: providerID},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveModelState(&config.ModelState{EnabledModels: []config.ModelRef{{
		Provider: providerID, Model: "Qwen3.8-27B-MLX-8bit",
	}}}); err != nil {
		t.Fatal(err)
	}

	s := &Server{registry: model.NewModelRegistry()}
	req := httptest.NewRequest(http.MethodGet, "/api/providers/Local/models", nil)
	req.SetPathValue("id", providerID)
	rec := httptest.NewRecorder()
	s.handleProviderCatalog(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog: code=%d body=%q", rec.Code, rec.Body.String())
	}
	var got []struct {
		ID    string `json:"id"`
		Added bool   `json:"added"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "Qwen3.8-27B-MLX-8bit" || !got[0].Added {
		t.Fatalf("live custom catalog lost persisted visibility: %#v", got)
	}
}

func TestManagedProviderCatalogUsesLiveAccountModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	err := config.SaveConfig(&config.Config{
		Providers: map[string]*config.ProviderConfig{
			"github-copilot": {
				Auth: &config.ProviderAuthBinding{Method: string(providerauth.MethodGitHubCopilot), AccountID: "account-1"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		registry: model.NewModelRegistry(),
		providerAuth: &fakeProviderAuthService{models: []providerauth.Model{
			{ID: "claude-sonnet-5", Name: "Claude Sonnet 5", Vendor: "anthropic", Protocol: providerauth.ProtocolChatCompletions, Kind: providerauth.ModelKindChat},
			{ID: "gpt-5.6", Name: "GPT-5.6 Terra", Vendor: "openai", Protocol: providerauth.ProtocolResponses, Kind: providerauth.ModelKindChat},
		}},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/providers/github-copilot/models", nil)
	req.SetPathValue("id", "github-copilot")
	rec := httptest.NewRecorder()
	s.handleProviderCatalog(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog: code=%d body=%s", rec.Code, rec.Body.String())
	}
	var got []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Added bool   `json:"added"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "claude-sonnet-5" || got[1].ID != "gpt-5.6" ||
		got[0].Name != "Claude Sonnet 5" || got[1].Name != "GPT-5.6 Terra" {
		t.Fatalf("live catalog = %#v", got)
	}
}

func TestEnableManagedModelPersistsRuntimeMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	err := config.SaveConfig(&config.Config{
		Providers: map[string]*config.ProviderConfig{
			"github-copilot": {
				Auth: &config.ProviderAuthBinding{Method: string(providerauth.MethodGitHubCopilot), AccountID: "account-1"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg:        &config.Config{},
		registry:   model.NewModelRegistry(),
		needsSetup: true,
		providerAuth: &fakeProviderAuthService{models: []providerauth.Model{
			{ID: "gpt-5.6", Name: "GPT-5.6 Terra", Vendor: "openai", Protocol: providerauth.ProtocolResponses, Kind: providerauth.ModelKindChat},
		}},
	}
	recorder := httptest.NewRecorder()
	s.handleToggleModelEnabled(recorder, httptest.NewRequest(
		http.MethodPost, "/api/model-state/enabled",
		strings.NewReader(`{"provider":"github-copilot","model":"gpt-5.6","enabled":true}`),
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("enable model: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	models := loaded.Providers["github-copilot"].CustomModels
	if len(models) != 1 || !models[0].Managed || models[0].Protocol != string(providerauth.ProtocolResponses) ||
		models[0].Vendor != "openai" || models[0].Name != "GPT-5.6 Terra" {
		t.Fatalf("stored managed model = %#v", models)
	}
	if _, _, ok := s.registry.LookupModel("github-copilot", "gpt-5.6"); !ok {
		t.Fatal("live registry was not rebuilt with enabled managed model")
	}
	state, err := config.LoadModelState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.IsModelEnabled(config.ModelRef{Provider: "github-copilot", Model: "gpt-5.6"}, false) {
		t.Fatal("enabled managed model was not persisted in model state")
	}
}

func TestEnableCustomProviderLiveModelPersistsRuntimeMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{Providers: map[string]*config.ProviderConfig{
		"Local": {APIKey: "test", BaseURL: "http://127.0.0.1:1234/v1", Name: "Local"},
	}}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg:        &config.Config{},
		registry:   model.NewModelRegistry(),
		needsSetup: true,
	}
	recorder := httptest.NewRecorder()
	s.handleToggleModelEnabled(recorder, httptest.NewRequest(
		http.MethodPost, "/api/model-state/enabled",
		strings.NewReader(`{"provider":"Local","model":"Qwen3.8-27B-MLX-8bit","enabled":true}`),
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("enable model: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	models := loaded.Providers["Local"].CustomModels
	if len(models) != 1 || models[0].ID != "Qwen3.8-27B-MLX-8bit" ||
		models[0].Name != "Qwen3.8-27B-MLX-8bit" || !models[0].ToolCall || models[0].Managed {
		t.Fatalf("stored custom live model = %#v", models)
	}
	if _, _, ok := s.registry.LookupModel("Local", "Qwen3.8-27B-MLX-8bit"); !ok {
		t.Fatal("live registry was not rebuilt with enabled custom model")
	}
	state, err := config.LoadModelState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.IsModelEnabled(config.ModelRef{Provider: "Local", Model: "Qwen3.8-27B-MLX-8bit"}, false) {
		t.Fatal("enabled custom model was not persisted in model state")
	}
}

func TestManagedModelConfigFromLiveUsesXAIImagePrice(t *testing.T) {
	got := managedModelConfigFromLive(model.NewModelRegistry(), "xai", providerauth.Model{
		ID: "grok-4.6", Name: "grok-4.6", Vendor: "xai",
		Protocol: providerauth.ProtocolResponses, Kind: providerauth.ModelKindChat,
		Attachment: true, Context: 500000,
	})
	if !got.Attachment || got.Context != 500000 || !got.Reasoning || !got.Managed {
		t.Fatalf("live grok-4.6 metadata = %#v", got)
	}
	if got.Name != "grok-4.6" {
		t.Fatalf("related sibling leaked display name: %#v", got)
	}
}

func TestManagedModelConfigFromLiveKeepsExactRegistryName(t *testing.T) {
	got := managedModelConfigFromLive(model.NewModelRegistry(), "xai", providerauth.Model{
		ID: "grok-4.5", Name: "grok-4.5", Vendor: "xai",
		Protocol: providerauth.ProtocolResponses, Kind: providerauth.ModelKindChat,
	})
	if got.Name != "Grok 4.5" {
		t.Fatalf("exact grok-4.5 should keep registry name, got %#v", got)
	}
}

func TestListModelsShowsImageSupportForPersistedGrok46(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Model: "xai/grok-4.6",
		Providers: map[string]*config.ProviderConfig{
			"xai": {
				Auth: &config.ProviderAuthBinding{Method: string(providerauth.MethodXAIOAuth), AccountID: "account-1"},
				CustomModels: []config.CustomModelConfig{{
					ID: "grok-4.6", Name: "Grok 4.6", ToolCall: true, Managed: true,
				}},
			},
		},
	}
	s := &Server{cfg: cfg, registry: model.NewModelRegistryWithConfig(cfg)}
	rec := httptest.NewRecorder()
	s.handleListModels(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Providers []struct {
			ID     string `json:"id"`
			Models []struct {
				ID              string   `json:"id"`
				ImageSupport    bool     `json:"image_support"`
				InputModalities []string `json:"input_modalities"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, provider := range response.Providers {
		if provider.ID != "xai" {
			continue
		}
		for _, item := range provider.Models {
			if item.ID != "grok-4.6" {
				continue
			}
			if !item.ImageSupport {
				t.Fatalf("grok-4.6 image_support = false: %#v", item)
			}
			for _, modality := range item.InputModalities {
				if modality == "image" {
					return
				}
			}
			t.Fatalf("grok-4.6 input_modalities missing image: %#v", item)
		}
	}
	t.Fatal("xai/grok-4.6 missing from /api/models")
}

func TestEnsureManagedModelRefreshesProviderRouting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	err := config.SaveConfig(&config.Config{
		Providers: map[string]*config.ProviderConfig{
			"github-copilot": {
				Auth: &config.ProviderAuthBinding{
					Method: string(providerauth.MethodGitHubCopilot), AccountID: "account-1",
				},
				CustomModels: []config.CustomModelConfig{{
					ID: "gpt-5.6", Name: "Old name", Managed: true,
					Protocol: string(providerauth.ProtocolChatCompletions), Vendor: "azure openai",
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		registry: model.NewModelRegistry(),
		providerAuth: &fakeProviderAuthService{models: []providerauth.Model{{
			ID: "gpt-5.6", Name: "GPT-5.6 Terra", Vendor: "openai",
			Protocol: providerauth.ProtocolResponses, Kind: providerauth.ModelKindChat,
		}}},
	}
	changed, err := s.ensureManagedModelConfigured(t.Context(), "github-copilot", "gpt-5.6")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("stale managed model routing was not refreshed")
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Providers["github-copilot"].CustomModels
	if len(got) != 1 || got[0].Protocol != string(providerauth.ProtocolResponses) ||
		got[0].Vendor != "openai" || got[0].Name != "GPT-5.6 Terra" {
		t.Fatalf("refreshed managed model = %#v", got)
	}
}

func TestListModelsExposesModalitiesAndExplicitImageCatalog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Model:      "custom/chat-model",
		ImageModel: "custom/image-model",
		Providers: map[string]*config.ProviderConfig{
			"custom": {
				APIKey: "secret", BaseURL: "https://chat.example.test/v1", Name: "Custom",
				CustomModels: []config.CustomModelConfig{{ID: "chat-model", Name: "Chat", ToolCall: true, Attachment: true}},
				ImageEndpoint: &config.ImageEndpointConfig{
					Protocol: "openai_images", BaseURL: "https://images.example.test/v1",
					Models: []config.ImageModelConfig{{ID: "image-model", Name: "Image", Sizes: []string{"1024x1024"}}},
				},
			},
		},
	}
	s := &Server{cfg: cfg}
	rec := httptest.NewRecorder()
	s.handleListModels(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		CurrentImage struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"current_image"`
		Providers []struct {
			ID     string `json:"id"`
			Models []struct {
				ID                     string   `json:"id"`
				InputModalities        []string `json:"input_modalities"`
				OutputModalities       []string `json:"output_modalities"`
				CapabilityAvailability string   `json:"capability_availability"`
				ImageSizes             []string `json:"image_sizes"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.CurrentImage.Provider != "custom" || response.CurrentImage.Model != "image-model" {
		t.Fatalf("current_image = %+v", response.CurrentImage)
	}
	modelsByID := make(map[string]struct {
		Input, Output []string
		Availability  string
		Sizes         []string
	})
	for _, provider := range response.Providers {
		if provider.ID != "custom" {
			continue
		}
		for _, item := range provider.Models {
			modelsByID[item.ID] = struct {
				Input, Output []string
				Availability  string
				Sizes         []string
			}{item.InputModalities, item.OutputModalities, item.CapabilityAvailability, item.ImageSizes}
		}
	}
	chat := modelsByID["chat-model"]
	if !hasModality(chat.Input, "image") || !hasModality(chat.Output, "text") || hasModality(chat.Output, "image") {
		t.Fatalf("chat modalities = input:%v output:%v", chat.Input, chat.Output)
	}
	if chat.Availability != "unsupported" {
		t.Fatalf("image-input-only chat model availability = %q", chat.Availability)
	}
	image := modelsByID["image-model"]
	if !hasModality(image.Output, "image") || image.Availability != "supported" {
		t.Fatalf("image catalog entry = %+v", image)
	}
	if len(image.Sizes) != 1 || image.Sizes[0] != "1024x1024" {
		t.Fatalf("image sizes = %v", image.Sizes)
	}
}

func TestListModelsProjectsPersistedCustomLiveModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		providerID = "Local"
		modelID    = "Qwen3.8-27B-MLX-8bit"
	)
	if err := config.SaveModelState(&config.ModelState{EnabledModels: []config.ModelRef{{
		Provider: providerID, Model: modelID,
	}}}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Providers: map[string]*config.ProviderConfig{
		providerID: {APIKey: "test", BaseURL: "http://127.0.0.1:1234/v1", Name: providerID},
	}}
	s := &Server{cfg: cfg}
	rec := httptest.NewRecorder()
	s.handleListModels(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Providers []struct {
			ID     string `json:"id"`
			Custom bool   `json:"custom"`
			Models []struct {
				ID               string   `json:"id"`
				Enabled          bool     `json:"enabled"`
				ToolCall         bool     `json:"tool_call"`
				InputModalities  []string `json:"input_modalities"`
				OutputModalities []string `json:"output_modalities"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, provider := range response.Providers {
		if provider.ID != providerID {
			continue
		}
		if !provider.Custom || len(provider.Models) != 1 {
			t.Fatalf("projected custom provider = %#v", provider)
		}
		got := provider.Models[0]
		if got.ID != modelID || !got.Enabled || !got.ToolCall ||
			!hasModality(got.InputModalities, "text") || !hasModality(got.OutputModalities, "text") {
			t.Fatalf("projected live model = %#v", got)
		}
		return
	}
	t.Fatalf("custom provider missing from /api/models: %s", rec.Body.String())
}

func TestListModelsExposesManagedXAIImageRole(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".jcode")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	accountJSON := `{"version":1,"methods":{"xai_oauth":{"accounts":{"account-1":{"id":"account-1","login":"grok-user","secret":"refresh-token","authenticated_at":"2026-08-10T00:00:00Z"}},"default_account_id":"account-1"}}}`
	if err := os.WriteFile(filepath.Join(configDir, "provider-auth.json"), []byte(accountJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Model:      "xai/grok-4.5",
		ImageModel: "xai/grok-imagine-image-quality",
		Providers: map[string]*config.ProviderConfig{
			"xai": {Auth: &config.ProviderAuthBinding{Method: string(providerauth.MethodXAIOAuth), AccountID: "account-1"}},
		},
	}
	s := &Server{cfg: cfg}
	rec := httptest.NewRecorder()
	s.handleListModels(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Providers []struct {
			ID     string `json:"id"`
			Models []struct {
				ID               string   `json:"id"`
				Output           []string `json:"output_modalities"`
				Availability     string   `json:"capability_availability"`
				AspectRatios     []string `json:"image_aspect_ratios"`
				ImageResolutions []string `json:"image_resolutions"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var imageIDs []string
	for _, provider := range response.Providers {
		if provider.ID != "xai" {
			continue
		}
		for _, candidate := range provider.Models {
			if len(candidate.Output) == 1 && candidate.Output[0] == "image" {
				if candidate.Availability != "supported" {
					t.Fatalf("image model %s availability = %q", candidate.ID, candidate.Availability)
				}
				if len(candidate.AspectRatios) != 14 || len(candidate.ImageResolutions) != 2 ||
					candidate.ImageResolutions[0] != "1k" || candidate.ImageResolutions[1] != "2k" {
					t.Fatalf("image model %s geometry = ratios:%v resolutions:%v", candidate.ID,
						candidate.AspectRatios, candidate.ImageResolutions)
				}
				imageIDs = append(imageIDs, candidate.ID)
			}
		}
	}
	if len(imageIDs) != 2 || imageIDs[0] != "grok-imagine-image" || imageIDs[1] != "grok-imagine-image-quality" {
		t.Fatalf("managed xAI image models = %#v", imageIDs)
	}
}

func TestConfiguredImageAvailabilityRejectsMissingManagedAccount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	availability := configuredImageAvailability(&config.ProviderConfig{
		Auth: &config.ProviderAuthBinding{Method: string(providerauth.MethodXAIOAuth), AccountID: "missing"},
	}, providertools.ImageModel{
		Provider: "xai", ID: "grok-imagine-image", Builtin: true, Supported: true,
	})
	if availability != "unsupported" {
		t.Fatalf("missing managed account availability = %q", availability)
	}
}

func TestSetImageModelPersistsIndependentRole(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Model: "chat/chat-model",
		Providers: map[string]*config.ProviderConfig{
			"chat": {APIKey: "chat-secret"},
			"image": {
				APIKey: "image-secret",
				ImageEndpoint: &config.ImageEndpointConfig{
					Protocol: "openai_images", BaseURL: "https://images.example.test/v1",
					Models: []config.ImageModelConfig{{ID: "canvas/v1"}},
				},
			},
		},
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg, needsSetup: true, wsBroker: NewWSBroker()}
	rec := httptest.NewRecorder()
	s.handleSetImageModel(rec, httptest.NewRequest(http.MethodPost, "/api/image-model", strings.NewReader(`{"provider":"image","model":"canvas/v1"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if cfg.ImageModel != "image/canvas/v1" || cfg.Model != "chat/chat-model" {
		t.Fatalf("roles after update: chat=%q image=%q", cfg.Model, cfg.ImageModel)
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ImageModel != "image/canvas/v1" {
		t.Fatalf("persisted ImageModel = %q", loaded.ImageModel)
	}
}

func TestSetImageModelReportsSavedButNotAppliedWhenAgentRebuildFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Model: "chat/chat-model", ImageModel: "image/canvas-1",
		Providers: map[string]*config.ProviderConfig{
			"chat": {APIKey: "chat-secret"},
			"image": {
				APIKey: "image-secret",
				ImageEndpoint: &config.ImageEndpointConfig{
					Protocol: "openai_images", BaseURL: "https://images.example.test/v1",
					Models: []config.ImageModelConfig{{ID: "canvas-1"}, {ID: "canvas-2"}},
				},
			},
		},
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	eng := &Engine{
		taskID: "idle-task", providerName: "chat", modelName: "chat-model",
		createAgent: func(_, _ string) (*adk.ChatModelAgent, error) {
			return nil, errors.New("image model rebuild failure canary")
		},
	}
	s := &Server{Engine: eng, cfg: cfg}
	rec := httptest.NewRecorder()
	s.handleSetImageModel(rec, httptest.NewRequest(
		http.MethodPost, "/api/image-model", strings.NewReader(`{"provider":"image","model":"canvas-2"}`),
	))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"status":"saved_but_not_applied"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "image-secret") || strings.Contains(rec.Body.String(), "rebuild failure canary") {
		t.Fatalf("apply failure response exposed internal or secret data: %s", rec.Body.String())
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ImageModel != "image/canvas-2" {
		t.Fatalf("saved image model mutation was lost: %q", loaded.ImageModel)
	}
}

func TestListModelsUsesExactProviderManifest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	makeResponse := func(baseURL string) string {
		cfg := &config.Config{Providers: map[string]*config.ProviderConfig{
			"zhipuai-coding-plan": {APIKey: "secret", BaseURL: baseURL},
		}}
		s := &Server{cfg: cfg}
		rec := httptest.NewRecorder()
		s.handleListModels(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	exact := makeResponse("https://open.bigmodel.cn/api/coding/paas/v4")
	if !strings.Contains(exact, `"id":"cogview-3-flash"`) || !strings.Contains(exact, `"capability_availability":"supported"`) {
		t.Fatalf("exact BigModel profile did not expose manifest image model: %s", exact)
	}
	custom := makeResponse("https://proxy.example.test/v4")
	if strings.Contains(custom, `"id":"cogview-3-flash"`) {
		t.Fatalf("custom base URL inherited a private BigModel image capability: %s", custom)
	}
}

func TestListModelsKeepsRegistryImageCandidatesOutOfSupportedCapabilities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{Providers: map[string]*config.ProviderConfig{
		"alibaba-token-plan": {APIKey: "secret"},
	}}
	s := &Server{cfg: cfg}
	rec := httptest.NewRecorder()
	s.handleListModels(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Providers []struct {
			ID     string `json:"id"`
			Models []struct {
				ID                     string   `json:"id"`
				ToolCall               bool     `json:"tool_call"`
				OutputModalities       []string `json:"output_modalities"`
				CapabilityAvailability string   `json:"capability_availability"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, provider := range response.Providers {
		if provider.ID != "alibaba-token-plan" {
			continue
		}
		for _, item := range provider.Models {
			if item.ID != "qwen-image-2.0" {
				continue
			}
			if item.ToolCall || !hasModality(item.OutputModalities, "image") || item.CapabilityAvailability != "unknown" {
				t.Fatalf("registry image candidate was treated as a chat/supported model: %+v", item)
			}
			return
		}
	}
	t.Fatal("registry image-only model was filtered out of /api/models")
}
