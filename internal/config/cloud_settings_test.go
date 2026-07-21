package config

import (
	"encoding/json"
	"testing"
)

func TestCloudSettingsDefaults(t *testing.T) {
	c := &Config{}
	if got := c.CloudSettings(); got.Enabled || got.URL != "" || got.AutoConnect != nil || got.E2EE != nil {
		t.Fatalf("CloudSettings() on absent block = %+v, want zero value", got)
	}
	if !CloudAutoConnect(c) {
		t.Fatal("CloudAutoConnect() on absent block = false, want default true")
	}
	if !CloudAutoConnect(nil) {
		t.Fatal("CloudAutoConnect(nil) = false, want default true")
	}
	if !CloudE2EE(c) {
		t.Fatal("CloudE2EE() on absent block = false, want default true")
	}
	if !CloudE2EE(nil) {
		t.Fatal("CloudE2EE(nil) = false, want default true")
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

func TestCloudE2EEDisableRoundTrip(t *testing.T) {
	off := false
	c := &Config{}
	c.SetCloud(&CloudConfig{Enabled: true, URL: "https://cloud.j-code.net", E2EE: &off})

	if CloudE2EE(c) {
		t.Fatal("CloudE2EE() = true, want explicit false")
	}
	got := c.CloudSettings()
	if got.E2EE == nil || *got.E2EE {
		t.Fatalf("CloudSettings().E2EE = %v, want explicit false", got.E2EE)
	}

	// Mutating the snapshot must not leak into the live config.
	*got.E2EE = true
	if CloudE2EE(c) {
		t.Fatal("snapshot mutation leaked into live config")
	}

	// JSON round-trip keeps the documented `e2ee` key and the explicit false.
	data, err := json.Marshal(c.CloudSettings())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := raw["e2ee"]; !ok || v != false {
		t.Fatalf("marshaled cloud block missing e2ee=false: %v", raw)
	}

	// An explicit true survives too (distinguishable from absent in the JSON,
	// equivalent for CloudE2EE).
	on := true
	c.SetCloud(&CloudConfig{Enabled: true, E2EE: &on})
	if !CloudE2EE(c) {
		t.Fatal("CloudE2EE() = false, want explicit true")
	}
}

func TestCloudSyncDefaultRoundTrip(t *testing.T) {
	c := &Config{}
	if CloudSyncDefault(c) {
		t.Fatal("CloudSyncDefault() on absent block = true, want default false")
	}
	if CloudSyncDefault(nil) {
		t.Fatal("CloudSyncDefault(nil) = true, want default false")
	}

	c.SetCloud(&CloudConfig{Enabled: true, SyncDefault: true})
	if !CloudSyncDefault(c) {
		t.Fatal("CloudSyncDefault() = false, want explicit true")
	}
	// Snapshot independence + JSON round-trip.
	data, err := json.Marshal(c.CloudSettings())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["sync_default"]; !ok || v != true {
		t.Fatalf("marshaled cloud block missing sync_default=true: %v", raw)
	}

	// Explicit false is indistinguishable from absent (both mean default OFF)
	// but must survive a read/modify/write of the block.
	c.SetCloud(&CloudConfig{Enabled: true, SyncDefault: false})
	if CloudSyncDefault(c) {
		t.Fatal("CloudSyncDefault() = true, want explicit false")
	}
}
