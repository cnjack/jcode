package cloud

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// machineIDTable covers every OS branch of machineID with mocked exec/file
// reads — no real ioreg/reg/machine-id involved.
func TestMachineID(t *testing.T) {
	failExec := func(string, ...string) ([]byte, error) { return nil, errors.New("exec failed") }
	failRead := func(string) ([]byte, error) { return nil, errors.New("read failed") }

	tests := []struct {
		name string
		deps fingerprintDeps
		want string
	}{
		{
			name: "darwin parses IOPlatformUUID",
			deps: fingerprintDeps{
				goos: "darwin",
				execOutput: func(string, ...string) ([]byte, error) {
					return []byte("+-o Root  <class IORegistryEntry>\n  \"IOPlatformUUID\" = \"1A2B3C4D-5E6F-7788-99AA-BBCCDDEEFF00\"\n"), nil
				},
			},
			want: "1A2B3C4D-5E6F-7788-99AA-BBCCDDEEFF00",
		},
		{
			name: "darwin ioreg failure",
			deps: fingerprintDeps{goos: "darwin", execOutput: failExec},
			want: "",
		},
		{
			name: "darwin output without the key",
			deps: fingerprintDeps{
				goos:       "darwin",
				execOutput: func(string, ...string) ([]byte, error) { return []byte("nothing here\n"), nil },
			},
			want: "",
		},
		{
			name: "linux reads /etc/machine-id",
			deps: fingerprintDeps{
				goos: "linux",
				readFile: func(path string) ([]byte, error) {
					if path != "/etc/machine-id" {
						t.Fatalf("readFile(%q), want /etc/machine-id first", path)
					}
					return []byte("a1b2c3d4e5f60718293a4b5c6d7e8f90\n"), nil
				},
			},
			want: "a1b2c3d4e5f60718293a4b5c6d7e8f90",
		},
		{
			name: "linux falls back to the dbus machine-id",
			deps: fingerprintDeps{
				goos: "linux",
				readFile: func(path string) ([]byte, error) {
					if path == "/var/lib/dbus/machine-id" {
						return []byte("deadbeef\n"), nil
					}
					return nil, errors.New("missing")
				},
			},
			want: "deadbeef",
		},
		{
			name: "linux empty machine-id keeps looking",
			deps: fingerprintDeps{
				goos: "linux",
				readFile: func(path string) ([]byte, error) {
					if path == "/var/lib/dbus/machine-id" {
						return []byte("cafe\n"), nil
					}
					return []byte("  \n"), nil
				},
			},
			want: "cafe",
		},
		{
			name: "linux unreadable",
			deps: fingerprintDeps{goos: "linux", readFile: failRead},
			want: "",
		},
		{
			name: "windows parses MachineGuid",
			deps: fingerprintDeps{
				goos: "windows",
				execOutput: func(string, ...string) ([]byte, error) {
					return []byte("\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\Cryptography\r\n    MachineGuid    REG_SZ    9f8e7d6c-5b4a-3928-1706-050403020100\r\n"), nil
				},
			},
			want: "9f8e7d6c-5b4a-3928-1706-050403020100",
		},
		{
			name: "windows reg failure",
			deps: fingerprintDeps{goos: "windows", execOutput: failExec},
			want: "",
		},
		{
			name: "unsupported platform",
			deps: fingerprintDeps{goos: "plan9"},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := machineID(tt.deps); got != tt.want {
				t.Fatalf("machineID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFingerprintHash(t *testing.T) {
	h := FingerprintHash("some-source")
	if len(h) != 64 {
		t.Fatalf("FingerprintHash() = %q, want 64 hex chars", h)
	}
	for _, c := range h {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("FingerprintHash() = %q, not lowercase hex", h)
		}
	}
	if FingerprintHash("some-source") != h {
		t.Fatal("FingerprintHash() is not deterministic")
	}
	if FingerprintHash("other-source") == h {
		t.Fatal("FingerprintHash() collides for different sources")
	}
}

// TestResolveFingerprintSourcePersisted: a fingerprint already stored in
// cloud.json always wins — even over the hardware id — so the device identity
// survives an ioreg/machine-id failure (M16: 一旦生成不再变).
func TestResolveFingerprintSourcePersisted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SaveCredentials(&Credentials{
		CloudURL: "https://cloud.example.com", DeviceID: "d1", DeviceToken: "t",
		Fingerprint: "persisted-source",
	}); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}
	src, err := ResolveFingerprintSource()
	if err != nil {
		t.Fatalf("ResolveFingerprintSource() error = %v", err)
	}
	if src != "persisted-source" {
		t.Fatalf("ResolveFingerprintSource() = %q, want the persisted value", src)
	}
}

// TestResolveFingerprintSourceStableAcrossCalls: without a persisted value the
// source is either the (stable) hardware id or a fallback that becomes stable
// once the caller persists it — the login flow's contract.
func TestResolveFingerprintSourceStableAcrossCalls(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	first, err := ResolveFingerprintSource()
	if err != nil {
		t.Fatalf("ResolveFingerprintSource() error = %v", err)
	}
	if first == "" {
		t.Fatal("ResolveFingerprintSource() = empty")
	}
	if HardwareFingerprintSource() != "" {
		// Hardware id present: resolution is stable without persisting.
		second, _ := ResolveFingerprintSource()
		if second != first {
			t.Fatalf("hardware-derived source changed: %q -> %q", first, second)
		}
		return
	}
	// Fallback path (no hardware id, e.g. a VM without machine-id): the caller
	// persists the generated source, and every later call returns it.
	if !strings.HasPrefix(first, "fallback:") {
		t.Fatalf("fallback source = %q, want fallback: prefix", first)
	}
	if err := SaveCredentials(&Credentials{
		CloudURL: "https://cloud.example.com", DeviceID: "d1", DeviceToken: "t",
		Fingerprint: first,
	}); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}
	second, err := ResolveFingerprintSource()
	if err != nil {
		t.Fatalf("ResolveFingerprintSource() error = %v", err)
	}
	if second != first {
		t.Fatalf("persisted fallback not reused: %q -> %q", first, second)
	}
}

// TestFingerprintRoundTripThroughCredentials: the fingerprint field survives
// the atomic save/load cycle and lands in the JSON file.
func TestFingerprintRoundTripThroughCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	creds := &Credentials{
		CloudURL: "https://cloud.example.com", DeviceID: "d1", DeviceToken: "tok",
		DeviceName: "n", PublicKey: "pk", PrivateKey: "sk", KeyGen: 1,
		Fingerprint: "fp-source",
	}
	if err := SaveCredentials(creds); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".jcode", "cloud.json"))
	if err != nil {
		t.Fatalf("read cloud.json: %v", err)
	}
	if !strings.Contains(string(raw), `"fingerprint": "fp-source"`) {
		t.Fatalf("cloud.json missing fingerprint field: %s", raw)
	}
	got, err := LoadCredentials()
	if err != nil || got == nil {
		t.Fatalf("LoadCredentials() = %+v, %v", got, err)
	}
	if got.Fingerprint != "fp-source" {
		t.Fatalf("loaded fingerprint = %q, want fp-source", got.Fingerprint)
	}
}

func TestFingerprintHashForCreds(t *testing.T) {
	// Persisted source wins.
	if got, want := fingerprintHashForCreds(&Credentials{Fingerprint: "src"}), FingerprintHash("src"); got != want {
		t.Fatalf("fingerprintHashForCreds(persisted) = %q, want %q", got, want)
	}
	// No persisted source: hardware id when available, "" otherwise.
	hw := HardwareFingerprintSource()
	got := fingerprintHashForCreds(&Credentials{})
	if hw == "" && got != "" {
		t.Fatalf("fingerprintHashForCreds(no hw) = %q, want empty", got)
	}
	if hw != "" && got != FingerprintHash(hw) {
		t.Fatalf("fingerprintHashForCreds(hw) = %q, want hash of %q", got, hw)
	}
}
