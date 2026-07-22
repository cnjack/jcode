package cloud

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialsRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := &Credentials{
		CloudURL:    "https://cloud.j-code.net",
		DeviceID:    "device-42",
		DeviceToken: "dev-token-abc",
		DeviceName:  "jack-macbook",
		PublicKey:   "cHVia2V5",
		PrivateKey:  "cHJpdmtleQ==",
		KeyGen:      1,
	}
	if err := SaveCredentials(want); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}

	got, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if got == nil {
		t.Fatal("LoadCredentials() = nil, want saved credentials")
	}
	if *got != *want {
		t.Fatalf("LoadCredentials() = %+v, want %+v", got, want)
	}

	assertPermission(t, filepath.Join(home, ".jcode"), 0o700)
	assertPermission(t, filepath.Join(home, ".jcode", credentialsFile), 0o600)
	metadata, err := os.ReadFile(filepath.Join(home, ".jcode", credentialsFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(metadata) == "" || containsAny(string(metadata), "dev-token-abc", "cHJpdmtleQ==") {
		t.Fatalf("cloud.json contains secret material: %s", metadata)
	}
}

func TestLoadCredentialsMigratesLegacySecrets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jcode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := Credentials{
		CloudURL: "https://cloud.j-code.net", DeviceID: "device-legacy",
		DeviceToken: "legacy-token", PrivateKey: "legacy-private", CEK: "legacy-cek",
		PublicKey: "legacy-public", KeyGen: 3,
	}
	data, _ := json.Marshal(&legacy)
	if err := os.WriteFile(filepath.Join(dir, credentialsFile), data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadCredentials()
	if err != nil || got == nil {
		t.Fatalf("LoadCredentials() = %+v, %v", got, err)
	}
	if got.DeviceToken != legacy.DeviceToken || got.PrivateKey != legacy.PrivateKey || got.CEK != legacy.CEK {
		t.Fatalf("migrated secrets = %+v", got)
	}
	metadata, _ := os.ReadFile(filepath.Join(dir, credentialsFile))
	if containsAny(string(metadata), legacy.DeviceToken, legacy.PrivateKey, legacy.CEK) {
		t.Fatalf("legacy secrets remained in cloud.json: %s", metadata)
	}
	if _, err := os.Stat(filepath.Join(dir, secretFile)); err != nil {
		t.Fatalf("migrated secret store missing: %v", err)
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func TestLoadCredentialsMissingMeansLoggedOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	got, err := LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v, want nil for missing file", err)
	}
	if got != nil {
		t.Fatalf("LoadCredentials() = %+v, want nil for missing file", got)
	}
}

func TestDeleteCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Deleting when absent is not an error.
	if err := DeleteCredentials(); err != nil {
		t.Fatalf("DeleteCredentials() (absent) error = %v", err)
	}

	if err := SaveCredentials(&Credentials{CloudURL: "https://cloud.j-code.net", KeyGen: 1}); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}
	if err := DeleteCredentials(); err != nil {
		t.Fatalf("DeleteCredentials() error = %v", err)
	}
	got, err := LoadCredentials()
	if err != nil || got != nil {
		t.Fatalf("LoadCredentials() after delete = %+v, %v; want nil, nil", got, err)
	}
}

func TestGenerateIdentityKeyPair(t *testing.T) {
	pub, priv, err := GenerateIdentityKeyPair()
	if err != nil {
		t.Fatalf("GenerateIdentityKeyPair() error = %v", err)
	}
	pubRaw, err := base64.StdEncoding.DecodeString(pub)
	if err != nil {
		t.Fatalf("public key is not valid base64: %v", err)
	}
	privRaw, err := base64.StdEncoding.DecodeString(priv)
	if err != nil {
		t.Fatalf("private key is not valid base64: %v", err)
	}
	if len(pubRaw) != 32 || len(privRaw) != 32 {
		t.Fatalf("X25519 raw key lengths = %d/%d, want 32/32", len(pubRaw), len(privRaw))
	}
	if pub == priv {
		t.Fatal("public and private keys must differ")
	}
}

func assertPermission(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permission %s = %#o, want %#o", path, got, want)
	}
}
