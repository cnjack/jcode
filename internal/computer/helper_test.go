package computer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/uitree"
)

// mockDaemon is a Go stand-in for the native helper: it speaks the exact wire
// protocol over an injected conn, serving canned answers. This is the "throwaway
// Go daemon over the real socket" the design's phase 1 calls for — it lets the
// entire client half (framing, handshake, the nine methods, ctx handling, error
// mapping) be exercised without a line of Swift or a bound socket.
//
// Handlers are keyed by request type; a handler returns the response envelope's
// (type, payload) or an error frame. The token the daemon requires is checked in
// the ping handler.
type mockDaemon struct {
	conn  net.Conn
	token string

	mu       sync.Mutex
	handlers map[string]func(id uint64, payload json.RawMessage) envelope
	requests []string // ordered log of request types seen

	// hooks for adversarial tests
	dropResponse bool // read the request, send nothing (simulate a hang)
	wrongID      bool // reply with a mismatched id
}

func newMockDaemon(conn net.Conn, token string) *mockDaemon {
	d := &mockDaemon{conn: conn, token: token, handlers: map[string]func(uint64, json.RawMessage) envelope{}}
	d.installDefaults()
	return d
}

func (d *mockDaemon) on(reqType string, fn func(id uint64, payload json.RawMessage) envelope) {
	d.mu.Lock()
	d.handlers[reqType] = fn
	d.mu.Unlock()
}

func result(id uint64, payload any) envelope {
	raw, _ := json.Marshal(payload)
	return envelope{Type: typeResult, ID: id, Payload: raw}
}

func errFrame(id uint64, code int, msg string) envelope {
	raw, _ := json.Marshal(errorPayload{Code: code, Message: msg})
	return envelope{Type: typeError, ID: id, Payload: raw}
}

func (d *mockDaemon) installDefaults() {
	d.on(typePing, func(id uint64, payload json.RawMessage) envelope {
		var p pingPayload
		_ = json.Unmarshal(payload, &p)
		if p.Token != d.token {
			return errFrame(id, codeSenderNotAuthenticated, "bad token")
		}
		if p.ClientAPIVersion != apiVersion {
			return errFrame(id, codeIncompatibleVersion, "version mismatch")
		}
		return envelope{Type: typePong, ID: id, Payload: mustJSON(pongPayload{
			ServerAPIVersion:          apiVersion,
			Platform:                  "darwin",
			HelperVersion:             "test-1.0",
			AccessibilityPermission:   PermissionGranted,
			ScreenRecordingPermission: PermissionDenied,
		})}
	})
	d.on(typeListApps, func(id uint64, _ json.RawMessage) envelope {
		return result(id, listAppsResult{Apps: []appWire{
			{BundleID: "com.apple.Notes", Name: "Notes", Running: true},
			{BundleID: "com.googlecode.iterm2", Name: "iTerm", Running: true},
		}})
	})
	d.on(typeFrontmost, func(id uint64, _ json.RawMessage) envelope {
		return result(id, frontmostResult{App: appWire{BundleID: "com.apple.Notes", Name: "Notes", Running: true}})
	})
	d.on(typeTree, func(id uint64, _ json.RawMessage) envelope {
		return result(id, treeResult{Gen: 1, Nodes: []uitree.Node{
			{ID: "1", Role: "window", Name: "Notes", ChildIDs: []string{"2"}},
			{ID: "2", Role: "button", Name: "New Note", Ref: 101},
		}})
	})
	d.on(typeCapture, func(id uint64, _ json.RawMessage) envelope {
		return result(id, captureResult{
			PNG: []byte("\x89PNG\r\n\x1a\nfake"),
			X:   40, Y: 80, Width: 800, Height: 600, PixelWidth: 1600, PixelHeight: 1200,
		})
	})
	d.on(typeLaunch, func(id uint64, _ json.RawMessage) envelope { return result(id, struct{}{}) })
	d.on(typeReadClipboard, func(id uint64, _ json.RawMessage) envelope {
		return result(id, readClipboardResult{Text: "clipboard text"})
	})
	d.on(typePerform, func(id uint64, _ json.RawMessage) envelope { return result(id, struct{}{}) })
}

// serve runs the daemon loop until the conn closes.
func (d *mockDaemon) serve() {
	for {
		var req envelope
		if err := readFrame(d.conn, &req); err != nil {
			return
		}
		d.mu.Lock()
		d.requests = append(d.requests, req.Type)
		h := d.handlers[req.Type]
		drop, wrong := d.dropResponse, d.wrongID
		d.mu.Unlock()
		// The handshake ping always answers normally: the adversarial hooks model
		// a daemon that misbehaves *after* connecting, not one that never connects.
		// Applying them to ping would just break the handshake and test nothing.
		misbehave := req.Type != typePing
		if drop && misbehave {
			continue // read but never answer — the client must time out
		}
		if h == nil {
			_ = writeFrame(d.conn, errFrame(req.ID, -1, "no handler for "+req.Type))
			continue
		}
		resp := h(req.ID, req.Payload)
		if wrong && misbehave {
			resp.ID = req.ID + 999
		}
		if err := writeFrame(d.conn, resp); err != nil {
			return
		}
	}
}

func (d *mockDaemon) seen() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.requests...)
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// dialMock wires a helperBackend to a mockDaemon over an in-memory pipe (no
// socket is bound, so this runs anywhere), completing the handshake.
func dialMock(t *testing.T, configure func(*mockDaemon)) (*helperBackend, *mockDaemon) {
	t.Helper()
	const token = "test-token"
	client, server := net.Pipe()
	d := newMockDaemon(server, token)
	if configure != nil {
		configure(d)
	}
	go d.serve()

	// newHelperConn handshakes synchronously, so do it off the test goroutine
	// and wait, to surface a handshake hang as a test timeout rather than a deadlock.
	type res struct {
		h   *helperBackend
		err error
	}
	ch := make(chan res, 1)
	go func() {
		h, err := newHelperConn(client, token)
		ch <- res{h, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("handshake: %v", r.err)
		}
		t.Cleanup(func() { _ = r.h.Close() })
		return r.h, d
	case <-time.After(5 * time.Second):
		t.Fatal("handshake did not complete")
		return nil, nil
	}
}

// --- happy path: every method round-trips ---

func TestHelperHandshakeAndMethods(t *testing.T) {
	h, d := dialMock(t, nil)
	ctx := context.Background()

	if h.platform != "darwin" || h.helperVersion != "test-1.0" {
		t.Errorf("handshake did not capture pong fields: platform=%q version=%q", h.platform, h.helperVersion)
	}
	if got := h.PermissionStatus(); got.Accessibility != PermissionGranted || got.ScreenRecording != PermissionDenied {
		t.Errorf("handshake permissions = %+v, want accessibility=granted screen-recording=denied", got)
	}

	apps, err := h.ListApps(ctx)
	if err != nil || len(apps) != 2 || apps[0].BundleID != "com.apple.Notes" {
		t.Fatalf("ListApps = %v, %v", apps, err)
	}
	front, err := h.Frontmost(ctx)
	if err != nil || front.BundleID != "com.apple.Notes" {
		t.Fatalf("Frontmost = %v, %v", front, err)
	}
	nodes, err := h.Tree(ctx, "com.apple.Notes")
	if err != nil || len(nodes) != 2 || nodes[1].Ref != 101 {
		t.Fatalf("Tree = %v, %v", nodes, err)
	}
	shot, err := h.CaptureVisual(ctx, "com.apple.Notes")
	if err != nil || !strings.HasPrefix(string(shot.PNG), "\x89PNG") ||
		shot.X != 40 || shot.Width != 800 || shot.PixelWidth != 1600 {
		t.Fatalf("CaptureVisual = %+v, %v", shot, err)
	}
	if err := h.Launch(ctx, "com.apple.Notes"); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	clip, err := h.ReadClipboard(ctx)
	if err != nil || clip != "clipboard text" {
		t.Fatalf("ReadClipboard = %q, %v", clip, err)
	}
	if err := h.Perform(ctx, Action{Kind: "click", BundleID: "com.apple.Notes", UID: "e1", Ref: 101}); err != nil {
		t.Fatalf("Perform: %v", err)
	}

	// The daemon saw ping first, then each method exactly once.
	got := d.seen()
	want := []string{typePing, typeListApps, typeFrontmost, typeTree, typeCapture, typeLaunch, typeReadClipboard, typePerform}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("request order = %v, want %v", got, want)
	}
}

func TestHelperOldPongDefaultsPermissionsToUnknown(t *testing.T) {
	h, _ := dialMock(t, func(d *mockDaemon) {
		d.on(typePing, func(id uint64, _ json.RawMessage) envelope {
			// The permission fields were added without changing the API version.
			// Their absence is how a new client recognizes an older daemon.
			return envelope{Type: typePong, ID: id, Payload: mustJSON(struct {
				ServerAPIVersion string `json:"server_api_version"`
				Platform         string `json:"platform"`
				HelperVersion    string `json:"helper_version"`
			}{apiVersion, "darwin", "old"})}
		})
	})

	if got := h.PermissionStatus(); got.Accessibility != PermissionUnknown || got.ScreenRecording != PermissionUnknown {
		t.Fatalf("old pong permissions = %+v, want unknown/unknown", got)
	}
}

func TestPongPermissionFieldsAreAdditiveForLegacyClients(t *testing.T) {
	raw := mustJSON(pongPayload{
		ServerAPIVersion:          apiVersion,
		Platform:                  "darwin",
		HelperVersion:             "new",
		AccessibilityPermission:   PermissionGranted,
		ScreenRecordingPermission: PermissionDenied,
	})
	var legacy struct {
		ServerAPIVersion string `json:"server_api_version"`
		Platform         string `json:"platform"`
		HelperVersion    string `json:"helper_version"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatalf("legacy client rejected additive pong fields: %v", err)
	}
	if legacy.ServerAPIVersion != apiVersion || legacy.Platform != "darwin" || legacy.HelperVersion != "new" {
		t.Fatalf("legacy pong decode lost original fields: %+v", legacy)
	}
}

func TestHelperUnrecognizedPermissionStateFailsClosed(t *testing.T) {
	h, _ := dialMock(t, func(d *mockDaemon) {
		d.on(typePing, func(id uint64, _ json.RawMessage) envelope {
			return envelope{Type: typePong, ID: id, Payload: mustJSON(pongPayload{
				ServerAPIVersion:          apiVersion,
				Platform:                  "darwin",
				AccessibilityPermission:   PermissionState("yes"),
				ScreenRecordingPermission: PermissionState("probably"),
			})}
		})
	})

	if got := h.PermissionStatus(); got.Accessibility != PermissionUnknown || got.ScreenRecording != PermissionUnknown {
		t.Fatalf("unrecognized pong permissions = %+v, want unknown/unknown", got)
	}
}

func TestHelperRefreshesPermissionStatusWithAuthenticatedPing(t *testing.T) {
	var pings int
	h, d := dialMock(t, func(d *mockDaemon) {
		d.on(typePing, func(id uint64, payload json.RawMessage) envelope {
			var p pingPayload
			_ = json.Unmarshal(payload, &p)
			if p.Token != d.token || p.ClientAPIVersion != apiVersion {
				return errFrame(id, codeSenderNotAuthenticated, "bad refresh credentials")
			}
			pings++
			state := PermissionDenied
			if pings > 1 {
				state = PermissionGranted
			}
			return envelope{Type: typePong, ID: id, Payload: mustJSON(pongPayload{
				ServerAPIVersion:          apiVersion,
				Platform:                  "darwin",
				AccessibilityPermission:   state,
				ScreenRecordingPermission: state,
			})}
		})
	})

	if got := h.PermissionStatus(); got.Accessibility != PermissionDenied || got.ScreenRecording != PermissionDenied {
		t.Fatalf("initial permissions = %+v, want denied/denied", got)
	}
	got, err := h.RefreshPermissionStatus(context.Background())
	if err != nil {
		t.Fatalf("RefreshPermissionStatus: %v", err)
	}
	if got.Accessibility != PermissionGranted || got.ScreenRecording != PermissionGranted {
		t.Fatalf("refreshed permissions = %+v, want granted/granted", got)
	}
	if gotPings := countRequests(d.seen(), typePing); gotPings != 2 {
		t.Fatalf("ping requests = %d, want handshake + refresh", gotPings)
	}
}

func TestHelperRequestPermissionsAppliesFreshStates(t *testing.T) {
	var gotRequest requestPermissionsPayload
	h, _ := dialMock(t, func(d *mockDaemon) {
		d.on(typeRequestPermissions, func(id uint64, payload json.RawMessage) envelope {
			_ = json.Unmarshal(payload, &gotRequest)
			// The daemon answers with the pong-shaped payload: the prompts are
			// async, but the user may already have granted between the handshake
			// and this request, so the states here are the fresh truth.
			return result(id, pongPayload{
				ServerAPIVersion:          apiVersion,
				Platform:                  "darwin",
				HelperVersion:             "test-1.0",
				AccessibilityPermission:   PermissionGranted,
				ScreenRecordingPermission: PermissionGranted,
			})
		})
	})

	// Handshake states: accessibility granted, screen recording denied.
	if got := h.PermissionStatus(); got.ScreenRecording != PermissionDenied {
		t.Fatalf("initial permissions = %+v, want screen-recording denied", got)
	}
	got, err := h.RequestPermissions(context.Background(), true, true)
	if err != nil {
		t.Fatalf("RequestPermissions: %v", err)
	}
	if !gotRequest.Accessibility || !gotRequest.ScreenRecording {
		t.Fatalf("request payload = %+v, want both grants requested", gotRequest)
	}
	if got.Accessibility != PermissionGranted || got.ScreenRecording != PermissionGranted {
		t.Fatalf("post-request permissions = %+v, want granted/granted", got)
	}
	// The fresh states stick: the next PermissionStatus read does not fall back
	// to the handshake snapshot.
	if again := h.PermissionStatus(); again != got {
		t.Fatalf("PermissionStatus after request = %+v, want %+v", again, got)
	}
}

func TestHelperRequestPermissionsOldDaemonIsActionable(t *testing.T) {
	h, _ := dialMock(t, func(d *mockDaemon) {
		// A daemon launched before request_permissions existed answers with the
		// generic unknown-type error (Swift Code.unknown = -10005). The client
		// must translate that into "restart to get the new helper", not surface
		// a raw protocol error.
		d.on(typeRequestPermissions, func(id uint64, _ json.RawMessage) envelope {
			return errFrame(id, -10005, "unknown request type: request_permissions")
		})
	})

	_, err := h.RequestPermissions(context.Background(), true, false)
	if err == nil || !strings.Contains(err.Error(), "restart jcode") {
		t.Fatalf("old-daemon error = %v, want an actionable restart hint", err)
	}
}

// --- per-instance admission and action wire fidelity (design §4) ---

func TestActionWirePreservesExplicitZeroCoordinates(t *testing.T) {
	wire := actionToWire(Action{
		Kind: "drag", BundleID: "com.example.Canvas",
		HasX: true, HasY: true, HasToX: true, HasToY: true,
	})
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"x", "y", "to_x", "to_y"} {
		value, ok := fields[name]
		if !ok || string(value) != "0" {
			t.Fatalf("explicit zero coordinate %q was lost: %s", name, encoded)
		}
	}

	withoutCoordinates, err := json.Marshal(actionToWire(Action{
		Kind: "press", BundleID: "com.example.Canvas", Key: "escape",
	}))
	if err != nil {
		t.Fatal(err)
	}
	var absent map[string]json.RawMessage
	if err := json.Unmarshal(withoutCoordinates, &absent); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"x", "y", "to_x", "to_y"} {
		if _, ok := absent[name]; ok {
			t.Fatalf("unused coordinate %q was unexpectedly encoded: %s", name, withoutCoordinates)
		}
	}
}

func TestHelperRejectsBadToken(t *testing.T) {
	client, server := net.Pipe()
	d := newMockDaemon(server, "the-real-token")
	go d.serve()
	_, err := newHelperConn(client, "a-different-token")
	if err == nil {
		t.Fatal("handshake succeeded with the wrong token; the token is supposed to be the boundary")
	}
	if !strings.Contains(err.Error(), "handshake") {
		t.Errorf("error should name the handshake: %v", err)
	}
}

// --- version mismatch is fatal, not retried ---

func TestHelperVersionMismatchIsFatal(t *testing.T) {
	client, server := net.Pipe()
	d := newMockDaemon(server, "t")
	d.on(typePing, func(id uint64, _ json.RawMessage) envelope {
		return envelope{Type: typePong, ID: id, Payload: mustJSON(pongPayload{
			ServerAPIVersion: "JcodeComputerIPC-999", Platform: "darwin",
		})}
	})
	go d.serve()
	_, err := newHelperConn(client, "t")
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("a version mismatch must be a hard error, got %v", err)
	}
}

// --- daemon error codes map onto sentinels ---

func TestHelperMapsErrorCodes(t *testing.T) {
	cases := []struct {
		code   int
		msg    string
		expect error
	}{
		{codeUserIntervened, "user took over", ErrControlInterrupted},
		{codeScreenLocked, "locked", ErrScreenLocked},
	}
	for _, c := range cases {
		h, _ := dialMock(t, func(d *mockDaemon) {
			d.on(typePerform, func(id uint64, _ json.RawMessage) envelope {
				return errFrame(id, c.code, c.msg)
			})
		})
		err := h.Perform(context.Background(), Action{Kind: "click", BundleID: "x"})
		if !errors.Is(err, c.expect) {
			t.Errorf("code %d mapped to %v, want %v", c.code, err, c.expect)
		}
	}

	// appNotAllowed maps to the typed NotAllowedError.
	h, _ := dialMock(t, func(d *mockDaemon) {
		d.on(typeFrontmost, func(id uint64, _ json.RawMessage) envelope {
			return errFrame(id, codeAppNotAllowed, "com.evil.app")
		})
	})
	_, err := h.Frontmost(context.Background())
	var na *NotAllowedError
	if !errors.As(err, &na) {
		t.Errorf("appNotAllowed mapped to %T, want *NotAllowedError", err)
	}
}

// --- ctx cancellation interrupts a hung round-trip ---

func TestHelperContextCancelInterruptsHang(t *testing.T) {
	h, _ := dialMock(t, func(d *mockDaemon) { d.dropResponse = true }) // daemon never answers
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()

	start := time.Now()
	_, err := h.ListApps(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a hung daemon must not return success")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error should be the cancellation, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("cancellation took %v; it should interrupt promptly", elapsed)
	}
}

// --- ctx deadline bounds a hung round-trip even without an explicit cancel ---

func TestHelperContextDeadlineBoundsHang(t *testing.T) {
	h, _ := dialMock(t, func(d *mockDaemon) { d.dropResponse = true })
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := h.Frontmost(ctx)
	if err == nil {
		t.Fatal("a hung daemon past the deadline must fail")
	}
	if time.Since(start) > 2*time.Second {
		t.Error("the deadline did not bound the hang")
	}
}

// --- a dead daemon reconnects on the next request, never by replaying one ---

func TestHelperReconnectsAfterEOFWithoutReplayingMutation(t *testing.T) {
	h, first := dialMock(t, func(d *mockDaemon) {
		d.on(typePerform, func(id uint64, _ json.RawMessage) envelope {
			_ = d.conn.Close() // request was received; response is lost
			return result(id, struct{}{})
		})
	})

	var reconnects int
	var second *mockDaemon
	h.redial = func(context.Context) (*helperBackend, error) {
		reconnects++
		fresh, daemon := dialMock(t, nil)
		second = daemon
		return fresh, nil
	}

	err := h.Perform(context.Background(), Action{Kind: "click", BundleID: "com.apple.Notes", Ref: 101})
	if err == nil || !strings.Contains(err.Error(), "outcome is unknown") {
		t.Fatalf("lost Perform response must report an unknown outcome, got %v", err)
	}
	if reconnects != 0 {
		t.Fatalf("failed mutation was replayed/redialed in the same call: reconnects=%d", reconnects)
	}

	apps, err := h.ListApps(context.Background())
	if err != nil || len(apps) == 0 {
		t.Fatalf("next read did not recover the helper: apps=%v err=%v", apps, err)
	}
	if reconnects != 1 {
		t.Fatalf("reconnects=%d, want exactly one", reconnects)
	}
	if got := countRequests(first.seen(), typePerform); got != 1 {
		t.Fatalf("first daemon saw Perform %d times, want 1", got)
	}
	if got := countRequests(second.seen(), typePerform); got != 0 {
		t.Fatalf("replacement daemon saw replayed Perform %d times", got)
	}
}

func TestHelperConcurrentCallsShareOneReconnect(t *testing.T) {
	h, _ := dialMock(t, nil)
	h.mu.Lock()
	h.markDeadLocked()
	h.mu.Unlock()

	var mu sync.Mutex
	reconnects := 0
	h.redial = func(context.Context) (*helperBackend, error) {
		mu.Lock()
		reconnects++
		mu.Unlock()
		fresh, _ := dialMock(t, nil)
		return fresh, nil
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.ListApps(context.Background()); err != nil {
				t.Errorf("ListApps after reconnect: %v", err)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if reconnects != 1 {
		t.Fatalf("concurrent callers caused %d reconnects, want 1", reconnects)
	}
}

func TestSessionInvalidatesUIDsWhenHelperReconnects(t *testing.T) {
	h, _ := dialMock(t, nil)
	mgr := NewManager(Config{Enabled: true, MaxActionsPerBatch: 20}, t.TempDir())
	sess := newSession(mgr, h)
	sess.Grant([]string{"com.apple.Notes"}, false, false, false)

	text, err := sess.Snapshot(context.Background(), "com.apple.Notes", "interactive", 0, true)
	if err != nil || !strings.Contains(text, "[e1]") {
		t.Fatalf("initial snapshot = %q, %v", text, err)
	}

	h.mu.Lock()
	h.markDeadLocked()
	h.mu.Unlock()
	var second *mockDaemon
	h.redial = func(context.Context) (*helperBackend, error) {
		fresh, daemon := dialMock(t, nil) // deliberately reuses Ref 101
		second = daemon
		return fresh, nil
	}

	_, err = sess.Act(context.Background(), []ActRequest{{Action: "click", UID: "e1"}})
	if err == nil || !strings.Contains(err.Error(), "no snapshot") {
		t.Fatalf("old uid survived a daemon generation change: %v", err)
	}
	if got := countRequests(second.seen(), typePerform); got != 0 {
		t.Fatalf("replacement daemon received an action for an old uid (%d Perform calls)", got)
	}
}

func countRequests(requests []string, want string) int {
	count := 0
	for _, request := range requests {
		if request == want {
			count++
		}
	}
	return count
}

// --- a desynced response id is detected, not silently accepted ---

func TestHelperDetectsIDDesync(t *testing.T) {
	h, _ := dialMock(t, func(d *mockDaemon) { d.wrongID = true })
	_, err := h.ListApps(context.Background())
	if err == nil || !strings.Contains(err.Error(), "desync") {
		t.Fatalf("a mismatched response id must be caught, got %v", err)
	}
}

func TestHelperMutationIDDesyncReportsUnknownOutcome(t *testing.T) {
	h, _ := dialMock(t, func(d *mockDaemon) { d.wrongID = true })
	err := h.Perform(context.Background(), Action{Kind: "click", BundleID: "com.apple.Notes", Ref: 101})
	if err == nil || !strings.Contains(err.Error(), "desync") || !strings.Contains(err.Error(), "outcome is unknown") {
		t.Fatalf("mutation protocol desync must report unknown outcome, got %v", err)
	}
}

// --- one request in flight: concurrent calls serialize, never interleave ---

func TestHelperSerializesConcurrentCalls(t *testing.T) {
	var inFlight, maxInFlight int
	var mu sync.Mutex
	h, _ := dialMock(t, func(d *mockDaemon) {
		d.on(typeFrontmost, func(id uint64, _ json.RawMessage) envelope {
			mu.Lock()
			inFlight++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
			mu.Unlock()
			time.Sleep(20 * time.Millisecond) // hold the "UI resource"
			mu.Lock()
			inFlight--
			mu.Unlock()
			return result(id, frontmostResult{App: appWire{BundleID: "com.apple.Notes"}})
		})
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = h.Frontmost(context.Background())
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if maxInFlight != 1 {
		t.Errorf("max concurrent requests at the daemon = %d, want 1 (UI automation is serial)", maxInFlight)
	}
}

// --- framing: the 8 MiB cap is enforced on decode ---

func TestFrameCapRejectsOversizeHeader(t *testing.T) {
	// A header claiming more than the cap, with no body — readFrame must reject
	// on the header alone, before trying to allocate or read the body.
	var buf oversizeHeader
	err := readFrame(&buf, &envelope{})
	if err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("an oversize length header must be rejected, got %v", err)
	}
}

// oversizeHeader is a reader that yields a 4-byte length of maxFrame+1, then EOF.
type oversizeHeader struct{ pos int }

func (o *oversizeHeader) Read(p []byte) (int, error) {
	hdr := []byte{0, 0, 0, 0}
	// little-endian maxFrame+1
	n := uint32(maxFrame + 1)
	hdr[0] = byte(n)
	hdr[1] = byte(n >> 8)
	hdr[2] = byte(n >> 16)
	hdr[3] = byte(n >> 24)
	if o.pos >= len(hdr) {
		return 0, io.EOF
	}
	c := copy(p, hdr[o.pos:])
	o.pos += c
	return c, nil
}

// --- framing round-trips a real payload ---

func TestFrameRoundTrip(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	want := envelope{Type: typeTree, ID: 7, Payload: mustJSON(treeRequest{App: "com.apple.Notes"})}
	go func() { _ = writeFrame(client, want) }()

	var got envelope
	if err := readFrame(server, &got); err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if got.Type != want.Type || got.ID != want.ID {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
}

// --- readShotRef refuses paths outside the shots dir ---

func TestReadShotRefRejectsTraversal(t *testing.T) {
	h := &helperBackend{shotsDir: t.TempDir()}
	for _, bad := range []string{"/etc/passwd", h.shotsDir + "/../secret", ".."} {
		if _, err := h.readShotRef(bad); err == nil {
			t.Errorf("readShotRef(%q) was accepted; it must stay inside the shots dir", bad)
		}
	}
	// A file genuinely inside the shots dir is fine.
	inside := h.shotsDir + "/shot.png"
	if err := os.WriteFile(inside, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := h.readShotRef(inside); err != nil || string(b) != "png" {
		t.Errorf("a file inside the shots dir should read: %q %v", b, err)
	}
	if _, err := os.Stat(inside); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("IPC screenshot handoff file was not removed after reading: %v", err)
	}
}

func TestReadShotRefRejectsOversizedFileAndRemovesHandoff(t *testing.T) {
	h := &helperBackend{shotsDir: t.TempDir()}
	inside := filepath.Join(h.shotsDir, "oversized.png")
	f, err := os.Create(inside)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxScreenshotBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.readShotRef(inside); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("oversized screenshot error=%v, want hard size rejection", err)
	}
	if _, err := os.Stat(inside); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("rejected IPC screenshot was not removed: %v", err)
	}
}

func TestReadShotRefRejectsSymlink(t *testing.T) {
	h := &helperBackend{shotsDir: t.TempDir()}
	target := filepath.Join(t.TempDir(), "private.png")
	if err := os.WriteFile(target, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(h.shotsDir, "shot.png")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := h.readShotRef(link); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink screenshot error=%v, want rejection", err)
	}
}

func TestSelectHelperBinSkipsCaptureWorker(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "jcode-computerd-capture-x86_64-apple-darwin")
	daemon := filepath.Join(dir, "jcode-computerd-x86_64-apple-darwin")
	for _, path := range []string{capture, daemon} {
		if err := os.WriteFile(path, []byte("test"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Capture sorts before x86_64, reproducing the dev/release directory shape
	// where the old first-executable logic selected the wrong process.
	if got := selectHelperBin([]string{capture, daemon}); got != daemon {
		t.Fatalf("selectHelperBin=%q, want daemon %q", got, daemon)
	}
}

func TestComputerPathsAreStableAndIsolatedPerProcessInstance(t *testing.T) {
	const firstInstance = "00112233445566778899aabbccddeeff"
	const secondInstance = "ffeeddccbbaa99887766554433221100"
	root := t.TempDir()
	first := computerPathsForInstance(root, 1001, firstInstance)
	reconnect := computerPathsForInstance(root, 1001, firstInstance)
	second := computerPathsForInstance(root, 1001, secondInstance)
	if first != reconnect {
		t.Fatalf("same process instance changed helper paths: first=%+v reconnect=%+v", first, reconnect)
	}
	if first.socket == second.socket || first.shotsDir == second.shotsDir {
		t.Fatalf("process-instance helper rendezvous collided: first=%+v second=%+v", first, second)
	}
	if first.tokenFile != second.tokenFile {
		t.Fatalf("same PID should keep its stable token file: first=%+v second=%+v", first, second)
	}
	if !strings.Contains(first.socket, firstInstance) || !strings.Contains(second.socket, secondInstance) ||
		!strings.Contains(first.shotsDir, "1001-"+firstInstance) ||
		!strings.Contains(second.shotsDir, "1001-"+secondInstance) {
		t.Fatalf("helper paths do not identify their process instance: first=%+v second=%+v", first, second)
	}
}

func TestHelperProcessInstanceIDIsStableAndCanonical(t *testing.T) {
	first, err := loadHelperProcessInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadHelperProcessInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("process instance changed across reconnects: %q != %q", first, second)
	}
	if !validHelperHandoffName("handoff-1001-" + first) {
		t.Fatalf("process instance id is not canonical lowercase hex: %q", first)
	}
}

func TestHelperHandoffCleanupIsProcessScoped(t *testing.T) {
	root := t.TempDir()
	first := computerPathsForInstance(root, 1001, "00112233445566778899aabbccddeeff").shotsDir
	second := computerPathsForInstance(root, 1001, "ffeeddccbbaa99887766554433221100").shotsDir
	if err := os.MkdirAll(first, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(second, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "orphan.png"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	secondShot := filepath.Join(second, "active.png")
	if err := os.WriteFile(secondShot, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := removeOwnedHelperHandoffDir(first); err != nil {
		t.Fatal(err)
	}
	requirePathState(t, first, false)
	requirePathState(t, secondShot, true)

	unsafe := t.TempDir()
	if err := removeOwnedHelperHandoffDir(unsafe); err == nil {
		t.Fatal("handoff cleanup accepted a directory without the handoff prefix")
	}
	requirePathState(t, unsafe, true)
	for _, name := range []string{
		"handoff-1001",
		"handoff-1001-00112233445566778899aabbccddeeff",
	} {
		if !validHelperHandoffName(name) {
			t.Errorf("validHelperHandoffName(%q)=false", name)
		}
	}
	for _, name := range []string{
		"handoff-owned", "handoff-1", "handoff-1001-short", "handoff-1001-ABCDEF00112233445566778899AABBCC",
	} {
		if validHelperHandoffName(name) {
			t.Errorf("validHelperHandoffName(%q)=true", name)
		}
	}
}

func TestHelperCloseRemovesOnlyOwnedHandoffDirectory(t *testing.T) {
	owned := filepath.Join(t.TempDir(), "handoff-1001-00112233445566778899aabbccddeeff")
	if err := os.MkdirAll(owned, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owned, "orphan.png"), []byte("pixels"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &helperBackend{shotsDir: owned, ownsShotsDir: true}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	requirePathState(t, owned, false)

	unowned := filepath.Join(t.TempDir(), "injected-test-shots")
	if err := os.MkdirAll(unowned, 0o700); err != nil {
		t.Fatal(err)
	}
	injected := &helperBackend{shotsDir: unowned}
	if err := injected.Close(); err != nil {
		t.Fatal(err)
	}
	requirePathState(t, unowned, true)
}
