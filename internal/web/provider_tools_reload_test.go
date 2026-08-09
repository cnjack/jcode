package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/providertools"
	"github.com/cnjack/jcode/internal/tools"
)

func TestRebuildProviderDependentsReconnectsBigModelMCP(t *testing.T) {
	var reloadCalls int
	s := &Server{
		cfg:        &config.Config{},
		needsSetup: true,
		reloadMCP: func(map[string]*config.MCPServer) ([]tools.MCPStatus, error) {
			reloadCalls++
			return nil, nil
		},
	}

	if err := s.rebuildProviderDependents(providertools.BigModelCodingProvider, "update"); err != nil {
		t.Fatal(err)
	}
	if reloadCalls != 1 {
		t.Fatalf("provider MCP reload calls = %d, want 1", reloadCalls)
	}
}

func TestUpdateBigModelProviderReloadsMCPAfterConfigUnlock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Model: providertools.BigModelCodingProvider + "/glm-4.7",
		Providers: map[string]*config.ProviderConfig{
			providertools.BigModelCodingProvider: {
				APIKey:  "provider-reload-secret",
				BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4",
				ProviderTools: map[string]config.ProviderToolPolicy{
					providertools.ToolWebSearch: {Enabled: true},
				},
			},
		},
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	var reloadCalls int
	s := &Server{
		cfg: cfg, needsSetup: true,
		reloadMCP: func(map[string]*config.MCPServer) ([]tools.MCPStatus, error) {
			reloadCalls++
			return nil, nil
		},
	}
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/providers/"+providertools.BigModelCodingProvider,
		strings.NewReader(`{"provider_tools":{"web_search":{"enabled":false}}}`),
	)
	req.SetPathValue("id", providertools.BigModelCodingProvider)
	rec := httptest.NewRecorder()

	s.handleUpdateProvider(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if reloadCalls != 1 {
		t.Fatalf("provider MCP reload calls = %d, want 1", reloadCalls)
	}
}

func TestUpdateBigModelProviderReportsSavedButNotAppliedWhenMCPReloadFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const secret = "provider-reload-secret-canary"
	cfg := &config.Config{
		Model: providertools.BigModelCodingProvider + "/glm-4.7",
		Providers: map[string]*config.ProviderConfig{
			providertools.BigModelCodingProvider: {
				APIKey: secret, BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4",
				ProviderTools: map[string]config.ProviderToolPolicy{
					providertools.ToolWebSearch: {Enabled: true},
				},
			},
		},
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg: cfg, needsSetup: true,
		reloadMCP: func(map[string]*config.MCPServer) ([]tools.MCPStatus, error) {
			return nil, errors.New("reload failed with " + secret)
		},
	}
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/providers/"+providertools.BigModelCodingProvider,
		strings.NewReader(`{"api_key":"rotated-provider-secret","provider_tools":{"web_search":{"enabled":false}}}`),
	)
	req.SetPathValue("id", providertools.BigModelCodingProvider)
	rec := httptest.NewRecorder()

	s.handleUpdateProvider(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"status":"saved_but_not_applied"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Body.String(), "rotated-provider-secret") {
		t.Fatalf("apply failure response leaked provider secret: %s", rec.Body.String())
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	policy := loaded.Providers[providertools.BigModelCodingProvider].ProviderTools[providertools.ToolWebSearch]
	if policy.Enabled || loaded.Providers[providertools.BigModelCodingProvider].APIKey != "rotated-provider-secret" {
		t.Fatalf("saved mutation was lost: provider=%#v", loaded.Providers[providertools.BigModelCodingProvider])
	}
}

func TestUpdateImageProviderReportsSavedButNotAppliedWhenAgentRebuildFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Model: "custom/chat", ImageModel: "custom/canvas",
		Providers: map[string]*config.ProviderConfig{
			"custom": {
				APIKey: "old-image-provider-secret", BaseURL: "https://chat.example.test/v1",
				CustomModels: []config.CustomModelConfig{{ID: "chat", ToolCall: true}},
				ProviderTools: map[string]config.ProviderToolPolicy{
					providertools.ToolImageGeneration: {Enabled: false},
				},
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
	eng := &Engine{
		taskID: "idle-task", providerName: "custom", modelName: "chat",
		createAgent: func(_, _ string) (*adk.ChatModelAgent, error) {
			return nil, errors.New("agent rebuild failure canary")
		},
	}
	s := &Server{Engine: eng, cfg: cfg, registry: model.NewModelRegistryWithConfig(cfg)}
	req := httptest.NewRequest(http.MethodPut, "/api/providers/custom", strings.NewReader(`{
		"api_key":"rotated-image-provider-secret",
		"provider_tools":{"image_generation":{"enabled":true}},
		"image_endpoint":{"protocol":"openai_images","base_url":"https://images.example.test/v2","models":[{"id":"canvas"}]}
	}`))
	req.SetPathValue("id", "custom")
	rec := httptest.NewRecorder()

	s.handleUpdateProvider(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"status":"saved_but_not_applied"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "rotated-image-provider-secret") || strings.Contains(rec.Body.String(), "agent rebuild failure canary") {
		t.Fatalf("apply failure response exposed internal or secret data: %s", rec.Body.String())
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	provider := loaded.Providers["custom"]
	if provider.APIKey != "rotated-image-provider-secret" || !provider.ProviderTools[providertools.ToolImageGeneration].Enabled ||
		provider.ImageEndpoint == nil || provider.ImageEndpoint.BaseURL != "https://images.example.test/v2" {
		t.Fatalf("saved provider mutation was lost: %#v", provider)
	}
}
