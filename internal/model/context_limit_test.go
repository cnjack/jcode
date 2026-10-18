package model

import (
	"testing"

	"github.com/cnjack/jcode/internal/config"
)

func TestResolveContextLimit(t *testing.T) {
	reg := NewModelRegistry()

	tests := []struct {
		name     string
		cfg      *config.Config
		provider string
		model    string
		want     int
	}{
		{
			name:     "config override provider/model wins over registry",
			cfg:      &config.Config{ContextLimits: map[string]int{"minimax/MiniMax-M3": 2_000_000}},
			provider: "minimax", model: "MiniMax-M3", want: 2_000_000,
		},
		{
			name:     "config override bare model id",
			cfg:      &config.Config{ContextLimits: map[string]int{"my-model": 333_000}},
			provider: "custom", model: "my-model", want: 333_000,
		},
		{
			name:     "registry value used (MiniMax-M3 corrected to 1M)",
			cfg:      nil,
			provider: "minimax", model: "MiniMax-M3", want: 1_000_000,
		},
		{
			// models.dev ships 1048756 here; a model id drift would make the
			// override silently miss and hand back the inflated window.
			name:     "openrouter gemini transposed digits corrected to 2^20",
			cfg:      nil,
			provider: "openrouter", model: "google/gemini-3.1-pro-preview-customtools", want: 1_048_576,
		},
		{
			// models.dev ships 131702 here.
			name:     "openrouter qwen3-14b transposed digits corrected to 2^17",
			cfg:      nil,
			provider: "openrouter", model: "qwen/qwen3-14b", want: 131_072,
		},
		{
			name:     "knownModels fallback when registry misses",
			cfg:      nil,
			provider: "unknown-provider", model: "gpt-4o", want: 128_000,
		},
		{
			name:     "cfg.DefaultContextLimit used when everything else misses",
			cfg:      &config.Config{DefaultContextLimit: 512_000},
			provider: "nope", model: "totally-unknown-model", want: 512_000,
		},
		{
			name:     "hard fallback when nothing known",
			cfg:      nil,
			provider: "nope", model: "totally-unknown-model", want: DefaultContextLimitFallback,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveContextLimit(reg, tt.cfg, tt.provider, tt.model)
			if got != tt.want {
				t.Errorf("ResolveContextLimit(%q,%q) = %d, want %d", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

// TestEffectiveContextLimit verifies the output/summary headroom carved out of
// the raw window before threshold math: min(DefaultOutputReserveTokens,
// limit/4), non-positive limits passed through, and tiny windows never driven
// to a non-positive result.
func TestEffectiveContextLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "standard 200k window reserves the full 20k", limit: 200_000, want: 180_000},
		{name: "1M window reserves the full 20k", limit: 1_000_000, want: 980_000},
		{name: "32k window clamps reserve to a quarter (8k)", limit: 32_000, want: 24_000},
		{name: "16k window clamps reserve to a quarter (4k)", limit: 16_000, want: 12_000},
		{name: "zero limit passes through", limit: 0, want: 0},
		{name: "negative limit passes through", limit: -1, want: -1},
		{name: "limit of 1 keeps a positive window", limit: 1, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveContextLimit(tt.limit); got != tt.want {
				t.Errorf("EffectiveContextLimit(%d) = %d, want %d", tt.limit, got, tt.want)
			}
		})
	}
}

// TestMiniMaxM3ContextOverride guards the headline correction: MiniMax-M3's
// advertised window is 1M, while models.dev has at times recorded only the 512K
// guaranteed minimum.
func TestMiniMaxM3ContextOverride(t *testing.T) {
	reg := NewModelRegistry()
	for _, prov := range []string{"minimax", "minimax-coding-plan"} {
		if got := reg.GetModelContextLimit(prov, "MiniMax-M3"); got != 1_000_000 {
			t.Errorf("%s/MiniMax-M3 context = %d, want 1000000", prov, got)
		}
	}
}

// TestGLM53CatalogMetadata checks GLM-5.3 on the first-party catalogs that
// advertise it. GLM-5.2 stays selectable but is no longer starred.
func TestGLM53CatalogMetadata(t *testing.T) {
	reg := NewModelRegistry()
	for _, prov := range []string{"zhipuai", "zai", "zai-coding-plan", "zhipuai-coding-plan"} {
		_, m, ok := reg.LookupModel(prov, "glm-5.3")
		if !ok || m == nil {
			t.Errorf("%s/glm-5.3 not found", prov)
			continue
		}
		if got := reg.GetModelContextLimit(prov, "glm-5.3"); got != 1_000_000 {
			t.Errorf("%s/glm-5.3 context = %d, want 1000000", prov, got)
		}
		if !m.Recommended || !m.DefaultEnabled {
			t.Errorf("%s/glm-5.3 should be Recommended + DefaultEnabled", prov)
		}
	}
}

// TestRecommendedModelsResolve guards the silent-ignore hazard: a starred model
// ID that disappears from (or is renamed in) the registry after a models.dev
// refresh would quietly lose its Recommended/DefaultEnabled flags.
func TestRecommendedModelsResolve(t *testing.T) {
	reg := NewModelRegistry()
	for provID, models := range recommendedModels {
		for modelID := range models {
			_, m, ok := reg.LookupModel(provID, modelID)
			if !ok || m == nil {
				t.Errorf("recommended model %s/%s not found in registry", provID, modelID)
				continue
			}
			if !m.Recommended || !m.DefaultEnabled {
				t.Errorf("%s/%s should be Recommended + DefaultEnabled", provID, modelID)
			}
		}
	}
}

// TestRecommendedFlagshipsAreLongContext checks the recommended flagships exist in
// the registry and expose large windows, so the resolver doesn't throttle them.
func TestRecommendedFlagshipsAreLongContext(t *testing.T) {
	reg := NewModelRegistry()
	cases := []struct {
		provider, model string
		minContext      int
	}{
		{"minimax", "MiniMax-M3", 1_000_000},
		{"deepseek", "deepseek-v4-pro", 1_000_000},
		{"deepseek", "deepseek-flash", 1_000_000},
		{"alibaba-cn", "qwen3.8-max", 1_000_000},
		{"anthropic", "claude-opus-5-5", 1_000_000},
		{"openai", "gpt-6.1-sol", 1_000_000},
		{"google", "gemini-4-argon", 1_000_000},
		{"zhipuai", "glm-5.3", 1_000_000},
		{"zai", "glm-5.3", 1_000_000},
		{"moonshotai", "kimi-k3", 1_000_000},
		{"xai", "grok-4.7", 500_000},
	}
	for _, c := range cases {
		_, m, ok := reg.LookupModel(c.provider, c.model)
		if !ok || m == nil {
			t.Errorf("recommended model %s/%s not found in registry", c.provider, c.model)
			continue
		}
		if !m.Recommended {
			t.Errorf("%s/%s should be flagged Recommended", c.provider, c.model)
		}
		if got := reg.GetModelContextLimit(c.provider, c.model); got < c.minContext {
			t.Errorf("%s/%s context = %d, want >= %d", c.provider, c.model, got, c.minContext)
		}
	}
}
