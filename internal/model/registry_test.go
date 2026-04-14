package model

import (
	"testing"
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
