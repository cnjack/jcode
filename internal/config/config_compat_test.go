package config

import (
	"encoding/json"
	"testing"
)

// TestRemovedKeysStillLoad locks backward compatibility for config files
// written before the dead fields were removed: "fallback_model" (never
// consumed) and "compaction.summary_model" (parsed but never honored) must be
// silently ignored, not break loading.
func TestRemovedKeysStillLoad(t *testing.T) {
	raw := `{
		"model": "openai/gpt-4o",
		"small_model": "openai/gpt-4o-mini",
		"fallback_model": "anthropic/claude-3-5-sonnet",
		"compaction": {"enabled": true, "summary_model": "openai/gpt-4o-mini"},
		"providers": {"openai": {"api_key": "sk-test"}}
	}`
	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("legacy config must still load: %v", err)
	}
	if c.Model != "openai/gpt-4o" {
		t.Errorf("model: %q", c.Model)
	}
	if c.SmallModel != "openai/gpt-4o-mini" {
		t.Errorf("small_model: %q", c.SmallModel)
	}
	if c.Compaction == nil || !c.Compaction.Enabled {
		t.Error("compaction settings around the removed key must survive")
	}
}
