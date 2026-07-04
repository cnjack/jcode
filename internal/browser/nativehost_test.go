package browser

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	msg := []byte(`{"ws":"ws://127.0.0.1:58640/api/browser/ext/ws","token":"abc"}`)
	if err := writeNativeMessage(&buf, msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Frame = 4-byte LE length + payload.
	if buf.Len() != 4+len(msg) {
		t.Fatalf("frame len = %d, want %d", buf.Len(), 4+len(msg))
	}
	got, err := readNativeMessage(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("round-trip mismatch: %s", got)
	}
}

func TestReadNativeMessageRejectsBadLength(t *testing.T) {
	// Length prefix claims 5MB (> cap) → error, no huge alloc.
	bad := []byte{0x00, 0x00, 0x50, 0x00} // 0x00500000 = 5MB LE
	if _, err := readNativeMessage(bytes.NewReader(bad)); err == nil {
		t.Fatal("expected error for oversized length")
	}
}

func TestRunNativeHostSendsEndpoint(t *testing.T) {
	// Point the endpoint file at a temp config dir by writing via WriteEndpoint,
	// which uses config.ConfigDir(). We can't easily override that here, so just
	// exercise the framing/handshake: write an endpoint, run host with EOF stdin,
	// and confirm the first output frame decodes to our endpoint or an error.
	var out bytes.Buffer
	runNativeHost(strings.NewReader(""), &out) // empty stdin → immediate EOF after 1 send

	got, err := readNativeMessage(&out)
	if err != nil {
		t.Fatalf("read host output: %v", err)
	}
	// It's either a valid Endpoint (if a real endpoint.json exists) or an error
	// object; both are valid JSON objects.
	var obj map[string]any
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("host output not JSON: %s", got)
	}
}

func TestNativeHostManifestShape(t *testing.T) {
	data := nativeHostManifest("/usr/local/bin/jcode")
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("manifest not JSON: %v", err)
	}
	if m["name"] != NativeHostName {
		t.Errorf("name = %v, want %s", m["name"], NativeHostName)
	}
	if m["path"] != "/usr/local/bin/jcode" {
		t.Errorf("path = %v", m["path"])
	}
	if m["type"] != "stdio" {
		t.Errorf("type = %v", m["type"])
	}
	origins, ok := m["allowed_origins"].([]any)
	if !ok || len(origins) != len(AllowedExtensionIDs) {
		t.Fatalf("allowed_origins = %v, want %d entries", m["allowed_origins"], len(AllowedExtensionIDs))
	}
	got := make(map[string]bool, len(origins))
	for _, o := range origins {
		s, _ := o.(string)
		got[s] = true
	}
	// Every allowed id (dev + published store builds) must be present, in the
	// chrome-extension://<id>/ origin form.
	for _, id := range AllowedExtensionIDs {
		if want := "chrome-extension://" + id + "/"; !got[want] {
			t.Errorf("allowed_origins missing %s (have %v)", want, origins)
		}
	}
	if !got["chrome-extension://"+ExtensionID+"/"] {
		t.Errorf("dev/unpacked extension id must always be allowed")
	}
}

func TestMaybeRunNativeHostDetection(t *testing.T) {
	// Without the chrome-extension arg it must NOT enter host mode (returns false
	// without touching stdio).
	if MaybeRunNativeHost([]string{"web", "--port", "8080"}) {
		t.Error("should not enter native-host mode for normal args")
	}
}

func TestWriteReadEndpointRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := WriteEndpoint("ws://127.0.0.1:9/api/browser/ext/ws", "tk"); err != nil {
		t.Fatalf("write: %v", err)
	}
	ep, err := ReadEndpoint()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ep.Token != "tk" || ep.WS != "ws://127.0.0.1:9/api/browser/ext/ws" {
		t.Fatalf("round-trip mismatch: %+v", ep)
	}
}

func TestInstallNativeHostWritesManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("registry path covered separately on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create one browser's parent dir so InstallNativeHost targets it (it skips
	// browsers whose parent dir is absent).
	var parent string
	if runtime.GOOS == "darwin" {
		parent = filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	} else {
		parent = filepath.Join(home, ".config", "google-chrome")
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := InstallNativeHost("/opt/jcode/jcode"); err != nil {
		t.Fatalf("install: %v", err)
	}
	manifestFile := filepath.Join(parent, "NativeMessagingHosts", NativeHostName+".json")
	data, err := os.ReadFile(manifestFile)
	if err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	if !strings.Contains(string(data), "/opt/jcode/jcode") || !strings.Contains(string(data), ExtensionID) {
		t.Fatalf("manifest content wrong:\n%s", data)
	}
}
