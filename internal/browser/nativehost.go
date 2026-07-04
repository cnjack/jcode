package browser

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

// NativeHostName is the Chrome Native Messaging host id the extension connects
// to via chrome.runtime.connectNative. Must match the extension's usage.
const NativeHostName = "com.jcode.bridge"

// Endpoint is what the native host hands back to the extension so it can dial
// the running jcode server without the user typing anything.
type Endpoint struct {
	WS    string `json:"ws"`
	Token string `json:"token"`
}

func endpointPath() string {
	return filepath.Join(config.ConfigDir(), "browser", "endpoint.json")
}

// WriteEndpoint persists the current server WS URL + a valid bridge token so a
// freshly-spawned native host process (a separate process from the server) can
// read it and hand it to the extension. 0600 — it grants browser control.
func WriteEndpoint(ws, token string) error {
	data, err := json.Marshal(Endpoint{WS: ws, Token: token})
	if err != nil {
		return err
	}
	p := endpointPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// ReadEndpoint loads the endpoint written by the running server.
func ReadEndpoint() (Endpoint, error) {
	var ep Endpoint
	data, err := os.ReadFile(endpointPath())
	if err != nil {
		return ep, err
	}
	return ep, json.Unmarshal(data, &ep)
}

// ---------------------------------------------------------------------------
// Native messaging stdio framing: 4-byte little-endian length + UTF-8 JSON.
// (Chrome uses native byte order; all supported desktop platforms are LE.)
// ---------------------------------------------------------------------------

const maxNativeMessage = 1 << 20 // 1 MB, Chrome's host→browser cap

func readNativeMessage(r io.Reader) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(lenBuf[:])
	if n == 0 || n > maxNativeMessage {
		return nil, fmt.Errorf("native message length out of range: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeNativeMessage(w io.Writer, data []byte) error {
	if len(data) > maxNativeMessage {
		return fmt.Errorf("native message too large: %d", len(data))
	}
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// ---------------------------------------------------------------------------
// Native host mode. Chrome launches `jcode chrome-extension://<id>/` when the
// extension calls connectNative. We detect that, read the endpoint the running
// server wrote, send it to the extension, and exit on stdin EOF.
// ---------------------------------------------------------------------------

// MaybeRunNativeHost checks argv for the native-messaging launch signature and,
// if present, runs the host loop and returns true (the caller should exit).
func MaybeRunNativeHost(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "chrome-extension://") || strings.HasPrefix(a, "extension://") {
			runNativeHost(os.Stdin, os.Stdout)
			return true
		}
	}
	return false
}

// runNativeHost sends the current endpoint immediately, then answers any request
// with the endpoint until stdin closes.
func runNativeHost(in io.Reader, out io.Writer) {
	sendEndpoint(out) // proactive: the extension can just read the first message.
	for {
		if _, err := readNativeMessage(in); err != nil {
			return // EOF / port closed
		}
		sendEndpoint(out)
	}
}

func sendEndpoint(out io.Writer) {
	ep, err := ReadEndpoint()
	var payload []byte
	if err != nil {
		payload, _ = json.Marshal(map[string]string{"error": "jcode is not running or browser use is disabled"})
	} else {
		payload, _ = json.Marshal(ep)
	}
	_ = writeNativeMessage(out, payload)
}

// ---------------------------------------------------------------------------
// Native host manifest install. macOS/Linux write a JSON file into each
// browser's NativeMessagingHosts dir; Windows writes the file + a registry key
// (see nativehost_windows.go).
// ---------------------------------------------------------------------------

// nativeHostManifest is the JSON Chrome/Edge read to find and authorize the host.
func nativeHostManifest(binPath string) []byte {
	origins := make([]string, len(AllowedExtensionIDs))
	for i, id := range AllowedExtensionIDs {
		origins[i] = fmt.Sprintf("chrome-extension://%s/", id)
	}
	m := map[string]any{
		"name":            NativeHostName,
		"description":     "jcode Browser Bridge native host",
		"path":            binPath,
		"type":            "stdio",
		"allowed_origins": origins,
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	return data
}

// browserManifestDirs returns the per-browser NativeMessagingHosts directories
// for the current user on macOS/Linux.
func browserManifestDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		return []string{
			filepath.Join(base, "Google", "Chrome", "NativeMessagingHosts"),
			filepath.Join(base, "Microsoft Edge", "NativeMessagingHosts"),
			filepath.Join(base, "Chromium", "NativeMessagingHosts"),
			filepath.Join(base, "BraveSoftware", "Brave-Browser", "NativeMessagingHosts"),
		}
	default: // linux & friends
		cfg := filepath.Join(home, ".config")
		return []string{
			filepath.Join(cfg, "google-chrome", "NativeMessagingHosts"),
			filepath.Join(cfg, "chromium", "NativeMessagingHosts"),
			filepath.Join(cfg, "microsoft-edge", "NativeMessagingHosts"),
		}
	}
}

// InstallNativeHost writes/refreshes the native-messaging host manifest so the
// extension can reach this jcode binary. Best-effort: it targets every browser
// dir it can and returns the first hard error (a missing browser dir is skipped,
// not an error). binPath should be os.Executable().
func InstallNativeHost(binPath string) error {
	manifest := nativeHostManifest(binPath)

	if runtime.GOOS == "windows" {
		// Windows: one manifest file on disk + registry keys pointing at it.
		dir := filepath.Join(config.ConfigDir(), "browser")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		manifestPath := filepath.Join(dir, NativeHostName+".json")
		if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
			return err
		}
		return registerWindowsHosts(manifestPath)
	}

	// macOS / Linux: write the manifest into each existing browser's dir. Create
	// the dir if the browser's parent config dir exists; skip browsers absent.
	var firstErr error
	for _, dir := range browserManifestDirs() {
		parent := filepath.Dir(dir)
		if _, err := os.Stat(parent); err != nil {
			continue // that browser isn't installed for this user
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, NativeHostName+".json"), manifest, 0o644); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
