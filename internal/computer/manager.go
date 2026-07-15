package computer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	Enabled            bool
	Backend            string // auto|helper|osa|fake
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
	mu      sync.Mutex
	cfg     Config
	shotDir string

	backend Backend
	// fake is injected by tests and by the agent-eval harness (backend=fake).
	fake Backend
}

// NewManager creates the process-wide manager.
func NewManager(cfg Config, home string) *Manager {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return &Manager{
		cfg:     cfg,
		shotDir: filepath.Join(home, ".jcode", "computer", "shots"),
	}
}

// SetConfig hot-swaps the configuration (the settings endpoint calls this, so
// no restart is needed).
func (m *Manager) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}

// GetConfig returns the current configuration.
func (m *Manager) GetConfig() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
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
	if m.cfg.MaxActionsPerBatch <= 0 {
		return defaultMaxBatch
	}
	return m.cfg.MaxActionsPerBatch
}

// SetFakeBackend installs a scripted backend. Used by tests and by the
// agent-eval harness when config pins backend=fake.
func (m *Manager) SetFakeBackend(b Backend) {
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
	m.mu.Lock()
	perms := append([]AppPermission(nil), m.cfg.AppPermissions...)
	m.mu.Unlock()

	out := map[string]Tier{}
	for _, p := range perms {
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

// OpenSession returns a task-scoped Session bound to a Backend.
//
// Backend selection mirrors browser-use's auto rule (extension-if-connected,
// else managed): auto → helper if the daemon answers, else osa.
func (m *Manager) OpenSession(ctx context.Context) (*Session, error) {
	m.mu.Lock()
	if !m.cfg.Enabled {
		m.mu.Unlock()
		return nil, fmt.Errorf("computer use is disabled; enable it in settings")
	}
	kind := m.cfg.Backend
	fake := m.fake
	m.mu.Unlock()

	if kind == "" {
		kind = "auto"
	}

	var b Backend
	switch kind {
	case "fake":
		if fake == nil {
			return nil, fmt.Errorf("backend=fake but no fake backend is installed")
		}
		b = fake
	case "helper":
		return nil, fmt.Errorf("the helper backend is not implemented yet; see internal-doc/computer-use-design.md §2.2")
	case "osa":
		return nil, fmt.Errorf("the osascript backend is not implemented yet; see internal-doc/computer-use-design.md §10.1")
	case "auto":
		if fake != nil {
			b = fake
			break
		}
		return nil, fmt.Errorf("no computer-use backend is available on this machine yet " +
			"(the helper daemon is not implemented; see internal-doc/computer-use-design.md §2.1)")
	default:
		return nil, fmt.Errorf("unknown computer backend %q (want auto, helper, osa or fake)", kind)
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
	// Blocker names the first shut gate: "disabled", "no_backend",
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
}

// Status reports the current state without opening a session.
func (m *Manager) Status(_ context.Context) Status {
	m.mu.Lock()
	cfg := m.cfg
	fake := m.fake
	m.mu.Unlock()

	st := Status{
		Enabled:         cfg.Enabled,
		Backend:         cfg.Backend,
		MaxBatch:        m.MaxBatch(),
		ClipboardRead:   cfg.ClipboardRead,
		ClipboardWrite:  cfg.ClipboardWrite,
		SystemKeyCombos: cfg.SystemKeyCombos,
	}
	if st.Backend == "" {
		st.Backend = "auto"
	}
	switch {
	case !cfg.Enabled:
		st.Blocker = "disabled"
		st.Detail = "Computer use is off. It is opt-in because it can reach any app on this machine."
	case fake != nil && (st.Backend == "fake" || st.Backend == "auto"):
		st.Available = true
		st.BackendKind = "fake"
		st.Detail = "A scripted backend is installed — this is a test rig, not real screen control."
	default:
		// The helper daemon is the shipping path and does not exist yet; say so
		// plainly rather than failing at the first tool call.
		st.Blocker = "no_backend"
		st.Detail = "No computer-use backend is available on this machine yet. " +
			"The helper daemon is not implemented; see internal-doc/computer-use-design.md §2.1."
	}

	// Report the tier for every app the user has a config row for, plus the
	// built-in families, so the UI can render badges without duplicating rules.
	st.Tiers = map[string]string{}
	for _, p := range cfg.AppPermissions {
		st.Tiers[p.BundleID] = DefaultTier(p.BundleID).String()
	}
	return st
}

// SaveScreenshot writes a PNG and returns its opaque id.
func (m *Manager) SaveScreenshot(png []byte) (string, error) {
	if err := os.MkdirAll(m.shotDir, 0o700); err != nil {
		return "", err
	}
	id := uuid.NewString()
	if err := os.WriteFile(filepath.Join(m.shotDir, id+".png"), png, 0o600); err != nil {
		return "", err
	}
	return id, nil
}

// ScreenshotPath resolves an id to a path. The id is re-parsed as a uuid before
// it touches the filesystem, so a crafted id cannot traverse out of shotDir.
// (browser/manager.go:171 does the same, for the same reason.)
func (m *Manager) ScreenshotPath(id string) (string, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return "", fmt.Errorf("invalid screenshot id")
	}
	return filepath.Join(m.shotDir, u.String()+".png"), nil
}

// Close tears down the backend.
func (m *Manager) Close() error {
	m.mu.Lock()
	b := m.backend
	m.backend = nil
	m.mu.Unlock()
	if b != nil {
		return b.Close()
	}
	return nil
}
