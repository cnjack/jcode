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
	// mu serializes round-trips: the protocol runs one request in flight at a
	// time (UI automation is a serial resource), and the mutex is what enforces
	// it. It guards every field below, including connection replacement after a
	// daemon crash.
	mu  sync.Mutex
	seq uint64

	conn       net.Conn
	dead       bool
	closed     bool
	generation uint64
	// redial is installed by dialHelper. Injected net.Pipe backends leave it nil
	// unless a reconnect test supplies one explicitly.
	redial func(context.Context) (*helperBackend, error)

	// cmd is the spawned daemon process, nil when the connection was injected
	// (tests) or the daemon was already running. Close kills it if we own it.
	cmd *exec.Cmd

	// shotsDir is where the daemon writes screenshots it passes by reference.
	// Empty in tests, which take the PNG-by-value path.
	shotsDir     string
	ownsShotsDir bool

	platform      string
	helperVersion string
	// token is retained so a connected client can send another authenticated
	// ping and refresh TCC state after the user changes it in System Settings.
	token                     string
	accessibilityPermission   PermissionState
	screenRecordingPermission PermissionState
}

// newHelperConn performs the handshake over an already-connected conn and
// returns a ready backend. Tests inject a net.Pipe; dialHelper injects a real
// socket. Either way the protocol logic below is identical and fully exercised.
func newHelperConn(conn net.Conn, token string) (*helperBackend, error) {
	return newHelperConnContext(context.Background(), conn, token)
}

func newHelperConnContext(ctx context.Context, conn net.Conn, token string) (*helperBackend, error) {
	h := &helperBackend{conn: conn, generation: 1, token: token}
	if err := h.handshake(ctx, token); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return h, nil
}

func (h *helperBackend) Kind() string { return "helper" }

// Generation changes whenever this object swaps in a newly connected daemon.
// Sessions use it to invalidate uid/ref bindings from the old daemon.
func (h *helperBackend) Generation() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.generation
}

// PermissionStatus returns the last permission state reported by the helper.
// Missing or unrecognized additive pong fields are normalized to unknown, never
// granted, so an old or malformed daemon cannot be mistaken for a ready one.
func (h *helperBackend) PermissionStatus() HelperPermissions {
	h.mu.Lock()
	defer h.mu.Unlock()
	return HelperPermissions{
		Accessibility:   normalizePermissionState(h.accessibilityPermission),
		ScreenRecording: normalizePermissionState(h.screenRecordingPermission),
	}
}

// RefreshPermissionStatus sends another authenticated ping over the existing
// connection. AXIsProcessTrusted and CGPreflightScreenCaptureAccess are sampled
// by the daemon for every pong, so a settings poll can notice a grant without
// restarting jcode or the daemon.
func (h *helperBackend) RefreshPermissionStatus(ctx context.Context) (HelperPermissions, error) {
	h.mu.Lock()
	token := h.token
	h.mu.Unlock()

	pong, err := h.requestPong(ctx, token)
	if err != nil {
		return h.PermissionStatus(), fmt.Errorf("refresh helper permission status: %w", err)
	}
	h.applyPong(pong)
	return h.PermissionStatus(), nil
}

// handshake sends ping (with the auth token) and validates pong. A version
// mismatch is fatal and non-retryable — an old daemon and a new client must not
// half-speak a protocol.
func (h *helperBackend) handshake(ctx context.Context, token string) error {
	pong, err := h.requestPong(ctx, token)
	if err != nil {
		return fmt.Errorf("helper handshake: %w", err)
	}
	h.applyPong(pong)
	return nil
}

func (h *helperBackend) requestPong(ctx context.Context, token string) (pongPayload, error) {
	var pong pongPayload
	err := h.roundTripTyped(ctx, typePing, pingPayload{
		ClientAPIVersion: apiVersion,
		Token:            token,
	}, typePong, &pong)
	if err != nil {
		return pongPayload{}, err
	}
	if pong.ServerAPIVersion != apiVersion {
		return pongPayload{}, fmt.Errorf("helper speaks %q, this client speaks %q — incompatible, not retrying",
			pong.ServerAPIVersion, apiVersion)
	}
	return pong, nil
}

func (h *helperBackend) applyPong(pong pongPayload) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.platform = pong.Platform
	h.helperVersion = pong.HelperVersion
	h.accessibilityPermission = normalizePermissionState(pong.AccessibilityPermission)
	h.screenRecordingPermission = normalizePermissionState(pong.ScreenRecordingPermission)
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
	if err := h.ensureConnectedLocked(ctx); err != nil {
		return envelope{}, err
	}
	conn := h.conn

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
	_ = conn.SetDeadline(deadline)

	// Watch ctx: a cancellation (not just a deadline) must interrupt a blocked
	// read/write. Setting the deadline to now forces the blocked syscall to
	// return immediately.
	stop := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			_ = conn.SetDeadline(time.Now())
		case <-stop:
		}
	}()
	defer func() {
		// Join the watcher before clearing the deadline. Without the join, a
		// simultaneous ctx cancellation could set a stale immediate deadline
		// after this request returned and make the next RPC fail spuriously.
		close(stop)
		<-watcherDone
		_ = conn.SetDeadline(time.Time{})
	}()

	if err := writeFrame(conn, envelope{Type: reqType, ID: id, Payload: raw}); err != nil {
		h.markDeadLocked()
		return envelope{}, transportErr(ctx, reqType, "write", err)
	}

	var resp envelope
	if err := readFrame(conn, &resp); err != nil {
		h.markDeadLocked()
		return envelope{}, transportErr(ctx, reqType, "read response to", err)
	}
	if resp.ID != id {
		h.markDeadLocked()
		return envelope{}, requestOutcomeErr(reqType,
			fmt.Errorf("response id %d does not match request id %d (protocol desync)", resp.ID, id))
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
		return requestOutcomeErr(reqType,
			fmt.Errorf("expected %q response to %s, got %q", wantType, reqType, resp.Type))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Payload, out); err != nil {
		return requestOutcomeErr(reqType, fmt.Errorf("decode %s result: %w", reqType, err))
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
	shot, err := h.CaptureVisual(ctx, bundleID)
	return shot.PNG, err
}

func (h *helperBackend) CaptureVisual(ctx context.Context, bundleID string) (Screenshot, error) {
	var res captureResult
	if err := h.roundTripTyped(ctx, typeCapture, appRequest{App: bundleID}, typeResult, &res); err != nil {
		return Screenshot{}, err
	}
	png := res.PNG
	if res.Ref != "" {
		// The daemon wrote the PNG to the shared shots dir and handed back a
		// path, keeping the image off the socket. Read it here.
		var err error
		png, err = h.readShotRef(res.Ref)
		if err != nil {
			return Screenshot{}, err
		}
	}
	return Screenshot{
		PNG: png, X: res.X, Y: res.Y, Width: res.Width, Height: res.Height,
		PixelWidth: res.PixelWidth, PixelHeight: res.PixelHeight,
	}, nil
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
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	err := h.dropConnectionLocked()
	if h.ownsShotsDir {
		if cleanupErr := removeOwnedHelperHandoffDir(h.shotsDir); err == nil {
			err = cleanupErr
		}
	}
	return err
}

// ensureConnectedLocked repairs a transport on the first RPC *after* a failed
// exchange. The failed RPC itself is never replayed: a click may have landed
// before the daemon died, so automatic retry could double-apply a mutation.
func (h *helperBackend) ensureConnectedLocked(ctx context.Context) error {
	if h.closed {
		return fmt.Errorf("computer-use helper is closed")
	}
	if h.conn != nil && !h.dead {
		return nil
	}
	if h.redial == nil {
		return fmt.Errorf("computer-use helper connection is unavailable")
	}
	fresh, err := h.redial(ctx)
	if err != nil {
		return fmt.Errorf("reconnect computer-use helper: %w", err)
	}
	// Transfer ownership from the temporary backend without closing the new
	// connection when it goes out of scope.
	h.conn = fresh.conn
	h.cmd = fresh.cmd
	h.shotsDir = fresh.shotsDir
	h.ownsShotsDir = fresh.ownsShotsDir
	h.platform = fresh.platform
	h.helperVersion = fresh.helperVersion
	h.token = fresh.token
	h.accessibilityPermission = fresh.accessibilityPermission
	h.screenRecordingPermission = fresh.screenRecordingPermission
	h.dead = false
	h.generation++
	fresh.conn = nil
	fresh.cmd = nil
	fresh.ownsShotsDir = false
	return nil
}

func (h *helperBackend) markDeadLocked() {
	h.dead = true
	_ = h.dropConnectionLocked()
}

func (h *helperBackend) dropConnectionLocked() error {
	var err error
	if h.conn != nil {
		err = h.conn.Close()
		h.conn = nil
	}
	if h.cmd != nil && h.cmd.Process != nil {
		// We spawned it; do not leave an automation daemon running past the
		// process that needed it.
		_ = h.cmd.Process.Kill()
		_ = h.cmd.Wait()
	}
	h.cmd = nil
	return err
}

// defaultRPCTimeout bounds an exchange when the caller passed no deadline. It is
// generous because the daemon auto-waits for the UI to settle (up to ~5s) before
// answering an action; it is finite because a hung daemon must not hang forever.
const defaultRPCTimeout = 30 * time.Second

func transportErr(ctx context.Context, reqType, phase string, cause error) error {
	mutating := reqType == typePerform || reqType == typeLaunch
	if contextErr := ctx.Err(); contextErr != nil {
		if mutating {
			return fmt.Errorf("%w; the request outcome is unknown — inspect the UI before deciding whether to retry", contextErr)
		}
		return contextErr
	}
	err := fmt.Errorf("%s %s: %w", phase, reqType, cause)
	if mutating {
		return fmt.Errorf("%w; the request outcome is unknown — inspect the UI before deciding whether to retry", err)
	}
	return err
}

func requestOutcomeErr(reqType string, err error) error {
	if reqType == typePerform || reqType == typeLaunch {
		return fmt.Errorf("%w; the request outcome is unknown — inspect the UI before deciding whether to retry", err)
	}
	return err
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
