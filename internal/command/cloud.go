package command

import (
	"context"
	"fmt"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

// startCloudConnector starts the jcloud relay connector in the background when
// the device is logged in and cloud.auto_connect is not disabled. The
// connector is strictly best-effort: every failure is logged as a warning and
// it never blocks or fails the web server. Its lifecycle follows ctx (the web
// server's shutdown context). Returns the started connector (nil when not
// started) so tests can assert the start decision.
func startCloudConnector(ctx context.Context, cfg *config.Config, port int, webToken string) *cloud.Connector {
	creds, err := cloud.LoadCredentials()
	if err != nil {
		config.Logger().Printf("[cloud] failed to load credentials, relay connector disabled: %v", err)
		return nil
	}
	conn := newCloudConnector(cfg, creds, port, webToken)
	if conn == nil {
		return nil
	}
	go conn.Run(ctx)
	return conn
}

// newCloudConnector is the pure start decision + construction behind
// startCloudConnector: nil when the connector should not run (not logged in,
// or auto_connect explicitly disabled).
func newCloudConnector(cfg *config.Config, creds *cloud.Credentials, port int, webToken string) *cloud.Connector {
	if !cloud.ShouldConnect(config.CloudAutoConnect(cfg), creds) {
		return nil
	}
	cloudURL := cfg.CloudSettings().URL
	if cloudURL == "" {
		cloudURL = creds.CloudURL
	}
	if cloudURL == "" {
		cloudURL = cloud.DefaultCloudURL
	}
	config.Logger().Printf("[cloud] starting relay connector (cloud=%s, device=%s)", cloudURL, creds.DeviceID)
	return cloud.NewConnector(cloud.ConnectorConfig{
		CloudURL:    cloudURL,
		Credentials: creds,
		// The control plane is always this process's own web server on
		// loopback, regardless of the --host bind.
		LocalBase:  fmt.Sprintf("http://127.0.0.1:%d", port),
		LocalToken: webToken,
		Version:    Version,
	})
}
