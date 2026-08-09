package imagegen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTokenPlanMultimodalTLSRoundTrip(t *testing.T) {
	pixels := pngBytes(t, 4, 3)
	var server *httptest.Server
	var postCalls, downloadCalls atomic.Int32
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case tokenPlanMultimodalPath:
			postCalls.Add(1)
			if r.Method != http.MethodPost {
				t.Errorf("method = %s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer token-plan-secret" {
				t.Errorf("Authorization = %q", got)
			}
			if got := r.Header.Get("X-DashScope-Async"); got != "" {
				t.Errorf("Token Plan sync request sent X-DashScope-Async=%q", got)
			}
			var body struct {
				Model string `json:"model"`
				Input struct {
					Messages []struct {
						Role    string `json:"role"`
						Content []struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"messages"`
				} `json:"input"`
				Parameters struct {
					Size string `json:"size"`
					N    int    `json:"n"`
				} `json:"parameters"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Model != "wan2.7-image-pro" || body.Parameters.Size != "1024*1024" ||
				body.Parameters.N != 1 || len(body.Input.Messages) != 1 ||
				body.Input.Messages[0].Role != "user" ||
				len(body.Input.Messages[0].Content) != 1 ||
				body.Input.Messages[0].Content[0].Text != "a blue basketball" {
				t.Errorf("request body = %#v", body)
			}
			jsonResponse := map[string]any{"output": map[string]any{"choices": []map[string]any{{
				"message": map[string]any{"content": []map[string]string{{
					"image": server.URL + "/signed/result.png",
				}}},
			}}}}
			writeImagegenJSON(t, w, http.StatusOK, jsonResponse)
		case "/signed/result.png":
			downloadCalls.Add(1)
			if r.Header.Get("Authorization") != "" || r.Header.Get("X-Provider-Secret") != "" {
				t.Errorf("provider credential leaked to signed image download")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(pixels)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	generator, err := NewGenerator(ClientConfig{
		Protocol:   ProtocolTokenPlanMultimodal,
		BaseURL:    server.URL + tokenPlanMultimodalPath,
		APIKey:     "token-plan-secret",
		Headers:    map[string]string{"X-Provider-Secret": "custom-secret"},
		Model:      "wan2.7-image-pro",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := generator.Generate(context.Background(), Request{
		Prompt: "a blue basketball", Size: "1024x1024", Count: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if postCalls.Load() != 1 || downloadCalls.Load() != 1 {
		t.Fatalf("calls post=%d download=%d", postCalls.Load(), downloadCalls.Load())
	}
	if len(result.Images) != 1 || result.Images[0].MIMEType != "image/png" ||
		result.Images[0].Width != 4 || result.Images[0].Height != 3 {
		t.Fatalf("result = %#v", result)
	}
}

func TestTokenPlanMultimodalDoesNotRetryOrExposeProviderError(t *testing.T) {
	const canary = "secret-bearing provider response and signed URL"
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, canary, http.StatusInternalServerError)
	}))
	defer server.Close()
	client, err := NewTokenPlanMultimodalClient(ClientConfig{
		Protocol:   ProtocolTokenPlanMultimodal,
		BaseURL:    server.URL + tokenPlanMultimodalPath,
		APIKey:     "credential-canary",
		Model:      "wan2.7-image",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(context.Background(), Request{Prompt: "one image", Count: 1})
	if err == nil || err.Error() != "image provider returned HTTP 500" {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), "credential-canary") {
		t.Fatalf("error exposed provider data: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("POST calls = %d, want exactly one", calls.Load())
	}
}

func TestTokenPlanMultimodalRejectsNonSingleImageResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeImagegenJSON(t, w, http.StatusOK, map[string]any{
			"output": map[string]any{
				"choices": []any{
					map[string]any{
						"message": map[string]any{
							"content": []any{
								map[string]string{"type": "image", "image": "https://one.example/a.png"},
								map[string]string{"type": "image", "image": "https://two.example/b.png"},
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()
	client, err := NewTokenPlanMultimodalClient(ClientConfig{
		Protocol:   ProtocolTokenPlanMultimodal,
		BaseURL:    server.URL + tokenPlanMultimodalPath,
		Model:      "wan2.7-image",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(context.Background(), Request{Prompt: "one image", Count: 1})
	if err == nil || !strings.Contains(err.Error(), "P0 requires exactly one") {
		t.Fatalf("error = %v", err)
	}
}

func TestTokenPlanMultimodalValidatesProtocolEndpointAndHeaders(t *testing.T) {
	tests := []ClientConfig{
		{Protocol: ProtocolTokenPlanMultimodal, BaseURL: "https://example.com/compatible-mode/v1", Model: "wan2.7-image"},
		{Protocol: ProtocolTokenPlanMultimodal, BaseURL: "http://example.com" + tokenPlanMultimodalPath, Model: "wan2.7-image"},
		{Protocol: ProtocolTokenPlanMultimodal, BaseURL: "https://example.com" + tokenPlanMultimodalPath, Model: "wan2.7-image", Headers: map[string]string{"X-DashScope-Async": "enable"}},
	}
	for _, cfg := range tests {
		if _, err := NewTokenPlanMultimodalClient(cfg); err == nil {
			t.Fatalf("NewTokenPlanMultimodalClient(%+v) succeeded", cfg)
		}
	}
	if _, err := NewGenerator(ClientConfig{Protocol: "unknown", BaseURL: "https://example.com", Model: "m"}); err == nil {
		t.Fatal("unknown protocol resolved")
	}
}

func TestTokenPlanMultimodalAllowsSingleLabelAcceleratedOSSAssetHost(t *testing.T) {
	client, err := NewTokenPlanMultimodalClient(ClientConfig{
		Protocol: ProtocolTokenPlanMultimodal,
		BaseURL:  "https://token-plan.cn-beijing.maas.aliyuncs.com" + tokenPlanMultimodalPath,
		Model:    "wan2.7-image",
		AssetHosts: []string{
			"*.oss-accelerate.aliyuncs.com",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !client.resolver.assetHostAllowed("dashscope-7c2c.oss-accelerate.aliyuncs.com") {
		t.Fatal("Token Plan accelerated OSS asset host was blocked")
	}
	for _, host := range []string{
		"oss-accelerate.aliyuncs.com",
		"nested.dashscope-7c2c.oss-accelerate.aliyuncs.com",
		"dashscope-7c2c.oss-accelerate.aliyuncs.com.evil.example",
	} {
		if client.resolver.assetHostAllowed(host) {
			t.Fatalf("accelerated OSS wildcard unexpectedly allowed %q", host)
		}
	}
}

func TestNormalizeTokenPlanSize(t *testing.T) {
	tests := map[string]string{
		"": "", "1k": "1K", "2K": "2K", "4096x4096": "4096*4096", "1024X768": "1024*768",
	}
	for input, want := range tests {
		got, err := normalizeTokenPlanSize(input)
		if err != nil || got != want {
			t.Fatalf("normalizeTokenPlanSize(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"1024", "0x1024", "1024x", "1x2x3"} {
		if _, err := normalizeTokenPlanSize(input); err == nil {
			t.Fatalf("normalizeTokenPlanSize(%q) succeeded", input)
		}
	}
}

func writeImagegenJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
}
