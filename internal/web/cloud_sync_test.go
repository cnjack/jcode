package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

type cloudSyncJSON struct {
	SyncDefault bool            `json:"sync_default"`
	Sessions    map[string]bool `json:"sessions"`
}

// GET /api/cloud/sync on a fresh install: default OFF, no per-session entries.
func TestCloudSyncGetDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{cfg: &config.Config{}}

	rec := httptest.NewRecorder()
	s.handleCloudSync(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/sync", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got cloudSyncJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SyncDefault {
		t.Error("sync_default must default to false (nothing syncs by default)")
	}
	if len(got.Sessions) != 0 {
		t.Errorf("sessions = %v, want empty", got.Sessions)
	}
}

// POST /api/cloud/sync/{id} persists the per-session switch to
// ~/.jcode/cloud-sessions.json (0600) and GET reflects it.
func TestCloudSyncSessionSetPersists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{cfg: &config.Config{}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/cloud/sync/s1", strings.NewReader(`{"enabled":true}`))
	req.SetPathValue("session_id", "s1")
	s.handleCloudSyncSession(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	// On-disk state (a fresh store instance sees it — the connector's view).
	path, err := cloud.SyncStorePath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := cloud.LoadSyncStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !store.Enabled("s1") {
		t.Error("s1 must persist as enabled")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("store perm = %o, want 600", perm)
	}

	// GET reflects the entry.
	rec = httptest.NewRecorder()
	s.handleCloudSync(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/sync", nil))
	var got cloudSyncJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Sessions["s1"] {
		t.Errorf("GET sessions = %v, want s1=true", got.Sessions)
	}

	// Explicit disable persists too (an explicit false survives restart,
	// winning over any global default).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/cloud/sync/s1", strings.NewReader(`{"enabled":false}`))
	req.SetPathValue("session_id", "s1")
	s.handleCloudSyncSession(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	store, err = cloud.LoadSyncStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Enabled("s1") || !store.Has("s1") {
		t.Error("s1 must persist as explicit-disabled")
	}
}

// POST /api/cloud/sync/default persists cloud.sync_default without touching
// the other cloud config fields; existing per-session entries are unchanged
// (no retroactive effect).
func TestCloudSyncDefaultToggle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	auto := false
	cfg := &config.Config{}
	cfg.SetCloud(&config.CloudConfig{Enabled: true, URL: "https://cloud.example.com", AutoConnect: &auto})
	s := &Server{cfg: cfg}

	// Pre-existing per-session entry.
	store, err := s.cloudSyncStoreLoad()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("s1", true); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleCloudSyncDefault(rec, httptest.NewRequest(http.MethodPost, "/api/cloud/sync/default", strings.NewReader(`{"enabled":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got cloudSyncJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.SyncDefault {
		t.Error("response must report sync_default=true")
	}
	if !got.Sessions["s1"] {
		t.Error("existing per-session entries must be untouched")
	}

	// The config block kept its other fields, and the value survives a
	// reload from disk (read the file directly: LoadConfig validates
	// providers, which a bare test config does not have).
	data, err := os.ReadFile(config.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	var onDisk config.Config
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatal(err)
	}
	cc := onDisk.CloudSettings()
	if !cc.SyncDefault {
		t.Error("sync_default must persist in config.json")
	}
	if !cc.Enabled || cc.URL != "https://cloud.example.com" || cc.AutoConnect == nil || *cc.AutoConnect {
		t.Errorf("other cloud fields clobbered: %+v", cc)
	}
}

// Validation: enabled is required on both POSTs.
func TestCloudSyncValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{cfg: &config.Config{}}

	rec := httptest.NewRecorder()
	s.handleCloudSyncDefault(rec, httptest.NewRequest(http.MethodPost, "/api/cloud/sync/default", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("default with missing enabled: status=%d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/cloud/sync/s1", strings.NewReader(`{}`))
	req.SetPathValue("session_id", "s1")
	s.handleCloudSyncSession(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("session with missing enabled: status=%d, want 400", rec.Code)
	}
}

// Session-creation stamping: cloud-originated sessions always opt in; local
// sessions follow cloud.sync_default; existing entries are never overwritten.
func TestStampCloudSync(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	local := &Server{cfg: &config.Config{}}
	local.stampCloudSync("new-local", "", true)
	local.stampCloudSync("resumed-local", "", false)
	if store, _ := local.cloudSyncStoreLoad(); store.Has("new-local") || store.Has("resumed-local") {
		t.Fatal("default OFF: local sessions must stay unstamped")
	}

	// Cloud channels stamp even with the default OFF.
	local.stampCloudSync("from-console", "console", true)
	local.stampCloudSync("from-mobile-resume", "mobile", false)
	store, _ := local.cloudSyncStoreLoad()
	if !store.Enabled("from-console") || !store.Enabled("from-mobile-resume") {
		t.Error("cloud-originated sessions must always be stamped enabled")
	}

	// Global default ON: new local sessions opt in; resumed ones don't
	// (no retroactive effect on pre-existing sessions).
	withDefault := &Server{cfg: &config.Config{}}
	withDefault.cfg.SetCloud(&config.CloudConfig{SyncDefault: true})
	withDefault.stampCloudSync("new-with-default", "", true)
	withDefault.stampCloudSync("resumed-with-default", "", false)
	store, _ = withDefault.cloudSyncStoreLoad()
	if !store.Enabled("new-with-default") {
		t.Error("sync_default=true must stamp new sessions enabled")
	}
	if store.Has("resumed-with-default") {
		t.Error("resumed sessions must not be retroactively stamped")
	}

	// An explicit user toggle wins over stamping.
	if err := store.Set("user-disabled", false); err != nil {
		t.Fatal(err)
	}
	withDefault.stampCloudSync("user-disabled", "console", true)
	if store.Enabled("user-disabled") {
		t.Error("stamping must never overwrite an explicit per-session entry")
	}
}

// Stamping lands in the same file the API reads (no split state).
func TestStampVisibleViaAPI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{cfg: &config.Config{}}
	s.stampCloudSync("s9", "console", true)

	rec := httptest.NewRecorder()
	s.handleCloudSync(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/sync", nil))
	var got cloudSyncJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Sessions["s9"] {
		t.Errorf("GET sessions = %v, want s9 stamped", got.Sessions)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".jcode", "cloud-sessions.json")); err != nil {
		t.Errorf("store file missing: %v", err)
	}
}
