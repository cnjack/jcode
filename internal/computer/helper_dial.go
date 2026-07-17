package computer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This file is the production side of helperBackend: resolving the daemon
// binary, the socket, and the auth token, then dialing (and spawning the daemon
// if it is not already answering). None of it is exercised by the unit tests,
// which inject a net.Pipe into newHelperConn — the protocol logic is tested
// there, and this is the thin, platform-specific plumbing around it.
//
// macOS only. There is intentionally no production fallback backend: callers on
// another platform fail closed before this path, and persisted settings cannot
// select a mock or AppleScript implementation.

// helperPaths bundles the filesystem rendezvous points, all under the config dir.
type helperPaths struct {
	dir       string // <config>/computer
	socket    string // <config>/computer/computerd-<process-instance>.sock
	tokenFile string // <config>/computer/helper-token-<jcode-pid>  (0600)
	shotsDir  string // <config>/computer/handoff-<jcode-pid>-<process-instance>
}

const helperInstanceIDBytes = 16

var loadHelperProcessInstanceID = sync.OnceValues(func() (string, error) {
	var raw [helperInstanceIDBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate computer helper process instance id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
})

func computerPaths(configDir string) (helperPaths, error) {
	instanceID, err := loadHelperProcessInstanceID()
	if err != nil {
		return helperPaths{}, err
	}
	return computerPathsForInstance(configDir, os.Getpid(), instanceID), nil
}

func computerPathsForInstance(configDir string, pid int, instanceID string) helperPaths {
	dir := filepath.Join(configDir, "computer")
	return helperPaths{
		dir:       dir,
		socket:    filepath.Join(dir, fmt.Sprintf("computerd-%s.sock", instanceID)),
		tokenFile: filepath.Join(dir, fmt.Sprintf("helper-token-%d", pid)),
		// Native capture files are a short-lived IPC handoff, not the public
		// screenshot cache. A process-instance nonce prevents PID reuse from
		// colliding with an old daemon or its handoff directory; reconnects in
		// this process reuse the same nonce and paths.
		shotsDir: filepath.Join(dir, fmt.Sprintf("handoff-%d-%s", pid, instanceID)),
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
	p, err := computerPaths(configDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return nil, fmt.Errorf("prepare helper dir: %w", err)
	}
	// This directory belongs only to the current jcode process instance. Clearing
	// it before every initial dial/reconnect recovers a handoff left when the
	// previous daemon died after producing a file but before this client consumed
	// it. PID reuse cannot redirect this cleanup because the nonce is stable only
	// for this process lifetime.
	if err := removeOwnedHelperHandoffDir(p.shotsDir); err != nil {
		return nil, fmt.Errorf("clean helper screenshot handoff: %w", err)
	}
	token, err := loadOrCreateToken(p.tokenFile)
	if err != nil {
		return nil, err
	}

	// First try: the daemon may already be running (a previous session left it,
	// or the desktop shell started it).
	if conn, err := net.DialTimeout("unix", p.socket, 500*time.Millisecond); err == nil {
		if h, herr := finishDial(ctx, conn, token, p.shotsDir, nil, configDir); herr == nil {
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
			_ = cmd.Wait()
			return nil, ctx.Err()
		}
		if conn, derr := net.DialTimeout("unix", p.socket, 200*time.Millisecond); derr == nil {
			return finishDial(ctx, conn, token, p.shotsDir, cmd, configDir)
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return nil, fmt.Errorf("helper daemon did not answer within 5s of launch")
}

func finishDial(ctx context.Context, conn net.Conn, token, shotsDir string, cmd *exec.Cmd, configDir string) (*helperBackend, error) {
	h, err := newHelperConnContext(ctx, conn, token)
	if err != nil {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		return nil, err
	}
	h.cmd = cmd
	h.shotsDir = shotsDir
	h.ownsShotsDir = true
	h.redial = func(ctx context.Context) (*helperBackend, error) {
		return dialHelper(ctx, configDir)
	}
	return h, nil
}

func removeOwnedHelperHandoffDir(dir string) error {
	if dir == "" {
		return nil
	}
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) || !validHelperHandoffName(filepath.Base(dir)) {
		return fmt.Errorf("refusing to remove non-handoff path %q", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove helper handoff directory: %w", err)
	}
	return nil
}

func validHelperHandoffName(name string) bool {
	const prefix = "handoff-"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(name, prefix), "-")
	if len(parts) != 1 && len(parts) != 2 {
		return false
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil || pid <= 1 {
		return false
	}
	if len(parts) == 1 {
		return true // migration compatibility for handoff-<pid>
	}
	if len(parts[1]) != helperInstanceIDBytes*2 {
		return false
	}
	decoded, err := hex.DecodeString(parts[1])
	return err == nil && len(decoded) == helperInstanceIDBytes && hex.EncodeToString(decoded) == parts[1]
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
		"--client-pid", fmt.Sprintf("%d", os.Getpid()),
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start helper daemon: %w", err)
	}
	return cmd, nil
}

// helperBinPath resolves jcode-computerd next to the running binary, mirroring
// jcode-ble's resolution (exact name, then the dev-mode target-triple glob),
// with an env override for the desktop shell. The .app bundle is preferred
// over the bare binary: only the bundle gives the helpers their own stable
// TCC identity ("jcode Computer Use", with its own icon) instead of a
// per-binary row in System Settings.
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
	if p := filepath.Join(dir, "jcode-computerd.app", "Contents", "MacOS", "jcode-computerd"); isExecutable(p) {
		return p
	}
	// Desktop shell: jcode runs from jcode-desktop.app/Contents/MacOS and the
	// helper bundle ships in the app's Resources (tauri.macos.conf.json).
	if p := filepath.Join(dir, "..", "Resources", "jcode-computerd.app", "Contents", "MacOS", "jcode-computerd"); isExecutable(p) {
		return p
	}
	if p := filepath.Join(dir, "jcode-computerd"); isExecutable(p) {
		return p
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "jcode-computerd-*")); len(matches) > 0 {
		if candidate := selectHelperBin(matches); candidate != "" {
			return candidate
		}
	}
	return ""
}

func selectHelperBin(matches []string) string {
	for _, match := range matches {
		// The capture worker and onboarding UI intentionally share the
		// jcode-computerd prefix. Never try to launch either as the socket
		// server (on x86 triples they sort before the daemon).
		if strings.HasPrefix(filepath.Base(match), "jcode-computerd-capture") ||
			strings.HasPrefix(filepath.Base(match), "jcode-computerd-onboarding") ||
			strings.HasPrefix(filepath.Base(match), "jcode-computerd.app") {
			continue
		}
		if isExecutable(match) {
			return match
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
	pathInfo, err := os.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("screenshot reference %q is a symbolic link", ref)
	}
	f, err := os.Open(clean)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
		// The tool layer persists its own public copy. This reference is an IPC
		// handoff file and should not accumulate indefinitely, including when a
		// malformed/oversized file is rejected.
		_ = os.Remove(clean)
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("screenshot reference %q is not a regular file", ref)
	}
	if !os.SameFile(pathInfo, info) {
		return nil, fmt.Errorf("screenshot reference %q changed while opening", ref)
	}
	if info.Size() > MaxScreenshotBytes {
		return nil, fmt.Errorf("screenshot is %d bytes; maximum is %d", info.Size(), MaxScreenshotBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxScreenshotBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxScreenshotBytes {
		return nil, fmt.Errorf("screenshot exceeds maximum of %d bytes", MaxScreenshotBytes)
	}
	return data, nil
}
