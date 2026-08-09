package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImageConfigRoundTripAndFailClosedDefaults(t *testing.T) {
	raw := `{
		"model":"kimi-for-coding/k3",
		"image_model":"custom/canvas-v1",
		"providers":{"custom":{
			"api_key":"secret",
			"protocol":"responses",
			"provider_tools":{"image_generation":{"enabled":true,"max_calls_per_turn":2,"max_calls_per_session":7}},
			"image_endpoint":{"protocol":"openai_images","base_url":"https://images.example.test/v1","models":[{"id":"canvas-v1","name":"Canvas","sizes":["1024x1024"]}],"asset_hosts":["cdn.example.test"]}
		}},
		"media":{"retention_days":30,"max_total_bytes":2147483648}
	}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	pc := cfg.Providers["custom"]
	if pc == nil || pc.ImageEndpoint == nil || len(pc.ImageEndpoint.Models) != 1 {
		t.Fatalf("image endpoint did not load: %+v", pc)
	}
	if !pc.ProviderTools["image_generation"].Enabled {
		t.Fatal("explicit image_generation policy did not load")
	}
	if cfg.ImageModel != "custom/canvas-v1" || cfg.Media == nil || cfg.Media.RetentionDays != 30 {
		t.Fatalf("global image settings did not load: %+v", cfg)
	}

	encoded, err := json.Marshal(&cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var roundTrip Config
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("round-trip json.Unmarshal: %v", err)
	}
	if got := roundTrip.Providers["custom"].ImageEndpoint.Models[0].Sizes; len(got) != 1 || got[0] != "1024x1024" {
		t.Fatalf("round-trip image sizes = %v", got)
	}

	var zero Config
	zeroPC := &ProviderConfig{}
	if zero.ImageModel != "" || zero.Media != nil || zeroPC.ProviderTools != nil || zeroPC.ImageEndpoint != nil {
		t.Fatalf("zero-value image config must be unavailable and disabled: cfg=%+v provider=%+v", zero, zeroPC)
	}
}

func TestGenericMCPServerDropsCredentialRef(t *testing.T) {
	var srv MCPServer
	if err := json.Unmarshal([]byte(`{"type":"http","url":"https://mcp.example.test","credential_ref":"provider-secret"}`), &srv); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	encoded, err := json.Marshal(&srv)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(encoded), "credential_ref") || strings.Contains(string(encoded), "provider-secret") {
		t.Fatalf("generic MCP config persisted credential_ref: %s", encoded)
	}
}
