package tools

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	appconfig "github.com/cnjack/jcode/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	SSHHostKeyUnknown              = "ssh_host_key_unknown"
	SSHHostKeyChanged              = "ssh_host_key_changed"
	SSHHostKeyConfirmationMismatch = "ssh_host_key_confirmation_mismatch"
)

// SSHHostKeyError is safe to expose to a UI so it can distinguish first-use
// trust from a changed key. A changed key is never accepted by the normal TOFU
// flow; it requires the user to repair the trust store out of band.
type SSHHostKeyError struct {
	Code                string
	Host                string
	Fingerprint         string
	KeyType             string
	OldFingerprint      string
	ExpectedFingerprint string
	cause               error
}

func (e *SSHHostKeyError) Error() string {
	switch e.Code {
	case SSHHostKeyUnknown:
		return fmt.Sprintf("SSH host key for %s is not trusted (%s %s)", e.Host, e.KeyType, e.Fingerprint)
	case SSHHostKeyChanged:
		return fmt.Sprintf("SSH host key for %s changed (received %s %s; trusted %s)", e.Host, e.KeyType, e.Fingerprint, e.OldFingerprint)
	case SSHHostKeyConfirmationMismatch:
		return fmt.Sprintf("SSH host key confirmation for %s does not match (expected %s; received %s)", e.Host, e.ExpectedFingerprint, e.Fingerprint)
	default:
		return fmt.Sprintf("SSH host key verification failed for %s", e.Host)
	}
}

func (e *SSHHostKeyError) Unwrap() error { return e.cause }

// SSHHostKeyPolicy controls JCode's TOFU trust flow. AcceptUnknown must only be
// set after displaying the unknown-key details to the user. ExpectedFingerprint
// must contain that displayed SHA256 fingerprint, preventing a key swap between
// the prompt and the confirmation request.
type SSHHostKeyPolicy struct {
	AcceptUnknown       bool
	ExpectedFingerprint string
	// KnownHostsPath is an internal override used by callers that need an
	// isolated store. Empty selects ~/.jcode/known_hosts.
	KnownHostsPath string
}

// SSHKnownHostsPath returns JCode's private host-key trust store.
func SSHKnownHostsPath() string {
	return filepath.Join(appconfig.ConfigDir(), "known_hosts")
}

// NewSSHHostKeyCallback builds a strict known_hosts callback. Reading an
// unknown host does not create or modify any file. Only an explicit,
// fingerprint-bound AcceptUnknown policy persists a new key.
func NewSSHHostKeyCallback(policy SSHHostKeyPolicy) (ssh.HostKeyCallback, error) {
	path := policy.KnownHostsPath
	if path == "" {
		path = SSHKnownHostsPath()
	}
	checker, err := loadKnownHostsCallback(path)
	if err != nil {
		return nil, fmt.Errorf("load SSH known_hosts: %w", err)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		host := knownhosts.Normalize(hostname)
		fingerprint := ssh.FingerprintSHA256(key)
		if checker != nil {
			err := checker(hostname, remote, key)
			if err == nil {
				return nil
			}
			if hostKeyErr := classifyKnownHostsError(err, host, key); hostKeyErr != nil {
				if hostKeyErr.Code == SSHHostKeyChanged {
					return hostKeyErr
				}
			} else {
				return fmt.Errorf("verify SSH host key for %s: %w", host, err)
			}
		}

		unknown := &SSHHostKeyError{
			Code:        SSHHostKeyUnknown,
			Host:        host,
			Fingerprint: fingerprint,
			KeyType:     key.Type(),
		}
		if !policy.AcceptUnknown {
			return unknown
		}

		expected := strings.TrimSpace(policy.ExpectedFingerprint)
		if expected == "" || expected != fingerprint {
			return &SSHHostKeyError{
				Code:                SSHHostKeyConfirmationMismatch,
				Host:                host,
				Fingerprint:         fingerprint,
				KeyType:             key.Type(),
				ExpectedFingerprint: expected,
			}
		}
		return trustUnknownHostKey(path, hostname, remote, key)
	}, nil
}

func classifyKnownHostsError(err error, host string, key ssh.PublicKey) *SSHHostKeyError {
	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) {
		if len(keyErr.Want) == 0 {
			return &SSHHostKeyError{
				Code:        SSHHostKeyUnknown,
				Host:        host,
				Fingerprint: ssh.FingerprintSHA256(key),
				KeyType:     key.Type(),
				cause:       err,
			}
		}
		return changedHostKeyError(host, key, keyErr.Want[0].Key, err)
	}
	var revoked *knownhosts.RevokedError
	if errors.As(err, &revoked) {
		return changedHostKeyError(host, key, revoked.Revoked.Key, err)
	}
	return nil
}

func changedHostKeyError(host string, received, trusted ssh.PublicKey, cause error) *SSHHostKeyError {
	oldFingerprint := ""
	if trusted != nil {
		oldFingerprint = ssh.FingerprintSHA256(trusted)
	}
	return &SSHHostKeyError{
		Code:           SSHHostKeyChanged,
		Host:           host,
		Fingerprint:    ssh.FingerprintSHA256(received),
		KeyType:        received.Type(),
		OldFingerprint: oldFingerprint,
		cause:          cause,
	}
}

var knownHostsWriteMu sync.Mutex

// trustUnknownHostKey re-checks under the writer mutex before appending. This
// keeps two simultaneous confirmations from duplicating a key and refuses to
// overwrite a key that another connection trusted first.
func trustUnknownHostKey(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	knownHostsWriteMu.Lock()
	defer knownHostsWriteMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create SSH trust directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("secure SSH trust directory: %w", err)
	}
	lock, err := acquireKnownHostsFileLock(path + ".lock")
	if err != nil {
		return fmt.Errorf("lock SSH known_hosts: %w", err)
	}
	defer func() {
		if releaseErr := lock.release(); releaseErr != nil {
			appconfig.Logger().Printf("[ssh] release known_hosts lock: %v", releaseErr)
		}
	}()

	checker, err := loadKnownHostsCallback(path)
	if err != nil {
		return fmt.Errorf("reload SSH known_hosts: %w", err)
	}
	if checker != nil {
		if err := checker(hostname, remote, key); err == nil {
			return nil
		} else if hostKeyErr := classifyKnownHostsError(err, knownhosts.Normalize(hostname), key); hostKeyErr != nil && hostKeyErr.Code == SSHHostKeyChanged {
			return hostKeyErr
		} else if hostKeyErr == nil {
			return fmt.Errorf("verify SSH host key before trust: %w", err)
		}
	}

	current, err := readPrivateKnownHosts(path)
	if err != nil {
		return err
	}
	if len(current) > 0 && current[len(current)-1] != '\n' {
		current = append(current, '\n')
	}
	// Normalize explicitly so a non-default port is persisted as
	// [host]:port and round-trips through knownhosts.New.
	current = append(current, knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)...)
	current = append(current, '\n')
	if err := durableWriteKnownHosts(path, current); err != nil {
		return fmt.Errorf("persist SSH host key: %w", err)
	}
	return nil
}

func loadKnownHostsCallback(path string) (ssh.HostKeyCallback, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing non-regular known_hosts file %s", path)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("secure known_hosts permissions: %w", err)
	}
	return knownhosts.New(path)
}

func readPrivateKnownHosts(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read SSH known_hosts: %w", err)
	}
	return data, nil
}

func durableWriteKnownHosts(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".known_hosts.tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	tmpPath = ""
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}
