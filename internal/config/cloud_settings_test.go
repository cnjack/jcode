package config

import (
	"encoding/json"
	"testing"
)

func TestCloudSettingsDefaults(t *testing.T) {
	c := &Config{}
	if got := c.CloudSettings(); got.Enabled || got.URL != "" || got.AutoConnect != nil {
		t.Fatalf("CloudSettings() on absent block = %+v, want zero value", got)
	}
	if !CloudAutoConnect(c) {
		t.Fatal("CloudAutoConnect() on absent block = false, want default true")
	}
	if !CloudAutoConnect(nil) {
		t.Fatal("CloudAutoConnect(nil) = false, want default true")
	}
}

func TestSetCloudSnapshotAndRoundTrip(t *testing.T) {
	auto := false
	c := &Config{}
	c.SetCloud(&CloudConfig{Enabled: true, URL: "https://cloud.j-code.net", AutoConnect: &auto})

	got := c.CloudSettings()
	if !got.Enabled || got.URL != "https://cloud.j-code.net" || got.AutoConnect == nil || *got.AutoConnect {
		t.Fatalf("CloudSettings() = %+v", got)
	}
	if CloudAutoConnect(c) {
		t.Fatal("CloudAutoConnect() = true, want explicit false")
	}

	// Mutating the returned snapshot must not leak into the live config.
	got.URL = "https://evil.example.com"
	*got.AutoConnect = true
	if again := c.CloudSettings(); again.URL != "https://cloud.j-code.net" || *again.AutoConnect {
		t.Fatalf("snapshot mutation leaked into live config: %+v", again)
	}

	// JSON round-trip uses the documented snake_case keys.
	data, err := json.Marshal(c.CloudSettings())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["enabled"] != true || raw["url"] != "https://cloud.j-code.net" || raw["auto_connect"] != false {
		t.Fatalf("marshaled cloud block = %v", raw)
	}
}

func TestSetCloudNilRemovesBlock(t *testing.T) {
	c := &Config{}
	c.SetCloud(&CloudConfig{Enabled: true, URL: "https://cloud.j-code.net"})
	c.SetCloud(nil)
	if c.Cloud != nil {
		t.Fatalf("SetCloud(nil) left block: %+v", c.Cloud)
	}
	if got := c.CloudSettings(); got.Enabled || got.URL != "" {
		t.Fatalf("CloudSettings() after removal = %+v", got)
	}
}
