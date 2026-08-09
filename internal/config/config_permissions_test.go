package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestSaveConfigRenameFailurePreservesExistingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, configDir)
	path := filepath.Join(dir, configFile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  \"model\": \"old/model\"\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	renameErr := errors.New("injected rename failure")
	err := saveConfig(&Config{Model: "new/model"}, func(oldPath, newPath string) error {
		if filepath.Dir(oldPath) != dir || newPath != path {
			t.Fatalf("rename(%q, %q), want same-directory temp -> %q", oldPath, newPath, path)
		}
		assertConfigPermission(t, oldPath, 0o600)
		data, readErr := os.ReadFile(oldPath)
		if readErr != nil {
			t.Fatalf("read temporary config: %v", readErr)
		}
		if !strings.Contains(string(data), `"model": "new/model"`) {
			t.Fatalf("temporary config did not contain new data: %s", data)
		}
		return renameErr
	})
	if !errors.Is(err, renameErr) {
		t.Fatalf("SaveConfig() error = %v, want injected rename error", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original config after failed save: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("failed save changed original config:\n got: %q\nwant: %q", got, original)
	}
	assertConfigPermission(t, path, 0o600)
	temps, err := filepath.Glob(filepath.Join(dir, "."+configFile+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) != 0 {
		t.Fatalf("failed save left temporary files: %v", temps)
	}
}

func TestSaveConfigRequiresDirectoryDurabilityBeforePublishingRevision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, configDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{Model: "new/model"}
	syncErr := errors.New("injected directory sync failure")
	err := saveConfigWithSync(cfg, os.Rename, func(path string) error {
		if path != dir {
			t.Fatalf("sync path = %q, want %q", path, dir)
		}
		return syncErr
	})
	if !errors.Is(err, syncErr) {
		t.Fatalf("saveConfigWithSync() error = %v, want directory sync failure", err)
	}
	if cfg.diskRevision != "" {
		t.Fatalf("failed durability barrier published disk revision %q", cfg.diskRevision)
	}
	data, readErr := os.ReadFile(filepath.Join(dir, configFile))
	if readErr != nil {
		t.Fatalf("read renamed config: %v", readErr)
	}
	if !strings.Contains(string(data), `"model": "new/model"`) {
		t.Fatalf("rename did not commit new config before durability failure: %s", data)
	}
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
