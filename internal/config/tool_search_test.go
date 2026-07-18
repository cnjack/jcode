package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func toolSearchBool(v bool) *bool {
	return &v
}

func TestToolSearchEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{name: "nil config", cfg: nil, want: false},
		{name: "absent block", cfg: &Config{}, want: false},
		{name: "nil enabled pointer", cfg: &Config{ToolSearch: &ToolSearchConfig{}}, want: false},
		{name: "explicit false", cfg: &Config{ToolSearch: &ToolSearchConfig{Enabled: toolSearchBool(false)}}, want: false},
		{name: "explicit true", cfg: &Config{ToolSearch: &ToolSearchConfig{Enabled: toolSearchBool(true)}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToolSearchEnabled(tt.cfg); got != tt.want {
				t.Fatalf("ToolSearchEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolSearchJSONUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "absent block", raw: `{}`, want: false},
		{name: "null block", raw: `{"tool_search":null}`, want: false},
		{name: "nil enabled pointer", raw: `{"tool_search":{}}`, want: false},
		{name: "explicit false", raw: `{"tool_search":{"enabled":false}}`, want: false},
		{name: "explicit true", raw: `{"tool_search":{"enabled":true}}`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			if err := json.Unmarshal([]byte(tt.raw), &cfg); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if got := ToolSearchEnabled(&cfg); got != tt.want {
				t.Fatalf("ToolSearchEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolSearchJSONSemanticRoundTrip(t *testing.T) {
	for _, want := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[want], func(t *testing.T) {
			original := Config{ToolSearch: &ToolSearchConfig{Enabled: toolSearchBool(want)}}
			data, err := json.Marshal(&original)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}

			var decoded Config
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if decoded.ToolSearch == nil || decoded.ToolSearch.Enabled == nil {
				t.Fatalf("explicit enabled=%v was not preserved: %s", want, data)
			}
			if got := ToolSearchEnabled(&decoded); got != want {
				t.Fatalf("round-trip ToolSearchEnabled() = %v, want %v", got, want)
			}
		})
	}
}

func TestLoadLegacyConfigWithoutToolSearchDefaultsDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jcode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `{
		"model":"openai/gpt-4o",
		"providers":{"openai":{"api_key":"sk-test"}}
	}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if ToolSearchEnabled(cfg) {
		t.Fatal("legacy config without tool_search unexpectedly enabled tool search")
	}
}
