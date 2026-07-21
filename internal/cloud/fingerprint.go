package cloud

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Machine fingerprint (M16 — cloud/docs/17-jcode-device-relay.md §3): a stable
// per-machine identity the orchestrator uses to dedup repeated `jcode login`
// runs from the same computer (one devices row per machine, not one per
// login). The source string itself NEVER leaves the machine — only its
// sha256 (FingerprintHash) is sent, on the token poll and on register.
//
// Source resolution order (ResolveFingerprintSource):
//  1. the value already persisted in ~/.jcode/cloud.json — it never changes
//     once written, so a hardware-id failure later cannot split the device;
//  2. the OS-level machine id (macOS IOPlatformUUID, Linux /etc/machine-id,
//     Windows registry MachineGuid);
//  3. a fallback of hostname + a persistent random — generated once and saved
//     into cloud.json's fingerprint field with the rest of the credentials.

// fingerprintDeps carries every side effect machineID needs, so tests can
// drive all three OS branches table-driven.
type fingerprintDeps struct {
	goos       string
	execOutput func(name string, args ...string) ([]byte, error)
	readFile   func(path string) ([]byte, error)
}

func defaultFingerprintDeps() fingerprintDeps {
	return fingerprintDeps{
		goos: runtime.GOOS,
		execOutput: func(name string, args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return exec.CommandContext(ctx, name, args...).Output()
		},
		readFile: os.ReadFile,
	}
}

// machineID returns the OS-level stable machine id, or "" when the platform
// is unknown or the id cannot be read (the caller then falls back).
func machineID(d fingerprintDeps) string {
	switch d.goos {
	case "darwin":
		out, err := d.execOutput("ioreg", "-rd1", "-c", "IOPlatformExpertDevice")
		if err != nil {
			return ""
		}
		return parseIOPlatformUUID(string(out))
	case "linux":
		for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			b, err := d.readFile(path)
			if err != nil {
				continue
			}
			if id := strings.TrimSpace(string(b)); id != "" {
				return id
			}
		}
		return ""
	case "windows":
		out, err := d.execOutput("reg", "query", `HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid")
		if err != nil {
			return ""
		}
		return parseMachineGUID(string(out))
	}
	return ""
}

// parseIOPlatformUUID extracts the UUID from `ioreg -rd1 -c
// IOPlatformExpertDevice` output: a line like
//
//	"IOPlatformUUID" = "1A2B3C4D-...."
func parseIOPlatformUUID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "IOPlatformUUID") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !strings.Contains(key, "IOPlatformUUID") {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	return ""
}

// parseMachineGUID extracts the guid from `reg query ... /v MachineGuid`
// output: a line like
//
//	MachineGuid    REG_SZ    1a2b3c4d-....
func parseMachineGUID(out string) string {
	fields := strings.Fields(out)
	for i, f := range fields {
		if f == "MachineGuid" && i+2 < len(fields) && strings.EqualFold(fields[i+1], "REG_SZ") {
			return fields[i+2]
		}
	}
	return ""
}

// HardwareFingerprintSource returns the OS-level machine id ("" when
// unavailable) — exported for callers (login --status, the connector) that
// must NOT generate a fresh fallback random on the spot.
func HardwareFingerprintSource() string {
	return machineID(defaultFingerprintDeps())
}

// FingerprintHash maps a fingerprint source to the ONLY form that ever leaves
// the machine: sha256 hex with a domain separator. The raw hardware id is
// never sent to the orchestrator.
func FingerprintHash(source string) string {
	sum := sha256.Sum256([]byte("jcode-device-fingerprint:" + source))
	return hex.EncodeToString(sum[:])
}

// ResolveFingerprintSource returns the stable fingerprint source for this
// machine: the persisted cloud.json value if present (stable by definition),
// else the OS machine id, else a freshly generated fallback
// ("fallback:<hostname>:<random16B>").
//
// The fallback is stable only once persisted: the caller MUST write the
// returned source into Credentials.Fingerprint when it saves the credentials
// file (both login paths do). Saving a hardware-derived source too is
// deliberate: it pins the identity against a later ioreg/machine-id failure.
func ResolveFingerprintSource() (string, error) {
	creds, err := LoadCredentials()
	if err != nil {
		return "", err
	}
	if creds != nil && creds.Fingerprint != "" {
		return creds.Fingerprint, nil
	}
	if id := HardwareFingerprintSource(); id != "" {
		return id, nil
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown-host"
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "fallback:" + hostname + ":" + hex.EncodeToString(b[:]), nil
}

// fingerprintHashForCreds is the connector's register-time fingerprint: the
// persisted source when present, the hardware id otherwise (never a fresh
// fallback — the connector does not own the credentials file, so it could not
// persist one). "" when neither exists (register then simply omits the field).
func fingerprintHashForCreds(creds *Credentials) string {
	source := creds.Fingerprint
	if source == "" {
		source = HardwareFingerprintSource()
	}
	if source == "" {
		return ""
	}
	return FingerprintHash(source)
}
