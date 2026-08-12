package tools

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSSHKnownHostsTOFURoundTripNonDefaultPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".jcode", "known_hosts")
	key := testSSHPublicKey(t)
	hostname := "example.test:2222"
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 2222}

	strict, err := NewSSHHostKeyCallback(SSHHostKeyPolicy{KnownHostsPath: path})
	if err != nil {
		t.Fatal(err)
	}
	err = strict(hostname, remoteAddr, key)
	assertHostKeyError(t, err, SSHHostKeyUnknown, "[example.test]:2222", ssh.FingerprintSHA256(key))
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unknown lookup wrote trust store: stat error = %v", statErr)
	}

	accept, err := NewSSHHostKeyCallback(SSHHostKeyPolicy{
		AcceptUnknown:       true,
		ExpectedFingerprint: ssh.FingerprintSHA256(key),
		KnownHostsPath:      path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := accept(hostname, remoteAddr, key); err != nil {
		t.Fatalf("accept first-use key: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("known_hosts mode = %o, want 600", got)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(contents), "[example.test]:2222 ") {
		t.Fatalf("non-default port was not canonicalized: %q", contents)
	}

	reloaded, err := NewSSHHostKeyCallback(SSHHostKeyPolicy{KnownHostsPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded(hostname, remoteAddr, key); err != nil {
		t.Fatalf("trusted key did not round-trip: %v", err)
	}
}

func TestSSHKnownHostsChangedKeyCannotBeAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	trusted := testSSHPublicKey(t)
	received := testSSHPublicKey(t)
	hostname := "host.example:22"
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("192.0.2.20"), Port: 22}

	acceptTrusted, err := NewSSHHostKeyCallback(SSHHostKeyPolicy{
		AcceptUnknown:       true,
		ExpectedFingerprint: ssh.FingerprintSHA256(trusted),
		KnownHostsPath:      path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptTrusted(hostname, remoteAddr, trusted); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	acceptChanged, err := NewSSHHostKeyCallback(SSHHostKeyPolicy{
		AcceptUnknown:       true,
		ExpectedFingerprint: ssh.FingerprintSHA256(received),
		KnownHostsPath:      path,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = acceptChanged(hostname, remoteAddr, received)
	hostKeyErr := assertHostKeyError(t, err, SSHHostKeyChanged, "host.example", ssh.FingerprintSHA256(received))
	if hostKeyErr.OldFingerprint != ssh.FingerprintSHA256(trusted) {
		t.Fatalf("old fingerprint = %q, want %q", hostKeyErr.OldFingerprint, ssh.FingerprintSHA256(trusted))
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("changed-key confirmation modified known_hosts")
	}
}

func TestSSHKnownHostsConfirmationBindsFingerprint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	displayed := testSSHPublicKey(t)
	received := testSSHPublicKey(t)
	hostname := "race.example:22"
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("192.0.2.30"), Port: 22}
	callback, err := NewSSHHostKeyCallback(SSHHostKeyPolicy{
		AcceptUnknown:       true,
		ExpectedFingerprint: ssh.FingerprintSHA256(displayed),
		KnownHostsPath:      path,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = callback(hostname, remoteAddr, received)
	assertHostKeyError(t, err, SSHHostKeyConfirmationMismatch, "race.example", ssh.FingerprintSHA256(received))
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("mismatched confirmation wrote trust store: stat error = %v", statErr)
	}
}

func testSSHPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func assertHostKeyError(t *testing.T, err error, code, host, fingerprint string) SSHHostKeyError {
	t.Helper()
	var hostKeyErr *SSHHostKeyError
	if !errors.As(err, &hostKeyErr) {
		t.Fatalf("error = %v, want *SSHHostKeyError", err)
	}
	if hostKeyErr.Code != code || hostKeyErr.Host != host || hostKeyErr.Fingerprint != fingerprint {
		t.Fatalf("host key error = %+v, want code=%q host=%q fingerprint=%q", hostKeyErr, code, host, fingerprint)
	}
	if hostKeyErr.KeyType == "" {
		t.Fatal("host key error omitted key type")
	}
	return *hostKeyErr
}
