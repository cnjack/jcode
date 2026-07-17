package computer

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/cnjack/jcode/internal/uitree"
)

// The helper wire protocol. See internal-doc/computer-helper-design.md §2, §3.
//
// Framing: a 4-byte little-endian length prefix followed by that many bytes of
// UTF-8 JSON. The cap is enforced on both encode and decode so a corrupt or
// hostile length can never make either side allocate gigabytes.
//
// Payload: a tagged envelope {"type": "...", "id": N, "payload": {...}}. The
// type discriminates the request; the id pairs a response with its request (the
// protocol runs one request in flight at a time, but the id makes a stray or
// duplicated frame detectable rather than silently mismatched).

// apiVersion is bumped when the wire format changes incompatibly. The daemon
// echoes its own; a mismatch is a hard, non-retryable error — an old daemon and
// a new client must not half-speak a protocol.
const apiVersion = "JcodeComputerIPC-1"

// maxFrame bounds a single frame at 8 MiB. A full-window PNG can approach this;
// anything past it is a bug or an attack, not a real message. Screenshots that
// would exceed it are passed by file reference instead (see capture).
const maxFrame = 8 << 20

// frame types. Request types are the nine Backend methods plus the handshake.
const (
	typePing          = "ping"
	typePong          = "pong"
	typeListApps      = "list_apps"
	typeFrontmost     = "frontmost"
	typeTree          = "tree"
	typeCapture       = "capture"
	typeLaunch        = "launch"
	typeReadClipboard = "read_clipboard"
	typePerform       = "perform"
	// request_permissions is not a Backend method: it drives the macOS consent
	// prompts (Settings → Request permission, /computer grant) and answers with
	// a pong-shaped payload so the client refreshes both grant states in one
	// round trip. An old daemon answers "unknown request type"; callers map
	// that to a stale-helper error rather than a protocol failure.
	typeRequestPermissions = "request_permissions"
	typeResult             = "result"
	typeError              = "error"
)

// envelope is one framed message in either direction.
type envelope struct {
	Type    string          `json:"type"`
	ID      uint64          `json:"id"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// --- handshake ---

type pingPayload struct {
	ClientAPIVersion string `json:"client_api_version"`
	// Token binds a connection to this daemon launch and rejects stale/accidental
	// rendezvous. The kernel-reported PID is checked separately. Neither is a
	// hostile same-uid boundary: that uid can read a 0600 file or start a new
	// helper instance. See the signed-parent/XPC gap in design §4.
	Token string `json:"token"`
}

type pongPayload struct {
	ServerAPIVersion          string          `json:"server_api_version"`
	Platform                  string          `json:"platform"`
	HelperVersion             string          `json:"helper_version"`
	AccessibilityPermission   PermissionState `json:"accessibility_permission,omitempty"`
	ScreenRecordingPermission PermissionState `json:"screen_recording_permission,omitempty"`
}

// PermissionState is the native helper's non-prompting view of a macOS TCC
// grant. Unknown is intentionally distinct from denied: an older daemon omits
// the additive pong fields, and treating that absence as granted would make the
// settings UI claim Computer Use is ready without evidence.
type PermissionState string

const (
	PermissionUnknown PermissionState = "unknown"
	PermissionDenied  PermissionState = "denied"
	PermissionGranted PermissionState = "granted"
)

func normalizePermissionState(state PermissionState) PermissionState {
	switch state {
	case PermissionGranted, PermissionDenied, PermissionUnknown:
		return state
	default:
		return PermissionUnknown
	}
}

// HelperPermissions is the pair of macOS grants needed by the native helper.
// Accessibility covers AX inspection and input; ScreenRecording covers window
// pixels. The states are snapshots and can be refreshed with
// helperBackend.RefreshPermissionStatus.
type HelperPermissions struct {
	Accessibility   PermissionState
	ScreenRecording PermissionState
}

// --- per-method request/response payloads ---
//
// These mirror the Backend interface one-for-one. App and Action already live in
// computer.go; the wire uses their JSON tags directly.

type appWire struct {
	BundleID string `json:"bundle_id"`
	Name     string `json:"name"`
	Running  bool   `json:"running"`
}

func (a appWire) toApp() App { return App(a) }

type listAppsResult struct {
	Apps []appWire `json:"apps"`
}

type frontmostResult struct {
	App appWire `json:"app"`
}

type appRequest struct {
	App string `json:"app"`
}

type treeRequest struct {
	App         string `json:"app"`
	DisableDiff bool   `json:"disable_diff"`
}

// treeResult carries the flattened accessibility tree. The nodes are
// uitree.Node directly — the helper is a mirror of the same shape the Go side
// renders, so no translation is needed here. Gen is the daemon's per-app read
// generation, advisory only (the Go side owns staleness via uitree).
type treeResult struct {
	Nodes []uitree.Node `json:"nodes"`
	Gen   int           `json:"gen"`
}

// captureResult returns a PNG either by value (base64) or, preferably, by
// reference to a file the daemon wrote under the shared shots dir — keeping
// large images off the socket. Exactly one of Ref/PNG is set.
type captureResult struct {
	Ref         string  `json:"ref,omitempty"`
	PNG         []byte  `json:"png,omitempty"`
	X           float64 `json:"x,omitempty"`
	Y           float64 `json:"y,omitempty"`
	Width       float64 `json:"width,omitempty"`
	Height      float64 `json:"height,omitempty"`
	PixelWidth  int     `json:"pixel_width,omitempty"`
	PixelHeight int     `json:"pixel_height,omitempty"`
}

type performRequest struct {
	Action actionWire `json:"action"`
}

// actionWire is Action on the wire. BundleID is the resolved target pinned at
// gate time; the daemon never re-resolves an app name (design §4.3).
type actionWire struct {
	Kind      string   `json:"kind"`
	BundleID  string   `json:"bundle_id"`
	UID       string   `json:"uid,omitempty"`
	Ref       int64    `json:"ref,omitempty"`
	Value     string   `json:"value,omitempty"`
	Key       string   `json:"key,omitempty"`
	Text      string   `json:"text,omitempty"`
	Name      string   `json:"name,omitempty"`
	X         *float64 `json:"x,omitempty"`
	Y         *float64 `json:"y,omitempty"`
	ToX       *float64 `json:"to_x,omitempty"`
	ToY       *float64 `json:"to_y,omitempty"`
	Direction string   `json:"direction,omitempty"`
	Pages     float64  `json:"pages,omitempty"`
}

func actionToWire(a Action) actionWire {
	w := actionWire{
		Kind: a.Kind, BundleID: a.BundleID, UID: a.UID, Ref: a.Ref,
		Value: a.Value, Key: a.Key, Text: a.Text, Name: a.Name,
		Direction: a.Direction, Pages: a.Pages,
	}
	// Non-zero inference preserves direct Backend callers. Session callers also
	// carry explicit presence bits so a legitimate zero survives omitempty.
	if a.HasX || a.X != 0 {
		w.X = float64Pointer(a.X)
	}
	if a.HasY || a.Y != 0 {
		w.Y = float64Pointer(a.Y)
	}
	if a.HasToX || a.ToX != 0 {
		w.ToX = float64Pointer(a.ToX)
	}
	if a.HasToY || a.ToY != 0 {
		w.ToY = float64Pointer(a.ToY)
	}
	return w
}

func float64Pointer(value float64) *float64 { return &value }

type readClipboardResult struct {
	Text string `json:"text"`
}

// requestPermissionsPayload asks the daemon to surface the macOS consent
// prompt for the named grants. The prompts are asynchronous (the user answers
// in a system dialog), so the pong-shaped response reports the state at answer
// time — "denied" means "not granted yet", not "refused".
type requestPermissionsPayload struct {
	Accessibility   bool `json:"accessibility,omitempty"`
	ScreenRecording bool `json:"screen_recording,omitempty"`
}

// errorPayload carries a daemon-side failure. Code mirrors the parent's error
// taxonomy (design §7); the client maps the codes it cares about onto sentinel
// errors (ErrControlInterrupted, ErrScreenLocked, …).
type errorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// error codes, mirroring codex's taxonomy 1:1 (design §7).
const (
	codeSenderNotAuthenticated = -10000
	codeAppNotAllowed          = -10006
	codeAccessibilityError     = -10008
	codePermissionsNotGranted  = -10009
	codeIncompatibleVersion    = -10013
	codeUserIntervened         = -10016
	codeCouldNotGetSenderPID   = -10017
	codeAmbiguousApp           = -10018
	codeScreenLocked           = -10020
)

// --- framing ---

// writeFrame length-prefixes and writes one JSON message.
func writeFrame(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	if len(body) > maxFrame {
		return fmt.Errorf("frame of %d bytes exceeds the %d-byte cap", len(body), maxFrame)
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// readFrame reads one length-prefixed JSON message into v. The cap is checked
// before allocating, so a hostile length header cannot force a huge allocation.
func readFrame(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n > maxFrame {
		return fmt.Errorf("incoming frame claims %d bytes, over the %d-byte cap", n, maxFrame)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}
