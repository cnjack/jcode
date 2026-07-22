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

// Logout signs the device out of jcloud while preserving its encryption
// identity. Only the revocable access token is cleared; the device key pair,
// CEK and fingerprint remain in the system keyring so the next login can
// resume without breaking paired browsers/mobile clients.
func Logout(ctx context.Context, warnf func(format string, args ...any)) error {
	return logout(ctx, false, warnf)
}

// Forget signs out and removes the full local identity. This is intentionally
// separate from Logout because forgetting keys requires other clients to pair
// with the device again.
func Forget(ctx context.Context, warnf func(format string, args ...any)) error {
	return logout(ctx, true, warnf)
}

func logout(ctx context.Context, forget bool, warnf func(format string, args ...any)) error {
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

	if creds.DeviceToken != "" {
		client := NewClient(creds.CloudURL)
		if err := client.RevokeDevice(ctx, creds.DeviceToken); err != nil {
			warn("failed to revoke device token on %s: %v — signing out locally anyway", creds.CloudURL, err)
		}
	}

	if forget {
		if err := DeleteCredentials(); err != nil {
			return err
		}
	} else {
		creds.DeviceToken = ""
		if err := SaveCredentials(creds); err != nil {
			return err
		}
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
		SyncDefault: current.SyncDefault,
	})
	return config.SaveConfig(cfg)
}
