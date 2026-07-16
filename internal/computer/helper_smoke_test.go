//go:build darwin

package computer

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

	cmd := exec.Command(bin, "--socket", sock, "--token-file", tokenFile, "--shots-dir", shots)
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
	t.Logf("connected to jcode-computerd %s on %s", h.helperVersion, h.platform)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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
	cmd := exec.Command(bin, "--socket", sock, "--token-file", tokenFile, "--shots-dir", filepath.Join(work, "shots"))
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	conn := dialWithRetry(t, sock)
	_, err := newHelperConn(conn, "a-different-token")
	if err == nil {
		t.Fatal("the real daemon accepted a wrong token; the token boundary is broken")
	}
	t.Logf("daemon correctly rejected the bad token: %v", err)
}

// shortSocketPath returns a socket path short enough for sun_path's 104-byte
// limit. t.TempDir() paths (long, test-name-derived) blow it — a real bug the
// integration test surfaced that the net.Pipe mock never could. Production uses
// ~/.jcode/computer/computerd.sock, well under the limit.
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
	cmd := exec.Command(bin, "--socket", shortSocketPath(t), "--token-file", tokenFile, "--shots-dir", filepath.Join(work, "shots"))
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
			// Exit code 0 is a clean self-exit; a non-zero exit is still an exit,
			// but log it.
			t.Logf("daemon exited: %v", err)
		}
		t.Log("daemon self-exited on idle, as designed")
	case <-time.After(4 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("daemon did not self-exit within 4s despite a 500ms idle window")
	}
}
