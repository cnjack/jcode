//go:build darwin

package computer

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestSmokeSwiftDaemon drives the REAL Swift daemon over a REAL unix socket,
// proving the wire format is symmetric across the language boundary — the mock
// daemon in helper_test.go only proves Go talks to Go.
//
// Gated behind JCODE_COMPUTERD_SMOKE=1 because it needs a swiftc-built binary and
// the ability to bind a unix socket, neither guaranteed in CI (mirrors
// browser/smoke_test.go's gate). Build the daemon first:
//
//	swiftc -O -o /tmp/jcode-computerd cmd/jcode-computerd/main.swift
//	JCODE_COMPUTERD_SMOKE=1 go test ./internal/computer/ -run TestSmokeSwiftDaemon -v
func TestSmokeSwiftDaemon(t *testing.T) {
	if os.Getenv("JCODE_COMPUTERD_SMOKE") == "" {
		t.Skip("set JCODE_COMPUTERD_SMOKE=1 (needs a swiftc-built jcode-computerd and socket bind)")
	}
	bin := os.Getenv("JCODE_COMPUTERD_BIN")
	if bin == "" {
		bin = "/tmp/jcode-computerd"
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("daemon binary %s not found (build it with swiftc): %v", bin, err)
	}

	work := t.TempDir()
	sock := shortSocketPath(t)
	tokenFile := filepath.Join(work, "token")
	const token = "smoke-token-9f3a"
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	shots := filepath.Join(work, "shots")

	cmd := exec.Command(bin, "--socket", sock, "--token-file", tokenFile, "--shots-dir", shots,
		"--client-pid", strconv.Itoa(os.Getpid()))
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	conn := dialWithRetry(t, sock)

	// The real handshake, against the real daemon.
	h, err := newHelperConn(conn, token)
	if err != nil {
		t.Fatalf("handshake with the real daemon failed: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	if h.platform != "darwin" {
		t.Errorf("daemon reported platform %q, want darwin", h.platform)
	}
	permissions := h.PermissionStatus()
	if permissions.Accessibility == PermissionUnknown || permissions.ScreenRecording == PermissionUnknown {
		t.Errorf("current daemon omitted permission status: %+v", permissions)
	}
	t.Logf("connected to jcode-computerd %s on %s (Accessibility=%s ScreenRecording=%s)",
		h.helperVersion, h.platform, permissions.Accessibility, permissions.ScreenRecording)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if refreshed, err := h.RefreshPermissionStatus(ctx); err != nil {
		t.Fatalf("refresh permission status: %v", err)
	} else if refreshed.Accessibility == PermissionUnknown || refreshed.ScreenRecording == PermissionUnknown {
		t.Errorf("refreshed permission status is unknown: %+v", refreshed)
	}

	// These need no TCC grant, so they must genuinely work end to end.
	apps, err := h.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) == 0 {
		t.Error("ListApps returned nothing — there is always at least Finder")
	}
	t.Logf("ListApps returned %d apps; first: %s", len(apps), apps[0].BundleID)

	front, err := h.Frontmost(ctx)
	if err != nil {
		t.Fatalf("Frontmost: %v", err)
	}
	if front.BundleID == "" {
		t.Error("Frontmost returned an empty bundle id")
	}
	t.Logf("Frontmost: %s (%s)", front.Name, front.BundleID)

	if _, err := h.ReadClipboard(ctx); err != nil {
		t.Errorf("ReadClipboard: %v", err)
	}

	// Tree needs the Accessibility grant. Either it works (granted) or it returns
	// permissionsNotGranted — both are correct; a crash or a hang is not.
	_, terr := h.Tree(ctx, front.BundleID)
	if terr != nil {
		t.Logf("Tree returned (expected without Accessibility grant): %v", terr)
	} else {
		t.Logf("Tree succeeded — Accessibility appears granted")
	}
}

// TestSmokeSwiftDaemonHandshake is the CI-safe subset of the real daemon
// smoke test. It proves the correct-token protocol and permission reporting
// without assuming the runner has a logged-in GUI session or a frontmost app.
func TestSmokeSwiftDaemonHandshake(t *testing.T) {
	if os.Getenv("JCODE_COMPUTERD_SMOKE") == "" {
		t.Skip("set JCODE_COMPUTERD_SMOKE=1")
	}
	bin := os.Getenv("JCODE_COMPUTERD_BIN")
	if bin == "" {
		bin = "/tmp/jcode-computerd"
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("daemon binary %s not found (build it with swiftc): %v", bin, err)
	}

	work := t.TempDir()
	sock := shortSocketPath(t)
	tokenFile := filepath.Join(work, "token")
	const token = "ci-handshake-token-1c92"
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "--socket", sock, "--token-file", tokenFile,
		"--shots-dir", filepath.Join(work, "shots"), "--client-pid", strconv.Itoa(os.Getpid()))
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	conn := dialWithRetry(t, sock)
	h, err := newHelperConn(conn, token)
	if err != nil {
		t.Fatalf("handshake with the real daemon failed: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	if h.platform != "darwin" {
		t.Errorf("daemon reported platform %q, want darwin", h.platform)
	}
	if h.helperVersion == "" {
		t.Error("daemon omitted helper version")
	}
	assertKnownHelperPermissions(t, "handshake", h.PermissionStatus())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	refreshed, err := h.RefreshPermissionStatus(ctx)
	if err != nil {
		t.Fatalf("refresh permission status: %v", err)
	}
	assertKnownHelperPermissions(t, "refresh", refreshed)
}

func assertKnownHelperPermissions(t *testing.T, stage string, permissions HelperPermissions) {
	t.Helper()
	if permissions.Accessibility == PermissionUnknown || permissions.ScreenRecording == PermissionUnknown {
		t.Errorf("%s permission status is unknown: %+v", stage, permissions)
	}
}

// TestSmokeSwiftDaemonRejectsBadToken proves the daemon's own auth boundary: a
// wrong token is refused by the real daemon, not just by the Go mock.
func TestSmokeSwiftDaemonRejectsBadToken(t *testing.T) {
	if os.Getenv("JCODE_COMPUTERD_SMOKE") == "" {
		t.Skip("set JCODE_COMPUTERD_SMOKE=1")
	}
	bin := os.Getenv("JCODE_COMPUTERD_BIN")
	if bin == "" {
		bin = "/tmp/jcode-computerd"
	}
	work := t.TempDir()
	sock := shortSocketPath(t)
	tokenFile := filepath.Join(work, "token")
	if err := os.WriteFile(tokenFile, []byte("the-real-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--socket", sock, "--token-file", tokenFile, "--shots-dir", filepath.Join(work, "shots"),
		"--client-pid", strconv.Itoa(os.Getpid()))
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	conn := dialWithRetry(t, sock)
	_, err := newHelperConn(conn, "a-different-token")
	if err == nil {
		t.Fatal("the real daemon accepted a wrong per-instance handshake token")
	}
	t.Logf("daemon correctly rejected the bad token: %v", err)
}

// shortSocketPath returns a socket path short enough for sun_path's 104-byte
// limit. t.TempDir() paths (long, test-name-derived) blow it — a real bug the
// integration test surfaced that the net.Pipe mock never could. Production uses
// ~/.jcode/computer/computerd-<instance>.sock, normally under the limit.
func shortSocketPath(t *testing.T) string {
	f, err := os.CreateTemp("", "jcc-*.sock")
	if err != nil {
		t.Fatal(err)
	}
	p := f.Name()
	_ = f.Close()
	_ = os.Remove(p) // the daemon binds it; we just wanted a short unique name
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

func dialWithRetry(t *testing.T, sock string) net.Conn {
	t.Helper()
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("unix", sock); err == nil {
			return c
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("daemon did not bind the socket within 5s")
	return nil
}

// TestSmokeDaemonIdleExit proves the daemon self-exits after its idle window,
// so a crashed jcode does not leave an automation daemon running (design §5, §8).
// Uses JCODE_COMPUTERD_IDLE_MS to shrink the window from 5min to 500ms.
func TestSmokeDaemonIdleExit(t *testing.T) {
	if os.Getenv("JCODE_COMPUTERD_SMOKE") == "" {
		t.Skip("set JCODE_COMPUTERD_SMOKE=1")
	}
	bin := os.Getenv("JCODE_COMPUTERD_BIN")
	if bin == "" {
		bin = "/tmp/jcode-computerd"
	}
	work := t.TempDir()
	tokenFile := filepath.Join(work, "token")
	if err := os.WriteFile(tokenFile, []byte("t"), 0o600); err != nil {
		t.Fatal(err)
	}
	const (
		currentInstance = "00112233445566778899aabbccddeeff"
		oldInstance     = "ffeeddccbbaa99887766554433221100"
		deadInstance    = "0123456789abcdef0123456789abcdef"
	)
	pidText := strconv.Itoa(os.Getpid())
	shots := filepath.Join(work, "handoff-"+pidText+"-"+currentInstance)
	samePIDStale := filepath.Join(work, "handoff-"+pidText+"-"+oldInstance)
	staleLegacy := filepath.Join(work, "handoff-2147483646")
	staleNonce := filepath.Join(work, "handoff-2147483646-"+deadInstance)
	malformed := filepath.Join(work, "handoff-2147483646-short")

	live := exec.Command("sleep", "10")
	if err := live.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = live.Process.Kill()
		_ = live.Wait()
	})
	livePID := strconv.Itoa(live.Process.Pid)
	liveLegacy := filepath.Join(work, "handoff-"+livePID)
	liveNonce := filepath.Join(work, "handoff-"+livePID+"-"+deadInstance)

	removeOnSweep := []string{shots, samePIDStale, staleLegacy, staleNonce}
	preserveOnSweep := []string{malformed, liveLegacy, liveNonce}
	for _, dir := range append(removeOnSweep, preserveOnSweep...) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "orphan.png"), []byte("pixels"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Legacy PID-only directories get a migration grace so a PID-reuse race
	// cannot immediately remove a new old-client directory.
	old := time.Now().Add(-11 * time.Minute)
	if err := os.Chtimes(staleLegacy, old, old); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--socket", shortSocketPath(t), "--token-file", tokenFile, "--shots-dir", shots,
		"--client-pid", strconv.Itoa(os.Getpid()))
	cmd.Env = append(os.Environ(), "JCODE_COMPUTERD_IDLE_MS=500")
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Never connect. The daemon should exit on its own within the idle window.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("daemon exited with an error instead of its clean idle shutdown: %v", err)
		}
		t.Log("daemon self-exited on idle, as designed")
		for _, dir := range removeOnSweep {
			if _, statErr := os.Lstat(dir); !os.IsNotExist(statErr) {
				t.Fatalf("daemon left handoff directory %s behind: %v", dir, statErr)
			}
		}
		for _, dir := range preserveOnSweep {
			if _, statErr := os.Lstat(dir); statErr != nil {
				t.Fatalf("daemon removed non-owned/live handoff directory %s: %v", dir, statErr)
			}
		}
	case <-time.After(4 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("daemon did not self-exit within 4s despite a 500ms idle window")
	}
}
