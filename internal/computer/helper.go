package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/uitree"
)

// helperBackend is the Backend that talks to the native helper daemon over a
// socket. It is a thin RPC mirror of the nine Backend methods: each marshals a
// request frame, reads a response frame, and honors the caller's context.
//
// It is identical on every OS — it speaks JSON, not AX or UIA — so it carries no
// build tags. The platform-specific piece is only how the socket is dialed
// (unix socket vs named pipe), which lives in dialHelper.
//
// See internal-doc/computer-helper-design.md §1, §3.
type helperBackend struct {
	conn net.Conn

	// mu serializes round-trips: the protocol runs one request in flight at a
	// time (UI automation is a serial resource), and the mutex is what enforces
	// it. It also guards seq.
	mu  sync.Mutex
	seq uint64

	// cmd is the spawned daemon process, nil when the connection was injected
	// (tests) or the daemon was already running. Close kills it if we own it.
	cmd *exec.Cmd

	// shotsDir is where the daemon writes screenshots it passes by reference.
	// Empty in tests, which take the PNG-by-value path.
	shotsDir string

	platform      string
	helperVersion string
}

// newHelperConn performs the handshake over an already-connected conn and
// returns a ready backend. Tests inject a net.Pipe; dialHelper injects a real
// socket. Either way the protocol logic below is identical and fully exercised.
func newHelperConn(conn net.Conn, token string) (*helperBackend, error) {
	h := &helperBackend{conn: conn}
	if err := h.handshake(context.Background(), token); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return h, nil
}

func (h *helperBackend) Kind() string { return "helper" }

// handshake sends ping (with the auth token) and validates pong. A version
// mismatch is fatal and non-retryable — an old daemon and a new client must not
// half-speak a protocol.
func (h *helperBackend) handshake(ctx context.Context, token string) error {
	var pong pongPayload
	err := h.roundTripTyped(ctx, typePing, pingPayload{
		ClientAPIVersion: apiVersion,
		Token:            token,
	}, typePong, &pong)
	if err != nil {
		return fmt.Errorf("helper handshake: %w", err)
	}
	if pong.ServerAPIVersion != apiVersion {
		return fmt.Errorf("helper speaks %q, this client speaks %q — incompatible, not retrying",
			pong.ServerAPIVersion, apiVersion)
	}
	h.platform = pong.Platform
	h.helperVersion = pong.HelperVersion
	return nil
}

// roundTrip is the single choke point for a request/response exchange. It holds
// the mutex for the whole exchange (one request in flight), stamps a sequence
// id, honors ctx by forcing a deadline on the conn when ctx fires, and returns
// the raw response envelope for the caller to decode.
//
// An unanswered permission prompt on the daemon side presents as a silent hang
// (design's load-bearing note on the Backend interface), so a caller that passes
// a ctx with no deadline gets one imposed here — the socket must never be the
// thing that wedges the agent.
func (h *helperBackend) roundTrip(ctx context.Context, reqType string, payload any) (envelope, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.seq++
	id := h.seq

	raw, err := json.Marshal(payload)
	if err != nil {
		return envelope{}, fmt.Errorf("marshal %s payload: %w", reqType, err)
	}

	// Bound the exchange. Prefer the caller's deadline; impose a generous default
	// when it has none, so a hung daemon cannot hang the agent forever.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultRPCTimeout)
	}
	_ = h.conn.SetDeadline(deadline)
	defer func() { _ = h.conn.SetDeadline(time.Time{}) }()

	// Watch ctx: a cancellation (not just a deadline) must interrupt a blocked
	// read/write. Setting the deadline to now forces the blocked syscall to
	// return immediately.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = h.conn.SetDeadline(time.Now())
		case <-stop:
		}
	}()

	if err := writeFrame(h.conn, envelope{Type: reqType, ID: id, Payload: raw}); err != nil {
		return envelope{}, ctxErr(ctx, fmt.Errorf("write %s: %w", reqType, err))
	}

	var resp envelope
	if err := readFrame(h.conn, &resp); err != nil {
		return envelope{}, ctxErr(ctx, fmt.Errorf("read response to %s: %w", reqType, err))
	}
	if resp.ID != id {
		return envelope{}, fmt.Errorf("response id %d does not match request id %d (protocol desync)", resp.ID, id)
	}
	if resp.Type == typeError {
		return resp, decodeDaemonError(resp.Payload)
	}
	return resp, nil
}

// roundTripTyped is roundTrip plus decoding the result into out, asserting the
// response type. Most methods use this; capture/tree use roundTrip directly.
func (h *helperBackend) roundTripTyped(ctx context.Context, reqType string, payload any, wantType string, out any) error {
	resp, err := h.roundTrip(ctx, reqType, payload)
	if err != nil {
		return err
	}
	if resp.Type != wantType {
		return fmt.Errorf("expected %q response to %s, got %q", wantType, reqType, resp.Type)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Payload, out); err != nil {
		return fmt.Errorf("decode %s result: %w", reqType, err)
	}
	return nil
}

// --- Backend interface ---

func (h *helperBackend) ListApps(ctx context.Context) ([]App, error) {
	var res listAppsResult
	if err := h.roundTripTyped(ctx, typeListApps, struct{}{}, typeResult, &res); err != nil {
		return nil, err
	}
	apps := make([]App, len(res.Apps))
	for i, a := range res.Apps {
		apps[i] = a.toApp()
	}
	return apps, nil
}

func (h *helperBackend) Frontmost(ctx context.Context) (App, error) {
	var res frontmostResult
	if err := h.roundTripTyped(ctx, typeFrontmost, struct{}{}, typeResult, &res); err != nil {
		return App{}, err
	}
	return res.App.toApp(), nil
}

func (h *helperBackend) Tree(ctx context.Context, bundleID string) ([]uitree.Node, error) {
	var res treeResult
	if err := h.roundTripTyped(ctx, typeTree, treeRequest{App: bundleID}, typeResult, &res); err != nil {
		return nil, err
	}
	return res.Nodes, nil
}

func (h *helperBackend) Capture(ctx context.Context, bundleID string) ([]byte, error) {
	var res captureResult
	if err := h.roundTripTyped(ctx, typeCapture, appRequest{App: bundleID}, typeResult, &res); err != nil {
		return nil, err
	}
	if res.Ref != "" {
		// The daemon wrote the PNG to the shared shots dir and handed back a
		// path, keeping the image off the socket. Read it here.
		return h.readShotRef(res.Ref)
	}
	return res.PNG, nil
}

func (h *helperBackend) Launch(ctx context.Context, bundleID string) error {
	return h.roundTripTyped(ctx, typeLaunch, appRequest{App: bundleID}, typeResult, nil)
}

func (h *helperBackend) ReadClipboard(ctx context.Context) (string, error) {
	var res readClipboardResult
	if err := h.roundTripTyped(ctx, typeReadClipboard, struct{}{}, typeResult, &res); err != nil {
		return "", err
	}
	return res.Text, nil
}

func (h *helperBackend) Perform(ctx context.Context, act Action) error {
	return h.roundTripTyped(ctx, typePerform, performRequest{Action: actionToWire(act)}, typeResult, nil)
}

func (h *helperBackend) Close() error {
	err := h.conn.Close()
	if h.cmd != nil && h.cmd.Process != nil {
		// We spawned it; do not leave an automation daemon running past the
		// process that needed it.
		_ = h.cmd.Process.Kill()
	}
	return err
}

// defaultRPCTimeout bounds an exchange when the caller passed no deadline. It is
// generous because the daemon auto-waits for the UI to settle (up to ~5s) before
// answering an action; it is finite because a hung daemon must not hang forever.
const defaultRPCTimeout = 30 * time.Second

// ctxErr prefers the context's own error when it fired, so a cancellation reads
// as a cancellation rather than as an opaque "i/o timeout" from the forced
// deadline.
func ctxErr(ctx context.Context, fallback error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fallback
}

// decodeDaemonError maps an error frame onto a Go error, translating the codes
// the tool layer keys on into their sentinels.
func decodeDaemonError(payload json.RawMessage) error {
	var ep errorPayload
	if err := json.Unmarshal(payload, &ep); err != nil {
		return fmt.Errorf("daemon returned an undecodable error: %w", err)
	}
	switch ep.Code {
	case codeUserIntervened:
		return ErrControlInterrupted
	case codeScreenLocked:
		return ErrScreenLocked
	case codeAppNotAllowed:
		return &NotAllowedError{AppName: ep.Message}
	}
	if ep.Message == "" {
		ep.Message = fmt.Sprintf("daemon error %d", ep.Code)
	}
	return fmt.Errorf("%s", ep.Message)
}
