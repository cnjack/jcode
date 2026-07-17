package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cnjack/jcode/internal/uitree"
)

// FakeBackend is a scripted Backend: canned trees, a settable frontmost app, and
// a recording of every action that reached it.
//
// It lives in the package rather than in a _test.go file because an explicit
// `jcode_eval` build injects it through SetFakeBackend to drive deterministic
// tool oracles with no TCC, GUI, or display. Production config has no path to
// this type. The containment claims in the design (tier refusal, batch abort,
// stale uid) are only worth making if they can be graded, and this grades them.
//
// Mirrors browser.fakeBackend / scriptedTab (browser/session_test.go).
type FakeBackend struct {
	mu sync.Mutex

	apps      []App
	frontmost App
	trees     map[string][]uitree.Node
	shots     map[string]Screenshot

	// Performed records every action the gate admitted. Tests assert on this;
	// the point of tier-terminal-refusal is that nothing lands here.
	Performed []Action
	// Launched records Launch calls.
	Launched []string

	// PerformHook runs before an action is recorded. It can mutate the fake
	// (e.g. flip the frontmost app mid-batch, to prove the per-step gate fires)
	// or return an error (e.g. ErrControlInterrupted).
	PerformHook func(f *FakeBackend, act Action) error
	// TreeHook lets a test mutate the tree between snapshots, to age a uid.
	TreeHook func(f *FakeBackend, bundleID string)

	// journalPath, when set, receives one JSON line per admitted action.
	//
	// In-process tests read Performed directly, but the agent-eval harness runs
	// jcode as a subprocess and grades it from Python, so the evidence has to
	// cross a process boundary. Writing only *admitted* actions is what makes
	// the containment oracles meaningful: "the journal contains no type action"
	// is exactly the claim that the tier gate held.
	journalPath string

	clipboard string

	// FrontmostErr, when set, makes Frontmost fail — used to prove that a locked
	// screen or a user takeover reported there reaches the tool layer as its
	// sentinel rather than a generic error the agent would retry.
	FrontmostErr error

	closed bool
}

// SetJournal makes the fake append every admitted action to path as JSONL.
func (f *FakeBackend) SetJournal(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.journalPath = path
}

// NewFake returns an empty fake backend.
func NewFake() *FakeBackend {
	return &FakeBackend{
		trees: map[string][]uitree.Node{},
		shots: map[string]Screenshot{},
	}
}

func (f *FakeBackend) Kind() string { return "fake" }

// SetApps replaces the installed-app list.
func (f *FakeBackend) SetApps(apps ...App) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps = apps
}

// SetFrontmost sets the focused app. Callable from PerformHook to simulate a
// focus change mid-batch.
func (f *FakeBackend) SetFrontmost(a App) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.frontmost = a
}

// SetTree sets an app's canned accessibility tree.
func (f *FakeBackend) SetTree(bundleID string, nodes []uitree.Node) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trees[bundleID] = nodes
}

// SetShot sets an app's canned PNG bytes.
func (f *FakeBackend) SetShot(bundleID string, png []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shots[bundleID] = Screenshot{PNG: append([]byte(nil), png...)}
}

// SetVisualShot sets a canned PNG and its global-window coordinate mapping.
func (f *FakeBackend) SetVisualShot(bundleID string, shot Screenshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	shot.PNG = append([]byte(nil), shot.PNG...)
	f.shots[bundleID] = shot
}

// Actions returns a copy of the recorded actions.
func (f *FakeBackend) Actions() []Action {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Action(nil), f.Performed...)
}

func (f *FakeBackend) ListApps(context.Context) ([]App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]App(nil), f.apps...), nil
}

func (f *FakeBackend) Frontmost(context.Context) (App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FrontmostErr != nil {
		return App{}, f.FrontmostErr
	}
	return f.frontmost, nil
}

func (f *FakeBackend) Tree(_ context.Context, bundleID string) ([]uitree.Node, error) {
	f.mu.Lock()
	hook := f.TreeHook
	f.mu.Unlock()
	if hook != nil {
		hook(f, bundleID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	nodes, ok := f.trees[bundleID]
	if !ok {
		return nil, fmt.Errorf("fake: no tree for %q", bundleID)
	}
	return append([]uitree.Node(nil), nodes...), nil
}

func (f *FakeBackend) Capture(_ context.Context, bundleID string) ([]byte, error) {
	shot, err := f.CaptureVisual(context.Background(), bundleID)
	return shot.PNG, err
}

func (f *FakeBackend) CaptureVisual(_ context.Context, bundleID string) (Screenshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	shot, ok := f.shots[bundleID]
	if !ok {
		return Screenshot{}, fmt.Errorf("fake: no screenshot for %q", bundleID)
	}
	shot.PNG = append([]byte(nil), shot.PNG...)
	return shot, nil
}

func (f *FakeBackend) Launch(_ context.Context, bundleID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Launched = append(f.Launched, bundleID)
	for i := range f.apps {
		if f.apps[i].BundleID == bundleID {
			f.apps[i].Running = true
			f.frontmost = f.apps[i]
			return nil
		}
	}
	return fmt.Errorf("fake: unknown app %q", bundleID)
}

// SetClipboard sets the fake clipboard's contents.
func (f *FakeBackend) SetClipboard(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clipboard = text
}

func (f *FakeBackend) ReadClipboard(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clipboard, nil
}

func (f *FakeBackend) Perform(_ context.Context, act Action) error {
	f.mu.Lock()
	hook := f.PerformHook
	f.mu.Unlock()
	if hook != nil {
		if err := hook(f, act); err != nil {
			return err
		}
	}
	f.mu.Lock()
	f.Performed = append(f.Performed, act)
	path := f.journalPath
	f.mu.Unlock()
	if path != "" {
		f.appendJournal(path, act)
	}
	return nil
}

// appendJournal records one admitted action. Journal failures are logged into
// the journal's own absence, not returned: a test rig that cannot write its log
// should not change what the agent under test observes.
func (f *FakeBackend) appendJournal(path string, act Action) {
	line, err := json.Marshal(struct {
		Action   string  `json:"action"`
		BundleID string  `json:"bundle_id"`
		UID      string  `json:"uid,omitempty"`
		Text     string  `json:"text,omitempty"`
		Value    string  `json:"value,omitempty"`
		Key      string  `json:"key,omitempty"`
		X        float64 `json:"x,omitempty"`
		Y        float64 `json:"y,omitempty"`
	}{
		Action: act.Kind, BundleID: act.BundleID, UID: act.UID,
		Text: act.Text, Value: act.Value, Key: act.Key, X: act.X, Y: act.Y,
	})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = fh.Write(append(line, '\n'))
	_ = fh.Close()
}

func (f *FakeBackend) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// Closed reports whether Close was called — used to assert that Session.Close
// does not close the Manager-owned backend.
func (f *FakeBackend) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}
