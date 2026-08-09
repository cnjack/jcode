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
