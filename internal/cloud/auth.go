// auth.go holds the shared login/logout helpers used by both the CLI
// (`jcode login` / `jcode logout`) and the web API (POST /api/cloud/login,
// /api/cloud/logout) so the two entry points can never drift apart.
package cloud

import (
	"context"
	"encoding/json"
	"os"

	"github.com/cnjack/jcode/internal/config"
)

// Logout signs the device out of jcloud: the device token is revoked remotely
// (best effort — a network failure or a missing server endpoint must not trap
// the local credentials), the credentials file and the in-process CEK cache
// are cleared, and config.cloud is updated (enabled=false, URL and user
// preferences preserved). warnf receives non-fatal warnings (revoke/config
// failures); it may be nil. A missing credentials file is a no-op.
func Logout(ctx context.Context, warnf func(format string, args ...any)) error {
	warn := func(format string, args ...any) {
		if warnf != nil {
			warnf(format, args...)
		}
	}
	creds, err := LoadCredentials()
	if err != nil {
		return err
	}
	if creds == nil {
		return nil // not logged in
	}

	client := NewClient(creds.CloudURL)
	if err := client.RevokeDevice(ctx, creds.DeviceToken); err != nil {
		warn("failed to revoke device token on %s: %v — clearing local credentials anyway", creds.CloudURL, err)
	}

	if err := DeleteCredentials(); err != nil {
		return err
	}
	ResetCEKCache()

	if err := UpdateConfigCloud("", false); err != nil {
		warn("failed to update %s: %v", config.ConfigPath(), err)
	}
	return nil
}

// UpdateConfigCloud sets config.cloud while preserving the stored url (when
// the url argument is empty, i.e. logout) and the user's auto_connect/e2ee
// preferences. Login/logout must not require a fully configured provider set,
// so a LoadConfig failure falls back to a best-effort raw read of the file
// (unknown fields may be dropped in that case).
func UpdateConfigCloud(url string, enabled bool) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = &config.Config{}
		if data, readErr := os.ReadFile(config.ConfigPath()); readErr == nil {
			_ = json.Unmarshal(data, cfg)
		}
	}
	current := cfg.CloudSettings()
	if url == "" {
		url = current.URL
	}
	cfg.SetCloud(&config.CloudConfig{
		Enabled:     enabled,
		URL:         url,
		AutoConnect: current.AutoConnect,
		E2EE:        current.E2EE,
	})
	return config.SaveConfig(cfg)
}
