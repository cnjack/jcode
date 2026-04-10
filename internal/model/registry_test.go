package model

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestModelRegistry_ReadWriteCache(t *testing.T) {
	tmpDir := t.TempDir()
	r := &ModelRegistry{
		cacheDir: tmpDir,
	}

	providers := map[string]*RegistryProvider{
		"openai": {
			ID:   "openai",
			Name: "OpenAI",
			API:  "https://api.openai.com/v1",
			Env:  []string{"OPENAI_API_KEY"},
			Models: map[string]*RegistryModel{
				"gpt-4o": {
					ID:       "gpt-4o",
					Name:     "GPT-4o",
					Family:   "gpt",
					ToolCall: true,
					Cost:     &ModelCost{Input: 2.5, Output: 10},
					Limit:    &ModelLimit{Context: 128000, Output: 16384},
				},
			},
		},
	}

	// Write
	if err := r.writeCache(providers); err != nil {
		t.Fatalf("writeCache: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filepath.Join(tmpDir, registryCacheFile)); err != nil {
		t.Fatalf("cache file not found: %v", err)
	}

	// Read back
	got, err := r.readCache()
	if err != nil {
		t.Fatalf("readCache: %v", err)
	}

	prov, ok := got["openai"]
	if !ok {
		t.Fatal("openai provider not found in cache")
	}
	if prov.Name != "OpenAI" {
		t.Errorf("provider name = %q, want %q", prov.Name, "OpenAI")
	}
	m, ok := prov.Models["gpt-4o"]
	if !ok {
		t.Fatal("gpt-4o model not found")
	}
	if m.Limit.Context != 128000 {
		t.Errorf("context limit = %d, want 128000", m.Limit.Context)
	}
	if m.Cost.Input != 2.5 {
		t.Errorf("cost input = %f, want 2.5", m.Cost.Input)
	}
}

func TestModelRegistry_LookupModel(t *testing.T) {
	r := &ModelRegistry{
		providers: map[string]*RegistryProvider{
			"anthropic": {
				ID:   "anthropic",
				Name: "Anthropic",
				API:  "https://api.anthropic.com/v1",
				Models: map[string]*RegistryModel{
					"claude-sonnet-4": {
						ID:       "claude-sonnet-4",
						Name:     "Claude Sonnet 4",
						ToolCall: true,
						Limit:    &ModelLimit{Context: 200000, Output: 64000},
						Cost:     &ModelCost{Input: 3, Output: 15},
					},
				},
			},
		},
		loadedAt: time.Now(),
	}

	// Exact match
	prov, model, ok := r.LookupModel("anthropic", "claude-sonnet-4")
	if !ok {
		t.Fatal("expected to find claude-sonnet-4")
	}
	if prov.ID != "anthropic" {
		t.Errorf("provider id = %q, want anthropic", prov.ID)
	}
	if model.Limit.Context != 200000 {
		t.Errorf("context = %d, want 200000", model.Limit.Context)
	}

	// Missing provider
	_, _, ok = r.LookupModel("missing", "gpt-4o")
	if ok {
		t.Error("expected not found for missing provider")
	}

	// Missing model
	_, _, ok = r.LookupModel("anthropic", "missing-model")
	if ok {
		t.Error("expected not found for missing model")
	}
}

func TestModelRegistry_GetModelContextLimit(t *testing.T) {
	r := &ModelRegistry{
		providers: map[string]*RegistryProvider{
			"openai": {
				ID: "openai",
				Models: map[string]*RegistryModel{
					"gpt-4o": {
						ID:    "gpt-4o",
						Limit: &ModelLimit{Context: 128000, Output: 16384},
					},
				},
			},
		},
		loadedAt: time.Now(),
	}

	if got := r.GetModelContextLimit("openai", "gpt-4o"); got != 128000 {
		t.Errorf("context limit = %d, want 128000", got)
	}
	if got := r.GetModelContextLimit("openai", "missing"); got != 0 {
		t.Errorf("context limit = %d, want 0", got)
	}
}

func TestModelRegistry_GetModelCost(t *testing.T) {
	r := &ModelRegistry{
		providers: map[string]*RegistryProvider{
			"deepseek": {
				ID: "deepseek",
				Models: map[string]*RegistryModel{
					"deepseek-chat": {
						ID:   "deepseek-chat",
						Cost: &ModelCost{Input: 0.28, Output: 0.42},
					},
				},
			},
		},
		loadedAt: time.Now(),
	}

	inCost, outCost := r.GetModelCost("deepseek", "deepseek-chat")
	if inCost != 0.28 || outCost != 0.42 {
		t.Errorf("cost = (%f, %f), want (0.28, 0.42)", inCost, outCost)
	}

	inCost, outCost = r.GetModelCost("deepseek", "missing")
	if inCost != 0 || outCost != 0 {
		t.Errorf("missing model cost = (%f, %f), want (0, 0)", inCost, outCost)
	}
}
