package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	pathpkg "path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/remote"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
)

// pendingConnTTL bounds how long an established-but-unbound SSH connection is
// kept alive while the user works through the wizard's directory picker.
const pendingConnTTL = 10 * time.Minute

// pendingConn is a remote connection (SSH or Docker) created by the
// remote-connect wizard that has not yet been bound to the live env.
type pendingConn struct {
	exec tools.RemoteExecutor
	kind string // "ssh" | "docker"
	// SSH reconnect prefill.
	host string // host:port as dialed
	user string
	port int // originally requested port
	// Docker reconnect prefill.
	container string // container name or id
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
	rg.sweepLocked()
	pc := rg.conns[id]
	if pc != nil {
		// The TTL is idle time, not total wizard time. Directory browsing may
		// legitimately take longer than ten minutes on a large remote tree.
		pc.createdAt = time.Now()
	}
	return pc
}

// claim removes a connection WITHOUT closing it while a bind is in progress.
// This prevents a concurrent cancel or second bind from closing/reusing the
// executor during candidate construction. The caller either transfers ownership
// to the published engine or restores the same id on failure.
func (rg *remoteConnRegistry) claim(id string) *pendingConn {
	rg.mu.Lock()
	defer rg.mu.Unlock()
	rg.sweepLocked()
	pc := rg.conns[id]
	delete(rg.conns, id)
	return pc
}

func (rg *remoteConnRegistry) restore(id string, pc *pendingConn) {
	if id == "" || pc == nil {
		return
	}
	rg.mu.Lock()
	if _, exists := rg.conns[id]; !exists {
		pc.createdAt = time.Now()
		rg.conns[id] = pc
	}
	rg.mu.Unlock()
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

// closeAll releases connections that were established in the wizard but never
// bound to an Engine. Engine teardown only sees published runtimes, so pending
// SSH transports need their own shutdown path.
func (rg *remoteConnRegistry) closeAll() {
	rg.mu.Lock()
	conns := rg.conns
	rg.conns = make(map[string]*pendingConn)
	rg.mu.Unlock()
	for _, pc := range conns {
		if pc != nil && pc.exec != nil {
			_ = pc.exec.Close()
		}
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
		Type       string `json:"type"` // "ssh" | "docker"
		Host       string `json:"host"`
		Port       int    `json:"port"`
		User       string `json:"user"`
		AuthMethod string `json:"auth_method"` // "password" | "key"
		Password   string `json:"password"`
		KeyPath    string `json:"key_path"`
		Passphrase string `json:"passphrase"`
		Container  string `json:"container"` // docker: container id or name
		// SSH TOFU confirmation: both fields must be supplied together on the
		// retry after an ssh_host_key_unknown response.
		AcceptHostKey      bool   `json:"accept_host_key"`
		HostKeyFingerprint string `json:"host_key_fingerprint"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Type == "docker" {
		s.connectDockerWizard(w, r, strings.TrimSpace(req.Container))
		return
	}
	if req.Type != "" && req.Type != "ssh" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported connection type"})
		return
	}
	if strings.TrimSpace(req.Host) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "host is required"})
		return
	}

	opts := remote.SSHOptions{
		Host:               req.Host,
		Port:               req.Port,
		User:               req.User,
		AcceptHostKey:      req.AcceptHostKey,
		HostKeyFingerprint: req.HostKeyFingerprint,
	}
	if req.AuthMethod == "password" {
		opts.Password = req.Password
	} else {
		opts.KeyPath = req.KeyPath
		opts.Passphrase = req.Passphrase
	}

	exec, err := remote.ConnectContext(r.Context(), opts)
	if err != nil {
		if writeSSHHostKeyError(w, err) {
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	remotePwd := remote.DiscoverPwd(r.Context(), exec, "/root")
	id := s.remoteConns.add(&pendingConn{
		exec:      exec,
		kind:      "ssh",
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

// writeSSHHostKeyError writes the stable API contract used by Desktop's trust
// prompt. It deliberately returns 409 for all trust-state conflicts while
// keeping ordinary authentication/network failures on the existing 502 path.
func writeSSHHostKeyError(w http.ResponseWriter, err error) bool {
	var hostKeyErr *remote.SSHHostKeyError
	if !errors.As(err, &hostKeyErr) {
		return false
	}
	payload := map[string]any{
		"error":       hostKeyErr.Error(),
		"code":        hostKeyErr.Code,
		"host":        hostKeyErr.Host,
		"fingerprint": hostKeyErr.Fingerprint,
		"key_type":    hostKeyErr.KeyType,
	}
	if hostKeyErr.OldFingerprint != "" {
		payload["old_fingerprint"] = hostKeyErr.OldFingerprint
	}
	if hostKeyErr.ExpectedFingerprint != "" {
		payload["expected_fingerprint"] = hostKeyErr.ExpectedFingerprint
	}
	writeJSON(w, http.StatusConflict, payload)
	return true
}

// connectDocker binds (starting if stopped) the named container and parks it in
// the pending registry, mirroring the SSH connect flow.
func (s *Server) connectDockerWizard(w http.ResponseWriter, r *http.Request, containerRef string) {
	if containerRef == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "container is required"})
		return
	}
	exec, err := remote.ConnectDocker(r.Context(), containerRef)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	remotePwd := remote.DiscoverPwd(r.Context(), exec, "/")
	id := s.remoteConns.add(&pendingConn{
		exec:      exec,
		kind:      "docker",
		container: exec.Name(),
		createdAt: time.Now(),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"connection_id": id,
		"remote_pwd":    remotePwd,
		"platform":      exec.Platform(),
		"container":     exec.Name(),
	})
}

// handleListContainers returns the daemon's containers for the wizard picker.
func (s *Server) handleListContainers(w http.ResponseWriter, r *http.Request) {
	if s.needsSetup {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "setup required: please configure a provider first"})
		return
	}
	containers, err := remote.ListContainers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": containers})
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

// handleRemoteBind atomically turns an explicitly authenticated/trusted pending
// connection into either a new task or the replacement runtime for a persisted
// remote conversation. The candidate is hydrated before publication, so a
// session id never briefly resolves to an empty-history engine.
func (s *Server) handleRemoteBind(w http.ResponseWriter, r *http.Request) {
	// No running gate: binding a remote workspace builds a NEW engine; the
	// previous task keeps running in the background.
	if s.newRemoteEngine == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "remote workspaces are not supported"})
		return
	}
	var req struct {
		ConnectionID string `json:"connection_id"`
		Path         string `json:"path"`
		SessionID    string `json:"session_id,omitempty"`
		Focus        bool   `json:"focus,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	// Preserve the original wizard contract for new workspaces: older clients
	// did not send a focus flag because bind always foregrounded the new task.
	// Existing-session reconnects opt in explicitly so a candidate can be
	// prepared in the background while its history page is still loading.
	focus := req.Focus || req.SessionID == ""
	pc := s.remoteConns.claim(req.ConnectionID)
	if pc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "connection expired or not found"})
		return
	}
	remotePwd := strings.TrimSpace(req.Path)
	if remotePwd == "" {
		remotePwd = remote.DiscoverPwd(r.Context(), pc.exec, "/root")
	}

	label := pc.exec.ProjectLabel(remotePwd)
	target, targetErr := parseConversationTarget(label)
	if targetErr != nil {
		s.remoteConns.restore(req.ConnectionID, pc)
		writeConversationActivationError(w, fmt.Errorf("%w: %v", errInvalidConversationTarget, targetErr))
		return
	}

	s.taskCreateMu.Lock()
	var old *Engine
	if req.SessionID != "" {
		old = s.resolveEngine(req.SessionID)
	}
	buildMode := mode.Approval.String()
	var (
		meta  *session.SessionMeta
		state *session.SessionState
		err   error
	)
	if req.SessionID != "" {
		meta, err = session.FindSessionMeta(req.SessionID)
		if err == nil && meta == nil {
			err = fmt.Errorf("%w: %s", errConversationNotFound, req.SessionID)
		}
		if err == nil {
			var entries []session.Entry
			entries, err = session.LoadSession(req.SessionID)
			if err == nil {
				state = session.ReconstructState(entries)
			}
		}
		if err == nil {
			persistedTarget, parseErr := parseConversationTarget(meta.Project)
			if parseErr != nil || !sameConversationLocation(persistedTarget, target) {
				err = fmt.Errorf("%w: authenticated target %q does not match persisted project %q", errInvalidConversationTarget, label, meta.Project)
			}
		}
		if err == nil {
			if savedMode, modeErr := session.LoadSessionModeStrict(req.SessionID); modeErr == nil {
				buildMode = restoredWebSessionMode(savedMode).String()
			}
		}
	}
	if err == nil && old != nil && old.running.Load() {
		err = fmt.Errorf("%w: %s", errConversationBusy, req.SessionID)
	}
	var eng *Engine
	if err == nil {
		eng, err = s.assembleRemoteEngine(req.SessionID, pc.exec, remotePwd, buildMode)
	}
	if err == nil && state != nil {
		hydrateConversationEngine(eng, state, mode.Parse(buildMode))
	}
	if err == nil {
		err = s.publishEngineCandidate(eng, old)
	}
	s.taskCreateMu.Unlock()
	if err != nil {
		if eng != nil {
			// The pending registry still owns pc.exec on failure. Clearing env.Exec
			// prevents candidate teardown from closing that retryable connection.
			if eng.env != nil {
				eng.env.Exec = nil
			}
			eng.teardown()
		}
		s.remoteConns.restore(req.ConnectionID, pc)
		writeConversationActivationError(w, fmt.Errorf("bind remote conversation: %w", err))
		return
	}

	// Ownership transferred when the hydrated candidate was published; the
	// claimed registry entry intentionally stays absent.
	if focus {
		prevTaskID := ""
		if cur := s.activeEngine(); cur != nil {
			prevTaskID = cur.taskID
		}
		if prevTaskID != "" && prevTaskID != eng.taskID {
			s.ptyMgr.closeForTask(prevTaskID)
		}
		s.setActiveEngine(eng)
		s.wsBroker.Broadcast(WSEvent{
			TaskID: eng.taskID, Type: "project_switched",
			Data: map[string]string{"pwd": remotePwd, "label": label},
		})
	}
	if req.SessionID == "" {
		s.stampCloudSync(eng.taskID, "", true)
	}

	result := activationSnapshot(eng, target.kind, true)
	result.Focused = focus
	writeJSON(w, http.StatusOK, map[string]any{
		"status": result.Status, "session_id": result.SessionID,
		"kind": result.Kind, "pwd": result.Pwd, "project": result.Project,
		"workspace_key": result.WorkspaceKey, "provider": result.Provider,
		"model": result.Model, "agent": result.Agent, "mode": result.Mode,
		"running": result.Running, "activated": result.Activated, "focused": result.Focused,
		"label": label, "name": pathpkg.Base(remotePwd), "host": pc.host,
		"user": pc.user, "port": pc.port, "container": pc.container,
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
	s.cfgMu.Lock()
	if s.cfg == nil {
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	previous := append([]config.SSHAlias(nil), s.cfg.SSHAliases...)
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
	if err != nil {
		s.cfg.SSHAliases = previous
	}
	s.cfgMu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRemoteSaveDockerAlias upserts a saved Docker alias (name/container/path)
// into config so it appears for one-click reconnects and the switch_env tool.
func (s *Server) handleRemoteSaveDockerAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		Container string `json:"container"`
		Path      string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Container) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and container are required"})
		return
	}

	s.cfgMu.Lock()
	if s.cfg == nil {
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	previous := append([]config.DockerAlias(nil), s.cfg.DockerAliases...)
	updated := false
	for i := range s.cfg.DockerAliases {
		if s.cfg.DockerAliases[i].Name == req.Name {
			s.cfg.DockerAliases[i].Container = req.Container
			s.cfg.DockerAliases[i].Path = req.Path
			updated = true
			break
		}
	}
	if !updated {
		s.cfg.DockerAliases = append(s.cfg.DockerAliases, config.DockerAlias{Name: req.Name, Container: req.Container, Path: req.Path})
	}
	err := config.SaveConfig(s.cfg)
	if err != nil {
		s.cfg.DockerAliases = previous
	}
	s.cfgMu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- SSH list handler ---

func (s *Server) handleListSSH(w http.ResponseWriter, r *http.Request) {
	type sshItem struct {
		Name string `json:"name"`
		Addr string `json:"addr"`
		Path string `json:"path,omitempty"`
	}

	var items []sshItem
	s.cfgMu.Lock()
	if s.cfg != nil {
		for _, a := range s.cfg.SSHAliases {
			items = append(items, sshItem{
				Name: a.Name,
				Addr: a.Addr,
				Path: a.Path,
			})
		}
	}
	s.cfgMu.Unlock()
	if items == nil {
		items = []sshItem{}
	}

	current := "local"
	if eng := s.activeEngine(); eng != nil && eng.env != nil && eng.env.IsRemote() {
		current = "ssh"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"current": current,
		"aliases": items,
	})
}
