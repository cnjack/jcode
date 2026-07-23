package cloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListCloudModelsEnrichesKnownDesktopCatalogModel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/device/cloud-models", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer device-token" {
			t.Errorf("Authorization=%q want Bearer device-token", got)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"models": []map[string]any{{
				"model_id":          "cloud-model-id",
				"provider_id":       "cloud-provider-id",
				"kind":              "zhipuai-coding-plan",
				"provider_name":     "Zhipu AI Coding Plan",
				"model_name":        "GLM-5.2",
				"upstream_model_id": "glm-5.2",
				"scope":             "account",
				"scope_id":          "account-id",
				"capabilities":      map[string]bool{},
				"context_window":    0,
			}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	models, err := NewClient(srv.URL).ListCloudModels(context.Background(), "device-token")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("models=%d want 1", len(models))
	}
	got := models[0]
	if !got.Capabilities.Reasoning || !got.Capabilities.Tools || got.Capabilities.Image {
		t.Fatalf("capabilities=%+v want reasoning/tools true and image false", got.Capabilities)
	}
	if got.ContextWindow != 1_000_000 {
		t.Fatalf("context_window=%d want 1000000", got.ContextWindow)
	}
	if len(got.ReasoningOptions) != 1 ||
		got.ReasoningOptions[0].Type != "effort" ||
		len(got.ReasoningOptions[0].Values) != 2 ||
		got.ReasoningOptions[0].Values[0] != "high" ||
		got.ReasoningOptions[0].Values[1] != "max" {
		t.Fatalf("reasoning_options=%+v want effort [high max]", got.ReasoningOptions)
	}
}

func TestListCloudModelsPreservesUnknownModelMetadata(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/device/cloud-models", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"models": []map[string]any{{
				"model_id":          "custom-cloud-model",
				"provider_id":       "custom-provider",
				"kind":              "custom-provider-kind",
				"provider_name":     "Custom",
				"model_name":        "Custom Reasoner",
				"upstream_model_id": "custom-reasoner-v7",
				"scope":             "account",
				"scope_id":          "account-id",
				"capabilities": map[string]bool{
					"reasoning": true,
					"tools":     false,
					"image":     true,
				},
				"context_window": 12345,
				"reasoning_options": []map[string]any{{
					"type":   "effort",
					"values": []string{"low", "high"},
				}},
			}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	models, err := NewClient(srv.URL).ListCloudModels(context.Background(), "device-token")
	if err != nil {
		t.Fatal(err)
	}
	got := models[0]
	if !got.Capabilities.Reasoning || got.Capabilities.Tools || !got.Capabilities.Image {
		t.Fatalf("capabilities changed for unknown model: %+v", got.Capabilities)
	}
	if got.ContextWindow != 12345 ||
		len(got.ReasoningOptions) != 1 ||
		got.ReasoningOptions[0].Values[1] != "high" {
		t.Fatalf("unknown model metadata changed: context=%d options=%+v", got.ContextWindow, got.ReasoningOptions)
	}
}
