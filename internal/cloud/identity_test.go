package cloud

import (
	"encoding/base64"
	"os"
	"path/filepath"
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
