package cloud

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

func TestInitialProviderSyncConflictRejectsSecretOverwrite(t *testing.T) {
	local := &config.ProviderConfig{
		APIKey:  "local-secret",
		BaseURL: "https://local.example/v1",
	}
	state := &config.ModelState{
		Favorite: []config.ModelRef{{Provider: "zhipuai", Model: "glm-5.2"}},
	}
	remote := &syncedProvider{
		SchemaVersion: providerSyncSchemaVersion,
		ProviderID:    "zhipuai",
		Config: &config.ProviderConfig{
			APIKey:  "remote-secret",
			BaseURL: "https://remote.example/v1",
		},
		UpdatedAt: time.Now().UTC(),
	}
	if !initialProviderSyncConflict(local, state, "zhipuai", remote) {
		t.Fatal("different local secret was accepted as the initial Cloud replica")
	}
	remote.Deleted = true
	remote.Config = nil
	if !initialProviderSyncConflict(local, state, "zhipuai", remote) {
		t.Fatal("Cloud tombstone was allowed to delete an untracked local provider")
	}
}

func TestInitialProviderSyncAcceptsEquivalentReplica(t *testing.T) {
	local := &config.ProviderConfig{
		APIKey:  "same-secret",
		BaseURL: "https://api.example/v1",
		Headers: map[string]string{"X-Tenant": "alpha"},
	}
	state := &config.ModelState{
		EnabledModels: []config.ModelRef{{Provider: "custom", Model: "coder"}},
		EffortOverrides: map[string]string{
			"custom/coder": "high",
		},
	}
	remote := &syncedProvider{
		SchemaVersion: providerSyncSchemaVersion,
		ProviderID:    "custom",
		Config:        cloneProviderConfig(local),
		ModelState:    providerModelStateSnapshot(state, "custom"),
		UpdatedAt:     time.Now().UTC(),
	}
	if initialProviderSyncConflict(local, state, "custom", remote) {
		t.Fatal("equivalent initial replicas were reported as conflicting")
	}
}

func TestProviderSyncConflictIsClassifiable(t *testing.T) {
	err := fmt.Errorf("%w: provider %q", ErrProviderSyncConflict, "custom")
	if !errors.Is(err, ErrProviderSyncConflict) {
		t.Fatalf("errors.Is(%v, ErrProviderSyncConflict) = false", err)
	}
}
