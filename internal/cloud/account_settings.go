package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/theme"
)

const accountSettingsSchemaVersion = 1

// AccountPreferences is the complete portable whitelist. It intentionally has
// no provider credentials, headers, local paths, aliases, channel state, or
// permission policy fields.
type AccountPreferences struct {
	SchemaVersion int       `json:"schema_version"`
	Model         string    `json:"model,omitempty"`
	SmallModel    string    `json:"small_model,omitempty"`
	Language      string    `json:"language,omitempty"`
	Theme         string    `json:"theme,omitempty"`
	DefaultMode   string    `json:"default_mode"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type AccountSettingsRemote struct {
	Version   int64           `json:"version"`
	Envelope  json.RawMessage `json:"envelope"`
	UpdatedAt *time.Time      `json:"updated_at,omitempty"`
}

func (c *Client) GetAccountSettings(ctx context.Context, token string) (*AccountSettingsRemote, error) {
	var out AccountSettingsRemote
	if _, err := c.get(ctx, "/internal/v1/device/account-settings", token, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) PutAccountSettings(ctx context.Context, token string, baseVersion int64, envelope json.RawMessage) (*AccountSettingsRemote, error) {
	var out AccountSettingsRemote
	err := c.put(ctx, "/internal/v1/device/account-settings", token, map[string]any{
		"base_version": baseVersion,
		"envelope":     envelope,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func portableMode(cfg *config.Config) string {
	switch cfg.DefaultMode {
	case "approval", "plan", "full_access":
		return cfg.DefaultMode
	default:
		return "approval"
	}
}

func snapshotAccountPreferences(cfg *config.Config, at time.Time) AccountPreferences {
	return AccountPreferences{
		SchemaVersion: accountSettingsSchemaVersion,
		Model:         cfg.Model, SmallModel: cfg.SmallModel, Language: cfg.Language,
		Theme: cfg.Theme, DefaultMode: portableMode(cfg), UpdatedAt: at.UTC(),
	}
}

func supportedLanguage(language string) bool {
	switch language {
	case "", "en", "zh-Hans", "zh-Hant", "ja", "ko":
		return true
	default:
		return false
	}
}

func portableModelAvailable(cfg *config.Config, ref string) bool {
	if ref == "" {
		return true
	}
	provider, model, ok := strings.Cut(ref, "/")
	return ok && provider != "" && model != "" && cfg.GetProviders()[provider] != nil
}

func validateAccountPreferences(prefs AccountPreferences) error {
	if prefs.SchemaVersion != accountSettingsSchemaVersion {
		return fmt.Errorf("unsupported account settings schema %d", prefs.SchemaVersion)
	}
	if !supportedLanguage(prefs.Language) {
		return fmt.Errorf("unsupported language %q", prefs.Language)
	}
	switch prefs.DefaultMode {
	case "approval", "plan", "full_access":
	default:
		return fmt.Errorf("unsupported default mode %q", prefs.DefaultMode)
	}
	if prefs.Theme != "" {
		if _, ok := theme.Get(prefs.Theme); !ok {
			return fmt.Errorf("unknown theme %q", prefs.Theme)
		}
	}
	if prefs.UpdatedAt.IsZero() {
		return errors.New("account settings updated_at is required")
	}
	return nil
}

func applyAccountPreferences(cfg *config.Config, prefs AccountPreferences, version int64) error {
	if err := validateAccountPreferences(prefs); err != nil {
		return err
	}
	// Model refs are portable only when this installation has the provider.
	// An unavailable remote model never disables a working local configuration.
	if portableModelAvailable(cfg, prefs.Model) {
		cfg.Model = prefs.Model
	}
	if portableModelAvailable(cfg, prefs.SmallModel) {
		cfg.SmallModel = prefs.SmallModel
	}
	cfg.Language = prefs.Language
	cfg.Theme = prefs.Theme
	cfg.DefaultMode = prefs.DefaultMode
	cfg.AccountSettingsVersion = version
	cfg.AccountSettingsUpdatedAt = prefs.UpdatedAt.UTC()
	return config.SaveConfig(cfg)
}

func (c *Connector) accountSettingsCipher() (*EnvelopeCipher, error) {
	if cipher := c.cipherSnapshot(); cipher != nil {
		return cipher, nil
	}
	return EnsureCEK()
}

func (c *Connector) openAccountSettings(remote *AccountSettingsRemote) (AccountPreferences, error) {
	var prefs AccountPreferences
	cipher, err := c.accountSettingsCipher()
	if err != nil {
		return prefs, err
	}
	plain, err := cipher.Open(remote.Envelope)
	if err != nil {
		return prefs, fmt.Errorf("decrypt account settings: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(plain)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&prefs); err != nil {
		return prefs, fmt.Errorf("parse account settings: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return prefs, fmt.Errorf("parse account settings: trailing JSON")
	}
	if err := validateAccountPreferences(prefs); err != nil {
		return prefs, err
	}
	return prefs, nil
}

func (c *Connector) putAccountPreferences(ctx context.Context, baseVersion int64, prefs AccountPreferences) (*AccountSettingsRemote, error) {
	cipher, err := c.accountSettingsCipher()
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(prefs)
	if err != nil {
		return nil, err
	}
	envelope, err := cipher.Seal(plain)
	if err != nil {
		return nil, err
	}
	return c.client.PutAccountSettings(ctx, c.token, baseVersion, envelope)
}

// ReconcileAccountSettings performs a timestamp merge at connector start and
// on the periodic loop. CAS prevents silent overwrites between clients.
func (c *Connector) ReconcileAccountSettings(ctx context.Context) error {
	c.accountSettingsMu.Lock()
	defer c.accountSettingsMu.Unlock()
	return c.reconcileAccountSettings(ctx)
}

func (c *Connector) reconcileAccountSettings(ctx context.Context) error {
	cfg := c.cfg.AppConfig
	if cfg == nil || c.cfg.CipherDisabled {
		return nil
	}
	remote, err := c.client.GetAccountSettings(ctx, c.token)
	if err != nil {
		return err
	}
	if remote.Version == 0 || len(remote.Envelope) == 0 || string(remote.Envelope) == "null" {
		at := cfg.AccountSettingsUpdatedAt
		if at.IsZero() {
			at = time.Now().UTC()
		}
		created, err := c.putAccountPreferences(ctx, 0, snapshotAccountPreferences(cfg, at))
		if err != nil {
			return err
		}
		cfg.AccountSettingsVersion = created.Version
		cfg.AccountSettingsUpdatedAt = at
		return config.SaveConfig(cfg)
	}
	var envelope Envelope
	if err := json.Unmarshal(remote.Envelope, &envelope); err != nil {
		return fmt.Errorf("parse account settings envelope: %w", err)
	}
	if cipher, cipherErr := c.accountSettingsCipher(); cipherErr == nil && envelope.KeyGen < cipher.KeyGen() {
		// CEK revoke advanced the account generation. The old settings envelope
		// is intentionally no longer decryptable; overwrite it with the current
		// local whitelist under the new key using the remote CAS version.
		at := cfg.AccountSettingsUpdatedAt
		if at.IsZero() {
			at = time.Now().UTC()
		}
		updated, err := c.putAccountPreferences(ctx, remote.Version, snapshotAccountPreferences(cfg, at))
		if err != nil {
			return err
		}
		cfg.AccountSettingsVersion = updated.Version
		cfg.AccountSettingsUpdatedAt = at
		return config.SaveConfig(cfg)
	}
	prefs, err := c.openAccountSettings(remote)
	if err != nil {
		return err
	}
	if cfg.AccountSettingsUpdatedAt.IsZero() || prefs.UpdatedAt.After(cfg.AccountSettingsUpdatedAt) {
		return applyAccountPreferences(cfg, prefs, remote.Version)
	}
	if cfg.AccountSettingsUpdatedAt.After(prefs.UpdatedAt) {
		updated, err := c.putAccountPreferences(ctx, remote.Version,
			snapshotAccountPreferences(cfg, cfg.AccountSettingsUpdatedAt))
		if err != nil {
			return err
		}
		cfg.AccountSettingsVersion = updated.Version
		return config.SaveConfig(cfg)
	}
	cfg.AccountSettingsVersion = remote.Version
	return config.SaveConfig(cfg)
}

// PushAccountSettings marks the current whitelist as locally changed, then
// reconciles against the newest remote CAS version.
func (c *Connector) PushAccountSettings(ctx context.Context) error {
	c.accountSettingsMu.Lock()
	defer c.accountSettingsMu.Unlock()
	if c.cfg.AppConfig == nil || c.cfg.CipherDisabled {
		return nil
	}
	c.cfg.AppConfig.AccountSettingsUpdatedAt = time.Now().UTC()
	if err := config.SaveConfig(c.cfg.AppConfig); err != nil {
		return err
	}
	return c.reconcileAccountSettings(ctx)
}

func (c *Connector) accountSettingsLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.ReconcileAccountSettings(ctx); err != nil {
				c.logf("account settings sync: %v", err)
			}
		}
	}
}
