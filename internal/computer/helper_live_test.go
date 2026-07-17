//go:build darwin

package computer

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/uitree"
)

// TestLiveDriveNotes is the one thing no other test can be: the daemon driving a
// REAL macOS app under a REAL TCC grant. It launches Notes, reads its live
// accessibility tree, and — if Accessibility is granted — presses "New Note" and
// types text you can watch appear on screen.
//
// It never fails on a missing grant (that's an external condition, not a bug):
// without the grant it prints exactly how to grant it and returns. Run it, grant
// Accessibility to ~/.jcode/computer/jcode-computerd, run it again.
//
//	swiftc -O -o ~/.jcode/computer/jcode-computerd cmd/jcode-computerd/main.swift
//	JCODE_COMPUTERD_LIVE=1 JCODE_COMPUTERD_BIN=$HOME/.jcode/computer/jcode-computerd \
//	  go test ./internal/computer/ -run TestLiveDriveNotes -v
func TestLiveDriveNotes(t *testing.T) {
	if os.Getenv("JCODE_COMPUTERD_LIVE") == "" {
		t.Skip("set JCODE_COMPUTERD_LIVE=1 to drive a real app (needs a TCC grant you approve by hand)")
	}
	h := liveDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const notes = "com.apple.Notes"
	t.Log("launching Notes…")
	if err := h.Launch(ctx, notes); err != nil {
		t.Fatalf("Launch Notes: %v", err)
	}
	time.Sleep(2 * time.Second) // let the window come up

	// Synthesized input goes to whatever is frontmost, so Notes must actually be
	// frontmost or the typing lands elsewhere. Wait for it, and report if it does
	// not come forward — that is the likely reason text "disappears".
	var front App
	for i := 0; i < 12; i++ {
		front, _ = h.Frontmost(ctx)
		if front.BundleID == notes {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("frontmost app before typing: %s (%s)", front.Name, front.BundleID)
	if front.BundleID != notes {
		t.Logf("⚠ Notes is NOT frontmost (%s is) — a background daemon's launch did not steal focus; "+
			"input will miss. This is the real 'daemon can't force the foreground' constraint.", front.BundleID)
	}

	nodes, err := h.Tree(ctx, notes)
	if err != nil {
		if strings.Contains(err.Error(), "not granted") || strings.Contains(err.Error(), "permission") {
			t.Log("──────────────────────────────────────────────────────────────")
			t.Log("Accessibility is NOT yet granted to the daemon. To grant it:")
			t.Log("  1. Open  System Settings › Privacy & Security › Accessibility")
			t.Log("  2. Click +  and add:  ~/.jcode/computer/jcode-computerd")
			t.Log("     (in the file picker press ⌘⇧G and paste that path)")
			t.Log("  3. Toggle it ON, then re-run this test.")
			t.Log("──────────────────────────────────────────────────────────────")
			return
		}
		t.Fatalf("Tree: %v", err)
	}

	// Granted — this is a live read of Notes's real UI.
	t.Logf("✅ read %d live AX nodes from Notes", len(nodes))
	rendered := uitree.Build(toUITree(nodes), "interactive", 1, 60, nil, 0)
	t.Logf("live Notes snapshot:\n%s", rendered.Text)

	// New note via cmd+N — more reliable than finding a button, and it puts the
	// caret in the note body so the typed text actually lands somewhere visible.
	// (The first demo typed into whatever had focus, which was not an editable
	// note — a faithful illustration of "synthesized input goes to the current
	// focus", design §4.3.)
	t.Log("pressing cmd+N to create a new note…")
	if err := h.Perform(ctx, Action{Kind: "press", BundleID: notes, Key: "cmd+n"}); err != nil {
		t.Logf("cmd+N returned: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)

	msg := "jcode computer-use is driving Notes for real"
	t.Logf("typing into the new note: %q", msg)
	if err := h.Perform(ctx, Action{Kind: "type", BundleID: notes, Text: msg}); err != nil {
		t.Fatalf("type: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Read the tree back and confirm the text actually landed in the UI — proof
	// it went into Notes, not into the void.
	after, err := h.Tree(ctx, notes)
	if err == nil {
		found := false
		for _, n := range after {
			if strings.Contains(n.Value, "driving Notes for real") || strings.Contains(n.Name, "driving Notes for real") {
				found = true
				break
			}
		}
		if found {
			t.Log("✅ VERIFIED: the typed text is present in Notes's live AX tree")
		} else {
			t.Log("⚠ typed, but the text was not found in the tree — check whether Notes was frontmost")
		}
	}
	t.Log("✅ done — look at Notes: the text above was typed by the daemon.")
}

// liveDaemon connects to the daemon. Two modes:
//
//   - JCODE_COMPUTERD_SOCK set → connect to an ALREADY-RUNNING daemon (spawned by
//     an authorized parent). This is the mode that actually gets a TCC grant: AX
//     authorization is attributed to the daemon's responsible process (its
//     spawner), and go test as an intermediary breaks that chain. Spawn the
//     daemon from an authorized process, point this at its socket.
//   - otherwise → spawn the fixed-path daemon ourselves (works only if go test's
//     own responsible process is authorized).
func liveDaemon(t *testing.T) *helperBackend {
	t.Helper()
	if sock := os.Getenv("JCODE_COMPUTERD_SOCK"); sock != "" {
		token := os.Getenv("JCODE_COMPUTERD_TOKEN")
		conn, err := net.Dial("unix", sock)
		if err != nil {
			t.Fatalf("dial existing daemon at %s: %v", sock, err)
		}
		h, err := newHelperConn(conn, token)
		if err != nil {
			t.Fatalf("handshake with existing daemon: %v", err)
		}
		t.Cleanup(func() { _ = h.Close() })
		return h
	}
	bin := os.Getenv("JCODE_COMPUTERD_BIN")
	if bin == "" {
		bin = filepath.Join(os.Getenv("HOME"), ".jcode", "computer", "jcode-computerd")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("daemon %s not found — build it: swiftc -O -o %s cmd/jcode-computerd/main.swift", bin, bin)
	}
	work := t.TempDir()
	sock := shortSocketPath(t)
	tokenFile := filepath.Join(work, "token")
	const token = "live-token"
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

	var conn net.Conn
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("unix", sock); err == nil {
			conn = c
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("daemon did not bind the socket")
	}
	h, err := newHelperConn(conn, token)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

// toUITree adapts the wire nodes to the shared renderer's input (they are already
// uitree.Node, so this is identity — kept explicit for clarity).
func toUITree(nodes []uitree.Node) []uitree.Node { return nodes }
