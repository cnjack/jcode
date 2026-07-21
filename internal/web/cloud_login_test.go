package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
)

// loginMockState counts orchestrator calls made by the login flow.
type loginMockState struct {
	codeCalls     int32
	pollCalls     int32
	registerCalls int32
	revokeCalls   int32
}

// newLoginServer builds a mock orchestrator with the device-auth endpoints;
// onToken decides each token poll's HTTP status and body.
func newLoginServer(t *testing.T, onToken func(poll int32) (int, string)) (*httptest.Server, *loginMockState) {
	t.Helper()
	st := &loginMockState{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/device/code", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&st.codeCalls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dc-1",
			"user_code":        "ABCD-EFGH",
			"verification_uri": "https://cloud.example.com/device",
			"expires_in":       300,
			"interval":         5,
		})
	})
	mux.HandleFunc("POST /auth/device/token", func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&st.pollCalls, 1)
		status, body := onToken(n)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("POST /internal/v1/device/register", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&st.registerCalls, 1)
		if r.Header.Get("Authorization") != "Bearer dev-token" {
			t.Errorf("register: Authorization = %q, want the device token", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("POST /internal/v1/device/revoke", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&st.revokeCalls, 1)
		_, _ = w.Write([]byte(`{}`))
	})
	return httptest.NewServer(mux), st
}

const (
	tokenPending = `{"error":{"code":"authorization_pending","message":"pending"}}`
	tokenDenied  = `{"error":{"code":"access_denied","message":"user denied the code"}}`
	tokenExpired = `{"error":{"code":"expired_token","message":"code expired"}}`
	tokenSuccess = `{"access_token":"dev-token","token_type":"bearer","device_id":"dev-1"}`
)

// newCloudLoginTestServer returns a Server whose login flow polls fast.
func newCloudLoginTestServer(t *testing.T, sup CloudSupervisor) *Server {
	t.Helper()
	s := &Server{cfg: &config.Config{}, version: "test", cloudSupervisor: sup}
	s.loginFlow().pollInterval = time.Millisecond
	return s
}

func postCloudLogin(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleCloudLogin(rec, httptest.NewRequest(http.MethodPost, "/api/cloud/login", strings.NewReader(body)))
	return rec
}

func getCloudLoginStatus(t *testing.T, s *Server) cloudLoginStatusResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleCloudLoginStatus(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/login/status", nil))
	var st cloudLoginStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func waitLoginState(t *testing.T, s *Server, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := s.loginFlow().status().State; got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("login state = %q, want %q", s.loginFlow().status().State, want)
}

// Full flow: code → pending (poll blocked at the mock) → success writes
// credentials + config and kicks the supervisor; repeat POST re-joins the
// pending flow; logout revokes and clears everything.
func TestCloudLoginFlowSuccessAndLogout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	release := make(chan struct{})
	srv, st := newLoginServer(t, func(int32) (int, string) {
		<-release // hold every poll until the test releases the gate
		return http.StatusOK, tokenSuccess
	})
	defer srv.Close()
	fake := &fakeCloudSupervisor{}
	s := newCloudLoginTestServer(t, fake)

	rec := postCloudLogin(t, s, `{"cloud_url":"`+srv.URL+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST login: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var start cloudLoginStartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	if start.UserCode != "ABCD-EFGH" || start.VerificationURI == "" || start.ExpiresAt == "" {
		t.Fatalf("start response = %+v", start)
	}

	// The poll goroutine is blocked at the mock: status is deterministically
	// pending and carries the user-facing code.
	if got := getCloudLoginStatus(t, s); got.State != loginPending || got.UserCode != "ABCD-EFGH" {
		t.Fatalf("status = %+v, want pending with the user_code", got)
	}
	// A repeated POST re-joins the in-flight flow instead of starting over.
	rec = postCloudLogin(t, s, `{"cloud_url":"`+srv.URL+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat POST login: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var again cloudLoginStartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &again); err != nil {
		t.Fatal(err)
	}
	if again.UserCode != start.UserCode || atomic.LoadInt32(&st.codeCalls) != 1 {
		t.Fatalf("repeat POST started a new flow: %+v, code calls=%d", again, st.codeCalls)
	}

	close(release)
	waitLoginState(t, s, loginSuccess)

	creds, err := cloud.LoadCredentials()
	if err != nil || creds == nil {
		t.Fatalf("credentials after login: %v, %v", creds, err)
	}
	if creds.DeviceID != "dev-1" || creds.DeviceToken != "dev-token" || creds.CloudURL != srv.URL ||
		creds.PublicKey == "" || creds.PrivateKey == "" || creds.KeyGen != 1 {
		t.Fatalf("credentials = %+v", creds)
	}
	if atomic.LoadInt32(&st.registerCalls) != 1 {
		t.Fatalf("register calls = %d, want 1", st.registerCalls)
	}
	persisted := readPersistedCloud(t)
	if persisted == nil || !persisted.Enabled || persisted.URL != srv.URL {
		t.Fatalf("persisted config.cloud = %+v, want enabled with url %s", persisted, srv.URL)
	}
	if fake.syncCalls != 1 {
		t.Fatalf("supervisor sync calls = %d, want 1 (connector kick after login)", fake.syncCalls)
	}
	if got := getCloudLoginStatus(t, s); got.State != loginSuccess || got.UserCode != "" {
		t.Fatalf("status = %+v, want success without user_code", got)
	}

	// Logged in now: a fresh login POST conflicts.
	if rec := postCloudLogin(t, s, `{}`); rec.Code != http.StatusConflict {
		t.Fatalf("POST login while logged in: status=%d, want 409", rec.Code)
	}

	// Logout: revoke + clear + connector stop + flow reset, fresh status back.
	rec = httptest.NewRecorder()
	s.handleCloudLogout(rec, httptest.NewRequest(http.MethodPost, "/api/cloud/logout", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST logout: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var st2 cloudStatusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &st2); err != nil {
		t.Fatal(err)
	}
	if st2.LoggedIn {
		t.Fatalf("status after logout = %+v, want logged_in=false", st2)
	}
	if creds, _ := cloud.LoadCredentials(); creds != nil {
		t.Fatalf("credentials after logout = %+v, want nil", creds)
	}
	if atomic.LoadInt32(&st.revokeCalls) != 1 {
		t.Fatalf("revoke calls = %d, want 1", st.revokeCalls)
	}
	if fake.syncCalls != 2 {
		t.Fatalf("supervisor sync calls = %d, want 2 (login + logout)", fake.syncCalls)
	}
	if persisted := readPersistedCloud(t); persisted == nil || persisted.Enabled {
		t.Fatalf("persisted config.cloud after logout = %+v, want enabled=false", persisted)
	}
	if got := getCloudLoginStatus(t, s); got.State != loginIdle {
		t.Fatalf("login state after logout = %q, want idle", got.State)
	}
}

// Denial lands the flow in the error state; a later POST retries cleanly.
func TestCloudLoginDeniedThenRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var mode int32 // 0 = deny, 1 = succeed
	srv, _ := newLoginServer(t, func(int32) (int, string) {
		if atomic.LoadInt32(&mode) == 0 {
			return http.StatusBadRequest, tokenDenied
		}
		return http.StatusOK, tokenSuccess
	})
	defer srv.Close()
	s := newCloudLoginTestServer(t, nil)

	if rec := postCloudLogin(t, s, `{"cloud_url":"`+srv.URL+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST login: status=%d body=%s", rec.Code, rec.Body.String())
	}
	waitLoginState(t, s, loginError)
	if got := getCloudLoginStatus(t, s); !strings.Contains(got.Error, "denied") {
		t.Fatalf("status = %+v, want the denial message", got)
	}
	if creds, _ := cloud.LoadCredentials(); creds != nil {
		t.Fatalf("credentials = %+v, want nil after denial", creds)
	}

	atomic.StoreInt32(&mode, 1)
	if rec := postCloudLogin(t, s, `{"cloud_url":"`+srv.URL+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("retry POST login: status=%d body=%s", rec.Code, rec.Body.String())
	}
	waitLoginState(t, s, loginSuccess)
}

// An expired device code lands in the expired state (UI offers retry).
func TestCloudLoginExpired(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, _ := newLoginServer(t, func(int32) (int, string) {
		return http.StatusBadRequest, tokenExpired
	})
	defer srv.Close()
	s := newCloudLoginTestServer(t, nil)

	if rec := postCloudLogin(t, s, `{"cloud_url":"`+srv.URL+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST login: status=%d body=%s", rec.Code, rec.Body.String())
	}
	waitLoginState(t, s, loginExpired)
	if creds, _ := cloud.LoadCredentials(); creds != nil {
		t.Fatalf("credentials = %+v, want nil after expiry", creds)
	}
}

// Validation: bad JSON → 400, an unreachable/invalid cloud URL → 502, and
// polling stays pending while the user has not authorized.
func TestCloudLoginValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newCloudLoginTestServer(t, nil)

	if rec := postCloudLogin(t, s, `{`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: status=%d, want 400", rec.Code)
	}
	// Plain http on a non-localhost host violates the scheme policy.
	if rec := postCloudLogin(t, s, `{"cloud_url":"http://example.com"}`); rec.Code != http.StatusBadGateway {
		t.Fatalf("invalid cloud url: status=%d, want 502", rec.Code)
	}
	// Never-authorized flow: status stays pending (the poll goroutine exits
	// with an error once the mock server closes at test end).
	srv, _ := newLoginServer(t, func(int32) (int, string) {
		return http.StatusBadRequest, tokenPending
	})
	defer srv.Close()
	if rec := postCloudLogin(t, s, `{"cloud_url":"`+srv.URL+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST login: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := getCloudLoginStatus(t, s); got.State != loginPending {
		t.Fatalf("status = %+v, want pending", got)
	}
}

// Logout without credentials is a 200 no-op.
func TestCloudLogoutNotLoggedIn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newCloudLoginTestServer(t, nil)
	rec := httptest.NewRecorder()
	s.handleCloudLogout(rec, httptest.NewRequest(http.MethodPost, "/api/cloud/logout", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST logout: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var st cloudStatusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.LoggedIn {
		t.Fatalf("status = %+v, want logged_in=false", st)
	}
}

type persistedCloud struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

func readPersistedCloud(t *testing.T) *persistedCloud {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".jcode", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk struct {
		Cloud *persistedCloud `json:"cloud"`
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	return disk.Cloud
}
