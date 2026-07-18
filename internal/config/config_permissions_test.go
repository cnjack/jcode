package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveConfigCreatesOwnerOnlyArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &Config{
		Model: "provider/model",
		Providers: map[string]*ProviderConfig{
			"provider": {APIKey: "credential-canary"},
		},
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	assertConfigPermission(t, filepath.Join(home, configDir), 0o700)
	assertConfigPermission(t, filepath.Join(home, configDir, configFile), 0o600)
}

func TestSaveConfigTightensLegacyPermissiveArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, configDir)
	path := filepath.Join(dir, configFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"providers":{"old":{"api_key":"old-secret"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SaveConfig(&Config{Model: "provider/model"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	assertConfigPermission(t, dir, 0o700)
	assertConfigPermission(t, path, 0o600)
}

func assertConfigPermission(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permission %s = %#o, want %#o", path, got, want)
	}
}
