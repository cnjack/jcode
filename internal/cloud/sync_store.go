// sync_store.go persists the M19 per-session cloud-sync opt-in at
// ~/.jcode/cloud-sessions.json (0600): a flat {session_id: true|false} map.
// A session WITHOUT an entry is "unset" and never syncs (default OFF) — the
// global cloud.sync_default config is consulted only once, when the web layer
// stamps a brand-new session, so flipping the default never retroactively
// changes existing sessions.
//
// The web API and the relay connector hold separate SyncStore instances on
// the same file; both transparently re-read it when its mtime/size changes,
// so a toggle via the API takes effect in the connector without any explicit
// wiring (event filtering on the next event, metadata upsert on the next
// session-sync tick).
package cloud

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const syncSessionsFile = "cloud-sessions.json"

// SyncStore is the per-session cloud-sync state backed by one JSON file.
// All methods are safe for concurrent use.
type SyncStore struct {
	path string

	mu       sync.RWMutex
	sessions map[string]bool
	// modTime/size fingerprint of the last load, used by refresh to pick up
	// external writes (the web API's own store instance).
	modTime time.Time
	size    int64
}

// SyncStorePath returns the default store path (~/.jcode/cloud-sessions.json).
func SyncStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".jcode", syncSessionsFile), nil
}

// LoadSyncStore opens the store at path. A missing file is not an error — it
// means "no session opted in yet". A parse/IO error IS returned: callers
// decide whether to fail closed (connector: nothing syncs) or surface it
// (web API: 500).
func LoadSyncStore(path string) (*SyncStore, error) {
	s := &SyncStore{path: path, sessions: make(map[string]bool)}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// Path returns the store's backing file path (the connector's session-sync
// loop watches it for mtime changes).
func (s *SyncStore) Path() string {
	return s.path
}

// reload re-reads the file, replacing the in-memory map. Missing file =
// empty map. Callers must not hold s.mu.
func (s *SyncStore) reload() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.mu.Lock()
			s.sessions = make(map[string]bool)
			s.modTime = time.Time{}
			s.size = -1
			s.mu.Unlock()
			return nil
		}
		return fmt.Errorf("failed to read sync store %s: %w", s.path, err)
	}
	var sessions map[string]bool
	if err := json.Unmarshal(data, &sessions); err != nil {
		return fmt.Errorf("failed to parse sync store %s: %w", s.path, err)
	}
	if sessions == nil {
		sessions = make(map[string]bool)
	}
	fi, err := os.Stat(s.path)
	if err != nil {
		return fmt.Errorf("failed to stat sync store %s: %w", s.path, err)
	}
	s.mu.Lock()
	s.sessions = sessions
	s.modTime = fi.ModTime()
	s.size = fi.Size()
	s.mu.Unlock()
	return nil
}

// refresh reloads the file when it changed on disk since the last load
// (mtime or size). Failures keep the previous in-memory view — a half-written
// file must never flip sessions to unsynced mid-stream.
func (s *SyncStore) refresh() {
	fi, err := os.Stat(s.path)
	if err != nil {
		return // missing/unreadable: keep the current view
	}
	s.mu.RLock()
	same := fi.ModTime().Equal(s.modTime) && fi.Size() == s.size
	s.mu.RUnlock()
	if same {
		return
	}
	_ = s.reload()
}

// Enabled reports whether the session has an explicit sync opt-in. Unset
// sessions report false (default OFF).
func (s *SyncStore) Enabled(sessionID string) bool {
	s.refresh()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[sessionID]
}

// Snapshot returns a copy of the explicit per-session map.
func (s *SyncStore) Snapshot() map[string]bool {
	s.refresh()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]bool, len(s.sessions))
	for k, v := range s.sessions {
		out[k] = v
	}
	return out
}

// Has reports whether the session has an explicit entry (set vs unset).
func (s *SyncStore) Has(sessionID string) bool {
	s.refresh()
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.sessions[sessionID]
	return ok
}

// Set records the session's sync state and persists the store atomically
// (0600), overwriting any existing entry.
func (s *SyncStore) Set(sessionID string, enabled bool) error {
	s.refresh() // merge external writes before our read-modify-write
	s.mu.Lock()
	s.sessions[sessionID] = enabled
	err := s.saveLocked()
	s.mu.Unlock()
	return err
}

// Delete forgets the local per-session preference after the session itself is
// deleted. The connector observes the file change and sends a replacement
// snapshot that removes the corresponding cloud mirror.
func (s *SyncStore) Delete(sessionID string) error {
	s.refresh()
	s.mu.Lock()
	delete(s.sessions, sessionID)
	err := s.saveLocked()
	s.mu.Unlock()
	return err
}

// SetIfAbsent records the session's sync state only when no explicit entry
// exists yet (session-creation stamping must never clobber a user toggle).
// It reports whether an entry was written.
func (s *SyncStore) SetIfAbsent(sessionID string, enabled bool) (bool, error) {
	s.refresh()
	s.mu.Lock()
	if _, ok := s.sessions[sessionID]; ok {
		s.mu.Unlock()
		return false, nil
	}
	s.sessions[sessionID] = enabled
	err := s.saveLocked()
	s.mu.Unlock()
	return true, err
}

// saveLocked writes the map atomically (tmp + rename) with owner-only
// permissions. The caller holds s.mu. The in-memory fingerprint is updated
// so the next refresh does not re-read our own write.
func (s *SyncStore) saveLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(s.sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal sync store: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+syncSessionsFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary sync store in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("failed to secure temporary sync store %s: %w", tmpPath, err)
	}
	if n, err := tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write temporary sync store %s: %w", tmpPath, err)
	} else if n != len(data) {
		return fmt.Errorf("failed to write temporary sync store %s: %w", tmpPath, io.ErrShortWrite)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to sync temporary sync store %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temporary sync store %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("failed to replace sync store %s: %w", s.path, err)
	}
	tmpPath = ""
	if fi, err := os.Stat(s.path); err == nil {
		s.modTime = fi.ModTime()
		s.size = fi.Size()
	}
	return nil
}
