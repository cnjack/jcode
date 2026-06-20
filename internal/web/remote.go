package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	pathpkg "path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/remote"
	"github.com/cnjack/jcode/internal/tools"
)

// pendingConnTTL bounds how long an established-but-unbound SSH connection is
// kept alive while the user works through the wizard's directory picker.
const pendingConnTTL = 10 * time.Minute

// pendingConn is an SSH connection created by the remote-connect wizard that has
// not yet been bound to the live env.
type pendingConn struct {
	exec      *tools.SSHExecutor
	host      string // host:port as dialed
	user      string
	port      int // originally requested port (for reconnect prefill)
	createdAt time.Time
}

// remoteConnRegistry holds pending SSH connections keyed by connection id.
type remoteConnRegistry struct {
	mu    sync.Mutex
	conns map[string]*pendingConn
}

func newRemoteConnRegistry() *remoteConnRegistry {
	return &remoteConnRegistry{conns: make(map[string]*pendingConn)}
}

func (rg *remoteConnRegistry) add(pc *pendingConn) string {
	id := uuid.New().String()
	rg.mu.Lock()
	defer rg.mu.Unlock()
	rg.sweepLocked()
	rg.conns[id] = pc
	return id
}

func (rg *remoteConnRegistry) get(id string) *pendingConn {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	return rg.conns[id]
}

// take removes a connection WITHOUT closing it: ownership transfers to the
// caller (e.g. the live env after a successful bind).
func (rg *remoteConnRegistry) take(id string) *pendingConn {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	pc := rg.conns[id]
	delete(rg.conns, id)
	return pc
}

// drop removes and closes a pending connection (cancel / abandon).
func (rg *remoteConnRegistry) drop(id string) {
	rg.mu.Lock()
	pc := rg.conns[id]
	delete(rg.conns, id)
	rg.mu.Unlock()
	if pc != nil && pc.exec != nil {
		_ = pc.exec.Close()
	}
}

// sweepLocked closes and removes connections older than the TTL. Caller holds mu.
func (rg *remoteConnRegistry) sweepLocked() {
	now := time.Now()
	for id, pc := range rg.conns {
		if now.Sub(pc.createdAt) > pendingConnTTL {
			if pc.exec != nil {
				_ = pc.exec.Close()
			}
			delete(rg.conns, id)
		}
	}
}

// handleRemoteConnect establishes an SSH connection from the wizard's config
// step and parks it in the pending registry, returning a connection id the
// client uses to browse remote directories and ultimately bind.
func (s *Server) handleRemoteConnect(w http.ResponseWriter, r *http.Request) {
	if s.needsSetup {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "setup required: please configure a provider first"})
		return
	}
	var req struct {
		Type       string `json:"type"` // "ssh" (docker reserved for later)
		Host       string `json:"host"`
		Port       int    `json:"port"`
		User       string `json:"user"`
		AuthMethod string `json:"auth_method"` // "password" | "key"
		Password   string `json:"password"`
		KeyPath    string `json:"key_path"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Type != "" && req.Type != "ssh" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "only ssh connections are supported"})
		return
	}
	if strings.TrimSpace(req.Host) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "host is required"})
		return
	}

	opts := remote.SSHOptions{Host: req.Host, Port: req.Port, User: req.User}
	if req.AuthMethod == "password" {
		opts.Password = req.Password
	} else {
		opts.KeyPath = req.KeyPath
		opts.Passphrase = req.Passphrase
	}

	exec, err := remote.Connect(opts)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	remotePwd := remote.DiscoverPwd(r.Context(), exec, "/root")
	id := s.remoteConns.add(&pendingConn{
		exec:      exec,
		host:      exec.Host(),
		user:      exec.User(),
		port:      req.Port,
		createdAt: time.Now(),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"connection_id": id,
		"remote_pwd":    remotePwd,
		"platform":      exec.Platform(),
		"user":          exec.User(),
		"host":          exec.Host(),
	})
}

// handleRemoteListDir lists sub-directories of a path on a pending connection,
// driving the wizard's remote directory picker.
func (s *Server) handleRemoteListDir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConnectionID string `json:"connection_id"`
		Path         string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	pc := s.remoteConns.get(req.ConnectionID)
	if pc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "connection expired or not found"})
		return
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		path = remote.DiscoverPwd(r.Context(), pc.exec, "/root")
	}
	dirs, err := remote.ListDirs(r.Context(), pc.exec, path)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "dirs": dirs})
}

// handleRemoteBind commits a pending connection: it binds the shared env to the
// remote executor at the chosen directory and rebuilds the agent (same path as
// a local project switch).
func (s *Server) handleRemoteBind(w http.ResponseWriter, r *http.Request) {
	if s.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent is running, cannot switch workspace"})
		return
	}
	if s.switchToRemote == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "remote workspaces are not supported"})
		return
	}
	var req struct {
		ConnectionID string `json:"connection_id"`
		Path         string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	pc := s.remoteConns.get(req.ConnectionID)
	if pc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "connection expired or not found"})
		return
	}
	remotePwd := strings.TrimSpace(req.Path)
	if remotePwd == "" {
		remotePwd = remote.DiscoverPwd(r.Context(), pc.exec, "/root")
	}

	// Tear down local PTYs (they belonged to the previous workspace).
	s.ptyMgr.closeAll()

	ag, rec, err := s.switchToRemote(pc.exec, remotePwd)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to bind remote workspace: %v", err)})
		return
	}

	label := remote.ProjectLabel(pc.exec, remotePwd)

	s.mu.Lock()
	s.pwd = remotePwd
	s.agent = ag
	s.recorder = rec
	s.history = nil
	s.mu.Unlock()

	s.todoStore.Update(nil)

	// Ownership of the executor has transferred to the live env; remove the
	// pending entry WITHOUT closing it.
	s.remoteConns.take(req.ConnectionID)

	s.wsBroker.Broadcast(WSEvent{
		Type: "project_switched",
		Data: map[string]string{"pwd": remotePwd, "label": label},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"pwd":         remotePwd,
		"label":       label,
		"name":        pathpkg.Base(remotePwd),
		"host":        pc.host,
		"user":        pc.user,
		"port":        pc.port,
		"remote_path": remotePwd,
	})
}

// handleRemoteCancel closes and discards a pending connection the user
// abandoned mid-wizard.
func (s *Server) handleRemoteCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConnectionID string `json:"connection_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	s.remoteConns.drop(req.ConnectionID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRemoteSaveAlias upserts a saved SSH alias (name/addr/path) into config so
// it appears in GET /api/ssh for one-click reconnects. Secrets are never stored.
func (s *Server) handleRemoteSaveAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Addr string `json:"addr"` // user@host
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Addr) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and addr are required"})
		return
	}

	// Mutate + persist under the lock (the config save must be atomic with the
	// in-memory edit), but release it before writing the HTTP response so a slow
	// client cannot stall other handlers — mirrors handleCreateMCP/handleToggleSkill.
	s.mu.Lock()
	if s.cfg == nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	updated := false
	for i := range s.cfg.SSHAliases {
		if s.cfg.SSHAliases[i].Name == req.Name {
			s.cfg.SSHAliases[i].Addr = req.Addr
			s.cfg.SSHAliases[i].Path = req.Path
			updated = true
			break
		}
	}
	if !updated {
		s.cfg.SSHAliases = append(s.cfg.SSHAliases, config.SSHAlias{Name: req.Name, Addr: req.Addr, Path: req.Path})
	}
	err := config.SaveConfig(s.cfg)
	s.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
