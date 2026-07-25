package config

import (
	"os"
	"strconv"
)

// Environment variable names for configuration overrides. These provide the
// highest-precedence config layer — above both global (~/.jcode/config.json)
// and project-level (.jcode/config.json) settings.
const (
	// EnvModel overrides the active model ("provider/model" format).
	EnvModel = "JCODE_MODEL"
	// EnvSmallModel overrides the small/fast model ("provider/model" format).
	EnvSmallModel = "JCODE_SMALL_MODEL"
	// EnvMaxIterations overrides the agent iteration cap.
	EnvMaxIterations = "JCODE_MAX_ITERATIONS"
	// EnvTheme overrides the color theme name.
	EnvTheme = "JCODE_THEME"
	// EnvLanguage overrides the UI locale.
	EnvLanguage = "JCODE_LANGUAGE"
	// EnvDefaultMode overrides the session mode ("approval", "plan", "full_access").
	EnvDefaultMode = "JCODE_DEFAULT_MODE"
	// EnvConfigFile overrides the config file path (default ~/.jcode/config.json).
	EnvConfigFile = "JCODE_CONFIG"
)

// ApplyEnvOverlay applies environment variable overrides to cfg. Environment
// variables have the highest precedence — they override both global and
// project-level config values. This is useful for CI/CD pipelines, containerized
// deployments, and quick one-off overrides without editing config files.
//
// Unlike project config, env vars MAY override DefaultMode because they are set
// by the user (or their CI system) directly, not by an untrusted repository.
func ApplyEnvOverlay(cfg *Config) {
	if cfg == nil {
		return
	}
	if v := os.Getenv(EnvModel); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv(EnvSmallModel); v != "" {
		cfg.SmallModel = v
	}
	if v := os.Getenv(EnvMaxIterations); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxIterations = n
		} else {
			Logger().Printf("[config] ignoring invalid %s=%q (must be a positive integer)", EnvMaxIterations, v)
		}
	}
	if v := os.Getenv(EnvTheme); v != "" {
		cfg.Theme = v
	}
	if v := os.Getenv(EnvLanguage); v != "" {
		cfg.Language = v
	}
	if v := os.Getenv(EnvDefaultMode); v != "" {
		switch v {
		case "approval", "plan", "auto", "full_access":
			cfg.DefaultMode = v
		default:
			Logger().Printf("[config] ignoring invalid %s=%q (must be one of: approval, plan, auto, full_access)", EnvDefaultMode, v)
		}
	}
}
