package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/remote"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
	managedworkspace "github.com/cnjack/jcode/internal/workspace"
)

type conversationKind string

const (
	conversationLocal  conversationKind = "local"
	conversationSSH    conversationKind = "ssh"
	conversationDocker conversationKind = "docker"
)

var (
	errConversationNotFound      = errors.New("conversation not found")
	errInvalidConversationTarget = errors.New("invalid conversation target")
	errConversationBusy          = errors.New("conversation runtime is busy")
	errWorkspaceNotAllowed       = errors.New("workspace is not allowed")
)

type conversationTarget struct {
	kind      conversationKind
	project   string
	pwd       string
	sshHost   string
	sshUser   string
	container string
}

// parseConversationTarget treats the session index's project key as the
// execution authority. Remote keys are produced by RemoteExecutor.ProjectLabel
// and contain enough information to reconnect without relying on a mutable
// alias. Unknown URI schemes are rejected instead of being downgraded to a
// local filesystem path.
func parseConversationTarget(project string) (conversationTarget, error) {
	if project == "" {
		return conversationTarget{}, fmt.Errorf("conversation project is empty")
	}
	if !strings.Contains(project, "://") {
		return conversationTarget{kind: conversationLocal, project: project, pwd: project}, nil
	}

	u, err := url.Parse(project)
	if err != nil {
		return conversationTarget{}, fmt.Errorf("parse conversation project %q: %w", project, err)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return conversationTarget{}, fmt.Errorf("conversation project %q must not contain a query or fragment", project)
	}
	pwd := pathpkg.Clean(u.Path)
	if !strings.HasPrefix(pwd, "/") {
		return conversationTarget{}, fmt.Errorf("conversation project %q has no absolute remote path", project)
	}

	switch strings.ToLower(u.Scheme) {
	case string(conversationSSH):
		if u.Host == "" || u.User == nil || strings.TrimSpace(u.User.Username()) == "" {
			return conversationTarget{}, fmt.Errorf("conversation SSH project %q requires user and host", project)
		}
		if _, hasPassword := u.User.Password(); hasPassword {
			return conversationTarget{}, fmt.Errorf("conversation SSH project %q must not embed a password", project)
		}
		return conversationTarget{
			kind: conversationSSH, project: project, pwd: pwd,
			sshHost: u.Host, sshUser: u.User.Username(),
		}, nil
	case string(conversationDocker):
		if u.Host == "" || u.User != nil {
			return conversationTarget{}, fmt.Errorf("conversation Docker project %q requires a container", project)
		}
		return conversationTarget{
			kind: conversationDocker, project: project, pwd: pwd, container: u.Host,
		}, nil
	default:
		return conversationTarget{}, fmt.Errorf("unsupported conversation project scheme %q", u.Scheme)
	}
}

type activationResult struct {
	Status        string                `json:"status"`
	SessionID     string                `json:"session_id"`
	Kind          conversationKind      `json:"kind"`
	Pwd           string                `json:"pwd"`
	Project       string                `json:"project"`
	WorkspaceKey  string                `json:"workspace_key"`
	WorkspaceKind session.WorkspaceKind `json:"workspace_kind"`
	Provider      string                `json:"provider,omitempty"`
	Model         string                `json:"model,omitempty"`
	Agent         string                `json:"agent,omitempty"`
	Mode          string                `json:"mode"`
	Running       bool                  `json:"running"`
	Activated     bool                  `json:"activated"`
	Focused       bool                  `json:"focused"`
}

func activationSnapshot(eng *Engine, kind conversationKind, activated bool) activationResult {
	provider, modelName, modeName := eng.modelSnapshot()
	project := engineProject(eng)
	return activationResult{
		Status: "ready", SessionID: eng.taskID, Kind: kind, Pwd: eng.pwd,
		Project: project, WorkspaceKey: project,
		WorkspaceKind: session.NormalizeWorkspaceKind(eng.workspaceKind),
		Provider:      provider, Model: modelName, Agent: eng.curAgentRole(), Mode: modeName,
		Running: eng.running.Load(), Activated: activated,
	}
}

// handleActivateSession makes a conversation executable without changing the
// Desktop foreground. It deliberately returns no transcript; callers that need
// to render history continue to use GET /api/sessions/{id}.
func (s *Server) handleActivateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID   string `json:"session_id,omitempty"`
		ProjectPath string `json:"project_path,omitempty"`
		Source      string `json:"source,omitempty"`
		Focus       bool   `json:"focus,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	result, err := s.ensureConversation(r.Context(), req.SessionID, req.ProjectPath, req.Source)
	if err != nil {
		writeConversationActivationError(w, err)
		return
	}
	if req.Focus {
		if eng := s.resolveEngine(result.SessionID); eng != nil {
			s.setActiveEngine(eng)
			result.Focused = true
		}
	}
	writeJSON(w, http.StatusOK, result)
}

type conversationActivationError struct {
	code      string
	kind      conversationKind
	retryable bool
	err       error
}

func (e *conversationActivationError) Error() string { return e.err.Error() }
func (e *conversationActivationError) Unwrap() error { return e.err }

func writeConversationActivationError(w http.ResponseWriter, err error) {
	if writeSSHHostKeyError(w, err) {
		return
	}
	status := http.StatusServiceUnavailable
	code := "activation_failed"
	retryable := true
	kind := conversationKind("")
	switch {
	case errors.Is(err, errConversationNotFound):
		status, code, retryable = http.StatusNotFound, "conversation_not_found", false
	case errors.Is(err, errInvalidConversationTarget):
		status, code, retryable = http.StatusBadRequest, "invalid_conversation_target", false
	case errors.Is(err, errWorkspaceNotAllowed):
		status, code, retryable = http.StatusForbidden, "workspace_not_allowed", false
	case errors.Is(err, errConversationBusy):
		status, code = http.StatusConflict, "conversation_busy"
	}
	var activationErr *conversationActivationError
	if errors.As(err, &activationErr) {
		code, kind, retryable = activationErr.code, activationErr.kind, activationErr.retryable
		status = http.StatusBadGateway
		if code == "ssh_auth_required" {
			status = http.StatusConflict
		}
	}
	payload := map[string]any{"error": err.Error(), "code": code, "retryable": retryable}
	if kind != "" {
		payload["kind"] = kind
	}
	writeJSON(w, status, payload)
}

// ensureConversation resolves a live engine or cold-activates one from the
// durable session metadata and JSONL state. It never calls setActiveEngine.
func (s *Server) ensureConversation(
	ctx context.Context,
	sessionID, projectPath, source string,
) (activationResult, error) {
	return s.ensureConversationKind(ctx, sessionID, projectPath, source, "")
}

func (s *Server) ensureConversationKind(
	ctx context.Context,
	sessionID, projectPath, source string,
	requestedKind session.WorkspaceKind,
) (activationResult, error) {
	s.taskCreateMu.Lock()
	defer s.taskCreateMu.Unlock()
	return s.ensureConversationLocked(ctx, sessionID, projectPath, source, requestedKind)
}

func (s *Server) ensureConversationLocked(
	ctx context.Context,
	sessionID, projectPath, source string,
	requestedKind session.WorkspaceKind,
) (activationResult, error) {

	var meta *session.SessionMeta
	if sessionID != "" {
		var err error
		meta, err = session.FindSessionMeta(sessionID)
		if err != nil {
			return activationResult{}, fmt.Errorf("load conversation metadata: %w", err)
		}
	}

	if sessionID != "" && meta == nil {
		if s.resolveEngine(sessionID) == nil {
			return activationResult{}, fmt.Errorf("%w: %s", errConversationNotFound, sessionID)
		}
	}

	workspaceKind := requestedKind
	switch {
	case meta != nil:
		workspaceKind = session.NormalizeWorkspaceKind(meta.WorkspaceKind)
	case workspaceKind == "":
		workspaceKind = session.WorkspaceProject
		if sessionID == "" && projectPath == "" {
			if active := s.activeEngine(); active != nil && active.workspaceKind == session.WorkspaceScratch {
				workspaceKind = session.WorkspaceScratch
			}
		}
	default:
		workspaceKind = session.NormalizeWorkspaceKind(workspaceKind)
	}
	project := projectPath
	createdScratch := ""
	if meta == nil && sessionID == "" && workspaceKind == session.WorkspaceScratch {
		if projectPath != "" {
			return activationResult{}, fmt.Errorf("%w: scratch workspace path is managed by JCode", errInvalidConversationTarget)
		}
		var createErr error
		project, createErr = managedworkspace.CreateScratch(time.Now())
		if createErr != nil {
			return activationResult{}, createErr
		}
		createdScratch = project
	}
	cleanupScratch := func() {
		if createdScratch != "" {
			_ = os.Remove(createdScratch) // only removes a failed attempt that stayed empty
		}
	}
	buildMode := s.activeMode()
	var restoredState *session.SessionState
	if meta != nil {
		project = meta.Project
		entries, err := session.LoadSession(sessionID)
		if err != nil {
			return activationResult{}, fmt.Errorf("%w: %s: %v", errConversationNotFound, sessionID, err)
		}
		restoredState = session.ReconstructState(entries)
		buildMode = mode.Approval.String()
		if savedMode, err := session.LoadSessionModeStrict(sessionID); err != nil {
			config.Logger().Printf("[web] activation: mode journal unavailable for %s; restoring approval: %v", sessionID, err)
		} else {
			buildMode = restoredWebSessionMode(savedMode).String()
		}
	} else {
		if project == "" {
			project = engineProject(s.activeEngine())
		}
		// A Cloud-created task must not inherit Full access from whichever Desktop
		// conversation happens to be foregrounded. The command may explicitly
		// promote it to auto after activation.
		if source != "" {
			buildMode = mode.Approval.String()
		}
	}
	var old *Engine
	if sessionID != "" {
		old = s.resolveEngine(sessionID)
	}
	if project == "" {
		project = engineProject(old)
	}
	// The managed scratch root is reserved. A new project-classified session may
	// not bind one of those paths explicitly or by inheriting the active scratch
	// engine; callers must request scratch so a fresh directory is allocated.
	if meta == nil && workspaceKind == session.WorkspaceProject && project != "" {
		if err := managedworkspace.ValidateScratchPath(project); err == nil {
			return activationResult{}, fmt.Errorf("%w: managed scratch workspace cannot be opened as a project", errInvalidConversationTarget)
		}
	}
	if workspaceKind == session.WorkspaceScratch {
		if err := managedworkspace.ValidateScratchPath(project); err != nil {
			cleanupScratch()
			return activationResult{}, fmt.Errorf("%w: %v", errInvalidConversationTarget, err)
		}
	}

	target, err := parseConversationTarget(project)
	if err != nil {
		cleanupScratch()
		return activationResult{}, fmt.Errorf("%w: %v", errInvalidConversationTarget, err)
	}
	if meta == nil && source != "" && target.kind != conversationLocal {
		if err := s.authorizeCloudRemoteWorkspace(target); err != nil {
			return activationResult{}, err
		}
	}

	if old != nil {
		liveErr := validateLiveConversation(ctx, old, target)
		switch {
		case liveErr == nil:
			return activationSnapshot(old, target.kind, false), nil
		case old.running.Load():
			return activationResult{}, fmt.Errorf("%w: %s: %v", errConversationBusy, sessionID, liveErr)
		case meta == nil:
			return activationResult{}, fmt.Errorf("conversation %s cannot be repaired without persisted metadata: %w", sessionID, liveErr)
		default:
			config.Logger().Printf("[web] activation: replacing idle unhealthy runtime %s: %v", sessionID, liveErr)
		}
	}

	eng, err := s.assembleConversationEngine(ctx, sessionID, target, buildMode, workspaceKind)
	if err != nil {
		cleanupScratch()
		return activationResult{}, err
	}
	if restoredState != nil {
		hydrateConversationEngine(eng, restoredState, mode.Parse(buildMode))
	}
	if err := s.publishEngineCandidate(eng, old); err != nil {
		eng.teardown()
		cleanupScratch()
		return activationResult{}, fmt.Errorf("publish conversation %s: %w", eng.taskID, err)
	}
	if sessionID == "" {
		s.stampCloudSync(eng.taskID, source, true)
	}
	return activationSnapshot(eng, target.kind, true), nil
}

func (s *Server) assembleConversationEngine(
	ctx context.Context,
	sessionID string,
	target conversationTarget,
	modeName string,
	workspaceKind session.WorkspaceKind,
) (*Engine, error) {
	if target.kind == conversationLocal {
		factory := s.newEngine
		if session.NormalizeWorkspaceKind(workspaceKind) == session.WorkspaceScratch {
			factory = s.newScratchEngine
		}
		if factory == nil {
			return nil, fmt.Errorf("activate local conversation: task creation is not supported")
		}
		eng, err := s.assembleLocalEngine(sessionID, target.pwd, modeName, factory)
		if err != nil {
			return nil, fmt.Errorf("activate local conversation: %w", err)
		}
		return eng, nil
	}

	var (
		exec tools.RemoteExecutor
		err  error
	)
	switch target.kind {
	case conversationSSH:
		exec, err = s.cloneHealthySSHLease(ctx, target)
		if err == nil && exec != nil {
			break
		}
		if err != nil {
			config.Logger().Printf("[web] activation: healthy SSH lease clone failed for %s; dialing: %v", target.project, err)
		}
		if s.dialSSH != nil {
			exec, err = s.dialSSH(ctx, target.sshHost, target.sshUser)
		} else {
			exec, err = remote.ConnectContext(ctx, remote.SSHOptions{Host: target.sshHost, User: target.sshUser})
		}
	case conversationDocker:
		if s.dialDocker != nil {
			exec, err = s.dialDocker(ctx, target.container)
		} else {
			exec, err = remote.ConnectDocker(ctx, target.container)
		}
	}
	if err != nil {
		code := "docker_unavailable"
		if target.kind == conversationSSH {
			code = "ssh_connection_failed"
			if strings.Contains(strings.ToLower(err.Error()), "unable to authenticate") ||
				strings.Contains(strings.ToLower(err.Error()), "no ssh credentials") {
				code = "ssh_auth_required"
			}
		}
		return nil, &conversationActivationError{
			code: code, kind: target.kind, retryable: true,
			err: fmt.Errorf("connect %s conversation: %w", target.kind, err),
		}
	}
	eng, buildErr := s.assembleRemoteEngine(sessionID, exec, target.pwd, modeName)
	if buildErr != nil {
		_ = exec.Close()
		return nil, fmt.Errorf("activate %s conversation: %w", target.kind, buildErr)
	}
	return eng, nil
}

// cloneHealthySSHLease reuses a transport only through the executor's explicit
// ref-counted lease contract. Sharing the same executor pointer would let one
// Engine teardown close the connection out from under every other task.
func (s *Server) cloneHealthySSHLease(ctx context.Context, target conversationTarget) (tools.RemoteExecutor, error) {
	s.tasksMu.RLock()
	engines := make([]*Engine, 0, len(s.tasks))
	for _, eng := range s.tasks {
		engines = append(engines, eng)
	}
	s.tasksMu.RUnlock()
	for _, eng := range engines {
		if eng == nil || eng.env == nil {
			continue
		}
		live, err := parseConversationTarget(engineProject(eng))
		if err != nil || live.kind != conversationSSH || !sameConversationLocation(live, target) {
			continue
		}
		exec, ok := eng.env.Exec.(tools.RemoteExecutor)
		if !ok {
			continue
		}
		cloner, ok := exec.(tools.RemoteLeaseCloner)
		if !ok {
			continue
		}
		if err := exec.Probe(ctx); err != nil {
			continue
		}
		return cloner.CloneLease()
	}
	return nil, nil
}

func (s *Server) authorizeCloudRemoteWorkspace(target conversationTarget) error {
	all, err := session.ListAllSessions()
	if err != nil {
		return fmt.Errorf("check remote workspace index: %w", err)
	}
	for project := range all {
		indexed, parseErr := parseConversationTarget(project)
		if parseErr == nil && sameConversationLocation(indexed, target) {
			return nil
		}
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	if s.cfg != nil {
		switch target.kind {
		case conversationSSH:
			for _, alias := range s.cfg.SSHAliases {
				if strings.TrimSpace(alias.Path) == "" || strings.TrimSpace(alias.Addr) == "" {
					continue
				}
				candidate, parseErr := parseConversationTarget("ssh://" + strings.TrimSpace(alias.Addr) + pathpkg.Clean(alias.Path))
				if parseErr == nil && sameConversationLocation(candidate, target) {
					return nil
				}
			}
		case conversationDocker:
			for _, alias := range s.cfg.DockerAliases {
				if strings.TrimSpace(alias.Path) == "" || strings.TrimSpace(alias.Container) == "" {
					continue
				}
				candidate, parseErr := parseConversationTarget("docker://" + strings.TrimSpace(alias.Container) + pathpkg.Clean(alias.Path))
				if parseErr == nil && sameConversationLocation(candidate, target) {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("%w: remote project %q is neither indexed nor a saved alias", errWorkspaceNotAllowed, target.project)
}

func hydrateConversationEngine(
	eng *Engine,
	state *session.SessionState,
	restoredMode mode.SessionMode,
) {
	if eng.rebuildForRole != nil {
		eng.rebuildMu.Lock()
		provider, modelName, _ := eng.modelSnapshot()
		built, err := eng.rebuildForRole(state.Agent, provider, modelName)
		if err != nil {
			config.Logger().Printf("[web] activation: custom agent %q unavailable for %s: %v", state.Agent, eng.taskID, err)
			if fallback, fallbackErr := eng.rebuildForRole("", provider, modelName); fallbackErr == nil {
				eng.applyAgentRoleSwitch("", fallback)
			}
		} else {
			eng.applyAgentRoleSwitch(state.Agent, built)
		}
		eng.rebuildMu.Unlock()
	}

	eng.emu.Lock()
	eng.history = state.History
	eng.emu.Unlock()
	if eng.approvalState != nil {
		eng.approvalState.SetSessionMode(restoredMode)
	}
	if eng.todoStore != nil {
		items := make([]tools.TodoItem, len(state.Todos))
		for i, item := range state.Todos {
			items[i] = tools.TodoItem{ID: item.ID, Title: item.Title, Status: tools.TodoStatus(item.Status)}
		}
		eng.todoStore.Update(items)
	}
	if eng.env != nil && eng.env.GoalStore != nil {
		eng.env.GoalStore.RestoreFromSnapshot(state.Goal)
	}
}

func engineProject(eng *Engine) string {
	if eng == nil {
		return ""
	}
	eng.emu.Lock()
	recorder := eng.recorder
	eng.emu.Unlock()
	if recorder != nil && recorder.Project() != "" {
		return recorder.Project()
	}
	if eng.env != nil {
		if exec, ok := eng.env.Exec.(tools.RemoteExecutor); ok {
			return exec.ProjectLabel(eng.pwd)
		}
	}
	return eng.pwd
}

func validateLiveConversation(ctx context.Context, eng *Engine, target conversationTarget) error {
	if eng == nil || eng.env == nil {
		return fmt.Errorf("conversation %s has no execution environment", eng.taskID)
	}
	project := engineProject(eng)
	liveTarget, err := parseConversationTarget(project)
	if err != nil {
		return fmt.Errorf("conversation %s has invalid live project %q: %w", eng.taskID, project, err)
	}
	if liveTarget.kind != target.kind || !sameConversationLocation(liveTarget, target) {
		return fmt.Errorf("conversation %s is live on %q, persisted target is %q", eng.taskID, project, target.project)
	}
	if target.kind == conversationLocal {
		if eng.env.IsRemote() {
			return fmt.Errorf("conversation %s is unexpectedly bound to a remote executor", eng.taskID)
		}
		return nil
	}
	if !eng.env.IsRemote() {
		return fmt.Errorf("conversation %s is remote in the session index but live on the local executor", eng.taskID)
	}
	exec, ok := eng.env.Exec.(tools.RemoteExecutor)
	if !ok {
		return fmt.Errorf("conversation %s has an unsupported remote executor", eng.taskID)
	}
	if err := exec.Probe(ctx); err != nil {
		return fmt.Errorf("conversation %s %s connection is unhealthy: %w", eng.taskID, target.kind, err)
	}
	return nil
}

func sameConversationLocation(a, b conversationTarget) bool {
	if a.kind != b.kind || a.pwd != b.pwd {
		return false
	}
	switch a.kind {
	case conversationLocal:
		return filepath.Clean(a.project) == filepath.Clean(b.project)
	case conversationSSH:
		return sameSSHHost(a.sshHost, b.sshHost) && a.sshUser == b.sshUser
	case conversationDocker:
		return a.container == b.container
	default:
		return false
	}
}

func sameSSHHost(a, b string) bool {
	normalize := func(host string) string {
		host = strings.TrimSpace(host)
		name, port, err := net.SplitHostPort(host)
		if err != nil {
			name, port = strings.Trim(host, "[]"), "22"
		}
		return net.JoinHostPort(strings.ToLower(name), port)
	}
	return normalize(a) == normalize(b)
}
