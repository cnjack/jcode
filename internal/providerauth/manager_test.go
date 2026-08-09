package providerauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

type authRoundTripFunc func(*http.Request) (*http.Response, error)

func (function authRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func newTestClock() *testClock {
	return &testClock{now: time.Unix(1_900_000_000, 0).UTC()}
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *testClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

func writeJSON(t *testing.T, writer http.ResponseWriter, status int, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func testEndpoints(serverURL string) Endpoints {
	return Endpoints{
		CodexDeviceStart:   serverURL + "/codex/device",
		CodexDevicePoll:    serverURL + "/codex/poll",
		CodexToken:         serverURL + "/codex/token",
		CodexVerification:  "https://auth.openai.com/codex/device",
		CodexRuntime:       serverURL + "/codex/runtime",
		XAIDiscovery:       serverURL + "/xai/discovery",
		XAIRuntime:         serverURL + "/xai/runtime",
		CopilotDeviceStart: serverURL + "/copilot/device",
		CopilotOAuthToken:  serverURL + "/copilot/oauth",
		CopilotUser:        serverURL + "/github/user",
		CopilotToken:       serverURL + "/github/copilot-token",
		CopilotUsage:       serverURL + "/github/copilot-user",
		CopilotRuntime:     serverURL + "/copilot/fallback",
	}
}

func newTestManager(
	t *testing.T,
	dir string,
	server *httptest.Server,
	clock *testClock,
) *Manager {
	t.Helper()
	manager, err := NewManager(Options{
		ConfigDir:                  dir,
		HTTPClient:                 server.Client(),
		Now:                        clock.Now,
		Endpoints:                  testEndpoints(server.URL),
		AllowInsecureTestEndpoints: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestCodexDeviceFlowPersistsOnlyDurableSecret(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	var polls atomic.Int32
	var refreshes atomic.Int32
	idToken := testJWT(t, map[string]any{
		"chatgpt_account_id": "chatgpt-account-1",
		"email":              "alice@example.com",
	})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/codex/device":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"device_auth_id": "upstream-device-secret",
				"user_code":      "ABCD-EFGH",
				"expires_in":     900,
				"interval":       "1",
			})
		case "/codex/poll":
			if polls.Add(1) == 1 {
				writer.WriteHeader(http.StatusForbidden)
				return
			}
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"authorization_code": "one-time-code",
				"code_verifier":      "server-pkce-verifier",
			})
		case "/codex/token":
			if err := request.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			if request.Form.Get("grant_type") == "refresh_token" {
				refreshes.Add(1)
				writeJSON(t, writer, http.StatusOK, map[string]any{
					"access_token":  "codex-access-2",
					"refresh_token": "codex-refresh-2",
					"expires_in":    3600,
				})
				return
			}
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"access_token":  "codex-access-1",
				"refresh_token": "codex-refresh-1",
				"id_token":      idToken,
				"expires_in":    3600,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	manager := newTestManager(t, dir, server, clock)
	flow, err := manager.Start(context.Background(), MethodCodexOAuth)
	if err != nil {
		t.Fatal(err)
	}
	if flow.State != FlowStatePending || flow.ID == "" || flow.ID == "upstream-device-secret" {
		t.Fatalf("unexpected public flow: %+v", flow)
	}
	encoded, err := json.Marshal(flow)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "upstream-device-secret") {
		t.Fatal("public flow leaked upstream device token")
	}
	if result, err := manager.Poll(context.Background(), MethodCodexOAuth, flow.ID); err != nil ||
		result.State != FlowStatePending {
		t.Fatalf("first poll = %+v, %v", result, err)
	}
	clock.Advance(5 * time.Second)
	result, err := manager.Poll(context.Background(), MethodCodexOAuth, flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != FlowStateAuthorized || result.Account == nil ||
		result.Account.Login != "alice@example.com" {
		t.Fatalf("authorization result = %+v", result)
	}

	storeData, err := os.ReadFile(filepath.Join(dir, storeFileName))
	if err != nil {
		t.Fatal(err)
	}
	stored := string(storeData)
	if !strings.Contains(stored, "codex-refresh-1") {
		t.Fatal("durable refresh token was not stored")
	}
	for _, forbidden := range []string{
		"codex-access-1", "upstream-device-secret", "one-time-code", "server-pkce-verifier",
	} {
		if strings.Contains(stored, forbidden) {
			t.Fatalf("store leaked ephemeral secret %q", forbidden)
		}
	}
	info, err := os.Stat(filepath.Join(dir, storeFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("store mode = %o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %o", dirInfo.Mode().Perm())
	}

	restarted := newTestManager(t, dir, server, clock)
	credential, err := restarted.Credential(context.Background(), Binding{Method: MethodCodexOAuth})
	if err != nil {
		t.Fatal(err)
	}
	if credential.Token != "codex-access-2" || credential.Protocol != ProtocolResponses ||
		credential.BaseURL != server.URL+"/codex/runtime" ||
		credential.Headers["chatgpt-account-id"] != "chatgpt-account-1" ||
		credential.Headers["originator"] == "" || credential.Headers["version"] == "" {
		t.Fatalf("credential = %+v", credential)
	}
	if refreshes.Load() != 1 {
		t.Fatalf("refreshes = %d", refreshes.Load())
	}
	credentialJSON, err := json.Marshal(credential)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(credentialJSON), "codex-access-2") {
		t.Fatal("credential JSON leaked access token")
	}
}

func TestXAIRefreshSingleflightRotationAndRequiresReauth(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	idToken := testJWT(t, map[string]any{"sub": "xai-1", "email": "grok@example.com"})
	var refreshes atomic.Int32
	var rejectRefresh atomic.Bool
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/xai/discovery":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"issuer":                        xaiIssuer,
				"device_authorization_endpoint": serverURL + "/xai/device",
				"token_endpoint":                serverURL + "/xai/token",
			})
		case "/xai/device":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"device_code":               "xai-device-secret",
				"user_code":                 "GROK-CODE",
				"verification_uri":          "https://accounts.x.ai/oauth2/device",
				"verification_uri_complete": "https://accounts.x.ai/oauth2/device?code=GROK-CODE",
				"expires_in":                900,
				"interval":                  1,
			})
		case "/xai/token":
			if err := request.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			if request.Form.Get("grant_type") == "refresh_token" {
				refreshes.Add(1)
				if rejectRefresh.Load() {
					// Invalid refresh credentials are sometimes reported as an
					// empty/non-JSON 400. That still must fail closed as reauth.
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				time.Sleep(10 * time.Millisecond)
				writeJSON(t, writer, http.StatusOK, map[string]any{
					"access_token":  "xai-access-2",
					"refresh_token": "xai-refresh-2",
					"expires_in":    3600,
				})
				return
			}
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"access_token":  "xai-access-1",
				"refresh_token": "xai-refresh-1",
				"id_token":      idToken,
				"expires_in":    3600,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	serverURL = server.URL
	defer server.Close()

	dir := t.TempDir()
	manager := newTestManager(t, dir, server, clock)
	flow, err := manager.Start(context.Background(), MethodXAIOAuth)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Poll(context.Background(), MethodXAIOAuth, flow.ID)
	if err != nil || result.State != FlowStateAuthorized {
		t.Fatalf("poll = %+v, %v", result, err)
	}
	clock.Advance(2 * time.Hour)

	const callers = 24
	var wait sync.WaitGroup
	errorsSeen := make(chan error, callers)
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			credential, credentialErr := manager.Credential(
				context.Background(), Binding{Method: MethodXAIOAuth, AccountID: "xai-1"},
			)
			if credentialErr == nil && credential.Token != "xai-access-2" {
				credentialErr = fmt.Errorf("unexpected token %q", credential.Token)
			}
			errorsSeen <- credentialErr
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for credentialErr := range errorsSeen {
		if credentialErr != nil {
			t.Fatal(credentialErr)
		}
	}
	if refreshes.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshes.Load())
	}
	data, err := os.ReadFile(filepath.Join(dir, storeFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "xai-refresh-2") ||
		strings.Contains(string(data), "xai-access-2") {
		t.Fatalf("unexpected durable state: %s", data)
	}

	rejectRefresh.Store(true)
	restarted := newTestManager(t, dir, server, clock)
	_, err = restarted.Credential(
		context.Background(), Binding{Method: MethodXAIOAuth, AccountID: "xai-1"},
	)
	if !errors.Is(err, ErrRequiresReauth) {
		t.Fatalf("credential error = %v", err)
	}
	status, err := restarted.Status(context.Background(), MethodXAIOAuth)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Accounts) != 1 || !status.Accounts[0].RequiresReauth {
		t.Fatalf("status = %+v", status)
	}
}

func TestCopilotFlowUsesGitHubTokenAndDynamicRuntime(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	var serverURL string
	var exchanges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/copilot/device":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"device_code":      "github-device-secret",
				"user_code":        "GITHUB-CODE",
				"verification_uri": "https://github.com/login/device",
				"expires_in":       900,
				"interval":         1,
			})
		case "/copilot/oauth":
			writeJSON(t, writer, http.StatusOK, map[string]any{"access_token": "github-oauth-token"})
		case "/github/user":
			if request.Header.Get("Authorization") != "Bearer github-oauth-token" {
				t.Errorf("user authorization = %q", request.Header.Get("Authorization"))
			}
			writeJSON(t, writer, http.StatusOK, map[string]any{"id": 12345, "login": "octocat"})
		case "/github/copilot-token":
			exchanges.Add(1)
			if request.Header.Get("Authorization") != "token github-oauth-token" {
				t.Errorf("token authorization = %q", request.Header.Get("Authorization"))
			}
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"token": "short-lived-copilot-token", "expires_at": clock.Now().Add(time.Hour).Unix(),
			})
		case "/github/copilot-user":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"endpoints": map[string]any{"api": serverURL + "/copilot/dynamic"},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	serverURL = server.URL
	defer server.Close()

	dir := t.TempDir()
	manager := newTestManager(t, dir, server, clock)
	flow, err := manager.Start(context.Background(), MethodGitHubCopilot)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Poll(context.Background(), MethodGitHubCopilot, flow.ID)
	if err != nil || result.State != FlowStateAuthorized {
		t.Fatalf("poll = %+v, %v", result, err)
	}
	credential, err := manager.Credential(context.Background(), Binding{Method: MethodGitHubCopilot})
	if err != nil {
		t.Fatal(err)
	}
	if credential.Token != "short-lived-copilot-token" ||
		credential.BaseURL != server.URL+"/copilot/dynamic" ||
		credential.Protocol != ProtocolChatCompletions ||
		credential.Headers["editor-version"] == "" ||
		credential.Headers["openai-intent"] != "conversation-agent" {
		t.Fatalf("credential = %+v", credential)
	}
	if exchanges.Load() != 1 {
		t.Fatalf("Copilot token exchanges = %d", exchanges.Load())
	}
	data, err := os.ReadFile(filepath.Join(dir, storeFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "github-oauth-token") ||
		strings.Contains(string(data), "short-lived-copilot-token") ||
		strings.Contains(string(data), "github-device-secret") {
		t.Fatalf("unexpected durable Copilot state: %s", data)
	}
}

func TestCopilotEndpointFallbackIsStableAfterDiscoveryFailure(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	var usageCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/github/copilot-token":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"token": "short-lived-copilot-token", "expires_at": clock.Now().Add(time.Hour).Unix(),
			})
		case "/github/copilot-user":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"endpoints": map[string]any{"api": server.URL + "/copilot/dynamic"},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	baseClient := server.Client()
	baseTransport := baseClient.Transport
	client := *baseClient
	client.Transport = authRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/github/copilot-user" && usageCalls.Add(1) == 1 {
			return nil, errors.New("temporary endpoint discovery failure")
		}
		return baseTransport.RoundTrip(request)
	})
	manager, err := NewManager(Options{
		ConfigDir:                  t.TempDir(),
		HTTPClient:                 &client,
		Now:                        clock.Now,
		Endpoints:                  testEndpoints(server.URL),
		AllowInsecureTestEndpoints: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.upsertAccount(MethodGitHubCopilot, storedAccount{
		ID: "copilot-account", Login: "octocat", Secret: "github-oauth-token",
		AuthenticatedAt: clock.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	first, err := manager.Credential(context.Background(), Binding{
		Method: MethodGitHubCopilot, AccountID: "copilot-account",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Credential(context.Background(), Binding{
		Method: MethodGitHubCopilot, AccountID: "copilot-account",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.BaseURL != server.URL+"/copilot/fallback" || second.BaseURL != first.BaseURL {
		t.Fatalf("runtime profile drifted: first=%q second=%q", first.BaseURL, second.BaseURL)
	}
	if usageCalls.Load() != 1 {
		t.Fatalf("usage discovery calls = %d, want one cached failure", usageCalls.Load())
	}
}

func TestCancelWinsWhilePollIsInFlight(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	idToken := testJWT(t, map[string]any{"chatgpt_account_id": "cancelled-account"})
	pollStarted := make(chan struct{})
	releasePoll := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/codex/device":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"device_auth_id": "cancel-device", "user_code": "CANCEL", "interval": 1,
			})
		case "/codex/poll":
			close(pollStarted)
			<-releasePoll
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"authorization_code": "cancel-code", "code_verifier": "cancel-verifier",
			})
		case "/codex/token":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"access_token": "must-not-persist-access", "refresh_token": "must-not-persist-refresh",
				"id_token": idToken,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	manager := newTestManager(t, t.TempDir(), server, clock)
	flow, err := manager.Start(context.Background(), MethodCodexOAuth)
	if err != nil {
		t.Fatal(err)
	}
	pollResult := make(chan error, 1)
	go func() {
		_, pollErr := manager.Poll(context.Background(), MethodCodexOAuth, flow.ID)
		pollResult <- pollErr
	}()
	<-pollStarted
	if err := manager.Cancel(MethodCodexOAuth, flow.ID); err != nil {
		t.Fatal(err)
	}
	close(releasePoll)
	if err := <-pollResult; !errors.Is(err, ErrFlowNotFound) {
		t.Fatalf("poll error = %v", err)
	}
	status, err := manager.Status(context.Background(), MethodCodexOAuth)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Accounts) != 0 {
		t.Fatalf("cancelled flow persisted account: %+v", status)
	}
}

func TestStoreReloadMutationAndRefreshCAS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := NewManager(Options{ConfigDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewManager(Options{ConfigDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := first.upsertAccount(MethodCodexOAuth, storedAccount{
		ID: "one", Login: "one@example.com", Secret: "refresh-old", AuthenticatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := second.upsertAccount(MethodXAIOAuth, storedAccount{
		ID: "two", Login: "two@example.com", Secret: "refresh-xai", AuthenticatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := first.replaceSecret(MethodCodexOAuth, "one", "refresh-old", "refresh-new"); err != nil {
		t.Fatal(err)
	}
	if err := second.replaceSecret(MethodCodexOAuth, "one", "refresh-old", "refresh-stale"); !errors.Is(err, errSecretChanged) {
		t.Fatalf("stale CAS error = %v", err)
	}
	const concurrentAccounts = 24
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := range concurrentAccounts {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			manager := first
			if index%2 == 1 {
				manager = second
			}
			id := fmt.Sprintf("concurrent-%02d", index)
			if upsertErr := manager.upsertAccount(MethodCodexOAuth, storedAccount{
				ID: id, Login: id, Secret: "refresh-" + id, AuthenticatedAt: now,
			}); upsertErr != nil {
				t.Errorf("upsert %s: %v", id, upsertErr)
			}
		}()
	}
	close(start)
	wait.Wait()
	state, err := first.store.read()
	if err != nil {
		t.Fatal(err)
	}
	if state.method(MethodCodexOAuth).Accounts["one"].Secret != "refresh-new" ||
		state.method(MethodXAIOAuth).Accounts["two"].Secret != "refresh-xai" {
		t.Fatalf("reload-mutate lost state: %+v", state)
	}
	if got := len(state.method(MethodCodexOAuth).Accounts); got != concurrentAccounts+1 {
		t.Fatalf("concurrent reload-mutate account count = %d", got)
	}
}

func TestXAISlowDownAndDeniedDestroyFlow(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	var polls atomic.Int32
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/xai/discovery":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"issuer":                        xaiIssuer,
				"device_authorization_endpoint": serverURL + "/xai/device",
				"token_endpoint":                serverURL + "/xai/token",
			})
		case "/xai/device":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"device_code": "device", "user_code": "CODE",
				"verification_uri": "https://accounts.x.ai/oauth2/device", "interval": 1,
			})
		case "/xai/token":
			if polls.Add(1) == 1 {
				writeJSON(t, writer, http.StatusBadRequest, map[string]any{"error": "slow_down"})
				return
			}
			writeJSON(t, writer, http.StatusBadRequest, map[string]any{"error": "access_denied"})
		default:
			http.NotFound(writer, request)
		}
	}))
	serverURL = server.URL
	defer server.Close()
	manager := newTestManager(t, t.TempDir(), server, clock)
	flow, err := manager.Start(context.Background(), MethodXAIOAuth)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Poll(context.Background(), MethodXAIOAuth, flow.ID)
	if err != nil || result.State != FlowStatePending || result.IntervalSeconds != 9 {
		t.Fatalf("slow_down poll = %+v, %v", result, err)
	}
	clock.Advance(10 * time.Second)
	result, err = manager.Poll(context.Background(), MethodXAIOAuth, flow.ID)
	if err != nil || result.State != FlowStateDenied {
		t.Fatalf("denied poll = %+v, %v", result, err)
	}
	if _, err := manager.Poll(context.Background(), MethodXAIOAuth, flow.ID); !errors.Is(err, ErrFlowNotFound) {
		t.Fatalf("terminal flow remained in memory: %v", err)
	}
}

func TestHTTPGuardsAndDefaultSingleton(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	var followed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/redirect":
			http.Redirect(writer, request, "/followed", http.StatusFound)
		case "/followed":
			followed.Store(true)
			writeJSON(t, writer, http.StatusOK, map[string]any{})
		case "/oversized":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"padding":"` + strings.Repeat("x", maxOAuthResponseBytes) + `"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	_, err := NewManager(Options{
		ConfigDir: dir,
		Endpoints: Endpoints{CodexDeviceStart: server.URL + "/redirect"},
	})
	if err == nil {
		t.Fatal("endpoint override without explicit test mode succeeded")
	}
	manager, err := NewManager(Options{
		ConfigDir:                  dir,
		HTTPClient:                 server.Client(),
		Now:                        clock.Now,
		Endpoints:                  Endpoints{CodexDeviceStart: server.URL + "/redirect"},
		AllowInsecureTestEndpoints: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(context.Background(), MethodCodexOAuth); err == nil {
		t.Fatal("redirecting device endpoint succeeded")
	}
	if followed.Load() {
		t.Fatal("OAuth HTTP client followed a redirect")
	}
	manager.endpoints.CodexDeviceStart = server.URL + "/oversized"
	if _, err := manager.Start(context.Background(), MethodCodexOAuth); err == nil ||
		!strings.Contains(err.Error(), "64 KiB") {
		t.Fatalf("oversized response error = %v", err)
	}

	one, err := Default(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	two, err := Default(one.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if one != two {
		t.Fatal("Default did not return config-directory singleton")
	}
}

func TestCopilotRuntimeHeadersUseOneCorrelatedRequestID(t *testing.T) {
	headers := copilotRuntimeHeaders("8b0ff00d-6932-4e7c-b96e-459672620d7c")
	if headers["x-request-id"] == "" {
		t.Fatal("x-request-id is missing")
	}
	if headers["x-agent-task-id"] != headers["x-request-id"] {
		t.Fatalf(
			"x-agent-task-id = %q, want x-request-id %q",
			headers["x-agent-task-id"], headers["x-request-id"],
		)
	}
}

func TestFlowMethodScopePrecedesPollAndCancelSideEffects(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/codex/device":
			writeJSON(t, writer, http.StatusOK, map[string]any{
				"device_auth_id": "method-device", "user_code": "METHOD", "interval": 1,
			})
		case "/codex/poll":
			polls.Add(1)
			writer.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	manager := newTestManager(t, t.TempDir(), server, clock)
	flow, err := manager.Start(context.Background(), MethodCodexOAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Poll(
		context.Background(), MethodXAIOAuth, flow.ID,
	); !errors.Is(err, ErrFlowNotFound) {
		t.Fatalf("wrong-method poll error = %v", err)
	}
	if err := manager.Cancel(MethodXAIOAuth, flow.ID); !errors.Is(err, ErrFlowNotFound) {
		t.Fatalf("wrong-method cancel error = %v", err)
	}
	if polls.Load() != 0 {
		t.Fatalf("wrong-method operation reached upstream %d times", polls.Load())
	}
	result, err := manager.Poll(context.Background(), MethodCodexOAuth, flow.ID)
	if err != nil || result.State != FlowStatePending {
		t.Fatalf("correct-method poll = %+v, %v", result, err)
	}
	if polls.Load() != 1 {
		t.Fatalf("correct-method upstream polls = %d", polls.Load())
	}
	if err := manager.Cancel(MethodCodexOAuth, flow.ID); err != nil {
		t.Fatal(err)
	}
}

func TestLogoutInvalidatesInFlightPollAcrossManagers(t *testing.T) {
	for _, test := range []struct {
		name            string
		separateManager bool
	}{
		{name: "single_manager"},
		{name: "two_managers", separateManager: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := newTestClock()
			idToken := testJWT(t, map[string]any{
				"chatgpt_account_id": "logout-account",
				"email":              "logout@example.com",
			})
			pollStarted := make(chan struct{})
			releasePoll := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releasePoll) }) }
			defer release()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/codex/device":
					writeJSON(t, writer, http.StatusOK, map[string]any{
						"device_auth_id": "logout-device", "user_code": "LOGOUT", "interval": 1,
					})
				case "/codex/poll":
					close(pollStarted)
					<-releasePoll
					writeJSON(t, writer, http.StatusOK, map[string]any{
						"authorization_code": "logout-code", "code_verifier": "logout-verifier",
					})
				case "/codex/token":
					writeJSON(t, writer, http.StatusOK, map[string]any{
						"access_token": "must-not-survive-access", "refresh_token": "must-not-survive-refresh",
						"id_token": idToken, "expires_in": 3600,
					})
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()

			dir := t.TempDir()
			flowManager := newTestManager(t, dir, server, clock)
			logoutManager := flowManager
			if test.separateManager {
				logoutManager = newTestManager(t, dir, server, clock)
			}
			flow, err := flowManager.Start(context.Background(), MethodCodexOAuth)
			if err != nil {
				t.Fatal(err)
			}
			pollResult := make(chan error, 1)
			go func() {
				_, pollErr := flowManager.Poll(
					context.Background(), MethodCodexOAuth, flow.ID,
				)
				pollResult <- pollErr
			}()
			select {
			case <-pollStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("poll did not reach upstream")
			}

			logoutResult := make(chan error, 1)
			go func() {
				logoutResult <- logoutManager.Logout(context.Background(), MethodCodexOAuth)
			}()
			select {
			case err := <-logoutResult:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				release()
				t.Fatal("logout blocked behind an in-flight upstream poll")
			}

			state, err := logoutManager.store.read()
			if err != nil {
				t.Fatal(err)
			}
			entry := state.Methods[MethodCodexOAuth]
			if entry == nil || entry.Generation != 1 || len(entry.Accounts) != 0 {
				t.Fatalf("durable logout state = %+v", entry)
			}
			release()
			select {
			case err := <-pollResult:
				if !errors.Is(err, ErrFlowNotFound) {
					t.Fatalf("poll error after logout = %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("poll did not finish after release")
			}
			status, err := flowManager.Status(context.Background(), MethodCodexOAuth)
			if err != nil {
				t.Fatal(err)
			}
			if len(status.Accounts) != 0 {
				t.Fatalf("logout flow revived an account: %+v", status)
			}
			data, err := os.ReadFile(filepath.Join(dir, storeFileName))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "must-not-survive") {
				t.Fatalf("logout store contains poll credentials: %s", data)
			}
			if _, err := flowManager.Poll(
				context.Background(), MethodCodexOAuth, flow.ID,
			); !errors.Is(err, ErrFlowNotFound) {
				t.Fatalf("invalidated flow remained pollable: %v", err)
			}
		})
	}
}

func TestPendingFlowLimitReservesBeforeUpstream(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	const limit = 4
	const callers = 12
	entered := make(chan struct{}, callers)
	releaseRequests := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/codex/device" {
			http.NotFound(writer, request)
			return
		}
		entered <- struct{}{}
		<-releaseRequests
		writeJSON(t, writer, http.StatusOK, map[string]any{
			"device_auth_id": "limit-device", "user_code": "LIMIT", "interval": 1,
		})
	}))
	defer server.Close()

	manager := newTestManager(t, t.TempDir(), server, clock)
	manager.pendingFlowLimit = limit
	type outcome struct {
		flow Flow
		err  error
	}
	results := make(chan outcome, callers)
	for range callers {
		go func() {
			flow, err := manager.Start(context.Background(), MethodCodexOAuth)
			results <- outcome{flow: flow, err: err}
		}()
	}
	for range limit {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(releaseRequests)
			t.Fatal("reserved flow did not reach upstream")
		}
	}
	for range callers - limit {
		select {
		case result := <-results:
			if result.err == nil || !strings.Contains(result.err.Error(), "too many pending") {
				close(releaseRequests)
				t.Fatalf("overflow Start result = %+v", result)
			}
		case <-time.After(5 * time.Second):
			close(releaseRequests)
			t.Fatal("overflow Start reached upstream or did not return")
		}
	}
	select {
	case <-entered:
		close(releaseRequests)
		t.Fatal("pending-flow overflow reached upstream")
	default:
	}
	close(releaseRequests)
	for range limit {
		select {
		case result := <-results:
			if result.err != nil || result.flow.ID == "" {
				t.Fatalf("reserved Start result = %+v", result)
			}
			if err := manager.Cancel(MethodCodexOAuth, result.flow.ID); err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("reserved Start did not finish")
		}
	}
}

func TestVerificationURIPinningAndLocalTestOverride(t *testing.T) {
	production := &Manager{}
	for _, test := range []struct {
		uri  string
		host string
	}{
		{uri: "https://auth.openai.com/codex/device", host: "auth.openai.com"},
		{uri: "https://accounts.x.ai/oauth2/device?code=one", host: xaiVerificationHost},
		{uri: "https://github.com/login/device", host: "github.com"},
	} {
		if err := production.validateVerificationURI(test.uri, test.host); err != nil {
			t.Fatalf("valid verification URI %q: %v", test.uri, err)
		}
	}
	for _, uri := range []string{
		"http://github.com/login/device",
		"https://github.com.evil.example/login/device",
		"https://sub.github.com/login/device",
		"https://github.com:8443/login/device",
		"https://user@github.com/login/device",
		"file:///tmp/device",
	} {
		if err := production.validateVerificationURI(uri, "github.com"); err == nil {
			t.Fatalf("untrusted verification URI %q succeeded", uri)
		}
	}
	for _, uri := range []string{
		"http://accounts.x.ai/oauth2/device",
		"https://accounts.x.ai.evil.example/oauth2/device",
		"https://sub.accounts.x.ai/oauth2/device",
		"https://accounts.x.ai:8443/oauth2/device",
		"https://user@accounts.x.ai/oauth2/device",
	} {
		if err := production.validateVerificationURI(uri, xaiVerificationHost); err == nil {
			t.Fatalf("untrusted xAI verification URI %q succeeded", uri)
		}
	}
	if err := production.validateVerificationURIs(
		"https://github.com/login/device",
		"https://evil.example/login/device?user_code=one",
		"github.com",
	); err == nil {
		t.Fatal("untrusted verification_uri_complete succeeded")
	}

	clock := newTestClock()
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/copilot/device" {
			http.NotFound(writer, request)
			return
		}
		writeJSON(t, writer, http.StatusOK, map[string]any{
			"device_code": "local-device", "user_code": "LOCAL",
			"verification_uri": serverURL + "/verify", "interval": 1,
		})
	}))
	serverURL = server.URL
	defer server.Close()
	manager := newTestManager(t, t.TempDir(), server, clock)
	flow, err := manager.Start(context.Background(), MethodGitHubCopilot)
	if err != nil {
		t.Fatalf("local test verification URI: %v", err)
	}
	if err := manager.Cancel(MethodGitHubCopilot, flow.ID); err != nil {
		t.Fatal(err)
	}
	manager.allowUnsafe = false
	if _, err := manager.Start(context.Background(), MethodGitHubCopilot); err == nil ||
		!strings.Contains(err.Error(), "untrusted verification URI") {
		t.Fatalf("production verification URI error = %v", err)
	}
}
