package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

type cloudStatusJSON struct {
	LoggedIn    bool   `json:"logged_in"`
	AutoConnect bool   `json:"auto_connect"`
	State       string `json:"state"`
	DeviceName  string `json:"device_name"`
	CloudURL    string `json:"cloud_url"`
	Error       string `json:"error"`
}

func postCloudConfig(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleCloudConfig(rec, httptest.NewRequest(http.MethodPost, "/api/cloud/config", strings.NewReader(body)))
	return rec
}

// Nil supervisor (headless/test wiring): status is synthesized from the
// on-disk credentials + config with state "offline".
func TestCloudStatusNilSupervisor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{cfg: &config.Config{}}

	rec := httptest.NewRecorder()
	s.handleCloudStatus(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var st cloudStatusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.LoggedIn || st.State != "offline" || !st.AutoConnect {
		t.Fatalf("status = %+v, want logged_in=false auto_connect=true state=offline", st)
	}
	if st.CloudURL != cloud.DefaultCloudURL {
		t.Errorf("cloud_url = %q, want %q", st.CloudURL, cloud.DefaultCloudURL)
	}
	if st.DeviceName == "" {
		t.Error("device_name must fall back to the OS hostname")
	}
	if strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("empty error must be omitted: %s", rec.Body.String())
	}
}

// A wired supervisor drives both the status payload and the toggle.
func TestCloudStatusFromSupervisor(t *testing.T) {
	fake := &fakeCloudSupervisor{status: cloud.Status{
		LoggedIn:    true,
		AutoConnect: true,
		State:       "error",
		DeviceName:  "testbox",
		CloudURL:    "https://cloud.example.com",
		Error:       "dial tcp: connection refused",
	}}
	s := &Server{cfg: &config.Config{}, cloudSupervisor: fake}

	rec := httptest.NewRecorder()
	s.handleCloudStatus(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/status", nil))
	var st cloudStatusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.LoggedIn || st.State != "error" || st.Error != "dial tcp: connection refused" ||
		st.DeviceName != "testbox" || st.CloudURL != "https://cloud.example.com" {
		t.Fatalf("status = %+v, want the supervisor's snapshot", st)
	}
}

func TestCloudConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := &config.Config{}
	sup := cloud.NewSupervisor(cfg, 8080, "")
	s := &Server{cfg: cfg, cloudSupervisor: sup}

	// Validation: missing field, unknown field.
	for _, body := range []string{`{}`, `{"auto_connect":true,"bogus":1}`, `not json`} {
		if rec := postCloudConfig(t, s, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status=%d, want 400", body, rec.Code)
		}
	}

	// POST false persists and is reflected in the returned status.
	rec := postCloudConfig(t, s, `{"auto_connect":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rec.Code, rec.Body.String())
	}
	var st cloudStatusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.AutoConnect || st.State != "offline" {
		t.Fatalf("returned status = %+v, want auto_connect=false state=offline", st)
	}
	if config.CloudAutoConnect(cfg) {
		t.Fatal("live config not updated")
	}
	if got := readPersistedCloudAutoConnect(t, home); got == nil || *got {
		t.Fatalf("persisted auto_connect = %v, want false", got)
	}

	// POST true again: round-trips back. Still offline (no credentials → the
	// hot start stays idle).
	rec = postCloudConfig(t, s, `{"auto_connect":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !config.CloudAutoConnect(cfg) {
		t.Fatal("live config not re-enabled")
	}
	if got := readPersistedCloudAutoConnect(t, home); got == nil || !*got {
		t.Fatalf("persisted auto_connect = %v, want true", got)
	}
}

// A SaveConfig failure returns 500 and leaves the in-memory config untouched
// (rollback happens inside SetAutoConnect).
func TestCloudConfigSaveFailureRollsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	s := &Server{cfg: cfg, cloudSupervisor: cloud.NewSupervisor(cfg, 8080, "")}

	rec := postCloudConfig(t, s, `{"auto_connect":false}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s, want 500", rec.Code, rec.Body.String())
	}
	if !config.CloudAutoConnect(cfg) {
		t.Fatal("failed save changed the live auto_connect value")
	}
	if got := cfg.CloudSettings(); got != (config.CloudConfig{}) {
		t.Fatalf("failed save replaced an absent Cloud block: %+v", got)
	}
}

// Nil-supervisor POST persists directly (no live connector to hot-apply).
func TestCloudConfigNilSupervisorPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := &config.Config{}
	s := &Server{cfg: cfg}

	rec := postCloudConfig(t, s, `{"auto_connect":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if config.CloudAutoConnect(cfg) {
		t.Fatal("live config not updated")
	}
	if got := readPersistedCloudAutoConnect(t, home); got == nil || *got {
		t.Fatalf("persisted auto_connect = %v, want false", got)
	}
}

// A supervisor error (e.g. SaveConfig failure) surfaces as 500.
func TestCloudConfigSupervisorError(t *testing.T) {
	fake := &fakeCloudSupervisor{err: errors.New("save failed")}
	s := &Server{cfg: &config.Config{}, cloudSupervisor: fake}
	rec := postCloudConfig(t, s, `{"auto_connect":true}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s, want 500", rec.Code, rec.Body.String())
	}
	if len(fake.setCalls) != 1 || !fake.setCalls[0] {
		t.Fatalf("SetAutoConnect calls = %v, want [true]", fake.setCalls)
	}
}

type fakeCloudSupervisor struct {
	status          cloud.Status
	err             error
	setCalls        []bool
	configSyncCalls []bool

	syncCalls  atomic.Int32
	pairings   []cloud.Pairing
	lastPaired *cloud.PairedInfo
	pairingErr error
	approved   []string
	denied     []string
	revoked    []string
}

func (f *fakeCloudSupervisor) Status() cloud.Status { return f.status }
func (f *fakeCloudSupervisor) SetAutoConnect(enabled bool) error {
	f.setCalls = append(f.setCalls, enabled)
	return f.err
}
func (f *fakeCloudSupervisor) SetConfigSync(enabled bool) error {
	f.configSyncCalls = append(f.configSyncCalls, enabled)
	return f.err
}
func (f *fakeCloudSupervisor) SyncCredentials() { f.syncCalls.Add(1) }
func (f *fakeCloudSupervisor) PairingRecords(_ context.Context) ([]cloud.Pairing, error) {
	return f.pairings, f.pairingErr
}
func (f *fakeCloudSupervisor) ApprovePairing(_ context.Context, id string) error {
	f.approved = append(f.approved, id)
	return f.pairingErr
}
func (f *fakeCloudSupervisor) DenyPairing(_ context.Context, id string) error {
	f.denied = append(f.denied, id)
	return f.pairingErr
}
func (f *fakeCloudSupervisor) RevokePairing(_ context.Context, id string) error {
	f.revoked = append(f.revoked, id)
	return f.pairingErr
}
func (f *fakeCloudSupervisor) SyncAccountSettings(_ context.Context) error { return nil }
func (f *fakeCloudSupervisor) SyncProviderConfigs(_ context.Context) error { return nil }
func (f *fakeCloudSupervisor) AccountSyncKeyStatus(_ context.Context) (*cloud.AccountSyncKeyState, error) {
	return &cloud.AccountSyncKeyState{State: "ready", KeyGen: 1}, nil
}
func (f *fakeCloudSupervisor) PendingAccountSyncDevices(_ context.Context) ([]cloud.AccountSyncKeyRequest, error) {
	return nil, nil
}
func (f *fakeCloudSupervisor) ApprovedAccountSyncDevices(_ context.Context) ([]cloud.AccountSyncKeyRequest, error) {
	return nil, nil
}
func (f *fakeCloudSupervisor) ApproveAccountSyncDevice(_ context.Context, _ string) error {
	return nil
}
func (f *fakeCloudSupervisor) DenyAccountSyncDevice(_ context.Context, _ string) error {
	return nil
}
func (f *fakeCloudSupervisor) RevokeAccountSyncDevice(_ context.Context, _ string) error {
	return nil
}
func (f *fakeCloudSupervisor) LastPaired() (cloud.PairedInfo, bool) {
	if f.lastPaired == nil {
		return cloud.PairedInfo{}, false
	}
	return *f.lastPaired, true
}

func readPersistedCloudAutoConnect(t *testing.T, home string) *bool {
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
