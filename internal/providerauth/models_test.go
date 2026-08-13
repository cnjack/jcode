package providerauth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestManagedModelParsersCoverProviderShapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		method  Method
		payload any
		want    []Model
	}{
		{
			name:   "xai openai data",
			method: MethodXAIOAuth,
			payload: map[string]any{"data": []any{
				map[string]any{"id": "grok-4.5", "owned_by": "xai"},
				map[string]any{"id": "grok-2-image", "owned_by": "xai"},
			}},
			want: []Model{
				{ID: "grok-2-image", Name: "grok-2-image", Vendor: "xai", Protocol: ProtocolResponses, Kind: ModelKindChat},
				{ID: "grok-4.5", Name: "grok-4.5", Vendor: "xai", Protocol: ProtocolResponses, Kind: ModelKindChat},
			},
		},
		{
			name:   "codex model map",
			method: MethodCodexOAuth,
			payload: map[string]any{"models": map[string]any{
				"gpt-5.4":      map[string]any{"display_name": "GPT-5.4", "owned_by": "openai"},
				"gpt-5.4-mini": "gpt-5.4-mini",
			}},
			want: []Model{
				{ID: "gpt-5.4", Name: "GPT-5.4", Vendor: "openai", Protocol: ProtocolResponses, Kind: ModelKindChat},
				{ID: "gpt-5.4-mini", Name: "gpt-5.4-mini", Protocol: ProtocolResponses, Kind: ModelKindChat},
			},
		},
		{
			name:   "copilot picker and protocol",
			method: MethodGitHubCopilot,
			payload: map[string]any{"data": []any{
				map[string]any{"id": "gpt-5.6", "name": "GPT-5.6 Terra", "vendor": "openai", "model_picker_enabled": true},
				map[string]any{"id": "claude-sonnet-5", "name": "Claude Sonnet 5", "vendor": "anthropic", "model_picker_enabled": true},
				map[string]any{"id": "hidden", "name": "Hidden", "vendor": "openai", "model_picker_enabled": false},
			}},
			want: []Model{
				{ID: "claude-sonnet-5", Name: "Claude Sonnet 5", Vendor: "anthropic", Protocol: ProtocolChatCompletions, Kind: ModelKindChat},
				{ID: "gpt-5.6", Name: "GPT-5.6 Terra", Vendor: "openai", Protocol: ProtocolResponses, Kind: ModelKindChat},
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := parseManagedModels(test.method, test.payload); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("models = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseManagedModelsReadsXAIImagePriceAndContext(t *testing.T) {
	t.Parallel()
	models := parseManagedModels(MethodXAIOAuth, map[string]any{"data": []any{
		map[string]any{
			"id": "grok-4.6", "owned_by": "xai",
			"prompt_image_token_price": float64(20000),
			"context_length":           float64(500000),
		},
		map[string]any{
			"id":                       "grok-imagine-image-quality",
			"prompt_image_token_price": float64(20000),
		},
	}})
	if len(models) != 2 {
		t.Fatalf("models = %#v", models)
	}
	if models[0].ID != "grok-4.6" || !models[0].Attachment || models[0].Context != 500000 ||
		models[0].Kind != ModelKindChat {
		t.Fatalf("chat model = %#v", models[0])
	}
	if models[1].ID != "grok-imagine-image-quality" || models[1].Attachment || models[1].Kind != ModelKindImage {
		t.Fatalf("image model should not inherit chat vision: %#v", models[1])
	}
}

func TestParseManagedModelsReadsXAIImagePriceFromJSONNumbers(t *testing.T) {
	t.Parallel()
	decoder := json.NewDecoder(bytes.NewReader([]byte(`{
		"data":[{
			"id":"grok-4.6",
			"owned_by":"xai",
			"prompt_image_token_price":20000,
			"context_length":500000
		}]
	}`)))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	models := parseManagedModels(MethodXAIOAuth, payload)
	if len(models) != 1 || !models[0].Attachment || models[0].Context != 500000 {
		t.Fatalf("json-number catalog = %#v", models)
	}
}

func TestXAIModelKindsSeparateChatImageAndVideo(t *testing.T) {
	models := parseManagedModels(MethodXAIOAuth, map[string]any{"data": []any{
		map[string]any{"id": "grok-4.5"},
		map[string]any{"id": "grok-imagine-image-quality"},
		map[string]any{"id": "grok-imagine-video-1.5"},
	}})
	if len(models) != 3 || models[0].Kind != ModelKindChat ||
		models[1].Kind != ModelKindImage || models[2].Kind != ModelKindVideo {
		t.Fatalf("xAI model kinds = %#v", models)
	}
}

func TestManagedModelsUseAccountCredentialAndPinnedRuntime(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	seen := make(chan string, 3)
	baseURL := "https://provider-models.example.test"
	client := &http.Client{Transport: authRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Errorf("authorization = %q", got)
		}
		status := http.StatusOK
		body := ""
		switch request.URL.Path {
		case "/codex/runtime/models":
			if request.URL.Query().Get("client_version") != codexClientVersion ||
				request.Header.Get("chatgpt-account-id") != "codex-account" ||
				request.Header.Get("originator") == "" {
				t.Errorf("codex request missing required metadata: %s %#v", request.URL.String(), request.Header)
			}
			seen <- "codex"
			body = `{"models":[{"slug":"gpt-5.4"}]}`
		case "/xai/runtime/models":
			seen <- "xai"
			body = `{"data":[{"id":"grok-4.5"}]}`
		case "/copilot/runtime/models":
			if request.Header.Get("copilot-integration-id") != copilotIntegrationID ||
				request.Header.Get("editor-version") != copilotEditorVersion {
				t.Errorf("copilot request missing required metadata: %#v", request.Header)
			}
			seen <- "copilot"
			body = `{"data":[{"id":"gpt-5.6","vendor":"openai","model_picker_enabled":true}]}`
		default:
			status = http.StatusNotFound
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}

	manager, err := NewManager(Options{
		ConfigDir: t.TempDir(), HTTPClient: client, Now: clock.Now,
		Endpoints: testEndpoints(baseURL), AllowInsecureTestEndpoints: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	bindings := []Binding{
		{Method: MethodCodexOAuth, AccountID: "codex-account"},
		{Method: MethodXAIOAuth, AccountID: "xai-account"},
		{Method: MethodGitHubCopilot, AccountID: "copilot-account"},
	}
	for _, binding := range bindings {
		if err := manager.upsertAccount(binding.Method, storedAccount{
			ID: binding.AccountID, Login: binding.AccountID, Secret: "durable-secret", AuthenticatedAt: clock.Now(),
		}); err != nil {
			t.Fatal(err)
		}
		manager.cache(binding.Method, binding.AccountID, "access-token", clock.Now().Add(2*time.Hour))
	}
	manager.mu.Lock()
	manager.copilotEndpoints[accountKey(MethodGitHubCopilot, "copilot-account")] = baseURL + "/copilot/runtime"
	manager.mu.Unlock()

	for _, binding := range bindings {
		models, err := manager.Models(context.Background(), binding)
		if err != nil {
			t.Fatalf("%s models: %v", binding.Method, err)
		}
		if len(models) != 1 {
			t.Fatalf("%s models = %#v", binding.Method, models)
		}
	}
	close(seen)
	got := make(map[string]bool)
	for provider := range seen {
		got[provider] = true
	}
	for _, provider := range []string{"codex", "xai", "copilot"} {
		if !got[provider] {
			t.Errorf("missing %s catalog request", provider)
		}
	}
}

func TestManagedModelsDoNotExposeUpstreamErrorBody(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	baseURL := "https://provider-error.example.test"
	client := &http.Client{Transport: authRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":"token access-token device-code"}`)),
			Request:    request,
		}, nil
	})}
	manager, err := NewManager(Options{
		ConfigDir: t.TempDir(), HTTPClient: client, Now: clock.Now,
		Endpoints: testEndpoints(baseURL), AllowInsecureTestEndpoints: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.upsertAccount(MethodXAIOAuth, storedAccount{
		ID: "xai-account", Login: "xai", Secret: "durable-secret", AuthenticatedAt: clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	manager.cache(MethodXAIOAuth, "xai-account", "access-token", clock.Now().Add(2*time.Hour))
	_, err = manager.Models(context.Background(), Binding{Method: MethodXAIOAuth, AccountID: "xai-account"})
	if err == nil || strings.Contains(err.Error(), "access-token") || strings.Contains(err.Error(), "device-code") {
		t.Fatalf("unsafe error = %v", err)
	}
}
