package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProviderAuthBindingRoundTripContainsNoCredential(t *testing.T) {
	original := ProviderConfig{
		Auth: &ProviderAuthBinding{
			Method:    "codex_oauth",
			AccountID: "acct_123",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal provider config: %v", err)
	}
	serialized := string(data)
	if strings.Contains(serialized, "token") || strings.Contains(serialized, "secret") {
		t.Fatalf("provider auth binding serialized credential-shaped data: %s", serialized)
	}

	var restored ProviderConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal provider config: %v", err)
	}
	if restored.Auth == nil || restored.Auth.Method != "codex_oauth" || restored.Auth.AccountID != "acct_123" {
		t.Fatalf("restored auth binding = %#v", restored.Auth)
	}
}

func TestLegacyProviderConfigHasNoManagedAuth(t *testing.T) {
	var provider ProviderConfig
	if err := json.Unmarshal([]byte(`{"api_key":"legacy-key"}`), &provider); err != nil {
		t.Fatalf("unmarshal legacy provider: %v", err)
	}
	if provider.Auth != nil || provider.APIKey != "legacy-key" {
		t.Fatalf("legacy provider changed: %#v", provider)
	}
}
