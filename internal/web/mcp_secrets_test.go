package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/tools"
)

func TestMCPListMasksHeadersAndOAuthClientSecret(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		headerSecret = "bearer-super-secret-value"
		oauthSecret  = "oauth-super-secret-value"
	)
	s := &Server{
		cfg: &config.Config{MCPServers: map[string]*config.MCPServer{
			"search": {
				Type:    "http",
				URL:     "https://mcp.example.test/mcp",
				Headers: map[string]string{"Authorization": headerSecret, "X-Short": "tiny"},
				OAuth: &config.MCPOAuthConfig{
					Enabled: true, ClientID: "client-id", ClientSecret: oauthSecret, Scopes: []string{"search"},
				},
			},
		}},
		mcpStatuses: map[string]tools.MCPStatus{
			"search": {Name: "search", Error: errors.New("Authorization: " + headerSecret + " oauth=" + oauthSecret)},
		},
	}
	rec := httptest.NewRecorder()
	s.handleListMCP(rec, httptest.NewRequest(http.MethodGet, "/api/mcp", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, headerSecret) || strings.Contains(body, oauthSecret) {
		t.Fatalf("MCP list leaked a plaintext secret: %s", body)
	}
	if !strings.Contains(body, "[redacted]") {
		t.Fatalf("MCP status error was not safely redacted: %s", body)
	}
	var response struct {
		Servers map[string]mcpServerView `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	view := response.Servers["search"]
	if got := view.Headers["Authorization"]; got != maskSecret(headerSecret) {
		t.Errorf("masked Authorization = %q", got)
	}
	if got := view.Headers["X-Short"]; got != "****" {
		t.Errorf("masked short header = %q", got)
	}
	if view.OAuthConfig == nil || view.OAuthConfig.ClientSecret != maskSecret(oauthSecret) {
		t.Fatalf("safe OAuth view = %+v", view.OAuthConfig)
	}
}

func TestMergeMCPServerReqKeepsMaskedSecretsAndRequiresExplicitRemoval(t *testing.T) {
	existing := &config.MCPServer{
		Type: "http", URL: "https://mcp.example.test/mcp", Disabled: true,
		Headers: map[string]string{
			"Authorization": "bearer-existing-secret",
			"X-Keep":        "keep-existing-secret",
			"X-Delete":      "delete-existing-secret",
		},
		OAuth: &config.MCPOAuthConfig{
			Enabled: true, ClientID: "existing-client", ClientSecret: "existing-oauth-secret", Scopes: []string{"one"},
		},
	}
	req := &mcpServerReq{
		Type: "http", URL: existing.URL,
		Headers: map[string]string{
			"Authorization": maskSecret(existing.Headers["Authorization"]),
			"X-Keep":        "",
			"X-New":         "new-secret",
		},
		OAuth: &mcpOAuthReq{
			Enabled: true, ClientSecret: maskSecret(existing.OAuth.ClientSecret),
		},
	}
	got, err := mergeMCPServerReq(existing, req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Headers["Authorization"] != existing.Headers["Authorization"] || got.Headers["X-Keep"] != existing.Headers["X-Keep"] {
		t.Fatalf("masked/empty header did not keep stored secret: %#v", got.Headers)
	}
	if got.Headers["X-Delete"] != existing.Headers["X-Delete"] {
		t.Fatal("an omitted header was deleted without an explicit remove action")
	}
	if got.Headers["X-New"] != "new-secret" {
		t.Fatalf("new header = %q", got.Headers["X-New"])
	}
	if got.OAuth == nil || got.OAuth.ClientID != "existing-client" || got.OAuth.ClientSecret != "existing-oauth-secret" {
		t.Fatalf("masked OAuth update did not preserve credentials: %+v", got.OAuth)
	}
	if !got.Disabled {
		t.Fatal("update dropped disabled state")
	}

	removeReq := &mcpServerReq{
		Type: "http", URL: existing.URL, RemoveHeaders: []string{"Authorization"},
		OAuth: &mcpOAuthReq{Enabled: true, RemoveClientSecret: true},
	}
	removed, err := mergeMCPServerReq(existing, removeReq)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := removed.Headers["Authorization"]; ok {
		t.Fatal("explicit remove_headers did not delete Authorization")
	}
	if removed.OAuth == nil || removed.OAuth.ClientSecret != "" {
		t.Fatalf("explicit OAuth secret removal failed: %+v", removed.OAuth)
	}
}
