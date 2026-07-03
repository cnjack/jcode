package browser

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSmokeManagedChrome launches a real Chrome, opens a data URL, snapshots it,
// clicks a button, and screenshots. Gated behind JCODE_BROWSER_SMOKE=1 so it
// never runs in the normal suite (it needs a real browser + socket binding).
//
//	JCODE_BROWSER_SMOKE=1 go test ./internal/browser/ -run TestSmokeManagedChrome -v
func TestSmokeManagedChrome(t *testing.T) {
	if os.Getenv("JCODE_BROWSER_SMOKE") != "1" {
		t.Skip("set JCODE_BROWSER_SMOKE=1 to run the real-Chrome smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	backend, err := Launch(ctx, LaunchOptions{Headless: true, Viewport: "1280x720"})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	sess := NewSession(backend)
	defer sess.Close()

	page := "data:text/html," +
		"<title>Smoke</title><h1>Hello</h1>" +
		"<button id=b onclick=\"document.title='Clicked'\">Press me</button>" +
		"<input aria-label=Name>"

	snap, err := sess.Open(ctx, page, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Logf("open snapshot:\n%s", snap)
	if !strings.Contains(snap, "Smoke") {
		t.Errorf("expected page title in header")
	}

	full, err := sess.Snapshot(ctx, "interactive", 100)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	t.Logf("full snapshot:\n%s", full)
	if !strings.Contains(full, "button") {
		t.Errorf("expected a button uid in snapshot")
	}

	// Find the button uid (e1/e2…) and click it.
	uid := ""
	for _, line := range strings.Split(full, "\n") {
		if strings.Contains(line, "button") && strings.HasPrefix(strings.TrimSpace(line), "[e") {
			uid = strings.TrimPrefix(strings.Fields(strings.TrimSpace(line))[0], "[")
			uid = strings.TrimSuffix(uid, "]")
			break
		}
	}
	if uid == "" {
		t.Fatal("no button uid found")
	}
	res, err := sess.Act(ctx, ActRequest{Action: "click", UID: uid})
	if err != nil {
		t.Fatalf("Act click: %v", err)
	}
	t.Logf("act result:\n%s", res)

	png, err := sess.Screenshot(ctx, false)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if len(png) < 100 {
		t.Errorf("screenshot too small: %d bytes", len(png))
	}
	t.Logf("screenshot ok: %d bytes", len(png))
}
