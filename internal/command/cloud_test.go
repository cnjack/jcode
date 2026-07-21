package command

import (
	"testing"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

func boolPtr(b bool) *bool { return &b }

func TestNewCloudConnectorGate(t *testing.T) {
	creds := &cloud.Credentials{
		CloudURL:    "https://cloud.example.com",
		DeviceID:    "dev-1",
		DeviceToken: "tok",
	}
	build := func(cfg *config.Config, c *cloud.Credentials, webToken string) *cloud.Connector {
		return newCloudSupervisor(cfg, 8080, webToken).BuildConnector(c)
	}

	// Not logged in → no connector, regardless of config.
	if c := build(&config.Config{}, nil, ""); c != nil {
		t.Error("nil credentials must not start the connector")
	}

	// auto_connect explicitly false → no connector.
	cfg := &config.Config{}
	cfg.SetCloud(&config.CloudConfig{Enabled: true, URL: "https://cloud.example.com", AutoConnect: boolPtr(false)})
	if c := build(cfg, creds, ""); c != nil {
		t.Error("auto_connect=false must not start the connector")
	}

	// Logged in + default (absent) auto_connect → connector built, cloud URL
	// taken from config.
	cfg.SetCloud(&config.CloudConfig{Enabled: true, URL: "https://cloud.example.com"})
	c := build(cfg, creds, "webtok")
	if c == nil {
		t.Fatal("logged in with default auto_connect must start the connector")
	}

	// No config cloud block → URL falls back to the credentials'.
	c = build(&config.Config{}, creds, "")
	if c == nil {
		t.Fatal("logged in without a config cloud block must still start the connector")
	}
}
