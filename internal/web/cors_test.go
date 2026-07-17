package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIsAllowedWebOrigin locks the cross-origin gate that protects the
// loopback agent-control API + WebSocket. The legitimate flows (same-origin,
// loopback, the Vite dev proxy, LAN access) must pass; a cross-origin website
// must be rejected.
func TestIsAllowedWebOrigin(t *testing.T) {
	cases := []struct {
		name   string
		host   string // the server's own Host (request target)
		origin string // the browser-set Origin header
		want   bool
	}{
		{"empty origin (curl / native client)", "127.0.0.1:8080", "", true},
		{"same-origin loopback (desktop webview / local browser)", "127.0.0.1:53913", "http://127.0.0.1:53913", true},
		{"same-origin LAN (--host 0.0.0.0)", "192.168.1.5:8080", "http://192.168.1.5:8080", true},
		{"vite dev proxy localhost:5173 -> 127.0.0.1:8091", "127.0.0.1:8091", "http://localhost:5173", true},
		{"loopback origin, different port", "127.0.0.1:8080", "http://127.0.0.1:9999", true},
		{"localhost origin to 127.0.0.1 backend", "127.0.0.1:8080", "http://localhost:8080", true},
		{"tauri://localhost (macOS desktop shell, absolute-URL API)", "127.0.0.1:53913", "tauri://localhost", true},
		{"http://tauri.localhost (Windows/Linux desktop shell)", "127.0.0.1:53913", "http://tauri.localhost", true},
		{"cross-origin website", "127.0.0.1:53913", "https://evil.com", false},
		{"cross-origin website targeting LAN ip", "192.168.1.5:8080", "https://evil.example", false},
		{"non-loopback private ip, different origin", "127.0.0.1:8080", "http://10.0.0.9:8080", false},
		{"malformed origin", "127.0.0.1:8080", "://nonsense", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/health", nil)
			r.Host = c.host
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if got := isAllowedWebOrigin(r); got != c.want {
				t.Errorf("isAllowedWebOrigin(host=%q, origin=%q) = %v, want %v", c.host, c.origin, got, c.want)
			}
		})
	}
}

func TestCORSMiddlewareRejectsUntrustedSimpleRequestsBeforeSideEffects(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/computer/config", nil)
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Origin", "https://evil.example")
	// text/plain is a CORS-simple content type and reaches the server without a
	// preflight when a hostile page uses fetch(..., {mode: 'no-cors'}).
	r.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	corsMiddleware(inner).ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", rec.Code)
	}
	if called {
		t.Fatal("untrusted Origin reached the mutating handler")
	}
}

func TestCORSMiddlewareAllowsTrustedOriginsAndNonBrowserClients(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	for _, origin := range []string{"", "http://localhost:5173", "tauri://localhost"} {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/computer/config", nil)
		r.Host = "127.0.0.1:8080"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		corsMiddleware(inner).ServeHTTP(rec, r)
		if rec.Code != http.StatusTeapot {
			t.Errorf("origin=%q status=%d, want passthrough", origin, rec.Code)
		}
	}
}
