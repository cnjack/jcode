package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"github.com/cnjack/jcode/internal/config"
)

// ptySession represents a running PTY session.
type ptySession struct {
	id   string
	cmd  *exec.Cmd
	ptmx *os.File
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
	CheckOrigin: func(r *http.Request) bool { return true },
}

// create starts a new PTY session and returns its ID.
func (m *ptyManager) create(workDir string) (string, error) {
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

	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("pty_%d", m.nextID)
	sess := &ptySession{id: id, cmd: cmd, ptmx: ptmx}
	m.sessions[id] = sess
	m.mu.Unlock()

	// Clean up when shell exits.
	go func() {
		cmd.Wait()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		ptmx.Close()
	}()

	config.Logger().Printf("[pty] created session %s (shell=%s, dir=%s)", id, shell, workDir)
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
		sess.cmd.Process.Kill()
		sess.ptmx.Close()
	}
}

// closeAll terminates all PTY sessions.
func (m *ptyManager) closeAll() {
	m.mu.Lock()
	sessions := make([]*ptySession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*ptySession)
	m.mu.Unlock()
	for _, s := range sessions {
		s.cmd.Process.Kill()
		s.ptmx.Close()
	}
}

// serveWS handles the WebSocket connection for a PTY session.
// Data flows: PTY stdout → WebSocket → client, client → WebSocket → PTY stdin.
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
	defer conn.Close()

	// Handle resize messages from client.
	// Client sends JSON: {"type":"resize","cols":80,"rows":24}
	// or raw bytes for stdin input.

	// PTY → WebSocket (read from PTY, send to client)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := sess.ptmx.Read(buf)
			if err != nil {
				if err != io.EOF {
					config.Logger().Printf("[pty] read error: %v", err)
				}
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// WebSocket → PTY (read from client, write to PTY)
	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.TextMessage {
			// Check for resize command
			if len(msg) > 0 && msg[0] == '{' {
				m.handleControlMessage(sess, msg)
				continue
			}
		}
		// Write input to PTY
		if _, err := sess.ptmx.Write(msg); err != nil {
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
		pty.Setsize(sess.ptmx, &pty.Winsize{
			Cols: ctrl.Cols,
			Rows: ctrl.Rows,
		})
	}
}
