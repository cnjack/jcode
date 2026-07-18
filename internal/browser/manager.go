package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cnjack/jcode/internal/config"
	"github.com/google/uuid"
)

// Manager is the process-wide owner of browser-use infrastructure: the
// extension bridge, managed-Chrome lifecycle, screenshot store, and the
// resolved config. Tasks obtain a per-task Session from it. One per server.
type Manager struct {
	mu      sync.Mutex
	cfg     Config
	bridge  *Bridge
	managed Backend // shared managed backend (lazy, reused across tasks)
	shotDir string
}

// Config mirrors config.BrowserConfig, decoupled so internal/browser does not
// import a specific config layout beyond what it needs.
type Config struct {
	Enabled    bool
	Backend    string // auto | managed | extension
	ChromePath string
	Headless   bool
	Viewport   string
	DevMode    bool
}

// NewManager creates the manager. shotDir defaults to ~/.jcode/browser/shots.
func NewManager(cfg Config) *Manager {
	shotDir := filepath.Join(config.ConfigDir(), "browser", "shots")
	_ = os.MkdirAll(shotDir, 0o755)
	return &Manager{cfg: cfg, bridge: NewBridge(), shotDir: shotDir}
}

// Bridge exposes the extension bridge for route wiring.
func (m *Manager) Bridge() *Bridge { return m.bridge }

// SetConfig updates the live config (from the settings endpoint).
func (m *Manager) SetConfig(cfg Config) {
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
}

// GetConfig returns a copy of the live config.
func (m *Manager) GetConfig() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Enabled reports whether browser-use tools should be exposed to agents.
func (m *Manager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Enabled
}

// DevMode reports whether high-risk actions (eval / raw CDP) are unlocked.
func (m *Manager) DevMode() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.DevMode
}

// Status describes browser-use availability for the settings UI.
type Status struct {
	Enabled         bool   `json:"enabled"`
	Backend         string `json:"backend"`
	ChromeFound     bool   `json:"chrome_found"`
	ChromePath      string `json:"chrome_path,omitempty"`
	ChromeVersion   string `json:"chrome_version,omitempty"`
	ExtensionOnline bool   `json:"extension_online"`
	DevMode         bool   `json:"dev_mode"`
}

// Status computes the current status.
func (m *Manager) Status(ctx context.Context) Status {
	cfg := m.GetConfig()
	chromePath := FindChrome(cfg.ChromePath)
	st := Status{
		Enabled:         cfg.Enabled,
		Backend:         cfg.Backend,
		ChromeFound:     chromePath != "",
		ChromePath:      chromePath,
		ExtensionOnline: m.bridge.Connected(),
		DevMode:         cfg.DevMode,
	}
	if chromePath != "" {
		st.ChromeVersion = ChromeVersion(ctx, chromePath)
	}
	return st
}

// OpenSession creates a per-task Session, choosing a backend per config:
// "extension" requires the bridge; "managed" launches Chrome; "auto" prefers a
// connected extension, else managed.
func (m *Manager) OpenSession(ctx context.Context) (*Session, error) {
	cfg := m.GetConfig()
	if !cfg.Enabled {
		return nil, fmt.Errorf("browser use is disabled (enable it in settings)")
	}
	backendKind := cfg.Backend
	if backendKind == "" || backendKind == "auto" {
		if m.bridge.Connected() {
			backendKind = "extension"
		} else {
			backendKind = "managed"
		}
	}

	switch backendKind {
	case "extension":
		be, err := m.bridge.Backend()
		if err != nil {
			return nil, err
		}
		return NewSession(be), nil
	case "managed":
		be, err := m.getManaged(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return NewSession(be), nil
	default:
		return nil, fmt.Errorf("unknown backend %q", backendKind)
	}
}

// getManaged lazily launches (and reuses) the managed Chrome. Reuse gives us
// warm-start across tasks; the process is torn down on manager Close. A cached
// backend whose Chrome has since died (crashed, or the user quit the window) is
// dropped and relaunched so browser use recovers without a server restart.
func (m *Manager) getManaged(ctx context.Context, cfg Config) (Backend, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.managed != nil {
		if b, ok := m.managed.(interface{ alive() bool }); !ok || b.alive() {
			return m.managed, nil
		}
		// Cached Chrome is dead: tear down whatever's left and relaunch below.
		_ = m.managed.Close()
		m.managed = nil
	}
	be, err := Launch(ctx, LaunchOptions{
		ChromePath: cfg.ChromePath,
		Headless:   cfg.Headless,
		Viewport:   cfg.Viewport,
	})
	if err != nil {
		return nil, err
	}
	m.managed = be
	return be, nil
}

// SaveScreenshot writes PNG bytes to the shot store and returns its id.
func (m *Manager) SaveScreenshot(png []byte) (string, error) {
	id := uuid.NewString()
	path := filepath.Join(m.shotDir, id+".png")
	if err := os.WriteFile(path, png, 0o644); err != nil {
		return "", err
	}
	return id, nil
}

// ScreenshotPath returns the file path for a shot id (for the HTTP endpoint).
func (m *Manager) ScreenshotPath(id string) string {
	// Guard against path traversal: id must be a bare uuid.
	if _, err := uuid.Parse(id); err != nil {
		return ""
	}
	return filepath.Join(m.shotDir, id+".png")
}

// Close tears down the managed Chrome (if any).
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.managed != nil {
		err := m.managed.Close()
		m.managed = nil
		return err
	}
	return nil
}
