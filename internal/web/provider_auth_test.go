package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/providerauth"
)

type fakeProviderAuthService struct {
	status    providerauth.Status
	flow      providerauth.Flow
	validate  error
	cancelled string
}

func (f *fakeProviderAuthService) Start(context.Context, providerauth.Method) (providerauth.Flow, error) {
	return f.flow, nil
}

func (f *fakeProviderAuthService) Poll(context.Context, providerauth.Method, string) (providerauth.Flow, error) {
	return f.flow, nil
}

func (f *fakeProviderAuthService) Cancel(_ providerauth.Method, flowID string) error {
	f.cancelled = flowID
	return nil
}

func (f *fakeProviderAuthService) Status(context.Context, providerauth.Method) (providerauth.Status, error) {
	return f.status, nil
}

func (f *fakeProviderAuthService) SetDefault(context.Context, providerauth.Method, string) error {
	return nil
}

func (f *fakeProviderAuthService) Remove(context.Context, providerauth.Method, string) error {
	return nil
}

func (f *fakeProviderAuthService) Logout(context.Context, providerauth.Method) error {
	return nil
}

func (f *fakeProviderAuthService) ValidateBinding(context.Context, providerauth.Binding) error {
	return f.validate
}

func TestAddManagedProviderStoresOnlyBinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := &fakeProviderAuthService{}
	s := &Server{
		cfg:          &config.Config{},
		registry:     model.NewModelRegistry(),
		providerAuth: fake,
		needsSetup:   true,
	}
	body := `{
  "id":"xai",
  "api_key":"must-not-survive",
  "base_url":"https://attacker.example/v1",
  "headers":{"Authorization":"must-not-survive"},
  "auth_binding":{"method":" xai_oauth ","account_id":" acct-x "}
}`
	recorder := httptest.NewRecorder()
	s.handleAddProvider(recorder, httptest.NewRequest(http.MethodPost, "/api/providers", strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	provider := loaded.Providers["xai"]
	if provider == nil || provider.Auth == nil || provider.Auth.Method != "xai_oauth" || provider.Auth.AccountID != "acct-x" {
		t.Fatalf("stored provider auth = %#v", provider)
	}
	if provider.APIKey != "" || provider.BaseURL != "" || len(provider.Headers) != 0 {
		t.Fatalf("managed provider retained override or secret: %#v", provider)
	}
}

func TestAddProviderRejectsIncompatibleManagedMethod(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		cfg:          &config.Config{},
		registry:     model.NewModelRegistry(),
		providerAuth: &fakeProviderAuthService{},
		needsSetup:   true,
	}
	recorder := httptest.NewRecorder()
	s.handleAddProvider(recorder, httptest.NewRequest(
		http.MethodPost,
		"/api/providers",
		strings.NewReader(`{"id":"anthropic","auth_binding":{"method":"xai_oauth"}}`),
	))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestManagedProviderRejectsImageEndpointWithoutAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		cfg:          &config.Config{},
		registry:     model.NewModelRegistry(),
		providerAuth: &fakeProviderAuthService{},
		needsSetup:   true,
	}
	recorder := httptest.NewRecorder()
	s.handleAddProvider(recorder, httptest.NewRequest(
		http.MethodPost,
		"/api/providers",
		strings.NewReader(`{
			"id":"openai",
			"auth_binding":{"method":"codex_oauth"},
			"image_endpoint":{"protocol":"openai_images","base_url":"https://images.example.test/v1","models":[{"id":"canvas"}]}
		}`),
	))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "image_endpoint") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSwitchingProviderToManagedAuthClearsImageEndpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		Model:      "openai/gpt-5",
		ImageModel: "openai/canvas",
		Providers: map[string]*config.ProviderConfig{
			"openai": {
				APIKey: "api-key",
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
	s := &Server{
		cfg: cfg, registry: model.NewModelRegistryWithConfig(cfg),
		providerAuth: &fakeProviderAuthService{}, needsSetup: true,
	}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/providers/openai",
		strings.NewReader(`{"auth_binding":{"method":"codex_oauth"},"image_endpoint":null}`),
	)
	request.SetPathValue("id", "openai")
	recorder := httptest.NewRecorder()
	s.handleUpdateProvider(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	loaded, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	provider := loaded.Providers["openai"]
	if provider.Auth == nil || provider.ImageEndpoint != nil || provider.APIKey != "" {
		t.Fatalf("managed provider retained image endpoint or key: %+v", provider)
	}
	if loaded.ImageModel != "" {
		t.Fatalf("managed switch retained selected image model %q", loaded.ImageModel)
	}
}

func TestAddProviderStillRequiresAPIKeyInLegacyMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{cfg: &config.Config{}, registry: model.NewModelRegistry(), needsSetup: true}
	recorder := httptest.NewRecorder()
	s.handleAddProvider(recorder, httptest.NewRequest(
		http.MethodPost,
		"/api/providers",
		strings.NewReader(`{"id":"openai"}`),
	))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "api_key") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestProviderAuthStartReturnsOnlyPublicFlow(t *testing.T) {
	expires := time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second)
	fake := &fakeProviderAuthService{flow: providerauth.Flow{
		ID:              "public-flow-id",
		Method:          providerauth.MethodCodexOAuth,
		State:           providerauth.FlowStatePending,
		UserCode:        "ABCD-EFGH",
		VerificationURI: "https://auth.example/device",
		ExpiresAt:       expires,
		IntervalSeconds: 5,
	}}
	s := &Server{providerAuth: fake}
	request := httptest.NewRequest(http.MethodPost, "/api/provider-auth/codex_oauth/start", strings.NewReader(`{}`))
	request.SetPathValue("method", "codex_oauth")
	recorder := httptest.NewRecorder()
	s.handleProviderAuthStart(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, forbidden := range []string{"device_code", "access_token", "refresh_token", "authorization_code", "code_verifier"} {
		if _, exists := body[forbidden]; exists {
			t.Fatalf("public flow exposed %s: %s", forbidden, recorder.Body.String())
		}
	}
	if body["flow_id"] != "public-flow-id" || body["user_code"] != "ABCD-EFGH" {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
}

func TestProviderBindingValidationFailureIsConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		cfg:      &config.Config{},
		registry: model.NewModelRegistry(),
		providerAuth: &fakeProviderAuthService{
			validate: errors.Join(providerauth.ErrRequiresReauth, errors.New("sign in again")),
		},
		needsSetup: true,
	}
	recorder := httptest.NewRecorder()
	s.handleAddProvider(recorder, httptest.NewRequest(
		http.MethodPost,
		"/api/providers",
		strings.NewReader(`{"id":"openai","auth_binding":{"method":"codex_oauth"}}`),
	))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
