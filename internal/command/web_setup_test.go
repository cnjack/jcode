package command

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupModeHome returns a temp HOME that puts the process into setup mode:
// either no config file at all, or a config file with no providers. Both make
// NeedsSetup() true AND LoadConfig() fail — the combination that regressed
// first-run boot when buildWebTask started hard-failing on LoadConfig errors.
func setupModeHome(t *testing.T, writeEmptyConfig bool) string {
	t.Helper()
	// Not t.TempDir(): the bootstrap engine starts the background memory
	// pipeline even in setup mode (memory generation defaults on), and its
	// git writes under $HOME/.jcode/memory can race directory removal at test
	// end. Retry the removal instead of flaking on ENOTEMPTY.
	home, err := os.MkdirTemp("", "jcode-setup-home")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() {
		for range 100 {
			if err := os.RemoveAll(home); err == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Logf("cleanup: could not remove %s (background memory pipeline still writing?)", home)
	})
	if writeEmptyConfig {
		dir := filepath.Join(home, ".jcode")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"providers":{}}`), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	return home
}

// requireHealthOK polls GET /api/health until it returns 200, the server exits
// early (the regression: bootstrap aborts before listening), or the deadline
// passes.
func requireHealthOK(t *testing.T, baseURL string, errCh <-chan error) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(baseURL + "/api/health") //nolint:gosec // loopback test server
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case rerr := <-errCh:
			t.Fatalf("web server exited before serving /api/health: %v", rerr)
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("web server did not start in setup mode")
		}
	}
}

// Regression: the web server must boot and serve requests in setup mode (no
// usable config on disk) so the user can complete setup in the settings UI.
// buildWebTask's per-task LoadConfig hard-fail made the bootstrap engine error
// abort runWebServer before it ever listened, so first-run users could never
// reach the setup page.
func TestRunWebServerSetupModeBoots(t *testing.T) {
	for _, tc := range []struct {
		name       string
		emptyConfi bool
	}{
		{"no config file", false},
		{"config without providers", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", setupModeHome(t, tc.emptyConfi))

			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("reserve port: %v", err)
			}
			port := ln.Addr().(*net.TCPAddr).Port
			_ = ln.Close()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			errCh := make(chan error, 1)
			go func() { errCh <- runWebServer(ctx, port, "127.0.0.1", false, "") }()

			requireHealthOK(t, fmt.Sprintf("http://127.0.0.1:%d", port), errCh)

			cancel()
			select {
			case rerr := <-errCh:
				if rerr != nil {
					t.Fatalf("runWebServer returned error: %v", rerr)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("web server did not shut down after context cancel")
			}
		})
	}
}
