package command

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func managedReauthHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	configDir := filepath.Join(home, ".jcode")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	configJSON := `{
  "model": "xai/grok-4.6",
  "providers": {
    "xai": {
      "auth": {"method": "xai_oauth", "account_id": "account-1"},
      "custom_models": [
        {"id": "grok-4.6", "managed": true, "protocol": "responses"}
      ]
    }
  },
  "memory": {"enabled": false}
}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	authJSON := `{
  "version": 1,
  "methods": {
    "xai_oauth": {
      "accounts": {
        "account-1": {
          "id": "account-1",
          "login": "reauth@example.test",
          "secret": "expired-refresh-token",
          "authenticated_at": "2026-08-09T00:00:00Z",
          "requires_reauth": true
        }
      },
      "default_account_id": "account-1"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(configDir, "provider-auth.json"), []byte(authJSON), 0o600); err != nil {
		t.Fatalf("write provider auth: %v", err)
	}
	return home
}

// Regression: a selected managed account that needs reauthentication must not
// abort the sidecar before /api/health and the provider-auth recovery endpoints
// are reachable. Chat remains fail-closed until reauthentication succeeds.
func TestRunWebServerManagedReauthKeepsControlPlaneAvailable(t *testing.T) {
	t.Setenv("HOME", managedReauthHome(t))

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

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	requireHealthOK(t, baseURL, errCh)

	statusResp, err := http.Get(baseURL + "/api/provider-auth/xai_oauth") //nolint:gosec // loopback test server
	if err != nil {
		t.Fatalf("get provider auth status: %v", err)
	}
	statusBody, readErr := io.ReadAll(io.LimitReader(statusResp.Body, 1<<20))
	_ = statusResp.Body.Close()
	if readErr != nil {
		t.Fatalf("read provider auth status: %v", readErr)
	}
	if statusResp.StatusCode != http.StatusOK || !bytes.Contains(statusBody, []byte(`"requires_reauth":true`)) {
		t.Fatalf("provider auth status=%d body=%s", statusResp.StatusCode, statusBody)
	}

	// A failed lazy rebuild must release the task's running claim. The second
	// request should retry authentication and return the same actionable error,
	// not get stuck behind a false "already processing" conflict.
	for attempt := 1; attempt <= 2; attempt++ {
		chatResp, err := http.Post( //nolint:gosec // loopback test server
			baseURL+"/api/chat", "application/json", strings.NewReader(`{"message":"hello"}`),
		)
		if err != nil {
			t.Fatalf("post chat attempt %d: %v", attempt, err)
		}
		chatBody, readErr := io.ReadAll(io.LimitReader(chatResp.Body, 1<<20))
		_ = chatResp.Body.Close()
		if readErr != nil {
			t.Fatalf("read chat response attempt %d: %v", attempt, readErr)
		}
		if chatResp.StatusCode != http.StatusServiceUnavailable ||
			!bytes.Contains(chatBody, []byte("requires reauthentication")) {
			t.Fatalf("chat attempt %d status=%d body=%s", attempt, chatResp.StatusCode, chatBody)
		}
	}

	cancel()
	select {
	case runErr := <-errCh:
		if runErr != nil {
			t.Fatalf("runWebServer returned error: %v", runErr)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("web server did not shut down after context cancel")
	}
}
