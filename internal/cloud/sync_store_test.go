package cloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncStoreMissingFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloud-sessions.json")
	store, err := LoadSyncStore(path)
	if err != nil {
		t.Fatalf("missing file must not be an error: %v", err)
	}
	if store.Enabled("s1") {
		t.Error("unset session must report disabled (default OFF)")
	}
	if store.Has("s1") {
		t.Error("unset session must report Has=false")
	}
	if got := store.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot() = %v, want empty", got)
	}
}

func TestSyncStoreRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloud-sessions.json")
	store, err := LoadSyncStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("s1", true); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("s2", false); err != nil {
		t.Fatal(err)
	}
	if !store.Enabled("s1") || store.Enabled("s2") || store.Enabled("s3") {
		t.Fatal("in-memory state wrong after Set")
	}

	// Owner-only file permissions.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 600", perm)
	}

	// A fresh instance on the same file sees the persisted state.
	reloaded, err := LoadSyncStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Enabled("s1") {
		t.Error("s1 must persist as enabled")
	}
	if reloaded.Enabled("s2") {
		t.Error("s2 must persist as disabled")
	}
	if !reloaded.Has("s2") {
		t.Error("s2 must keep its explicit (disabled) entry")
	}
}

func TestSyncStoreSetIfAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloud-sessions.json")
	store, err := LoadSyncStore(path)
	if err != nil {
		t.Fatal(err)
	}
	written, err := store.SetIfAbsent("s1", true)
	if err != nil || !written {
		t.Fatalf("first SetIfAbsent = (%v, %v), want (true, nil)", written, err)
	}
	// An existing entry (the user's explicit toggle) is never clobbered.
	if err := store.Set("s1", false); err != nil {
		t.Fatal(err)
	}
	written, err = store.SetIfAbsent("s1", true)
	if err != nil || written {
		t.Fatalf("second SetIfAbsent = (%v, %v), want (false, nil)", written, err)
	}
	if store.Enabled("s1") {
		t.Error("SetIfAbsent must not overwrite the explicit entry")
	}
}

func TestSyncStoreDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloud-sessions.json")
	store, err := LoadSyncStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("s1", true); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("s1"); err != nil {
		t.Fatal(err)
	}
	if store.Has("s1") || store.Enabled("s1") {
		t.Fatal("deleted sync entry must be absent")
	}
}

// The connector and the web API hold separate store instances on the same
// file; an external write must become visible without a reload call.
func TestSyncStorePicksUpExternalWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloud-sessions.json")
	store, err := LoadSyncStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Enabled("s1") {
		t.Fatal("precondition: s1 unset")
	}
	// External writer (the web API's own instance) enables the session.
	other, err := LoadSyncStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Set("s1", true); err != nil {
		t.Fatal(err)
	}
	// mtime granularity guard: make sure the write is detectably newer.
	if fi, err := os.Stat(path); err == nil {
		_ = os.Chtimes(path, fi.ModTime().Add(time.Second), fi.ModTime().Add(time.Second))
	}
	if !store.Enabled("s1") {
		t.Error("external Set must be picked up via mtime refresh")
	}
}

// A corrupt file fails the initial load; during refresh the previous
// in-memory view is kept (a half-written file must never flip sessions to
// unsynced mid-stream).
func TestSyncStoreCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloud-sessions.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSyncStore(path); err == nil {
		t.Fatal("corrupt file must fail the initial load")
	}

	// Start healthy, then corrupt externally: the old view survives.
	if err := os.WriteFile(path, []byte(`{"s1":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadSyncStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err == nil {
		_ = os.Chtimes(path, fi.ModTime().Add(time.Second), fi.ModTime().Add(time.Second))
	}
	if !store.Enabled("s1") {
		t.Error("refresh over a corrupt file must keep the previous view")
	}
}

// The on-disk shape is the documented flat {session_id: bool} map.
func TestSyncStoreFileShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloud-sessions.json")
	store, err := LoadSyncStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("abc", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]bool
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("store file must be a flat map: %v", err)
	}
	if !raw["abc"] || len(raw) != 1 {
		t.Errorf("file content = %v", raw)
	}
}
