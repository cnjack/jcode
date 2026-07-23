// cloud_login.go implements the in-app device-code login (M11-W1): the web UI
// starts the RFC 8628 flow with POST /api/cloud/login, renders the user_code
// + verification URI (QR), and polls GET /api/cloud/login/status while the
// flow's goroutine polls the orchestrator. On success the credentials are
// written, config.cloud is enabled, and the supervisor starts the relay
// connector — no CLI involved. Logout revokes and clears credentials via the
// shared cloud.Logout (same code path as `jcode logout`).
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

// Login flow states served by GET /api/cloud/login/status.
const (
	loginIdle    = "idle"
	loginPending = "pending"
	loginSuccess = "success"
	loginError   = "error"
	loginExpired = "expired"
)

// cloudLoginStartResponse is the answer of POST /api/cloud/login.
type cloudLoginStartResponse struct {
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresAt       string `json:"expires_at"` // RFC 3339
}

// cloudLoginStatusResponse is the answer of GET /api/cloud/login/status.
type cloudLoginStatusResponse struct {
	State           string `json:"state"`
	UserCode        string `json:"user_code,omitempty"`
	VerificationURI string `json:"verification_uri,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	Error           string `json:"error,omitempty"`
}

// cloudLoginFlow is the device-code login state machine. Exactly one flow may
// be pending at a time; a repeated Start while pending returns the in-flight
// flow's user_code/URI instead of starting a second one.
type cloudLoginFlow struct {
	version string
	// afterLogin runs once after a successful login persists credentials and
	// config (wired to the supervisor's SyncCredentials). May be nil.
	afterLogin func()

	// Test hooks (zero values in production): newClient overrides the
	// orchestrator client factory, pollInterval shrinks the token poll cadence.
	newClient    func(baseURL string) *cloud.Client
	pollInterval time.Duration

	mu              sync.Mutex
	state           string
	cloudURL        string
	userCode        string
	verificationURI string
	expiresAt       time.Time
	err             string
}

func (f *cloudLoginFlow) client(baseURL string) *cloud.Client {
	if f.newClient != nil {
		return f.newClient(baseURL)
	}
	return cloud.NewClient(baseURL)
}

// status snapshots the flow state (idle before any Start).
func (f *cloudLoginFlow) status() cloudLoginStatusResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statusLocked()
}

func (f *cloudLoginFlow) statusLocked() cloudLoginStatusResponse {
	resp := cloudLoginStatusResponse{State: f.state}
	if resp.State == "" {
		resp.State = loginIdle
	}
	if f.state == loginPending {
		resp.UserCode = f.userCode
		resp.VerificationURI = f.verificationURI
		resp.ExpiresAt = f.expiresAt.UTC().Format(time.RFC3339)
	}
	if f.state == loginError {
		resp.Error = f.err
	}
	return resp
}

// reset returns the flow to idle (called on logout).
func (f *cloudLoginFlow) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = loginIdle
	f.cloudURL, f.userCode, f.verificationURI, f.err = "", "", "", ""
	f.expiresAt = time.Time{}
}

// start begins a device-code flow against rawURL (already validated by the
// caller or defaulted) and returns the user-facing code/URI. A pending flow
// is returned as-is; a finished one (success/error/expired) is replaced.
// parentCtx scopes the background polling goroutine (web server lifetime).
func (f *cloudLoginFlow) start(parentCtx context.Context, rawURL string) (cloudLoginStartResponse, error) {
	baseURL, err := cloud.ValidateCloudURL(rawURL)
	if err != nil {
		return cloudLoginStartResponse{}, err
	}

	f.mu.Lock()
	if f.state == loginPending {
		resp := cloudLoginStartResponse{
			UserCode:        f.userCode,
			VerificationURI: f.verificationURI,
			ExpiresAt:       f.expiresAt.UTC().Format(time.RFC3339),
		}
		f.mu.Unlock()
		return resp, nil
	}
	f.mu.Unlock()

	client := f.client(baseURL)
	dc, err := client.RequestDeviceCode(parentCtx, "jcode web "+f.version)
	if err != nil {
		return cloudLoginStartResponse{}, err
	}

	expiresIn := time.Duration(dc.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 10 * time.Minute
	}
	expiresAt := time.Now().Add(expiresIn)
	interval := time.Duration(dc.Interval) * time.Second
	if f.pollInterval > 0 {
		interval = f.pollInterval
	}

	f.mu.Lock()
	f.state = loginPending
	f.cloudURL = baseURL
	f.userCode = dc.UserCode
	f.verificationURI = dc.VerificationURI
	f.expiresAt = expiresAt
	f.err = ""
	f.mu.Unlock()

	go f.pollAndFinish(parentCtx, client, baseURL, dc.DeviceCode, interval, expiresIn)
	return cloudLoginStartResponse{
		UserCode:        dc.UserCode,
		VerificationURI: dc.VerificationURI,
		ExpiresAt:       expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

// pollAndFinish polls the token endpoint until the user authorizes, then
// persists the device identity (same steps as `jcode login`): identity key
// pair → device register → credentials file → config.cloud.
func (f *cloudLoginFlow) pollAndFinish(ctx context.Context, client *cloud.Client, baseURL, deviceCode string, interval, expiresIn time.Duration) {
	// M16: the machine fingerprint hash rides the poll (login dedup) and the
	// register call; the source is persisted into cloud.json.
	existing, err := cloud.LoadCredentials()
	if err != nil {
		f.fail(err)
		return
	}
	fpSource := ""
	if existing != nil && existing.CloudURL == baseURL {
		fpSource = existing.Fingerprint
	}
	if fpSource == "" {
		fpSource, err = cloud.ResolveFingerprintSource()
		if err != nil {
			f.fail(fmt.Errorf("failed to resolve the machine fingerprint: %w", err))
			return
		}
	}
	fpHash := cloud.FingerprintHash(fpSource)

	tok, err := client.PollForToken(ctx, deviceCode, fpHash, interval, expiresIn)
	if err != nil {
		f.fail(err)
		return
	}

	pubKey, privKey := "", ""
	if existing != nil && existing.CloudURL == baseURL {
		pubKey, privKey = existing.PublicKey, existing.PrivateKey
	}
	if pubKey == "" || privKey == "" {
		pubKey, privKey, err = cloud.GenerateIdentityKeyPair()
		if err != nil {
			f.fail(err)
			return
		}
	}
	hostname, _ := os.Hostname()
	name := hostname
	if name == "" {
		name = "jcode-device"
	}
	if err := client.RegisterDevice(ctx, tok.AccessToken, cloud.RegisterDeviceRequest{
		Name:         name,
		Hostname:     hostname,
		JcodeVersion: f.version,
		PubKey:       pubKey,
		Fingerprint:  fpHash,
	}); err != nil {
		f.fail(err)
		return
	}
	creds := &cloud.Credentials{
		CloudURL:    baseURL,
		DeviceID:    tok.DeviceID,
		DeviceToken: tok.AccessToken,
		DeviceName:  name,
		PublicKey:   pubKey,
		PrivateKey:  privKey,
		KeyGen:      1,
		Fingerprint: fpSource,
	}
	if existing != nil && existing.CloudURL == baseURL {
		creds.CEK = existing.CEK
		creds.ASK = existing.ASK
		creds.ASKKeyGen = existing.ASKKeyGen
		if existing.KeyGen > 0 {
			creds.KeyGen = existing.KeyGen
		}
	}
	if err := cloud.SaveCredentials(creds); err != nil {
		f.fail(err)
		return
	}
	if err := cloud.UpdateConfigCloud(baseURL, true); err != nil {
		config.Logger().Printf("[cloud] login: failed to update %s: %v", config.ConfigPath(), err)
	}

	f.mu.Lock()
	f.state = loginSuccess
	f.userCode, f.verificationURI = "", ""
	f.expiresAt = time.Time{}
	f.mu.Unlock()
	if f.afterLogin != nil {
		f.afterLogin()
	}
}

// fail lands the flow in its terminal error state: expired for an expired
// device code (UI offers retry), error otherwise (denied or any failure).
func (f *cloudLoginFlow) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if errors.Is(err, cloud.ErrDeviceCodeExpired) {
		f.state = loginExpired
	} else {
		f.state = loginError
	}
	f.err = err.Error()
	f.userCode, f.verificationURI = "", ""
	f.expiresAt = time.Time{}
}

// loginFlow returns the server's flow, constructing it on first use. The
// afterLogin hook is bound late so the supervisor starts the connector right
// after a successful web login.
func (s *Server) loginFlow() *cloudLoginFlow {
	s.cloudLoginMu.Lock()
	defer s.cloudLoginMu.Unlock()
	if s.cloudLogin == nil {
		s.cloudLogin = &cloudLoginFlow{
			version: s.version,
			afterLogin: func() {
				if s.cloudSupervisor != nil {
					s.cloudSupervisor.SyncCredentials()
				}
			},
		}
	}
	return s.cloudLogin
}

// handleCloudLogin serves POST /api/cloud/login: starts (or re-joins) the
// device-code flow. The optional body {"cloud_url": "..."} selects the
// orchestrator; empty falls back to config.cloud.url, then the public
// default. Already-logged-in devices get a 409.
func (s *Server) handleCloudLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CloudURL string `json:"cloud_url"`
	}
	if r.Body != nil {
		decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
		if err := decoder.Decode(&req); err != nil && err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid login request: " + err.Error()})
			return
		}
	}

	if creds, err := cloud.LoadCredentials(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	} else if creds != nil && creds.DeviceToken != "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already logged in — log out first to sign in again"})
		return
	}

	rawURL := req.CloudURL
	if rawURL == "" && s.cfg != nil {
		rawURL = s.cfg.CloudSettings().URL
	}
	if rawURL == "" {
		rawURL = cloud.DefaultCloudURL
	}

	parent := s.rootCtx()
	if parent == nil {
		parent = context.Background()
	}
	resp, err := s.loginFlow().start(parent, rawURL)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to start login: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleCloudLoginStatus serves GET /api/cloud/login/status.
func (s *Server) handleCloudLoginStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.loginFlow().status())
}

// handleCloudLogout serves POST /api/cloud/logout: revoke + clear credentials
// (shared cloud.Logout), stop the connector, reset the login flow, and return
// the fresh status so the badge updates in one round trip.
func (s *Server) handleCloudLogout(w http.ResponseWriter, r *http.Request) {
	if err := cloud.Logout(r.Context(), func(format string, args ...any) {
		config.Logger().Printf("[cloud] "+format, args...)
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if s.cloudSupervisor != nil {
		s.cloudSupervisor.SyncCredentials()
	}
	s.loginFlow().reset()
	writeJSON(w, http.StatusOK, s.cloudStatus())
}

// handleCloudForget serves POST /api/cloud/forget: revoke the token and erase
// the complete local device identity. Other clients will need to pair again.
func (s *Server) handleCloudForget(w http.ResponseWriter, r *http.Request) {
	if err := cloud.Forget(r.Context(), func(format string, args ...any) {
		config.Logger().Printf("[cloud] "+format, args...)
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if s.cloudSupervisor != nil {
		s.cloudSupervisor.SyncCredentials()
	}
	s.loginFlow().reset()
	writeJSON(w, http.StatusOK, s.cloudStatus())
}
