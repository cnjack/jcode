package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigMutationHelperProcess(t *testing.T) {
	if os.Getenv("JCODE_CONFIG_MUTATION_HELPER") != "1" {
		return
	}
	action := os.Getenv("JCODE_CONFIG_MUTATION_ACTION")
	_, err := MutateConfig(func(cfg *Config) error {
		switch action {
		case "provider":
			provider := cfg.Providers["custom"]
			provider.APIKey = "rotated-key"
			provider.ProviderTools[toolImageGenerationTestID] = ProviderToolPolicy{Enabled: false}
			if marker := os.Getenv("JCODE_CONFIG_MUTATION_MARKER"); marker != "" {
				if err := os.WriteFile(marker, []byte("locked"), 0o600); err != nil {
					return err
				}
			}
			time.Sleep(350 * time.Millisecond)
		case "image":
			cfg.ImageModel = "custom/canvas-2"
		default:
			t.Fatalf("unknown helper action %q", action)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

const toolImageGenerationTestID = "image_generation"

func TestMutateConfigSerializesReloadAndSaveAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	marker := filepath.Join(dir, "first-transaction-entered")
	t.Setenv(EnvConfigFile, path)
	initial := &Config{
		Model: "custom/chat", ImageModel: "custom/canvas-1",
		Providers: map[string]*ProviderConfig{
			"custom": {
				APIKey: "old-key",
				ProviderTools: map[string]ProviderToolPolicy{
					toolImageGenerationTestID: {Enabled: true},
				},
			},
		},
	}
	if err := SaveConfig(initial); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, firstOutput := configMutationHelperCommand(ctx, path, "provider", marker)
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first helper did not enter mutation: %s", firstOutput.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	second, secondOutput := configMutationHelperCommand(ctx, path, "image", "")
	if err := second.Run(); err != nil {
		t.Fatalf("second helper: %v\n%s", err, secondOutput.String())
	}
	if err := first.Wait(); err != nil {
		t.Fatalf("first helper: %v\n%s", err, firstOutput.String())
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	provider := loaded.Providers["custom"]
	if provider.APIKey != "rotated-key" || provider.ProviderTools[toolImageGenerationTestID].Enabled {
		t.Fatalf("provider mutation was lost: %#v", provider)
	}
	if loaded.ImageModel != "custom/canvas-2" {
		t.Fatalf("image mutation was lost: %q", loaded.ImageModel)
	}
}

func configMutationHelperCommand(
	ctx context.Context,
	configPath, action, marker string,
) (*exec.Cmd, *bytes.Buffer) {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConfigMutationHelperProcess$")
	cmd.Env = append(os.Environ(),
		"JCODE_CONFIG_MUTATION_HELPER=1",
		"JCODE_CONFIG_MUTATION_ACTION="+action,
		"JCODE_CONFIG_MUTATION_MARKER="+marker,
		EnvConfigFile+"="+configPath,
	)
	output := new(bytes.Buffer)
	cmd.Stdout = output
	cmd.Stderr = output
	return cmd, output
}

func TestMutateConfigOrCreateDoesNotReplaceMalformedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfigFile, path)
	original := []byte(`{"providers":`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := MutateConfigOrCreate(func(cfg *Config) error {
		cfg.Model = "should/not-save"
		return nil
	}); err == nil {
		t.Fatal("malformed config was silently replaced")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("malformed config changed: got=%q want=%q", got, original)
	}
}

func TestSaveConfigRejectsStaleSnapshotThatWouldReviveProviderState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfigFile, path)
	initial := &Config{
		Model: "custom/chat", ImageModel: "custom/canvas-1",
		Providers: map[string]*ProviderConfig{
			"custom": {
				APIKey: "old-key",
				ProviderTools: map[string]ProviderToolPolicy{
					toolImageGenerationTestID: {Enabled: true},
				},
			},
		},
	}
	if err := SaveConfig(initial); err != nil {
		t.Fatal(err)
	}
	providerWriter, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	unrelatedWriter, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}

	providerWriter.Providers["custom"].APIKey = "rotated-key"
	providerWriter.Providers["custom"].ProviderTools[toolImageGenerationTestID] = ProviderToolPolicy{Enabled: false}
	if err := SaveConfig(providerWriter); err != nil {
		t.Fatal(err)
	}
	// Process B changes an unrelated setting on its old full snapshot. The CAS
	// must reject the whole write instead of restoring old-key/enabled=true.
	unrelatedWriter.Theme = "dark"
	if err := SaveConfig(unrelatedWriter); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("stale SaveConfig error = %v, want ErrConfigConflict", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	provider := loaded.Providers["custom"]
	if provider.APIKey != "rotated-key" || provider.ProviderTools[toolImageGenerationTestID].Enabled {
		t.Fatalf("stale unrelated write revived provider state: %#v", provider)
	}
	if loaded.Theme == "dark" {
		t.Fatal("conflicting stale mutation was partially persisted")
	}
}
