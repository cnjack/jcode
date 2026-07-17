//go:build darwin

package computer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/uitree"
)

// TestCalculatorE2E drives a real, harmless system app through the compiled
// Swift daemon. It is opt-in because it changes the foreground UI and requires
// the user's Accessibility and Screen Recording grants.
func TestCalculatorE2E(t *testing.T) {
	if os.Getenv("JCODE_COMPUTERD_CALCULATOR_E2E") == "" {
		t.Skip("set JCODE_COMPUTERD_CALCULATOR_E2E=1 to drive Calculator with the real daemon")
	}
	bin := os.Getenv("JCODE_COMPUTERD_BIN")
	if bin == "" {
		bin = "/tmp/jcode-computerd"
	}
	h := startCalculatorDaemon(t, bin)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	apps, err := h.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	foundCalculator := false
	for _, app := range apps {
		if app.BundleID == "com.apple.calculator" {
			foundCalculator = true
			break
		}
	}
	if !foundCalculator {
		t.Fatal("installed app catalog does not include com.apple.calculator")
	}

	if err := h.Launch(ctx, "com.apple.calculator"); err != nil {
		t.Fatalf("Launch Calculator: %v", err)
	}
	// Clear any expression left by the user's existing Calculator session. On
	// current macOS, Escape is not consistently All Clear (and on a Chinese
	// locale may only delete one entry), so resolve the real AX button by ref.
	initial, err := h.Tree(ctx, "com.apple.calculator")
	if err != nil {
		t.Fatalf("initial Calculator Tree: %v", err)
	}
	clear := findNamedNode(initial, "All Clear", "全部清除", "AC")
	if clear == nil || clear.Ref == 0 {
		t.Fatalf("Calculator tree has no actionable All Clear button; nodes=%s", summarizeNodes(initial))
	}
	if err := h.Perform(ctx, Action{Kind: "click", BundleID: "com.apple.calculator", Ref: clear.Ref}); err != nil {
		t.Fatalf("clear Calculator by ref: %v", err)
	}

	nodes, err := h.Tree(ctx, "com.apple.calculator")
	if err != nil {
		t.Fatalf("Calculator Tree: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("Calculator Tree returned zero nodes")
	}
	seven := findNamedNode(nodes, "7")
	plus := findNamedNode(nodes, "Add", "加", "Plus")
	five := findNamedNode(nodes, "5")
	equals := findNamedNode(nodes, "Equals", "等于", "=")
	for label, node := range map[string]*uitree.Node{"7": seven, "+": plus, "5": five, "=": equals} {
		if node == nil {
			t.Fatalf("Calculator tree has no named %s button; nodes=%s", label, summarizeNodes(nodes))
		}
		if node.Role != "button" || node.Ref == 0 {
			t.Fatalf("%s node is not an actionable normalized button: %+v", label, *node)
		}
	}

	// Coordinates are deliberately omitted. This is the regression assertion for
	// the old bug that accepted a uid/ref and then clicked (0,0).
	for _, node := range []*uitree.Node{seven, plus, five, equals} {
		if err := h.Perform(ctx, Action{Kind: "click", BundleID: "com.apple.calculator", Ref: node.Ref}); err != nil {
			t.Fatalf("click %q by ref: %v", node.Name, err)
		}
	}

	after, err := h.Tree(ctx, "com.apple.calculator")
	if err != nil {
		t.Fatalf("Tree after calculation: %v", err)
	}
	if !treeContains(after, "12") {
		t.Fatalf("Calculator did not show 12 after 7 + 5; nodes=%s", summarizeNodes(after))
	}

	shot, err := h.CaptureVisual(ctx, "com.apple.calculator")
	if err != nil {
		t.Fatalf("Capture Calculator: %v", err)
	}
	if !strings.HasPrefix(string(shot.PNG), "\x89PNG\r\n\x1a\n") {
		t.Fatalf("Capture returned %d bytes without a PNG signature", len(shot.PNG))
	}
	if shot.Width <= 0 || shot.Height <= 0 || shot.PixelWidth <= 0 || shot.PixelHeight <= 0 {
		t.Fatalf("Capture did not return a usable window-coordinate mapping: %+v", shot)
	}
	if shot.PixelWidth > 2048 || shot.PixelHeight > 2048 {
		t.Fatalf("Capture worker did not apply its visual payload bound: %dx%d", shot.PixelWidth, shot.PixelHeight)
	}
	// A capture child failure used to abort the daemon. A successful request
	// immediately afterward proves the long-lived AX process survived.
	if _, err := h.ListApps(ctx); err != nil {
		t.Fatalf("daemon died after Capture: %v", err)
	}
}

func startCalculatorDaemon(t *testing.T, bin string) *helperBackend {
	t.Helper()
	work := t.TempDir()
	tokenFile := filepath.Join(work, "token")
	shotsDir := filepath.Join(work, "shots")
	const token = "calculator-e2e-token"
	if err := os.WriteFile(tokenFile, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	socket := shortSocketPath(t)
	cmd := exec.Command(bin,
		"--socket", socket,
		"--token-file", tokenFile,
		"--shots-dir", shotsDir,
		"--client-pid", fmt.Sprintf("%d", os.Getpid()),
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	conn := dialWithRetry(t, socket)
	h, err := newHelperConn(conn, token)
	if err != nil {
		t.Fatalf("daemon handshake: %v", err)
	}
	h.shotsDir = shotsDir
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func findNamedNode(nodes []uitree.Node, names ...string) *uitree.Node {
	for i := range nodes {
		for _, name := range names {
			if strings.EqualFold(strings.TrimSpace(nodes[i].Name), name) {
				return &nodes[i]
			}
		}
	}
	return nil
}

func treeContains(nodes []uitree.Node, text string) bool {
	for _, node := range nodes {
		if strings.Contains(node.Name, text) || strings.Contains(node.Value, text) {
			return true
		}
	}
	return false
}

func summarizeNodes(nodes []uitree.Node) string {
	var summary []string
	for _, node := range nodes {
		if strings.TrimSpace(node.Name) != "" || node.Ref != 0 {
			summary = append(summary, node.Role+":"+node.Name)
		}
		if len(summary) == 30 {
			break
		}
	}
	return strings.Join(summary, ", ")
}
