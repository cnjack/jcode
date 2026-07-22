package cloud

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	keyring "github.com/zalando/go-keyring"
)

const (
	credentialKeyringService = "net.j-code.jcode.cloud"
	credentialKeyringAccount = "device-credentials"
	secretBackendEnv         = "JCODE_CLOUD_SECRET_BACKEND"
	secretFile               = "cloud.secrets.json"
)

type credentialSecrets struct {
	DeviceToken string `json:"device_token,omitempty"`
	PrivateKey  string `json:"private_key,omitempty"`
	CEK         string `json:"cek,omitempty"`
}

func secretsFromCredentials(creds *Credentials) credentialSecrets {
	return credentialSecrets{
		DeviceToken: creds.DeviceToken,
		PrivateKey:  creds.PrivateKey,
		CEK:         creds.CEK,
	}
}

func applyCredentialSecrets(creds *Credentials, secrets credentialSecrets) {
	creds.DeviceToken = secrets.DeviceToken
	creds.PrivateKey = secrets.PrivateKey
	creds.CEK = secrets.CEK
}

func loadCredentialSecrets() (credentialSecrets, bool, error) {
	if os.Getenv(secretBackendEnv) == "file" {
		return loadCredentialSecretsFile()
	}
	value, err := keyring.Get(credentialKeyringService, credentialKeyringAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return credentialSecrets{}, false, nil
	}
	if err != nil {
		return credentialSecrets{}, false, fmt.Errorf("read cloud credentials from system keyring: %w", err)
	}
	var secrets credentialSecrets
	if err := json.Unmarshal([]byte(value), &secrets); err != nil {
		return credentialSecrets{}, false, fmt.Errorf("decode cloud credentials from system keyring: %w", err)
	}
	return secrets, true, nil
}

func saveCredentialSecrets(secrets credentialSecrets) error {
	data, err := json.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("encode cloud credentials for system keyring: %w", err)
	}
	if os.Getenv(secretBackendEnv) == "file" {
		return saveCredentialSecretsFile(data)
	}
	if err := keyring.Set(credentialKeyringService, credentialKeyringAccount, string(data)); err != nil {
		return fmt.Errorf("save cloud credentials to system keyring: %w", err)
	}
	return nil
}

func deleteCredentialSecrets() error {
	if os.Getenv(secretBackendEnv) == "file" {
		path, err := credentialSecretsPath()
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove cloud credentials file %s: %w", path, err)
		}
		return nil
	}
	if err := keyring.Delete(credentialKeyringService, credentialKeyringAccount); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("remove cloud credentials from system keyring: %w", err)
	}
	return nil
}

func credentialSecretsPath() (string, error) {
	path, err := CredentialsPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), secretFile), nil
}

func loadCredentialSecretsFile() (credentialSecrets, bool, error) {
	path, err := credentialSecretsPath()
	if err != nil {
		return credentialSecrets{}, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return credentialSecrets{}, false, nil
	}
	if err != nil {
		return credentialSecrets{}, false, fmt.Errorf("read cloud credentials file %s: %w", path, err)
	}
	var secrets credentialSecrets
	if err := json.Unmarshal(data, &secrets); err != nil {
		return credentialSecrets{}, false, fmt.Errorf("decode cloud credentials file %s: %w", path, err)
	}
	return secrets, true, nil
}

func saveCredentialSecretsFile(data []byte) error {
	path, err := credentialSecretsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cloud credentials directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write cloud credentials file %s: %w", path, err)
	}
	return os.Chmod(path, 0o600)
}
