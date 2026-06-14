package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

// ---------------------------------------------------------------------------
// MCP OAuth: persistent token store + interactive login driver.
//
// The mark3labs/mcp-go library implements the full OAuth flow (RFC 8414/9728
// metadata discovery, RFC 7591 dynamic client registration, PKCE, token
// refresh). We provide a file-backed transport.TokenStore so tokens survive
// restarts, and a thin driver that runs the authorization-code flow against a
// fixed loopback callback port and guides the user through the browser.
// ---------------------------------------------------------------------------

const (
	// mcpOAuthCallbackAddr is the fixed loopback address the OAuth redirect
	// lands on. The matching redirect URI is documentable so users can
	// pre-register it when an authorization server does not support dynamic
	// client registration.
	mcpOAuthCallbackAddr = "127.0.0.1:13380"
	mcpOAuthRedirectURI  = "http://127.0.0.1:13380/oauth/callback"
)

// ErrOAuthNeedsClientID indicates the authorization server does not support
// dynamic client registration and no client_id was configured. The caller
// should prompt the user to supply an OAuth Client ID (and optionally secret).
var ErrOAuthNeedsClientID = errors.New("dynamic client registration unavailable; set an OAuth Client ID")

// MCPOAuthDir returns the directory where MCP OAuth tokens are persisted.
func MCPOAuthDir() string {
	return filepath.Join(config.ConfigDir(), "oauth")
}

// sanitizeServerName makes a config server name safe to use as a filename.
func sanitizeServerName(name string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	return r.Replace(name)
}

func mcpTokenPath(name string) string {
	return filepath.Join(MCPOAuthDir(), sanitizeServerName(name)+".json")
}

// mcpFileTokenStore is a file-backed transport.TokenStore scoped to one server.
type mcpFileTokenStore struct {
	path string
	mu   sync.Mutex
}

// NewMCPTokenStore returns a transport.TokenStore that persists the token for
// the named server at ~/.jcode/oauth/<server>.json.
func NewMCPTokenStore(serverName string) transport.TokenStore {
	return &mcpFileTokenStore{path: mcpTokenPath(serverName)}
}

// GetToken returns the persisted token, or transport.ErrNoToken if none exists.
func (s *mcpFileTokenStore) GetToken(ctx context.Context) (*transport.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, transport.ErrNoToken
		}
		return nil, err
	}
	var tok transport.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("mcp oauth: decode token: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, transport.ErrNoToken
	}
	return &tok, nil
}

// SaveToken persists the token with 0600 permissions.
func (s *mcpFileTokenStore) SaveToken(ctx context.Context, token *transport.Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// HasMCPOAuthToken reports whether a (non-empty) token is stored for the server.
func HasMCPOAuthToken(serverName string) bool {
	data, err := os.ReadFile(mcpTokenPath(serverName))
	if err != nil {
		return false
	}
	var tok transport.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return false
	}
	return tok.AccessToken != ""
}

// DeleteMCPOAuthToken removes the persisted token for a server (no error if absent).
func DeleteMCPOAuthToken(serverName string) error {
	err := os.Remove(mcpTokenPath(serverName))
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

// ---------------------------------------------------------------------------
// Client construction (shared by LoadMCPTools and MCPManager)
// ---------------------------------------------------------------------------

// mcpUsesOAuth reports whether a server should be connected via the OAuth
// transport: either OAuth is explicitly enabled, or a token already exists.
func mcpUsesOAuth(name string, srv *config.MCPServer) bool {
	return (srv.OAuth != nil && srv.OAuth.Enabled) || HasMCPOAuthToken(name)
}

// mcpTimeout returns the configured request timeout, or 0 to use the library default.
func mcpTimeout(srv *config.MCPServer) time.Duration {
	if srv.TimeoutSeconds > 0 {
		return time.Duration(srv.TimeoutSeconds) * time.Second
	}
	return 0
}

func mcpOAuthConfig(name string, srv *config.MCPServer) transport.OAuthConfig {
	oa := srv.OAuth
	if oa == nil {
		oa = &config.MCPOAuthConfig{}
	}
	return transport.OAuthConfig{
		ClientID:              oa.ClientID,
		ClientSecret:          oa.ClientSecret,
		RedirectURI:           mcpOAuthRedirectURI,
		Scopes:                oa.Scopes,
		TokenStore:            NewMCPTokenStore(name),
		PKCEEnabled:           true,
		AuthServerMetadataURL: oa.AuthServerMetadataURL,
	}
}

// buildMCPClient constructs an MCP client for a server config, selecting the
// transport (stdio / streamable-http / SSE) and wiring OAuth + timeout +
// headers as appropriate.
func buildMCPClient(name string, srv *config.MCPServer) (*client.Client, error) {
	switch {
	case srv.Type == "http":
		var opts []transport.StreamableHTTPCOption
		if len(srv.Headers) > 0 {
			opts = append(opts, transport.WithHTTPHeaders(srv.Headers))
		}
		if d := mcpTimeout(srv); d > 0 {
			opts = append(opts, transport.WithHTTPTimeout(d))
		}
		if mcpUsesOAuth(name, srv) {
			return client.NewOAuthStreamableHttpClient(srv.URL, mcpOAuthConfig(name, srv), opts...)
		}
		return client.NewStreamableHttpClient(srv.URL, opts...)
	case srv.URL != "" || srv.Type == "sse":
		var opts []transport.ClientOption
		if len(srv.Headers) > 0 {
			opts = append(opts, transport.WithHeaders(srv.Headers))
		}
		if d := mcpTimeout(srv); d > 0 {
			opts = append(opts, transport.WithEndpointTimeout(d))
		}
		if mcpUsesOAuth(name, srv) {
			return client.NewOAuthSSEClient(srv.URL, mcpOAuthConfig(name, srv), opts...)
		}
		return client.NewSSEMCPClient(srv.URL, opts...)
	case srv.Command != "" || srv.Type == "stdio":
		return client.NewStdioMCPClient(srv.Command, srv.Env, srv.Args...)
	default:
		return nil, fmt.Errorf("invalid config: missing url or command")
	}
}

// isMCPAuthError reports whether an error indicates the server requires OAuth
// authorization (a 401 challenge or the library's auth-required sentinel).
func isMCPAuthError(err error) bool {
	return client.IsOAuthAuthorizationRequiredError(err) || client.IsAuthorizationRequiredError(err)
}

// ---------------------------------------------------------------------------
// Login driver
// ---------------------------------------------------------------------------

type oauthCallbackResult struct {
	code  string
	state string
	err   string
}

// PerformMCPOAuthLogin runs the OAuth 2.0 authorization-code flow for an
// HTTP/SSE MCP server: it discovers the authorization server, dynamically
// registers a client (falling back to a configured client_id/secret when
// registration is unsupported), opens a fixed loopback callback, and exchanges
// the returned code for a token persisted via NewMCPTokenStore.
//
// onAuthURL is invoked with the authorization URL once it is built — the caller
// is responsible for opening a browser and/or displaying the URL to the user.
//
// On success it mutates srv.OAuth (Enabled, and ClientID/ClientSecret when
// obtained via dynamic registration); the caller must persist the config.
// ctx should carry a timeout (the flow blocks waiting for the browser callback).
func PerformMCPOAuthLogin(ctx context.Context, name string, srv *config.MCPServer, onAuthURL func(url string)) error {
	if srv == nil {
		return fmt.Errorf("mcp oauth: nil server config")
	}
	if srv.URL == "" {
		return fmt.Errorf("mcp oauth: server %q has no URL (OAuth only applies to http/sse transports)", name)
	}
	if srv.OAuth == nil {
		srv.OAuth = &config.MCPOAuthConfig{}
	}

	oauthCfg := transport.OAuthConfig{
		ClientID:              srv.OAuth.ClientID,
		ClientSecret:          srv.OAuth.ClientSecret,
		RedirectURI:           mcpOAuthRedirectURI,
		Scopes:                srv.OAuth.Scopes,
		TokenStore:            NewMCPTokenStore(name),
		PKCEEnabled:           true,
		AuthServerMetadataURL: srv.OAuth.AuthServerMetadataURL,
	}
	h := transport.NewOAuthHandler(oauthCfg)
	h.SetBaseURL(srv.URL)

	// Start the loopback callback server before opening the browser.
	cbChan := make(chan oauthCallbackResult, 1)
	cbServer, err := startMCPCallbackServer(cbChan)
	if err != nil {
		return err
	}
	defer func() { _ = cbServer.Close() }()

	verifier, err := transport.GenerateCodeVerifier()
	if err != nil {
		return fmt.Errorf("mcp oauth: generate code verifier: %w", err)
	}
	challenge := transport.GenerateCodeChallenge(verifier)
	state, err := transport.GenerateState()
	if err != nil {
		return fmt.Errorf("mcp oauth: generate state: %w", err)
	}

	// Dynamic client registration (RFC 7591), with manual fallback. When a
	// client_id is already configured, GetClientID() != "" and we skip
	// registration entirely (the manual/fallback path).
	if h.GetClientID() == "" {
		if regErr := h.RegisterClient(ctx, "jcode"); regErr != nil {
			return fmt.Errorf("%w: %v", ErrOAuthNeedsClientID, regErr)
		}
	}

	authURL, err := h.GetAuthorizationURL(ctx, state, challenge)
	if err != nil {
		return fmt.Errorf("mcp oauth: build authorization URL: %w", err)
	}
	if onAuthURL != nil {
		onAuthURL(authURL)
	}

	select {
	case res := <-cbChan:
		if res.err != "" {
			return fmt.Errorf("mcp oauth: authorization denied: %s", res.err)
		}
		if res.state != state {
			return fmt.Errorf("mcp oauth: state mismatch (possible CSRF)")
		}
		if res.code == "" {
			return fmt.Errorf("mcp oauth: no authorization code returned")
		}
		if err := h.ProcessAuthorizationResponse(ctx, res.code, res.state, verifier); err != nil {
			return fmt.Errorf("mcp oauth: token exchange failed: %w", err)
		}
	case <-ctx.Done():
		return fmt.Errorf("mcp oauth: timed out waiting for browser authorization: %w", ctx.Err())
	}

	// Persist what we learned so reconnect uses the same (possibly dynamically
	// registered) client and re-attaches the bearer token.
	srv.OAuth.Enabled = true
	if id := h.GetClientID(); id != "" {
		srv.OAuth.ClientID = id
	}
	if sec := h.GetClientSecret(); sec != "" {
		srv.OAuth.ClientSecret = sec
	}
	config.Logger().Printf("[mcp oauth] %q: authorization successful", name)
	return nil
}

// startMCPCallbackServer binds the fixed loopback callback port and serves the
// OAuth redirect. The first request's query params are delivered on ch.
func startMCPCallbackServer(ch chan<- oauthCallbackResult) (*http.Server, error) {
	ln, err := net.Listen("tcp", mcpOAuthCallbackAddr)
	if err != nil {
		return nil, fmt.Errorf("mcp oauth: cannot bind callback %s (another login in progress?): %w", mcpOAuthCallbackAddr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		res := oauthCallbackResult{
			code:  q.Get("code"),
			state: q.Get("state"),
			err:   q.Get("error"),
		}
		if desc := q.Get("error_description"); desc != "" && res.err != "" {
			res.err = res.err + ": " + desc
		}
		select {
		case ch <- res:
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if res.err != "" {
			_, _ = w.Write([]byte(callbackHTML("Authorization failed", res.err)))
			return
		}
		_, _ = w.Write([]byte(callbackHTML("Authorization successful", "You can close this window and return to jcode.")))
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			config.Logger().Printf("[mcp oauth] callback server error: %v", serveErr)
		}
	}()
	return srv, nil
}

func callbackHTML(title, msg string) string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>` + title + `</title>
<style>body{font-family:-apple-system,system-ui,sans-serif;background:#111;color:#eee;display:flex;
align-items:center;justify-content:center;height:100vh;margin:0}div{text-align:center}h1{color:#FF8400}</style></head>
<body><div><h1>` + title + `</h1><p>` + msg + `</p><script>setTimeout(function(){window.close()},1500)</script></div></body></html>`
}
