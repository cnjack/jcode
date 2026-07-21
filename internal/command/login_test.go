package command

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

// loginTestServer implements the orchestrator-side device-auth contract:
// immediate approval on the first token poll, plus register and revoke.
func loginTestServer(t *testing.T, registered *bool, revoked *int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dev-code-1",
			"user_code":        "WXYZ-1234",
			"verification_uri": "http://example.com/auth/device",
			"expires_in":       60,
			"interval":         1,
		})
	})
	mux.HandleFunc("POST /auth/device/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "dev-token-1",
			"token_type":   "device",
			"device_id":    "device-7",
		})
	})
	mux.HandleFunc("POST /internal/v1/device/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer dev-token-1" {
			t.Errorf("register: Authorization = %q", r.Header.Get("Authorization"))
		}
		var req cloud.RegisterDeviceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("register: decode body: %v", err)
		}
		if req.Name != "test-device" || req.PubKey == "" || req.JcodeVersion == "" {
			t.Errorf("register: unexpected body %+v", req)
		}
		// M16: the register body carries the sha256 machine-fingerprint hash.
		if len(req.Fingerprint) != 64 {
			t.Errorf("register: fingerprint = %q, want 64 hex chars", req.Fingerprint)
		}
		*registered = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("POST /internal/v1/device/revoke", func(w http.ResponseWriter, r *http.Request) {
		*revoked++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	return httptest.NewServer(mux)
}

// stubOpenBrowser swaps the browser opener for a no-op while the test runs.
func stubOpenBrowser(t *testing.T) {
	t.Helper()
	orig := openBrowser
	openBrowser = func(string) error { return nil }
	t.Cleanup(func() { openBrowser = orig })
}

func TestRunLoginEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stubOpenBrowser(t)

	var registered bool
	var revoked int
	srv := loginTestServer(t, &registered, &revoked)
	defer srv.Close()

	if err := runLogin(context.Background(), srv.URL, "test-device"); err != nil {
		t.Fatalf("runLogin() error = %v", err)
	}
	if !registered {
		t.Fatal("device register endpoint was not called")
	}

	creds, err := cloud.LoadCredentials()
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if creds == nil {
		t.Fatal("LoadCredentials() = nil after login")
	}
	if creds.CloudURL != srv.URL || creds.DeviceID != "device-7" || creds.DeviceToken != "dev-token-1" || creds.DeviceName != "test-device" {
		t.Fatalf("credentials = %+v", creds)
	}
	if creds.PublicKey == "" || creds.PrivateKey == "" || creds.KeyGen != 1 {
		t.Fatalf("credentials keys = %+v", creds)
	}
	// M16: the fingerprint SOURCE is persisted (never sent over the wire) and
	// a second login attempt reuses it.
	if creds.Fingerprint == "" {
		t.Fatalf("credentials missing fingerprint: %+v", creds)
	}
	fpSource := creds.Fingerprint

	// The config block must be enabled and point at the cloud URL. Read the
	// file raw: LoadConfig requires providers, which login does not.
	raw, err := readRawConfig(t)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cloudBlock, ok := raw["cloud"].(map[string]any)
	if !ok {
		t.Fatalf("config has no cloud block: %v", raw)
	}
	if cloudBlock["enabled"] != true || cloudBlock["url"] != srv.URL {
		t.Fatalf("config cloud block = %v", cloudBlock)
	}

	// Logging in again without logout is a no-op.
	if err := runLogin(context.Background(), srv.URL, "test-device"); err != nil {
		t.Fatalf("runLogin() again error = %v", err)
	}
	// The no-op re-login must not touch the persisted fingerprint.
	creds, err = cloud.LoadCredentials()
	if err != nil || creds == nil || creds.Fingerprint != fpSource {
		t.Fatalf("fingerprint changed after no-op re-login: %+v, %v", creds, err)
	}

	if err := runLogout(context.Background()); err != nil {
		t.Fatalf("runLogout() error = %v", err)
	}
	if revoked != 1 {
		t.Fatalf("revoke called %d times, want 1", revoked)
	}
	creds, err = cloud.LoadCredentials()
	if err != nil || creds != nil {
		t.Fatalf("LoadCredentials() after logout = %+v, %v; want nil, nil", creds, err)
	}
	raw, err = readRawConfig(t)
	if err != nil {
		t.Fatalf("read config after logout: %v", err)
	}
	// enabled=false is omitted from the JSON by `omitempty`; absent means false.
	if block, ok := raw["cloud"].(map[string]any); !ok || block["enabled"] == true {
		t.Fatalf("cloud block after logout = %v", raw["cloud"])
	}
}

func TestRunLoginStatusNotLoggedIn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := runLoginStatus(); err != nil {
		t.Fatalf("runLoginStatus() error = %v", err)
	}
}

func TestRunLogoutNotLoggedIn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := runLogout(context.Background()); err != nil {
		t.Fatalf("runLogout() error = %v", err)
	}
}

func TestRunLoginRejectsInsecureURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := runLogin(context.Background(), "http://cloud.example.com", ""); err == nil {
		t.Fatal("runLogin() with http URL succeeded, want validation error")
	}
}

// readRawConfig parses ~/.jcode/config.json without LoadConfig's provider
// validation.
func readRawConfig(t *testing.T) (map[string]any, error) {
	t.Helper()
	data, err := os.ReadFile(config.ConfigPath())
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
