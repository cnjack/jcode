package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyEnvOverlay(t *testing.T) {
	// Isolate from the real HOME so config.ConfigDir() resolves to a temp dir.
	t.Setenv("HOME", t.TempDir())

	t.Run("nil config is a no-op", func(t *testing.T) {
		ApplyEnvOverlay(nil) // must not panic
	})

	t.Run("no env vars leaves config unchanged", func(t *testing.T) {
		// Clear all JCODE_* env vars for this test.
		for _, key := range []string{EnvModel, EnvSmallModel, EnvMaxIterations, EnvTheme, EnvLanguage, EnvDefaultMode} {
			t.Setenv(key, "")
		}
		cfg := &Config{Model: "openai/gpt-4o", SmallModel: "openai/gpt-4o-mini", MaxIterations: 500, Theme: "nord-dark", Language: "zh", DefaultMode: "approval"}
		ApplyEnvOverlay(cfg)
		if cfg.Model != "openai/gpt-4o" {
			t.Errorf("Model = %q, want %q", cfg.Model, "openai/gpt-4o")
		}
		if cfg.SmallModel != "openai/gpt-4o-mini" {
			t.Errorf("SmallModel = %q, want %q", cfg.SmallModel, "openai/gpt-4o-mini")
		}
		if cfg.MaxIterations != 500 {
			t.Errorf("MaxIterations = %d, want 500", cfg.MaxIterations)
		}
		if cfg.Theme != "nord-dark" {
			t.Errorf("Theme = %q, want %q", cfg.Theme, "nord-dark")
		}
		if cfg.Language != "zh" {
			t.Errorf("Language = %q, want %q", cfg.Language, "zh")
		}
		if cfg.DefaultMode != "approval" {
			t.Errorf("DefaultMode = %q, want %q", cfg.DefaultMode, "approval")
		}
	})

	t.Run("overrides all supported fields", func(t *testing.T) {
		t.Setenv(EnvModel, "anthropic/claude-sonnet-4-20250514")
		t.Setenv(EnvSmallModel, "anthropic/claude-haiku")
		t.Setenv(EnvMaxIterations, "42")
		t.Setenv(EnvTheme, "github-light")
		t.Setenv(EnvLanguage, "en")
		t.Setenv(EnvDefaultMode, "full_access")

		cfg := &Config{Model: "openai/gpt-4o", MaxIterations: 1000}
		ApplyEnvOverlay(cfg)

		if cfg.Model != "anthropic/claude-sonnet-4-20250514" {
			t.Errorf("Model = %q, want anthropic/claude-sonnet-4-20250514", cfg.Model)
		}
		if cfg.SmallModel != "anthropic/claude-haiku" {
			t.Errorf("SmallModel = %q, want anthropic/claude-haiku", cfg.SmallModel)
		}
		if cfg.MaxIterations != 42 {
			t.Errorf("MaxIterations = %d, want 42", cfg.MaxIterations)
		}
		if cfg.Theme != "github-light" {
			t.Errorf("Theme = %q, want github-light", cfg.Theme)
		}
		if cfg.Language != "en" {
			t.Errorf("Language = %q, want en", cfg.Language)
		}
		if cfg.DefaultMode != "full_access" {
			t.Errorf("DefaultMode = %q, want full_access", cfg.DefaultMode)
		}
	})

	t.Run("invalid max iterations is ignored", func(t *testing.T) {
		t.Setenv(EnvMaxIterations, "not-a-number")
		cfg := &Config{MaxIterations: 1000}
		ApplyEnvOverlay(cfg)
		if cfg.MaxIterations != 1000 {
			t.Errorf("MaxIterations = %d, want 1000 (unchanged)", cfg.MaxIterations)
		}
	})

	t.Run("negative max iterations is ignored", func(t *testing.T) {
		t.Setenv(EnvMaxIterations, "-5")
		cfg := &Config{MaxIterations: 1000}
		ApplyEnvOverlay(cfg)
		if cfg.MaxIterations != 1000 {
			t.Errorf("MaxIterations = %d, want 1000 (unchanged)", cfg.MaxIterations)
		}
	})

	t.Run("zero max iterations is ignored", func(t *testing.T) {
		t.Setenv(EnvMaxIterations, "0")
		cfg := &Config{MaxIterations: 1000}
		ApplyEnvOverlay(cfg)
		if cfg.MaxIterations != 1000 {
			t.Errorf("MaxIterations = %d, want 1000 (unchanged)", cfg.MaxIterations)
		}
	})

	t.Run("partial override preserves other fields", func(t *testing.T) {
		// Clear all then set only model.
		for _, key := range []string{EnvSmallModel, EnvMaxIterations, EnvTheme, EnvLanguage, EnvDefaultMode} {
			t.Setenv(key, "")
		}
		t.Setenv(EnvModel, "openai/o3")

		cfg := &Config{Model: "openai/gpt-4o", Theme: "nord-dark", Language: "zh", MaxIterations: 200}
		ApplyEnvOverlay(cfg)

		if cfg.Model != "openai/o3" {
			t.Errorf("Model = %q, want openai/o3", cfg.Model)
		}
		if cfg.Theme != "nord-dark" {
			t.Errorf("Theme = %q, want nord-dark (unchanged)", cfg.Theme)
		}
		if cfg.Language != "zh" {
			t.Errorf("Language = %q, want zh (unchanged)", cfg.Language)
		}
		if cfg.MaxIterations != 200 {
			t.Errorf("MaxIterations = %d, want 200 (unchanged)", cfg.MaxIterations)
		}
	})
	t.Run("invalid default mode is ignored", func(t *testing.T) {
		t.Setenv(EnvDefaultMode, "garbage")
		cfg := &Config{DefaultMode: "approval"}
		ApplyEnvOverlay(cfg)
		if cfg.DefaultMode != "approval" {
			t.Errorf("DefaultMode = %q, want approval (unchanged for invalid input)", cfg.DefaultMode)
		}
	})

	t.Run("valid default mode is accepted", func(t *testing.T) {
		for _, mode := range []string{"approval", "plan", "auto", "full_access"} {
			t.Setenv(EnvDefaultMode, mode)
			cfg := &Config{DefaultMode: "approval"}
			ApplyEnvOverlay(cfg)
			if cfg.DefaultMode != mode {
				t.Errorf("DefaultMode = %q, want %q", cfg.DefaultMode, mode)
			}
		}
	})
}

func TestConfigFilePathEnvOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	t.Run("JCODE_CONFIG overrides path", func(t *testing.T) {
		custom := filepath.Join(t.TempDir(), "custom-config.json")
		t.Setenv(EnvConfigFile, custom)

		p, err := configFilePath()
		if err != nil {
			t.Fatalf("configFilePath() error: %v", err)
		}
		if p != custom {
			t.Errorf("configFilePath() = %q, want %q", p, custom)
		}
	})

	t.Run("empty JCODE_CONFIG falls back to default", func(t *testing.T) {
		t.Setenv(EnvConfigFile, "")

		p, err := configFilePath()
		if err != nil {
			t.Fatalf("configFilePath() error: %v", err)
		}
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".jcode", "config.json")
		if p != want {
			t.Errorf("configFilePath() = %q, want %q", p, want)
		}
	})
}

func TestEnvOverlayPrecedenceOverProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Simulate: global config has model A, project sets model B, env sets model C.
	// Env must win.
	cfg := &Config{Model: "openai/gpt-4o", MaxIterations: 1000}

	// Simulate project overlay (model B).
	projectOverlay := &Config{Model: "anthropic/claude-sonnet-4-20250514"}
	MergeProjectConfig(cfg, projectOverlay)
	if cfg.Model != "anthropic/claude-sonnet-4-20250514" {
		t.Fatalf("project overlay failed: Model = %q", cfg.Model)
	}

	// Now env overlay (model C) must win.
	t.Setenv(EnvModel, "openai/o3")
	ApplyEnvOverlay(cfg)
	if cfg.Model != "openai/o3" {
		t.Errorf("Model = %q, want openai/o3 (env must override project)", cfg.Model)
	}
}
