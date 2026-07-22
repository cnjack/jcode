// Package cloud implements the jcloud (device relay) client side: device-code
// login (RFC 8628), device registration, and the on-disk device credentials.
// See cloud/docs/17-jcode-device-relay.md §3 for the interface contract.
package cloud

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const credentialsFile = "cloud.json"

// Credentials is the device identity. Non-sensitive metadata is persisted at
// ~/.jcode/cloud.json; DeviceToken, PrivateKey and CEK live in the operating
// system keyring. A missing metadata file means "not configured".
type Credentials struct {
	CloudURL    string `json:"cloud_url"`
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
	DeviceName  string `json:"device_name"`
	// PublicKey / PrivateKey are the X25519 device identity key pair, base64
	// (standard encoding) encoded raw keys. The public key is registered with
	// the orchestrator for later E2E key exchange (CEK wrapping).
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	// KeyGen is the current CEK generation this device holds.
	KeyGen int `json:"key_gen"`
	// CEK is the account-level AES-256-GCM content encryption key, base64.
	// Empty means "not initialized yet" (pre-M5 credentials file); it is
	// lazily generated on first need (see crypto.go EnsureCEK).
	CEK string `json:"cek,omitempty"`
	// Fingerprint is the stable machine fingerprint SOURCE (M16): the OS
	// machine id, or a "fallback:<hostname>:<random>" string generated once.
	// It never leaves the machine — only its sha256 (FingerprintHash) is sent
	// to the orchestrator for login dedup. Empty on pre-M16 files; resolved
	// on the fly then (see ResolveFingerprintSource).
	Fingerprint string `json:"fingerprint,omitempty"`
}

// GenerateIdentityKeyPair creates a fresh X25519 device identity key pair and
// returns both keys base64-encoded (standard encoding, raw 32-byte keys).
func GenerateIdentityKeyPair() (publicKey, privateKey string, err error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate X25519 identity key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()),
		base64.StdEncoding.EncodeToString(priv.Bytes()), nil
}

// CredentialsPath returns the full path to the credentials file
// (~/.jcode/cloud.json).
func CredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".jcode", credentialsFile), nil
}

// LoadCredentials reads ~/.jcode/cloud.json. A missing file is not an error:
// it returns (nil, nil) to mean "not logged in".
func LoadCredentials() (*Credentials, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read credentials file %s: %w", path, err)
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials file %s: %w", path, err)
	}

	// Migrate pre-keyring credentials in place. Save the secrets first and only
	// then rewrite cloud.json, so a keyring failure can never destroy a working
	// legacy login.
	legacySecrets := secretsFromCredentials(&creds)
	secrets, found, secretErr := loadCredentialSecrets()
	if !found && (legacySecrets.DeviceToken != "" || legacySecrets.PrivateKey != "" || legacySecrets.CEK != "") {
		if secretErr == nil {
			if err := saveCredentialSecrets(legacySecrets); err == nil {
				secrets, found = legacySecrets, true
				_ = writeCredentialMetadata(&creds, path)
			}
		}
	}
	if found {
		applyCredentialSecrets(&creds, secrets)
	} else if secretErr != nil && legacySecrets.DeviceToken == "" && legacySecrets.PrivateKey == "" && legacySecrets.CEK == "" {
		return nil, secretErr
	}
	return &creds, nil
}

// SaveCredentials writes credentials atomically to ~/.jcode/cloud.json with
// owner-only permissions (file 0600, directory 0700), mirroring the pattern
// used by config.saveConfig.
func SaveCredentials(creds *Credentials) error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	if err := saveCredentialSecrets(secretsFromCredentials(creds)); err != nil {
		return err
	}
	return writeCredentialMetadata(creds, path)
}

func writeCredentialMetadata(creds *Credentials, path string) error {

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("failed to secure config directory %s: %w", dir, err)
	}

	metadata := *creds
	metadata.DeviceToken = ""
	metadata.PrivateKey = ""
	metadata.CEK = ""
	data, err := json.MarshalIndent(&metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+credentialsFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary credentials file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	// The file carries the device token and private identity key; keep it
	// owner-only regardless of CreateTemp's own defaults.
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("failed to secure temporary credentials file %s: %w", tmpPath, err)
	}
	if n, err := tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write temporary credentials file %s: %w", tmpPath, err)
	} else if n != len(data) {
		return fmt.Errorf("failed to write temporary credentials file %s: %w", tmpPath, io.ErrShortWrite)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to sync temporary credentials file %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temporary credentials file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to replace credentials file %s: %w", path, err)
	}
	tmpPath = ""
	return nil
}

// DeleteCredentials forgets the complete local device identity, including
// secrets in the system keyring. A missing file/key is not an error.
func DeleteCredentials() error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove credentials file %s: %w", path, err)
	}
	return deleteCredentialSecrets()
}
