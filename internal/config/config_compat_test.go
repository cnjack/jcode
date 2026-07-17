package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestComputerLegacyBackendMigration(t *testing.T) {
	for _, backend := range []string{"", "auto", " helper ", "AUTO"} {
		t.Run("safe_"+backend, func(t *testing.T) {
			c := ComputerConfig{
				Enabled: true, Backend: backend,
				Approval:        map[string]string{"interact": "always_allow"},
				AppPermissions:  []ComputerAppPermission{{BundleID: "com.apple.Notes", Interact: "allow"}},
				ClipboardRead:   true,
				ClipboardWrite:  true,
				SystemKeyCombos: true,
			}
			if rejected := c.MigrateLegacyBackend(); rejected != "" {
				t.Fatalf("safe backend %q was rejected as %q", backend, rejected)
			}
			if c.Backend != "" || !c.Enabled || len(c.Approval) != 1 || len(c.AppPermissions) != 1 ||
				!c.ClipboardRead || !c.ClipboardWrite || !c.SystemKeyCombos {
				t.Fatalf("safe migration changed policy: %+v", c)
			}
		})
	}

	for _, backend := range []string{"fake", "osa", "mystery", " FAKE "} {
		t.Run("rejected_"+backend, func(t *testing.T) {
			c := ComputerConfig{
				Enabled: true, Backend: backend,
				Approval:        map[string]string{"launch": "always_allow"},
				AppPermissions:  []ComputerAppPermission{{BundleID: "com.apple.Notes", Launch: "allow"}},
				ClipboardRead:   true,
				ClipboardWrite:  true,
				SystemKeyCombos: true,
			}
			if rejected := c.MigrateLegacyBackend(); rejected == "" {
				t.Fatalf("unsafe backend %q was accepted", backend)
			}
			if c.Backend != "" || c.Enabled || c.Approval != nil || c.AppPermissions != nil ||
				c.ClipboardRead || c.ClipboardWrite || c.SystemKeyCombos {
				t.Fatalf("unsafe migration did not fail closed: %+v", c)
			}
		})
	}
}

func TestLoadConfigFailsClosedForLegacyFakeComputerBackend(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jcode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `{
		"model":"openai/gpt-4o",
		"providers":{"openai":{"api_key":"sk-test"}},
		"computer":{
			"enabled":true,
			"backend":"fake",
			"approval":{"interact":"always_allow"},
			"app_permissions":[{"bundle_id":"com.apple.Notes","interact":"allow"}],
			"clipboard_read":true,
			"clipboard_write":true,
			"system_key_combos":true
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Computer == nil {
		t.Fatal("computer config disappeared during migration")
	}
	c := cfg.Computer
	if c.Backend != "" || c.Enabled || c.Approval != nil || c.AppPermissions != nil ||
		c.ClipboardRead || c.ClipboardWrite || c.SystemKeyCombos {
		t.Fatalf("LoadConfig did not fail closed: %+v", *c)
	}
}
