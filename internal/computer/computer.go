// Package computer implements computer-use: reading and operating native
// desktop application UI.
//
// It is the sibling of internal/browser and is deliberately shaped like it:
// a process-lifetime Manager owns Backends, a task-lifetime Session owns
// per-task state, and Session.Close never closes the Backend. Snapshots are
// uid-annotated accessibility text rendered by internal/uitree, the same
// renderer browser-use uses, so the agent reads native apps with the same
// vocabulary it reads web pages.
//
// jcode is built CGO_ENABLED=0 (agent-eval finding F1: cgo SIGABRTs on
// subprocess fork on macOS 26), so the macOS AX / CGEvent / ScreenCaptureKit
// calls cannot live in this process. Production delegates them to the native
// macOS helper over a Unix socket. Tests and explicit jcode_eval builds may
// inject a scripted backend; persisted settings cannot select one.
//
// See internal-doc/computer-use-design.md.
package computer

import (
	"context"
	"errors"
	"fmt"

	"github.com/cnjack/jcode/internal/uitree"
)

// MaxScreenshotBytes bounds both native IPC handoff files and any injected
// backend result before the tool creates a Base64 copy for the model request.
const MaxScreenshotBytes int64 = 20 << 20

// ErrControlInterrupted reports that the human took over (moved the mouse,
// switched apps deliberately, hit a kill switch). The agent must stop rather
// than retry: if the human grabbed the mouse, they had a reason.
//
// Mirrors browser.ErrControlInterrupted and gets the same treatment in the tool
// layer — swallowed into a natural-language "stopping" message.
var ErrControlInterrupted = errors.New("computer control interrupted")

// ErrScreenLocked reports that the screen is locked. Not configurable: an agent
// driving a machine its owner believes is secured is not a feature.
var ErrScreenLocked = errors.New("screen is locked")

// ErrStaleUID reports a uid minted in an earlier snapshot generation. Rejecting
// it is load-bearing — on a native desktop a stale uid that now resolves to a
// different element means clicking the wrong button in a real app.
var ErrStaleUID = errors.New("stale element uid")

// App is one application known to the backend.
type App struct {
	BundleID string
	Name     string
	Running  bool
}

// Action is one UI interaction.
//
// BundleID is the *resolved* target, pinned at approval time and never
// re-derived from a display name later. Codex hit a real TOCTOU attack here —
// a mutable `app` field that returned "Calculator" to the approval check and
// "Terminal" to the executor — and defends with input freezing. Go copies
// structs by value, which gets us most of that, but the discipline is the same:
// resolve identity once, carry the resolved value.
type Action struct {
	Kind     string // click, type, press, set_value, scroll, drag, select_text, menu, hover, dblclick, rclick
	BundleID string
	UID      string
	Ref      int64 // resolved backend handle for UID
	Value    string
	Key      string
	Text     string
	Name     string // named AX secondary action for Kind=="menu"
	X, Y     float64
	ToX, ToY float64
	// Coordinate presence is separate from value because zero is a valid global
	// screen coordinate. Without these bits, JSON omitempty turns x=0 into a
	// missing field at the daemon boundary.
	HasX, HasY, HasToX, HasToY bool
	Direction                  string
	Pages                      float64
}

// Screenshot is a window-scoped visual observation. Bounds are global macOS
// screen coordinates; PixelWidth/PixelHeight describe the attached PNG after
// downscaling. Together they let a model map a pixel in custom-drawn UI back to
// the coordinate fallback accepted by computer_act.
type Screenshot struct {
	PNG                     []byte
	X, Y, Width, Height     float64
	PixelWidth, PixelHeight int
}

// VisualCaptureBackend is an optional richer capture contract. Backends that
// only implement Capture remain valid; native helpers implement this interface
// so screenshot coordinates are explicit rather than guessed.
type VisualCaptureBackend interface {
	CaptureVisual(ctx context.Context, bundleID string) (Screenshot, error)
}

// Backend is the platform side of computer use. Shipping binaries use
// helperBackend (the native macOS daemon over a Unix socket). FakeBackend is
// injectable only by tests and explicit jcode_eval wiring.
//
// Every method takes a ctx with a deadline. An unanswered TCC prompt presents as
// a silent multi-minute hang, not an error, so a missing deadline anywhere here
// is a wedged agent.
type Backend interface {
	Kind() string
	// ListApps returns installed/running apps. The names are attacker-
	// controllable and callers must treat them as tainted data.
	ListApps(ctx context.Context) ([]App, error)
	// Frontmost returns the app that currently has focus. This is the identity
	// the tier gate checks, and it must be read fresh immediately before every
	// action — a synthesized event goes to whatever holds focus now.
	Frontmost(ctx context.Context) (App, error)
	// Tree returns the accessibility tree of one app's windows.
	Tree(ctx context.Context, bundleID string) ([]uitree.Node, error)
	// Capture returns a PNG of one app's windows. Window-scoped, not
	// screen-scoped: a non-granted app is not filtered out of the capture, it
	// was never in it.
	Capture(ctx context.Context, bundleID string) ([]byte, error)
	// Launch starts or focuses an app.
	Launch(ctx context.Context, bundleID string) error
	// ReadClipboard returns the clipboard text. Gated by the clipboard_read
	// grant, which is deliberately separate from any app grant — the clipboard
	// belongs to the user, not to the app that happens to be in front.
	ReadClipboard(ctx context.Context) (string, error)
	// Perform executes one action. Implementations must auto-wait for the UI to
	// settle before returning.
	Perform(ctx context.Context, act Action) error
	Close() error
}

// TierError reports an action refused by the tier gate. It carries enough for
// the tool layer to explain the refusal usefully rather than just failing.
type TierError struct {
	BundleID string
	AppName  string
	Tier     Tier
	Action   string
}

func (e *TierError) Error() string {
	base := fmt.Sprintf("%q (%s) is at the %q tier, which does not permit %s",
		e.AppName, e.BundleID, e.Tier, e.Action)
	switch {
	case IsBrowser(e.BundleID):
		return base + ". Browsers are read-only for computer use because browser-use " +
			"can read the DOM and verify a URL before navigating, which a pixel click cannot. " +
			"Use the browser_* tools instead."
	case e.Tier == TierClick:
		return base + ". Terminals and IDEs cannot receive typed input from computer use, " +
			"because that would bypass jcode's approval system. Use the execute tool for shell commands."
	}
	return base
}

// NotAllowedError reports an app absent from the session allowlist.
type NotAllowedError struct{ BundleID, AppName string }

func (e *NotAllowedError) Error() string {
	return fmt.Sprintf("%q (%s) is not in the session allowlist; request access to it first",
		e.AppName, e.BundleID)
}
