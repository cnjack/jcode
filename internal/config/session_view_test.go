package config

import (
	"encoding/json"
	"testing"
	"time"
)

func sessionViewBool(v bool) *bool { return &v }

func TestSessionViewEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{name: "nil config fails closed", cfg: nil, want: false},
		{name: "absent section defaults to enabled", cfg: &Config{}, want: true},
		{name: "nil enabled pointer defaults to enabled", cfg: &Config{SessionView: &SessionViewConfig{}}, want: true},
		{name: "explicit false disables", cfg: &Config{SessionView: &SessionViewConfig{Enabled: sessionViewBool(false)}}, want: false},
		{name: "explicit true enables", cfg: &Config{SessionView: &SessionViewConfig{Enabled: sessionViewBool(true)}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SessionViewEnabled(tt.cfg); got != tt.want {
				t.Fatalf("SessionViewEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionViewSettingsDefaults(t *testing.T) {
	// Defaults per design §5.8: 20 results, 4 KiB per entry, 16 KiB per call,
	// 3 s search budget.
	defaults := SessionViewResolved{
		Enabled:        true,
		MaxResults:     20,
		MaxEntryBytes:  4096,
		MaxOutputBytes: 16384,
		TimeBudget:     3 * time.Second,
	}
	disabledDefaults := defaults
	disabledDefaults.Enabled = false

	tests := []struct {
		name string
		cfg  *Config
		want SessionViewResolved
	}{
		{name: "nil config", cfg: nil, want: disabledDefaults},
		{name: "absent section", cfg: &Config{}, want: defaults},
		{name: "empty section", cfg: &Config{SessionView: &SessionViewConfig{}}, want: defaults},
		{
			name: "zero values fall back to defaults",
			cfg: &Config{SessionView: &SessionViewConfig{
				MaxResults: 0, MaxEntryBytes: 0, MaxOutputBytes: 0, TimeBudgetMs: 0,
			}},
			want: defaults,
		},
		{
			name: "negative values fall back to defaults",
			cfg: &Config{SessionView: &SessionViewConfig{
				MaxResults: -5, MaxEntryBytes: -1, MaxOutputBytes: -100, TimeBudgetMs: -1,
			}},
			want: defaults,
		},
		{
			name: "explicit values are honored",
			cfg: &Config{SessionView: &SessionViewConfig{
				MaxResults: 5, MaxEntryBytes: 1024, MaxOutputBytes: 8192, TimeBudgetMs: 750,
			}},
			want: SessionViewResolved{
				Enabled:        true,
				MaxResults:     5,
				MaxEntryBytes:  1024,
				MaxOutputBytes: 8192,
				TimeBudget:     750 * time.Millisecond,
			},
		},
		{
			name: "max_results is clamped to the hard cap",
			cfg:  &Config{SessionView: &SessionViewConfig{MaxResults: 5000}},
			want: SessionViewResolved{
				Enabled:        true,
				MaxResults:     200,
				MaxEntryBytes:  4096,
				MaxOutputBytes: 16384,
				TimeBudget:     3 * time.Second,
			},
		},
		{
			name: "max_results at the cap is preserved",
			cfg:  &Config{SessionView: &SessionViewConfig{MaxResults: 200}},
			want: SessionViewResolved{
				Enabled:        true,
				MaxResults:     200,
				MaxEntryBytes:  4096,
				MaxOutputBytes: 16384,
				TimeBudget:     3 * time.Second,
			},
		},
		{
			name: "disabled config still resolves budgets",
			cfg: &Config{SessionView: &SessionViewConfig{
				Enabled: sessionViewBool(false), TimeBudgetMs: 1000,
			}},
			want: SessionViewResolved{
				Enabled:        false,
				MaxResults:     20,
				MaxEntryBytes:  4096,
				MaxOutputBytes: 16384,
				TimeBudget:     time.Second,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SessionViewSettings(tt.cfg)
			if got != tt.want {
				t.Fatalf("SessionViewSettings() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSessionViewConfigJSON(t *testing.T) {
	t.Run("absent section keeps the feature enabled", func(t *testing.T) {
		var cfg Config
		if err := json.Unmarshal([]byte(`{"model":"openai/gpt-5"}`), &cfg); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if !SessionViewEnabled(&cfg) {
			t.Fatal("absent session_view section must default to enabled")
		}
	})

	t.Run("explicit false survives unmarshal", func(t *testing.T) {
		var cfg Config
		if err := json.Unmarshal([]byte(`{"session_view":{"enabled":false}}`), &cfg); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if SessionViewEnabled(&cfg) {
			t.Fatal("explicit enabled:false must disable the feature")
		}
	})

	t.Run("unknown fields are tolerated", func(t *testing.T) {
		raw := `{
			"session_view": {
				"enabled": true,
				"max_results": 7,
				"max_entry_bytes": 2048,
				"max_output_bytes": 4096,
				"time_budget_ms": 1500,
				"future_option": {"nested": [1, 2, 3]},
				"another_unknown": "ignored"
			},
			"unknown_top_level": 42
		}`
		var cfg Config
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatalf("unknown fields must be ignored, got %v", err)
		}
		got := SessionViewSettings(&cfg)
		want := SessionViewResolved{
			Enabled:        true,
			MaxResults:     7,
			MaxEntryBytes:  2048,
			MaxOutputBytes: 4096,
			TimeBudget:     1500 * time.Millisecond,
		}
		if got != want {
			t.Fatalf("SessionViewSettings() = %+v, want %+v", got, want)
		}
	})

	t.Run("explicit false survives a marshal round-trip", func(t *testing.T) {
		original := Config{SessionView: &SessionViewConfig{Enabled: sessionViewBool(false)}}
		data, err := json.Marshal(&original)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		var decoded Config
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if decoded.SessionView == nil || decoded.SessionView.Enabled == nil {
			t.Fatalf("explicit enabled=false was not preserved: %s", data)
		}
		if SessionViewEnabled(&decoded) {
			t.Fatal("round-trip lost the disabled state")
		}
	})
}
