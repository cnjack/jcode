package computer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
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
			ServerAPIVersion: apiVersion, Platform: "darwin", HelperVersion: "test-1.0",
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
		return result(id, captureResult{PNG: []byte("\x89PNG\r\n\x1a\nfake")})
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
	png, err := h.Capture(ctx, "com.apple.Notes")
	if err != nil || !strings.HasPrefix(string(png), "\x89PNG") {
		t.Fatalf("Capture = %q, %v", png, err)
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

// --- the token is the actual boundary (design §4) ---

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

// --- a desynced response id is detected, not silently accepted ---

func TestHelperDetectsIDDesync(t *testing.T) {
	h, _ := dialMock(t, func(d *mockDaemon) { d.wrongID = true })
	_, err := h.ListApps(context.Background())
	if err == nil || !strings.Contains(err.Error(), "desync") {
		t.Fatalf("a mismatched response id must be caught, got %v", err)
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
}
