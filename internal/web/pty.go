package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/gorilla/websocket"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/tools"
)

// ptyBackend abstracts the transport behind a terminal session: a local PTY, or
// a `docker exec` TTY stream into a bound container.
type ptyBackend interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	Close() error
}

// ptySession represents a running terminal session.
type ptySession struct {
	id      string
	ownerID string // task id that created it, so a project/remote switch only closes its own
	backend ptyBackend
}

// ptyManager manages PTY sessions.
type ptyManager struct {
	mu       sync.Mutex
	sessions map[string]*ptySession
	nextID   int
}

func newPTYManager() *ptyManager {
	return &ptyManager{
		sessions: make(map[string]*ptySession),
	}
}

var upgrader = websocket.Upgrader{
	// Same cross-origin gate as the main event WebSocket. authMiddleware (auth.go)
	// runs first and enforces the token before this upgrade is reached; this is
	// the second line of defence. It previously returned true unconditionally,
	// which let any web page open a PTY (interactive shell) over the loopback /
	// exposed port — a direct RCE on a non-loopback bind.
	CheckOrigin:  isAllowedWebOrigin,
	Subprotocols: []string{wsAuthSubprotocol},
}

// register stores a backend under a fresh session id owned by ownerID.
func (m *ptyManager) register(ownerID string, backend ptyBackend) string {
	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("pty_%d", m.nextID)
	m.sessions[id] = &ptySession{id: id, ownerID: ownerID, backend: backend}
	m.mu.Unlock()
	return id
}

// remove drops a session from the map (without closing its backend).
func (m *ptyManager) remove(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

// create starts a new local PTY session owned by ownerID and returns its ID.
func (m *ptyManager) create(workDir, ownerID string) (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", err
	}

	backend := &localPTYBackend{cmd: cmd, ptmx: ptmx}
	id := m.register(ownerID, backend)

	// Clean up when the shell exits.
	go func() {
		_ = cmd.Wait()
		m.remove(id)
		_ = ptmx.Close()
	}()

	config.Logger().Printf("[pty] created local session %s (shell=%s, dir=%s)", id, shell, workDir)
	return id, nil
}

// createDocker starts a TTY `docker exec` session inside the container and
// returns its ID. The shell is bash if present, otherwise sh.
//
// The terminal holds its OWN container reference for its lifetime (via
// AcquireDockerContainer): a concurrent env switch releases the engine's
// reference, and without an independent one the last release would stop the
// container out from under a live terminal. For a container the user already had
// running this takes no reference and the matching Close() is a harmless no-op.
func (m *ptyManager) createDocker(containerRef, workDir, ownerID string) (string, error) {
	ctx := context.Background()
	exec, err := tools.AcquireDockerContainer(ctx, containerRef)
	if err != nil {
		return "", err
	}
	cli, err := tools.DockerClient()
	if err != nil {
		_ = exec.Close()
		return "", err
	}
	containerID := exec.ContainerID()
	resp, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          []string{"sh", "-c", "exec bash 2>/dev/null || exec sh"},
		WorkingDir:   workDir,
		Env:          []string{"TERM=xterm-256color"},
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		_ = exec.Close()
		return "", err
	}
	att, err := cli.ContainerExecAttach(ctx, resp.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		_ = exec.Close()
		return "", err
	}

	backend := &dockerPTYBackend{cli: cli, execID: resp.ID, att: att, exec: exec}
	id := m.register(ownerID, backend)

	// Clean up when the exec process exits. Nothing else waits on it, so poll
	// the exec state (mirrors the local cmd.Wait cleanup).
	go func() {
		for {
			time.Sleep(time.Second)
			insp, ierr := cli.ContainerExecInspect(context.Background(), resp.ID)
			if ierr != nil || !insp.Running {
				m.remove(id)
				_ = backend.Close()
				return
			}
		}
	}()

	config.Logger().Printf("[pty] created docker session %s (container=%s, dir=%s)", id, shortContainer(containerID), workDir)
	return id, nil
}

// get returns a PTY session by ID.
func (m *ptyManager) get(id string) *ptySession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// list returns all active session IDs.
func (m *ptyManager) list() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// kill terminates a PTY session.
func (m *ptyManager) kill(id string) {
	m.mu.Lock()
	sess := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if sess != nil {
		_ = sess.backend.Close()
	}
}

// closeAll terminates all PTY sessions (server shutdown).
func (m *ptyManager) closeAll() {
	m.mu.Lock()
	sessions := make([]*ptySession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*ptySession)
	m.mu.Unlock()
	for _, s := range sessions {
		_ = s.backend.Close()
	}
}

// closeForTask terminates only the PTY sessions owned by taskID, leaving other
// concurrent tasks' terminals alive. An empty taskID matches nothing.
func (m *ptyManager) closeForTask(taskID string) {
	if taskID == "" {
		return
	}
	m.mu.Lock()
	var sessions []*ptySession
	for id, s := range m.sessions {
		if s.ownerID == taskID {
			sessions = append(sessions, s)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()
	for _, s := range sessions {
		_ = s.backend.Close()
	}
}

// serveWS handles the WebSocket connection for a PTY session.
// Data flows: backend stdout → WebSocket → client, client → WebSocket → backend stdin.
func (m *ptyManager) serveWS(w http.ResponseWriter, r *http.Request, id string) {
	sess := m.get(id)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		config.Logger().Printf("[pty] websocket upgrade error: %v", err)
		return
	}
	defer func() { _ = conn.Close() }()

	// backend → WebSocket (read from backend, send to client)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := sess.backend.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					config.Logger().Printf("[pty] read error: %v", err)
				}
				_ = conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
		}
	}()

	// WebSocket → backend (read from client, write to backend stdin)
	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.TextMessage {
			// Check for resize command: {"type":"resize","cols":80,"rows":24}
			if len(msg) > 0 && msg[0] == '{' {
				m.handleControlMessage(sess, msg)
				continue
			}
		}
		if _, err := sess.backend.Write(msg); err != nil {
			break
		}
	}

	<-done
}

func (m *ptyManager) handleControlMessage(sess *ptySession, msg []byte) {
	// Parse JSON control message: {"type":"resize","cols":80,"rows":24}
	var ctrl struct {
		Type string `json:"type"`
		Cols uint16 `json:"cols"`
		Rows uint16 `json:"rows"`
	}
	if err := json.Unmarshal(msg, &ctrl); err != nil {
		return
	}
	if ctrl.Type == "resize" && ctrl.Cols > 0 && ctrl.Rows > 0 {
		_ = sess.backend.Resize(ctrl.Cols, ctrl.Rows)
	}
}

// ---------------------------------------------------------------------------
// Backends
// ---------------------------------------------------------------------------

// localPTYBackend is a PTY attached to a local shell process.
type localPTYBackend struct {
	cmd  *exec.Cmd
	ptmx *os.File
}

func (b *localPTYBackend) Read(p []byte) (int, error)  { return b.ptmx.Read(p) }
func (b *localPTYBackend) Write(p []byte) (int, error) { return b.ptmx.Write(p) }
func (b *localPTYBackend) Resize(cols, rows uint16) error {
	return pty.Setsize(b.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}
func (b *localPTYBackend) Close() error {
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	return b.ptmx.Close()
}

// dockerPTYBackend is a TTY `docker exec` stream into a container.
type dockerPTYBackend struct {
	cli    *client.Client
	execID string
	att    dockertypes.HijackedResponse
	exec   *tools.DockerExecutor // owns a container ref for the terminal's lifetime
}

func (b *dockerPTYBackend) Read(p []byte) (int, error)  { return b.att.Reader.Read(p) }
func (b *dockerPTYBackend) Write(p []byte) (int, error) { return b.att.Conn.Write(p) }
func (b *dockerPTYBackend) Resize(cols, rows uint16) error {
	return b.cli.ContainerExecResize(context.Background(), b.execID, container.ResizeOptions{
		Height: uint(rows),
		Width:  uint(cols),
	})
}
func (b *dockerPTYBackend) Close() error {
	b.att.Close()
	if b.exec != nil {
		// Release this terminal's container ref; on the last release of a
		// container we started, this stops it.
		_ = b.exec.Close()
	}
	return nil
}

func shortContainer(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func (s *Server) handleCreatePTY(w http.ResponseWriter, r *http.Request) {
	pwd, owner := "", ""
	var dockerExec *tools.DockerExecutor
	if eng := s.activeEngine(); eng != nil {
		pwd, owner = eng.pwd, eng.taskID
		// A container-bound engine gets a terminal INSIDE the container; SSH and
		// local engines keep a local shell (SSH-in-terminal remains a known gap).
		if eng.env != nil {
			if de, ok := eng.env.Exec.(*tools.DockerExecutor); ok {
				dockerExec = de
			}
		}
	}

	var (
		id  string
		err error
	)
	if dockerExec != nil {
		// createDocker acquires its own container ref (so an env switch can't stop
		// the container under a live terminal) and resolves the shared client itself.
		id, err = s.ptyMgr.createDocker(dockerExec.ContainerID(), pwd, owner)
	} else {
		id, err = s.ptyMgr.create(pwd, owner)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *Server) handleListPTY(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.ptyMgr.list()})
}

func (s *Server) handleKillPTY(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.ptyMgr.kill(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePTYWebSocket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.ptyMgr.serveWS(w, r, id)
}
