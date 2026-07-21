package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

// supervisorTestCreds writes a logged-in device identity to the temp HOME.
// CloudURL points at an unreachable loopback address so any started connector
// exercises the offline-retry path without network access.
func supervisorTestCreds(t *testing.T) {
	t.Helper()
	creds := &Credentials{
		CloudURL:    "http://127.0.0.1:1",
		DeviceID:    "dev-1",
		DeviceToken: "tok",
		DeviceName:  "testbox",
	}
	if err := SaveCredentials(creds); err != nil {
		t.Fatal(err)
	}
}

func readPersistedAutoConnect(t *testing.T, home string) *bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".jcode", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk struct {
		Cloud *struct {
			AutoConnect *bool `json:"auto_connect"`
		} `json:"cloud"`
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Cloud == nil {
		return nil
	}
	return disk.Cloud.AutoConnect
}

// M9-1 scenario 1: no cloud.json → not logged in, offline, no connector.
func TestSupervisorStatusNoCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sup := NewSupervisor(&config.Config{}, 8080, "")
	sup.Start(context.Background())

	sup.mu.Lock()
	started := sup.conn != nil
	sup.mu.Unlock()
	if started {
		t.Fatal("connector started without credentials")
	}

	st := sup.Status()
	if st.LoggedIn || st.State != StateOffline || st.Error != "" {
		t.Fatalf("status = %+v, want logged_in=false state=offline", st)
	}
	if !st.AutoConnect {
		t.Error("auto_connect must default to true when unset")
	}
	if st.CloudURL != DefaultCloudURL {
		t.Errorf("cloud_url = %q, want %q", st.CloudURL, DefaultCloudURL)
	}
	if st.DeviceName == "" {
		t.Error("device_name must fall back to the OS hostname")
	}
}

// M9-1 scenario 2: logged in but auto_connect=false → offline, no connector.
func TestSupervisorStatusAutoConnectDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	supervisorTestCreds(t)
	off := false
	cfg := &config.Config{}
	cfg.SetCloud(&config.CloudConfig{Enabled: true, URL: "https://cloud.example.com", AutoConnect: &off})

	sup := NewSupervisor(cfg, 8080, "")
	sup.Start(context.Background())

	sup.mu.Lock()
	started := sup.conn != nil
	sup.mu.Unlock()
	if started {
		t.Fatal("connector started despite auto_connect=false")
	}

	st := sup.Status()
	if !st.LoggedIn || st.AutoConnect || st.State != StateOffline {
		t.Fatalf("status = %+v, want logged_in=true auto_connect=false state=offline", st)
	}
	if st.DeviceName != "testbox" {
		t.Errorf("device_name = %q, want testbox (from credentials)", st.DeviceName)
	}
	if st.CloudURL != "https://cloud.example.com" {
		t.Errorf("cloud_url = %q, want config.cloud.url", st.CloudURL)
	}
}

// SetAutoConnect persists the toggle and hot-applies it: enabling starts the
// connector without a restart, disabling stops it. M9-1 scenario 3: the cloud
// is unreachable — start must not block and the error must surface in Status.
func TestSupervisorSetAutoConnectHotApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	supervisorTestCreds(t)
	off := false
	cfg := &config.Config{}
	// E2EE off keeps the test hermetic (no CEK written to the temp HOME).
	cfg.SetCloud(&config.CloudConfig{Enabled: true, AutoConnect: &off, E2EE: &off})

	sup := NewSupervisor(cfg, 8080, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup.Start(ctx)
	sup.mu.Lock()
	started := sup.conn != nil
	sup.mu.Unlock()
	if started {
		t.Fatal("connector started despite auto_connect=false")
	}

	// Enable: persists + starts the connector synchronously (the register
	// retry loop runs in the background against the unreachable cloud).
	if err := sup.SetAutoConnect(true); err != nil {
		t.Fatalf("SetAutoConnect(true): %v", err)
	}
	if got := readPersistedAutoConnect(t, home); got == nil || !*got {
		t.Fatalf("persisted auto_connect = %v, want true", got)
	}
	sup.mu.Lock()
	conn := sup.conn
	sup.mu.Unlock()
	if conn == nil {
		t.Fatal("enable did not start the connector")
	}
	waitFor(t, func() bool {
		st := sup.Status()
		return st.State == StateError && st.Error != ""
	}, "unreachable cloud surfaces state=error with a message")

	// Disable: persists + stops the connector; status falls back to offline.
	if err := sup.SetAutoConnect(false); err != nil {
		t.Fatalf("SetAutoConnect(false): %v", err)
	}
	if got := readPersistedAutoConnect(t, home); got == nil || *got {
		t.Fatalf("persisted auto_connect = %v, want false", got)
	}
	sup.mu.Lock()
	stopped := sup.conn == nil
	sup.mu.Unlock()
	if !stopped {
		t.Fatal("disable did not stop the connector")
	}
	if st := sup.Status(); st.State != StateOffline || st.Error != "" {
		t.Fatalf("status after disable = %+v, want offline", st)
	}

	// Re-enable: hot start again without a process restart.
	if err := sup.SetAutoConnect(true); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	sup.mu.Lock()
	restarted := sup.conn != nil
	sup.mu.Unlock()
	if !restarted {
		t.Fatal("re-enable did not restart the connector")
	}
}

// A SaveConfig failure must roll the in-memory config back and return the
// error (the web layer turns it into a 500).
func TestSupervisorSetAutoConnectSaveFailureRollsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Make ~/.jcode a regular file so SaveConfig cannot create config.json.
	if err := os.WriteFile(filepath.Join(home, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	sup := NewSupervisor(cfg, 8080, "")

	if err := sup.SetAutoConnect(false); err == nil {
		t.Fatal("want SaveConfig error")
	}
	if !config.CloudAutoConnect(cfg) {
		t.Fatal("failed save changed the live auto_connect value")
	}
	if got := cfg.CloudSettings(); got != (config.CloudConfig{}) {
		t.Fatalf("failed save replaced an absent Cloud block: %+v", got)
	}
}

// The register request carries the platform marker: desktop when the Tauri
// sidecar sets JCODE_DESKTOP=1, cli otherwise.
func TestDetectPlatform(t *testing.T) {
	if v, ok := os.LookupEnv("JCODE_DESKTOP"); ok {
		t.Skipf("JCODE_DESKTOP=%s set in the test environment", v)
	}
	if got := detectPlatform(); got != "cli" {
		t.Errorf("detectPlatform() = %q, want cli", got)
	}
	t.Setenv("JCODE_DESKTOP", "1")
	if got := detectPlatform(); got != "desktop" {
		t.Errorf("detectPlatform() = %q, want desktop", got)
	}
}

func TestRegisterLoopSendsPlatform(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("JCODE_DESKTOP", "1")
	var got RegisterDeviceRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	conn := newTestConnector(srv.URL, "http://127.0.0.1:1")
	if err := conn.registerLoop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got.Platform != "desktop" {
		t.Fatalf("register platform = %q, want desktop", got.Platform)
	}
	if state, _ := conn.Status(); state != StateOnline {
		t.Fatalf("state after successful register = %q, want online", state)
	}
}

// The register request reports the connector's ACTUAL encryption state as
// `e2ee` (M13): true only with an active CEK cipher and no cloud.e2ee
// disable — the orchestrator gates plaintext downlink on this flag.
func TestRegisterLoopSendsE2EE(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cek := make([]byte, 32)
	cipher, err := NewEnvelopeCipher(cek, 1)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		cipher   *EnvelopeCipher
		disabled bool
		want     bool
	}{
		{"cipher active", cipher, false, true},
		{"cipher but cloud.e2ee=false", cipher, true, false},
		{"no cipher (CEK not initialized)", nil, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got RegisterDeviceRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&got)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)

			conn := newTestConnector(srv.URL, "http://127.0.0.1:1")
			conn.cipher = tc.cipher
			conn.cfg.CipherDisabled = tc.disabled
			if err := conn.registerLoop(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got.E2EE != tc.want {
				t.Fatalf("register e2ee = %v, want %v", got.E2EE, tc.want)
			}
		})
	}
}

// Connector status transitions: connecting/error while the cloud is
// unreachable, offline after ctx cancel (graceful stop).
func TestConnectorStatusTransitions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	conn := newTestConnector("http://127.0.0.1:1", "http://127.0.0.1:1")
	if state, _ := conn.Status(); state != StateOffline {
		t.Fatalf("state before Run = %q, want offline", state)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { conn.Run(ctx); close(done) }()

	waitFor(t, func() bool {
		state, lastErr := conn.Status()
		return state == StateError && lastErr != ""
	}, "register failure surfaces state=error")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly after ctx cancel")
	}
	waitFor(t, func() bool {
		state, _ := conn.Status()
		return state == StateOffline
	}, "state=offline after stop")
}
