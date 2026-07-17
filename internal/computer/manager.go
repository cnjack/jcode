package computer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Config is the process-wide computer-use configuration.
//
// It deliberately mirrors config.ComputerConfig rather than importing it, so
// this package does not depend on the config package. Exactly one mapper
// converts between them (see internal/computer/configmap.go) — browser-use grew
// two near-duplicate mappers that already disagree on a default, and that fork
// is not being reproduced here.
type Config struct {
	Enabled bool
	// Backend is a compatibility field for internal callers compiled against the
	// old shape. Runtime selection deliberately ignores it; persisted legacy
	// values are migrated in config.ComputerConfig before reaching this package.
	//
	// Deprecated: inject a FakeBackend explicitly with SetFakeBackend in tests and
	// evals. Production always uses the native helper.
	Backend            string
	Approval           map[string]string
	AppPermissions     []AppPermission
	MaxActionsPerBatch int
	ClipboardRead      bool
	ClipboardWrite     bool
	SystemKeyCombos    bool
}

// AppPermission is a per-app configuration row.
type AppPermission struct {
	BundleID string
	Tier     string
	Launch   string
	Interact string
}

const defaultMaxBatch = 20

// Manager owns Backends for the process lifetime. Sessions borrow one and never
// close it. (browser/manager.go makes the same split: backends are expensive to
// start — a Chrome launch, a daemon handshake — and are reused across tasks.)
type Manager struct {
	mu sync.Mutex
	// uiMu serializes native observations/mutations across every task Session.
	// The helper is process-wide, so per-Session locks alone cannot prevent one
	// task from acting on UI another task changed.
	uiMu      sync.Mutex
	uiEpoch   uint64
	cfg       Config
	shotDir   string
	shotMu    sync.Mutex
	configDir string // <home>/.jcode — where the helper socket/token live
	closed    bool

	backend Backend
	// helper is the cached daemon connection, reused across sessions (a TCC
	// prompt should happen once, not once per task). nil until first use.
	helper *helperBackend
	// helperInit is the one in-flight helper dial. OpenSession can be called by
	// parallel agents, but starting two daemons against one socket can associate
	// a connection with the wrong owning exec.Cmd and make the losing dial kill
	// the winner's daemon. Every concurrent caller shares this result instead.
	helperInit *helperInitCall
	// helperDialer is injectable for the concurrency test. Production leaves it
	// nil and uses dialHelper.
	helperDialer func(context.Context, string) (*helperBackend, error)
	// fake is injected explicitly by tests and the agent-eval harness. Persisted
	// configuration never selects it.
	fake Backend
}

type helperInitCall struct {
	done   chan struct{}
	cancel context.CancelFunc
	helper *helperBackend
	err    error
}

// NewManager creates the process-wide manager.
func NewManager(cfg Config, home string) *Manager {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	m := &Manager{
		cfg:       cloneConfig(cfg),
		shotDir:   filepath.Join(home, ".jcode", "computer", "shots"),
		configDir: filepath.Join(home, ".jcode"),
	}
	// Best-effort startup sweep covers screenshots left by a prior crash. Save
	// and OpenScreenshot surface their own cleanup failures synchronously.
	_ = m.sweepScreenshotStore(time.Now())
	return m
}

func cloneConfig(cfg Config) Config {
	copy := cfg
	if cfg.Approval != nil {
		copy.Approval = make(map[string]string, len(cfg.Approval))
		for k, v := range cfg.Approval {
			copy.Approval[k] = v
		}
	}
	copy.AppPermissions = append([]AppPermission(nil), cfg.AppPermissions...)
	return copy
}

// getHelper returns the cached daemon connection, dialing (and spawning the
// daemon) on first use. The connection is reused across sessions so a TCC prompt
// happens once, not once per task.
//
// helperBackend repairs a dead transport in place on the request after the one
// that observed the failure. In-place replacement matters because existing
// Sessions retain this pointer; replacing only m.helper would strand them on a
// broken pipe.
func (m *Manager) getHelper(ctx context.Context) (*helperBackend, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("computer-use manager is closed")
	}
	if m.helper != nil {
		h := m.helper
		m.mu.Unlock()
		return h, nil
	}
	if call := m.helperInit; call != nil {
		m.mu.Unlock()
		select {
		case <-call.done:
			return call.helper, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	dialCtx, cancel := context.WithCancel(ctx)
	call := &helperInitCall{done: make(chan struct{}), cancel: cancel}
	m.helperInit = call
	dir := m.configDir
	dialer := m.helperDialer
	m.mu.Unlock()

	if dialer == nil {
		dialer = dialHelper
	}
	hb, err := dialer(dialCtx, dir)
	cancel()

	var discard *helperBackend
	m.mu.Lock()
	if err == nil && m.closed {
		discard = hb
		hb = nil
		err = fmt.Errorf("computer-use manager was closed while connecting the helper")
	} else if err == nil {
		m.helper = hb
		m.backend = hb
	}
	call.helper = hb
	call.err = err
	if m.helperInit == call {
		m.helperInit = nil
	}
	close(call.done)
	m.mu.Unlock()

	if discard != nil {
		_ = discard.Close()
	}
	return hb, err
}

// SetConfig hot-swaps the configuration (the settings endpoint calls this, so
// no restart is needed).
func (m *Manager) SetConfig(cfg Config) {
	// Every native Session operation holds uiMu while checking policy and talking
	// to the backend. Taking the same lock makes a settings change an atomic
	// boundary: an action either finishes under the old policy or starts under the
	// new one; it can never observe a half-updated policy mid-flight.
	m.uiMu.Lock()
	defer m.uiMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cloneConfig(cfg)
}

// GetConfig returns the current configuration.
func (m *Manager) GetConfig() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneConfig(m.cfg)
}

// Enabled reports whether computer use is on. It defaults off, unlike
// browser-use: computer use can touch anything on the machine.
func (m *Manager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Enabled
}

// MaxBatch returns the batch cap.
func (m *Manager) MaxBatch() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return effectiveMaxBatch(m.cfg.MaxActionsPerBatch)
}

// SetFakeBackend installs a scripted backend explicitly. Used only by tests and
// eval wiring; persisted user configuration cannot reach this path.
func (m *Manager) SetFakeBackend(b Backend) {
	m.uiMu.Lock()
	defer m.uiMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fake = b
}

// TierOverrides builds the validated per-app tier map from config.
//
// An override may only tighten. A config row trying to loosen a terminal to
// "full" is dropped with the built-in tier left in place: loosening is a
// deliberate act the settings UI gates behind a warning, and a hand-edited
// config file is not that gate. An unparseable tier is likewise dropped rather
// than defaulted, so a typo cannot silently weaken containment.
func (m *Manager) TierOverrides() map[string]Tier {
	return tierOverrides(m.GetConfig())
}

// Preapproved reads the live, mutex-protected policy used by settings hot
// reload. Approval callbacks can run concurrently with an HTTP config update;
// reading the shared config.Config pointer directly would race and could retain
// a stale always-allow decision.
func (m *Manager) Preapproved(bundleID, class string) bool {
	if bundleID == "" {
		return false
	}
	cfg := m.GetConfig()
	if !cfg.Enabled {
		return false
	}
	for _, p := range cfg.AppPermissions {
		if p.BundleID != bundleID {
			continue
		}
		var value string
		switch class {
		case "launch":
			value = p.Launch
		case "interact":
			value = p.Interact
		}
		if value != "" {
			return value == "allow"
		}
		break
	}
	return cfg.Approval[class] == "always_allow"
}

func tierOverrides(cfg Config) map[string]Tier {
	out := map[string]Tier{}
	for _, p := range cfg.AppPermissions {
		if p.Tier == "" {
			continue
		}
		t, ok := ParseTier(p.Tier)
		if !ok {
			continue
		}
		if t < DefaultTier(p.BundleID) {
			out[p.BundleID] = t
		}
	}
	return out
}

type sessionPolicy struct {
	enabled         bool
	maxBatch        int
	tierOverrides   map[string]Tier
	clipboardRead   bool
	clipboardWrite  bool
	systemKeyCombos bool
}

// sessionPolicy returns the complete live enforcement policy for an existing
// Session. Callers hold uiMu, which is also held by SetConfig, so the returned
// policy and the native operation governed by it share one atomic boundary.
func (m *Manager) sessionPolicy() sessionPolicy {
	cfg := m.GetConfig()
	return sessionPolicy{
		enabled:         cfg.Enabled,
		maxBatch:        effectiveMaxBatch(cfg.MaxActionsPerBatch),
		tierOverrides:   tierOverrides(cfg),
		clipboardRead:   cfg.ClipboardRead,
		clipboardWrite:  cfg.ClipboardWrite,
		systemKeyCombos: cfg.SystemKeyCombos,
	}
}

// OpenSession returns a task-scoped Session bound to a Backend.
//
// Production always uses the native macOS helper. Tests and eval builds may
// explicitly inject a deterministic Backend with SetFakeBackend; the deprecated
// Config.Backend value never participates in this choice.
func (m *Manager) OpenSession(ctx context.Context) (*Session, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("computer-use manager is closed")
	}
	if !m.cfg.Enabled {
		m.mu.Unlock()
		return nil, fmt.Errorf("computer use is disabled; enable it in settings")
	}
	fake := m.fake
	m.mu.Unlock()

	var b Backend
	if fake != nil {
		b = fake
	} else {
		hb, err := m.getHelper(ctx)
		if err != nil {
			return nil, fmt.Errorf("no computer-use backend available: %w", err)
		}
		b = hb
	}

	m.mu.Lock()
	m.backend = b
	m.mu.Unlock()

	s := newSession(m, b)
	s.SetTierOverrides(m.TierOverrides())

	// Seed the grant flags from config. Each is an explicit, persistent toggle
	// the user set in settings — that toggle *is* the approval, and there is no
	// second one.
	//
	// This was `_ = cfg` with a comment claiming the session "starts with none
	// until an approved request turns them on". Nothing ever turned them on:
	// Grant is only reached from Open, which passes all three false because an
	// app grant is not a clipboard grant. So every flag was permanently off and
	// the settings toggles were decorative. Found by adversarial review.
	//
	// They are seeded here rather than through Grant so the per-app path keeps
	// its property: approving "control Notes" still grants exactly Notes.
	cfg := m.GetConfig()
	s.Grant(nil, cfg.ClipboardRead, cfg.ClipboardWrite, cfg.SystemKeyCombos)
	return s, nil
}

// Status is what the settings UI needs to tell the user which gate is shut.
//
// Three independent things must be true before computer use works: it must be
// enabled, a backend must exist, and macOS must have granted permission. When it
// looks broken it is almost always one of those, and a UI that cannot say which
// one is the difference between a two-second fix and an abandoned feature. So
// each gate reports separately, and Blocker names the first one that is shut.
type Status struct {
	Enabled     bool   `json:"enabled"`
	Backend     string `json:"backend"`
	BackendKind string `json:"backend_kind"`
	// Available is true when a backend can actually serve a session.
	Available bool `json:"available"`
	// Blocker names the first shut gate: "disabled", "no_helper",
	// "permissions", or "" when nothing is blocking.
	Blocker string `json:"blocker"`
	// Detail is a human-readable explanation of Blocker.
	Detail   string `json:"detail,omitempty"`
	MaxBatch int    `json:"max_batch"`
	// Tiers exposes the built-in tier table for the apps the UI has rows for, so
	// the settings page never has to reimplement the rules.
	Tiers map[string]string `json:"tiers,omitempty"`
	// Grant flags, so the settings switches can render their real state rather
	// than guessing. Without these the UI shows them off on mount even when
	// config has them on, and the next save silently revokes them.
	ClipboardRead   bool `json:"clipboard_read"`
	ClipboardWrite  bool `json:"clipboard_write"`
	SystemKeyCombos bool `json:"system_key_combos"`
	// Helper reports installation/connection separately from TCC. A binary on
	// disk is not a ready backend: Settings actively handshakes before Connected
	// becomes true.
	Helper                    HelperStatus    `json:"helper"`
	AccessibilityPermission   PermissionState `json:"accessibility"`
	ScreenRecordingPermission PermissionState `json:"screen_recording"`
}

type HelperStatus struct {
	Installed bool   `json:"installed"`
	Connected bool   `json:"connected"`
	Version   string `json:"version,omitempty"`
}

const statusProbeTimeout = 3 * time.Second

// Status reports the current state without opening a session.
func (m *Manager) Status(ctx context.Context) Status {
	m.mu.Lock()
	cfg := cloneConfig(m.cfg)
	fake := m.fake
	helper := m.helper
	m.mu.Unlock()

	st := Status{
		Enabled:                   cfg.Enabled,
		Backend:                   "helper",
		MaxBatch:                  effectiveMaxBatch(cfg.MaxActionsPerBatch),
		ClipboardRead:             cfg.ClipboardRead,
		ClipboardWrite:            cfg.ClipboardWrite,
		SystemKeyCombos:           cfg.SystemKeyCombos,
		AccessibilityPermission:   PermissionUnknown,
		ScreenRecordingPermission: PermissionUnknown,
	}
	st.Tiers = map[string]string{}
	for _, p := range cfg.AppPermissions {
		st.Tiers[p.BundleID] = DefaultTier(p.BundleID).String()
	}
	st.Helper.Installed = helper != nil || (runtime.GOOS == "darwin" && helperBinPath() != "")
	if helper != nil {
		st.Helper.Connected = true
		st.Helper.Version = helperVersion(helper)
		perms := helper.PermissionStatus()
		st.AccessibilityPermission = perms.Accessibility
		st.ScreenRecordingPermission = perms.ScreenRecording
	}

	switch {
	case !cfg.Enabled:
		st.Blocker = "disabled"
		st.Detail = "Computer use is off. It is opt-in because it can reach any app on this machine."
	case fake != nil:
		st.Available = true
		st.BackendKind = "fake"
		st.AccessibilityPermission = PermissionGranted
		st.ScreenRecordingPermission = PermissionGranted
		st.Detail = "A scripted backend is installed — this is a test rig, not real screen control."
	default:
		m.populateHelperStatus(ctx, helper, &st)
	}
	return st
}

func effectiveMaxBatch(value int) int {
	if value <= 0 {
		return defaultMaxBatch
	}
	return value
}

func helperVersion(h *helperBackend) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.helperVersion
}

// populateHelperStatus actively connects to an installed helper and refreshes
// both permission probes. Merely finding a binary is never enough to report
// ready: a stale/incompatible daemon and missing TCC grants are distinct blockers
// the user must be able to act on from Settings.
func (m *Manager) populateHelperStatus(ctx context.Context, helper *helperBackend, st *Status) {
	if helper == nil {
		if !st.Helper.Installed {
			st.Blocker = "no_helper"
			if runtime.GOOS != "darwin" {
				st.Detail = "Computer use is supported on macOS only."
			} else {
				st.Detail = "The native computer-use helper is not installed."
			}
			return
		}
		probeCtx, cancel := context.WithTimeout(ctx, statusProbeTimeout)
		var err error
		helper, err = m.getHelper(probeCtx)
		cancel()
		if err != nil {
			st.Blocker = "no_helper"
			st.Detail = "The native computer-use helper could not be started or contacted: " + err.Error()
			return
		}
	}

	st.BackendKind = "helper"
	st.Helper.Installed = true
	st.Helper.Connected = true
	st.Helper.Version = helperVersion(helper)
	probeCtx, cancel := context.WithTimeout(ctx, statusProbeTimeout)
	permissions, err := helper.RefreshPermissionStatus(probeCtx)
	cancel()
	if err != nil {
		st.Helper.Connected = false
		st.Blocker = "no_helper"
		st.Detail = "The native computer-use helper stopped responding: " + err.Error()
		st.AccessibilityPermission = PermissionUnknown
		st.ScreenRecordingPermission = PermissionUnknown
		return
	}
	st.AccessibilityPermission = permissions.Accessibility
	st.ScreenRecordingPermission = permissions.ScreenRecording
	if permissions.Accessibility == PermissionUnknown || permissions.ScreenRecording == PermissionUnknown {
		st.Blocker = "no_helper"
		st.Detail = "The helper could not verify macOS permissions. Update or reinstall jcode, then check again."
		return
	}
	if permissions.Accessibility != PermissionGranted || permissions.ScreenRecording != PermissionGranted {
		st.Blocker = "permissions"
		st.Detail = "Computer use needs both Accessibility and Screen Recording permission in macOS System Settings."
		return
	}
	st.Available = true
	st.Detail = "The native computer-use helper is connected and both macOS permissions are granted."
}

// SaveScreenshot writes a PNG and returns its opaque id.
func (m *Manager) SaveScreenshot(png []byte) (string, error) {
	if len(png) == 0 || int64(len(png)) > MaxScreenshotBytes {
		return "", fmt.Errorf("screenshot is %d bytes; expected 1..%d", len(png), MaxScreenshotBytes)
	}
	// Native helper handoff PNGs live in a separate process-instance directory.
	// shotMu serializes this Manager; writeScreenshotToStore adds the advisory
	// file lock shared by every jcode process using this public cache.
	m.shotMu.Lock()
	defer m.shotMu.Unlock()
	id := uuid.NewString()
	// The textual tool result keeps this opaque reference after its Base64 image
	// has been consumed. Bound the private backing store on every write while
	// protecting the just-created file needed by the next model/UI request.
	if err := writeScreenshotToStore(
		m.shotDir, id+".png", png, time.Now(), defaultScreenshotStorePolicy,
	); err != nil {
		return "", fmt.Errorf("save computer screenshot: %w", err)
	}
	return id, nil
}

func (m *Manager) sweepScreenshotStore(now time.Time) error {
	m.shotMu.Lock()
	defer m.shotMu.Unlock()
	return pruneScreenshotStore(m.shotDir, "", now, defaultScreenshotStorePolicy)
}

// OpenScreenshot validates and opens an immutable screenshot while holding the
// cross-process store lock. Returning the already-open file closes the old
// validate-path-then-ReadFile race: later pruning may unlink its name, but it
// cannot change the bytes referenced by this handle.
func (m *Manager) OpenScreenshot(id string) (*os.File, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid screenshot id")
	}
	m.shotMu.Lock()
	defer m.shotMu.Unlock()
	return openScreenshotFromStore(
		m.shotDir, u.String()+".png", time.Now(), defaultScreenshotStorePolicy,
	)
}

// Close tears down the backend.
func (m *Manager) Close() error {
	// A long-running process may have crossed the TTL without another save.
	// Sweep before shutdown; a later process startup performs the same pass if
	// this process is killed and cannot close cleanly.
	_ = m.sweepScreenshotStore(time.Now())
	m.uiMu.Lock()
	defer m.uiMu.Unlock()
	m.mu.Lock()
	m.closed = true
	if m.helperInit != nil {
		m.helperInit.cancel()
	}
	b := m.backend
	h := m.helper
	m.backend = nil
	m.helper = nil
	m.mu.Unlock()
	// helper and backend may be the same object; close each at most once.
	if h != nil {
		err := h.Close()
		if b == Backend(h) {
			return err
		}
		if b != nil {
			_ = b.Close()
		}
		return err
	}
	if b != nil {
		return b.Close()
	}
	return nil
}
