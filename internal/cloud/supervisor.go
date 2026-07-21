// supervisor.go owns the cloud relay connector lifecycle so the relay can be
// stopped/started without a process restart (the settings auto_connect toggle
// hot-applies), and exposes the live Status served at GET /api/cloud/status.
package cloud

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/cnjack/jcode/internal/config"
)

// Status is the JSON snapshot of the cloud relay served by the web API.
type Status struct {
	LoggedIn    bool   `json:"logged_in"`
	AutoConnect bool   `json:"auto_connect"`
	State       string `json:"state"` // "offline" | "connecting" | "online" | "error"
	DeviceName  string `json:"device_name"`
	CloudURL    string `json:"cloud_url"`
	Error       string `json:"error,omitempty"`
}

// Supervisor owns the relay Connector lifecycle: Start launches the connector
// when the device is logged in with cloud.auto_connect enabled, SetAutoConnect
// persists and hot-applies the toggle, and Status reports the live snapshot.
// Like the connector itself it is strictly best-effort — failures are logged
// warnings, never errors that could affect the web server. All methods are
// safe for concurrent use.
type Supervisor struct {
	cfg      *config.Config
	port     int
	webToken string

	// Version is the jcode version reported at device register. It lives in
	// the command package (import cycle), so callers set it before Start.
	Version string

	mu      sync.Mutex
	rootCtx context.Context // web server shutdown context, captured by Start
	creds   *Credentials    // credentials the running connector was started with
	conn    *Connector
	cancel  context.CancelFunc
}

// NewSupervisor returns a Supervisor bound to cfg (mutated live by
// SetAutoConnect), the local web control plane port and its auth token.
func NewSupervisor(cfg *config.Config, port int, webToken string) *Supervisor {
	return &Supervisor{cfg: cfg, port: port, webToken: webToken}
}

// Start applies the current config/credentials: when the device is logged in
// and auto_connect is enabled the connector is launched under a child of ctx
// (web server shutdown tears it down); otherwise the supervisor stays idle.
func (s *Supervisor) Start(ctx context.Context) {
	creds, err := LoadCredentials()
	if err != nil {
		config.Logger().Printf("[cloud] failed to load credentials, relay connector disabled: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rootCtx = ctx
	s.creds = creds
	if ShouldConnect(config.CloudAutoConnect(s.cfg), creds) {
		s.startLocked()
	}
}

// Status snapshots the relay state. Credentials are re-read from disk so a
// login/logout from another process is reflected without a restart.
func (s *Supervisor) Status() Status {
	creds, _ := LoadCredentials()
	st := s.baseStatus(creds)
	if !st.LoggedIn || !st.AutoConnect {
		return st
	}
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return st
	}
	st.State, st.Error = conn.Status()
	return st
}

// baseStatus builds the Status without any live connector state ("offline").
// device_name falls back to the OS hostname; cloud_url follows the resolution
// chain config.cloud.url → credentials URL → DefaultCloudURL.
func (s *Supervisor) baseStatus(creds *Credentials) Status {
	loggedIn := creds != nil && creds.DeviceToken != ""
	deviceName := ""
	if creds != nil {
		deviceName = creds.DeviceName
	}
	if deviceName == "" {
		deviceName, _ = os.Hostname()
	}
	return Status{
		LoggedIn:    loggedIn,
		AutoConnect: config.CloudAutoConnect(s.cfg),
		State:       StateOffline,
		DeviceName:  deviceName,
		CloudURL:    s.cloudURL(creds),
	}
}

// SetAutoConnect persists cloud.auto_connect (preserving the stored
// Enabled/URL/E2EE fields, same read/modify/write pattern as `jcode login`)
// and hot-applies it: enabling starts the connector when credentials are
// valid, disabling stops it. On a SaveConfig failure the in-memory config is
// rolled back and the error returned.
func (s *Supervisor) SetAutoConnect(enabled bool) error {
	previous := s.cfg.CloudSettings()
	s.cfg.SetCloud(&config.CloudConfig{
		Enabled:     previous.Enabled,
		URL:         previous.URL,
		AutoConnect: &enabled,
		E2EE:        previous.E2EE,
		SyncDefault: previous.SyncDefault,
	})
	if err := config.SaveConfig(s.cfg); err != nil {
		if previous == (config.CloudConfig{}) {
			s.cfg.SetCloud(nil)
		} else {
			s.cfg.SetCloud(&previous)
		}
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !enabled {
		s.stopLocked()
		return nil
	}
	if s.rootCtx == nil || s.conn != nil {
		return nil
	}
	// Re-read credentials: a login may have happened after Start.
	creds, err := LoadCredentials()
	if err != nil {
		config.Logger().Printf("[cloud] failed to load credentials, relay connector disabled: %v", err)
		return nil
	}
	s.creds = creds
	if ShouldConnect(true, creds) {
		s.startLocked()
	}
	return nil
}

// BuildConnector is the pure start decision + construction behind Start: nil
// when the connector should not run (not logged in, or auto_connect explicitly
// disabled). Exported so the command package reuses one code path.
func (s *Supervisor) BuildConnector(creds *Credentials) *Connector {
	if !ShouldConnect(config.CloudAutoConnect(s.cfg), creds) {
		return nil
	}
	cloudURL := s.cloudURL(creds)
	config.Logger().Printf("[cloud] starting relay connector (cloud=%s, device=%s)", cloudURL, creds.DeviceID)
	return NewConnector(ConnectorConfig{
		CloudURL:    cloudURL,
		Credentials: creds,
		// The control plane is always this process's own web server on
		// loopback, regardless of the --host bind.
		LocalBase:  fmt.Sprintf("http://127.0.0.1:%d", s.port),
		LocalToken: s.webToken,
		Version:    s.Version,
		// cloud.e2ee=false keeps the M5 plaintext grey path: the connector
		// skips CEK initialization entirely.
		CipherDisabled: !config.CloudE2EE(s.cfg),
	})
}

// cloudURL resolves the orchestrator address: config.cloud.url, then the
// credentials' URL, then the public default.
func (s *Supervisor) cloudURL(creds *Credentials) string {
	u := s.cfg.CloudSettings().URL
	if u == "" && creds != nil {
		u = creds.CloudURL
	}
	if u == "" {
		u = DefaultCloudURL
	}
	return u
}

// startLocked builds and launches the connector. The caller holds s.mu and
// has already applied the ShouldConnect gate.
func (s *Supervisor) startLocked() {
	conn := s.BuildConnector(s.creds)
	if conn == nil {
		return
	}
	runCtx, cancel := context.WithCancel(s.rootCtx)
	s.conn = conn
	s.cancel = cancel
	go conn.Run(runCtx)
}

// stopLocked cancels the running connector (if any). Run returns
// asynchronously; the connector flips its own state to offline on exit.
func (s *Supervisor) stopLocked() {
	if s.cancel != nil {
		s.cancel()
	}
	s.conn = nil
	s.cancel = nil
}

// SyncCredentials re-reads the on-disk credentials after a login/logout (web
// API or another process) and reconciles the connector: logged out (or
// auto_connect off) stops it; fresh or replaced credentials (re)start it.
// Like everything cloud-side it is best-effort — a credentials read failure
// is logged and leaves the current state untouched.
func (s *Supervisor) SyncCredentials() {
	creds, err := LoadCredentials()
	if err != nil {
		config.Logger().Printf("[cloud] failed to load credentials, relay connector state unchanged: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.creds
	s.creds = creds
	if !ShouldConnect(config.CloudAutoConnect(s.cfg), creds) {
		s.stopLocked()
		return
	}
	if s.rootCtx == nil {
		return
	}
	// Restart when the identity changed under a running connector (logout +
	// login as a different device); otherwise start only when idle.
	identityChanged := previous == nil ||
		previous.DeviceID != creds.DeviceID || previous.DeviceToken != creds.DeviceToken
	if identityChanged {
		s.stopLocked()
	}
	if s.conn == nil {
		s.startLocked()
	}
}

// --- pairing inbox delegation (M11-W1 web approval endpoints) ---

// PendingPairings returns the live connector's pending pairing inbox, empty
// when the connector is not running.
func (s *Supervisor) PendingPairings() []PendingPairing {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.PendingPairings()
}

// LastPaired reports the live connector's most recent pairing approval.
func (s *Supervisor) LastPaired() (PairedInfo, bool) {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return PairedInfo{}, false
	}
	return conn.LastPaired()
}

// ApprovePairing approves a pending pairing through the live connector.
func (s *Supervisor) ApprovePairing(ctx context.Context, id string) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("cloud relay is not connected")
	}
	return conn.ApprovePairing(ctx, id)
}

// DenyPairing denies a pending pairing through the live connector.
func (s *Supervisor) DenyPairing(ctx context.Context, id string) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("cloud relay is not connected")
	}
	return conn.DenyPairing(ctx, id)
}
