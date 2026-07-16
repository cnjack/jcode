package computer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// This file is the production side of helperBackend: resolving the daemon
// binary, the socket, and the auth token, then dialing (and spawning the daemon
// if it is not already answering). None of it is exercised by the unit tests,
// which inject a net.Pipe into newHelperConn — the protocol logic is tested
// there, and this is the thin, platform-specific plumbing around it.
//
// macOS only. The design covers Windows (named pipe), but per the current scope
// the Windows helper is not implemented; dialHelper refuses cleanly elsewhere so
// the auto backend selection falls through to a clear "no backend" message
// rather than a confusing dial error.

// helperPaths bundles the filesystem rendezvous points, all under the config dir.
type helperPaths struct {
	dir       string // <config>/computer
	socket    string // <config>/computer/computerd.sock
	tokenFile string // <config>/computer/helper-token  (0600)
	shotsDir  string // <config>/computer/shots
}

func computerPaths(configDir string) helperPaths {
	dir := filepath.Join(configDir, "computer")
	return helperPaths{
		dir:       dir,
		socket:    filepath.Join(dir, "computerd.sock"),
		tokenFile: filepath.Join(dir, "helper-token"),
		shotsDir:  filepath.Join(dir, "shots"),
	}
}

// dialHelper connects to the daemon, spawning it if the socket is not already
// answering, and returns a ready backend. macOS only.
//
// The lazy-spawn shape mirrors jcode-ble (binary resolved next to the running
// executable) and browser/manager's getManaged (dial; if dead, launch; retry).
func dialHelper(ctx context.Context, configDir string) (*helperBackend, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("the computer-use helper is implemented on macOS only")
	}
	p := computerPaths(configDir)
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return nil, fmt.Errorf("prepare helper dir: %w", err)
	}
	token, err := loadOrCreateToken(p.tokenFile)
	if err != nil {
		return nil, err
	}

	// First try: the daemon may already be running (a previous session left it,
	// or the desktop shell started it).
	if conn, err := net.DialTimeout("unix", p.socket, 500*time.Millisecond); err == nil {
		if h, herr := finishDial(conn, token, p.shotsDir, nil); herr == nil {
			return h, nil
		}
		// Answered but handshake failed (stale/incompatible daemon) — fall
		// through to respawn.
	}

	cmd, err := spawnDaemon(p)
	if err != nil {
		return nil, err
	}
	// Retry the dial while the daemon comes up. 5s total, matching the parent
	// design's autolaunch budget.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			_ = cmd.Process.Kill()
			return nil, ctx.Err()
		}
		if conn, derr := net.DialTimeout("unix", p.socket, 200*time.Millisecond); derr == nil {
			return finishDial(conn, token, p.shotsDir, cmd)
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return nil, fmt.Errorf("helper daemon did not answer within 5s of launch")
}

func finishDial(conn net.Conn, token, shotsDir string, cmd *exec.Cmd) (*helperBackend, error) {
	h, err := newHelperConn(conn, token)
	if err != nil {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, err
	}
	h.cmd = cmd
	h.shotsDir = shotsDir
	return h, nil
}

// spawnDaemon launches the native helper. The socket path and the token *file*
// (not the token itself — a command line is world-readable via ps) are passed as
// flags.
func spawnDaemon(p helperPaths) (*exec.Cmd, error) {
	bin := helperBinPath()
	if bin == "" {
		return nil, fmt.Errorf("helper daemon binary (jcode-computerd) not found next to jcode")
	}
	cmd := exec.Command(bin,
		"--socket", p.socket,
		"--token-file", p.tokenFile,
		"--shots-dir", p.shotsDir,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start helper daemon: %w", err)
	}
	return cmd, nil
}

// helperBinPath resolves jcode-computerd next to the running binary, mirroring
// jcode-ble's resolution (exact name, then the dev-mode target-triple glob),
// with an env override for the desktop shell.
func helperBinPath() string {
	if p := os.Getenv("JCODE_COMPUTERD"); p != "" {
		if isExecutable(p) {
			return p
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	if p := filepath.Join(dir, "jcode-computerd"); isExecutable(p) {
		return p
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "jcode-computerd-*")); len(matches) > 0 {
		for _, m := range matches {
			if isExecutable(m) {
				return m
			}
		}
	}
	return ""
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// loadOrCreateToken reads the 0600 token file or mints one. Reused shape from
// browser/tokens.go's StableToken: a long-lived secret only the same user can
// read, presented on every connection.
func loadOrCreateToken(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(data)); tok != "" {
			return tok, nil
		}
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate helper token: %w", err)
	}
	tok := hex.EncodeToString(b[:])
	if err := os.WriteFile(path, []byte(tok), 0o600); err != nil {
		return "", fmt.Errorf("write helper token: %w", err)
	}
	return tok, nil
}

// readShotRef reads a PNG the daemon wrote to the shared shots dir. ref is
// validated to live inside shotsDir before touching the filesystem, so a
// compromised or buggy daemon cannot use it to read an arbitrary file.
func (h *helperBackend) readShotRef(ref string) ([]byte, error) {
	if h.shotsDir == "" {
		return nil, fmt.Errorf("daemon returned a screenshot reference but no shots dir is configured")
	}
	clean := filepath.Clean(ref)
	rel, err := filepath.Rel(h.shotsDir, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("screenshot reference %q is outside the shots dir", ref)
	}
	return os.ReadFile(clean)
}
