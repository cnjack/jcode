package web

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

// Cloud sync API (M19): the per-session sync switches and the global
// new-session default.
//
//   GET  /api/cloud/sync                → {sync_default, sessions:{id:bool}}
//   POST /api/cloud/sync/default        {enabled} → persists cloud.sync_default
//   POST /api/cloud/sync/{session_id}   {enabled} → writes ~/.jcode/cloud-sessions.json
//
// The connector holds its own SyncStore instance on the same file and picks
// writes up via mtime (event filter immediately, metadata upsert on the next
// session-sync tick), so no supervisor round-trip is needed here.

// cloudSyncPayload is the GET /api/cloud/sync response (and the POST
// /sync/default response — it returns the fresh state).
type cloudSyncPayload struct {
	SyncDefault bool            `json:"sync_default"`
	Sessions    map[string]bool `json:"sessions"`
}

// cloudSyncStoreLoad returns the server's lazy SyncStore (~/.jcode/
// cloud-sessions.json). Lazy because tests construct Server literals; a load
// failure is returned (POST → 500, GET → empty sessions map).
func (s *Server) cloudSyncStoreLoad() (*cloud.SyncStore, error) {
	s.cloudSyncMu.Lock()
	defer s.cloudSyncMu.Unlock()
	if s.cloudSyncStore != nil {
		return s.cloudSyncStore, nil
	}
	if s.cloudSyncErr != nil {
		return nil, s.cloudSyncErr
	}
	path, err := cloud.SyncStorePath()
	if err != nil {
		s.cloudSyncErr = err
		return nil, err
	}
	store, err := cloud.LoadSyncStore(path)
	if err != nil {
		s.cloudSyncErr = err
		return nil, err
	}
	s.cloudSyncStore = store
	return store, nil
}

// cloudSyncState builds the GET payload. A store load failure reports an
// empty sessions map (sync_default still reflects the config) — the settings
// UI must not break on an unreadable store file.
func (s *Server) cloudSyncState() cloudSyncPayload {
	payload := cloudSyncPayload{
		SyncDefault: config.CloudSyncDefault(s.cfg),
		Sessions:    map[string]bool{},
	}
	if store, err := s.cloudSyncStoreLoad(); err == nil {
		payload.Sessions = store.Snapshot()
	}
	return payload
}

// handleCloudSync serves GET /api/cloud/sync.
func (s *Server) handleCloudSync(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cloudSyncState())
}

// handleCloudSyncDefault serves POST /api/cloud/sync/default: persists
// cloud.sync_default (read/modify/write preserving the other cloud fields,
// same rollback pattern as handleCloudConfig). Only sessions created AFTER
// this change are affected; existing sessions keep their current state.
func (s *Server) handleCloudSyncDefault(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil || req.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled is required"})
		return
	}
	enabled := *req.Enabled

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if s.cfg == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	previous := s.cfg.CloudSettings()
	s.cfg.SetCloud(&config.CloudConfig{
		Enabled:     previous.Enabled,
		URL:         previous.URL,
		AutoConnect: previous.AutoConnect,
		E2EE:        previous.E2EE,
		SyncDefault: enabled,
	})
	if err := config.SaveConfig(s.cfg); err != nil {
		if previous == (config.CloudConfig{}) {
			s.cfg.SetCloud(nil)
		} else {
			s.cfg.SetCloud(&previous)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.cloudSyncState())
}

// handleCloudSyncSession serves POST /api/cloud/sync/{session_id}: writes the
// session's explicit sync opt-in/out. The connector picks the change up via
// the store file's mtime: event filtering applies to the very next event and
// a newly enabled session's metadata is upserted on the next session-sync
// tick. Disabling never deletes already-uploaded cloud history.
func (s *Server) handleCloudSyncSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session id is required"})
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil || req.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled is required"})
		return
	}
	store, err := s.cloudSyncStoreLoad()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := store.Set(sessionID, *req.Enabled); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session_id": sessionID, "enabled": *req.Enabled})
}

// stampCloudSync records the initial per-session sync state for a session the
// web layer is creating (M19). Semantics:
//
//   - cloud-originated sessions (the console/mobile relay channels) ALWAYS
//     opt in — a session created from the cloud must be visible there;
//   - otherwise new sessions follow cloud.sync_default;
//   - resumed local sessions (isNew=false, local source) are never stamped:
//     pre-M19 sessions stay exactly as they are (no retroactive changes);
//   - an existing explicit entry (a user toggle) is never overwritten.
//
// Best-effort: a store failure is logged, never fails session creation.
func (s *Server) stampCloudSync(sessionID, source string, isNew bool) {
	if sessionID == "" {
		return
	}
	cloudOriginated := source == "console" || source == "mobile"
	enabled := cloudOriginated || (isNew && config.CloudSyncDefault(s.cfg))
	if !enabled {
		// Nothing to record: an unset entry already means "not synced". This
		// also leaves resumed pre-M19 local sessions untouched (no retroactive
		// changes, whatever the default is).
		return
	}
	store, err := s.cloudSyncStoreLoad()
	if err != nil {
		config.Logger().Printf("[cloud] sync store unavailable, session %s sync state not stamped: %v", sessionID, err)
		return
	}
	if _, err := store.SetIfAbsent(sessionID, enabled); err != nil {
		config.Logger().Printf("[cloud] failed to stamp session %s sync state: %v", sessionID, err)
	}
}
