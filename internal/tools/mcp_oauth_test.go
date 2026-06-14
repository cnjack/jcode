package tools

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/mark3labs/mcp-go/client/transport"
)

func TestMCPFileTokenStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	store := NewMCPTokenStore("acme")

	// No token yet → ErrNoToken.
	if _, err := store.GetToken(ctx); !errors.Is(err, transport.ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
	if HasMCPOAuthToken("acme") {
		t.Fatal("expected HasMCPOAuthToken=false before save")
	}

	tok := &transport.Token{
		AccessToken:  "tok_abc",
		TokenType:    "Bearer",
		RefreshToken: "ref_xyz",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	if err := store.SaveToken(ctx, tok); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	got, err := store.GetToken(ctx)
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if got.AccessToken != "tok_abc" || got.RefreshToken != "ref_xyz" {
		t.Fatalf("token round-trip mismatch: %+v", got)
	}
	if !HasMCPOAuthToken("acme") {
		t.Fatal("expected HasMCPOAuthToken=true after save")
	}

	if err := DeleteMCPOAuthToken("acme"); err != nil {
		t.Fatalf("DeleteMCPOAuthToken: %v", err)
	}
	if HasMCPOAuthToken("acme") {
		t.Fatal("expected HasMCPOAuthToken=false after delete")
	}
	// Deleting a missing token is a no-op.
	if err := DeleteMCPOAuthToken("acme"); err != nil {
		t.Fatalf("DeleteMCPOAuthToken (missing): %v", err)
	}
}

// TestPerformMCPOAuthLoginManualFallback verifies that when the authorization
// server advertises no dynamic client registration endpoint and no client_id is
// configured, the login driver returns ErrOAuthNeedsClientID so callers can
// prompt for a manual client id.
func TestPerformMCPOAuthLoginManualFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// The driver binds a fixed loopback callback port; if it is unavailable
	// (busy or sandbox-restricted) skip rather than fail spuriously.
	if ln, err := net.Listen("tcp", mcpOAuthCallbackAddr); err != nil {
		t.Skipf("cannot bind %s: %v", mcpOAuthCallbackAddr, err)
	} else {
		_ = ln.Close()
	}

	// Authorization server metadata WITHOUT a registration_endpoint.
	var baseURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer": "` + baseURL + `",
			"authorization_endpoint": "` + baseURL + `/authorize",
			"token_endpoint": "` + baseURL + `/token"
		}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	baseURL = ts.URL

	srv := &config.MCPServer{
		Type: "http",
		URL:  ts.URL + "/mcp",
		OAuth: &config.MCPOAuthConfig{
			Enabled:               true,
			AuthServerMetadataURL: ts.URL + "/.well-known/oauth-authorization-server",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := PerformMCPOAuthLogin(ctx, "acme", srv, nil)
	if !errors.Is(err, ErrOAuthNeedsClientID) {
		t.Fatalf("expected ErrOAuthNeedsClientID, got %v", err)
	}
}
