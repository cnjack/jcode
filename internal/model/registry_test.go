package model

import (
	"testing"

	"github.com/cnjack/jcode/internal/config"
)

// TestLookupModel tests model lookup from the generated registry.
func TestLookupModel(t *testing.T) {
	r := NewModelRegistry()

	// Test a known provider/model (this assumes OpenAI exists in models.dev)
	prov, model, ok := r.LookupModel("openai", "gpt-4")
	if !ok {
		t.Skip("openai/gpt-4 not found in generated registry (models.dev may have changed)")
	}
	if prov == nil {
		t.Error("Expected provider to be non-nil")
	}
	if model == nil {
		t.Error("Expected model to be non-nil")
	}

	// Test non-existent provider
	_, _, ok = r.LookupModel("nonexistent", "model")
	if ok {
		t.Error("Expected lookup to fail for nonexistent provider")
	}
}

func TestGetModelContextLimit(t *testing.T) {
	r := NewModelRegistry()

	// This will skip if the model doesn't exist
	limit := r.GetModelContextLimit("openai", "gpt-4")
	if limit == 0 {
		t.Skip("openai/gpt-4 not found or has no context limit")
	}
	if limit < 0 {
		t.Errorf("Expected positive context limit, got %d", limit)
	}
}

func TestGetProviderAPI(t *testing.T) {
	r := NewModelRegistry()

	// Test a known provider
	api := r.GetProviderAPI("openai")
	if api == "" {
		t.Skip("openai not found in generated registry")
	}
	// Just verify it's a non-empty string
	if len(api) == 0 {
		t.Error("Expected non-empty API URL")
	}
}

func TestListProviderModels(t *testing.T) {
	r := NewModelRegistry()

	// Test listing all models for a provider
	models := r.ListProviderModels("openai", false)
	if len(models) == 0 {
		t.Skip("openai not found or has no models")
	}

	// Test that toolCallOnly filter works
	toolCallModels := r.ListProviderModels("openai", true)
	if len(toolCallModels) > len(models) {
		t.Error("toolCallOnly filter should not return more models than unfiltered")
	}
}

func TestHasProvider(t *testing.T) {
	r := NewModelRegistry()

	// Test with a likely provider
	if !r.HasProvider("openai") {
		t.Skip("openai not found in generated registry")
	}

	// Test with non-existent provider
	if r.HasProvider("definitely-not-a-real-provider-123") {
		t.Error("Expected HasProvider to return false for non-existent provider")
	}
}

func TestMergeConfigProvidersInheritsGrokFamilyImageSupport(t *testing.T) {
	registry := NewModelRegistryWithConfig(&config.Config{
		Providers: map[string]*config.ProviderConfig{
			"xai": {
				CustomModels: []config.CustomModelConfig{{
					ID: "grok-4.6", Name: "Grok 4.6", ToolCall: true, Managed: true,
				}},
			},
		},
	})
	_, got, ok := registry.LookupModel("xai", "grok-4.6")
	if !ok || got == nil {
		t.Fatal("managed grok-4.6 was not merged")
	}
	if !got.Attachment || !got.SupportsImageInput() {
		t.Fatalf("grok-4.6 should inherit grok-4.5 image support: %#v", got)
	}
	if !got.Reasoning || got.Limit == nil || got.Limit.Context <= 0 {
		t.Fatalf("grok-4.6 should inherit grok-4.5 reasoning/context: %#v", got)
	}
}

func TestRelatedRegistryModelDoesNotMatchImagineIDs(t *testing.T) {
	provider := NewModelRegistry().GetProvider("xai")
	if RelatedRegistryModel(provider, "grok-imagine-image") != nil {
		t.Fatal("image-generation ids must not inherit chat-family metadata")
	}
	related := RelatedRegistryModel(provider, "grok-4.6")
	if related == nil || related.ID != "grok-4.5" {
		t.Fatalf("related grok-4.6 = %#v", related)
	}
}

func TestRelatedRegistryModelIgnoresLiveCustomSiblings(t *testing.T) {
	registry := NewModelRegistry()
	provider := registry.GetProvider("xai")
	if provider == nil {
		t.Fatal("xai missing")
	}
	provider.Models["grok-4.7"] = &RegistryModel{
		ID: "grok-4.7", Name: "Custom Grok 4.7", DefaultEnabled: true,
	}
	related := RelatedRegistryModel(provider, "grok-4.6")
	if related == nil || related.ID != "grok-4.5" {
		t.Fatalf("related grok-4.6 should stay on baked-in grok-4.5, got %#v", related)
	}
}

func TestRelatedBakedInModelPicksClosestEnabledSibling(t *testing.T) {
	models := map[string]*RegistryModel{
		"grok-4.1": {ID: "grok-4.1", DefaultEnabled: true},
		"grok-4.5": {ID: "grok-4.5", DefaultEnabled: true},
		"grok-4.8": {ID: "grok-4.8", DefaultEnabled: false},
	}
	got := relatedBakedInModel(models, "grok-4.6")
	if got == nil || got.ID != "grok-4.5" {
		t.Fatalf("closest sibling for grok-4.6 = %#v", got)
	}

	models["grok-4.5-preview"] = &RegistryModel{ID: "grok-4.5-preview", DefaultEnabled: true}
	got = relatedBakedInModel(models, "grok-4.5-new")
	if got == nil || got.ID != "grok-4.5" {
		t.Fatalf("tied versions should pick lexicographically first enabled id, got %#v", got)
	}
}

func TestMergeConfigProvidersCopiesModalitiesWhenLiveAttachmentIsSet(t *testing.T) {
	registry := NewModelRegistryWithConfig(&config.Config{
		Providers: map[string]*config.ProviderConfig{
			"xai": {
				CustomModels: []config.CustomModelConfig{{
					ID: "grok-4.6", Name: "Grok 4.6", ToolCall: true, Managed: true, Attachment: true,
				}},
			},
		},
	})
	_, got, ok := registry.LookupModel("xai", "grok-4.6")
	if !ok || got == nil {
		t.Fatal("managed grok-4.6 was not merged")
	}
	if !got.Attachment || !got.SupportsImageInput() {
		t.Fatalf("live attachment should still copy related modalities: %#v", got)
	}
}
