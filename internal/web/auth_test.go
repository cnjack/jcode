package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLoopbackBind(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"localhost", true},
		{"0.0.0.0", false},
		{"::", false},
		{"", false},
		{"192.168.1.10", false},
		{"10.0.0.5", false},
		{"example.com", false}, // unresolvable hostname → assume exposed, fail safe
	}
	for _, c := range cases {
		if got := IsLoopbackBind(c.host); got != c.want {
			t.Errorf("IsLoopbackBind(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestExtractToken(t *testing.T) {
	t.Run("authorization header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		r.Header.Set("Authorization", "Bearer abc123")
		if got := extractToken(r); got != "abc123" {
			t.Fatalf("got %q, want abc123", got)
		}
	})
	t.Run("websocket subprotocol", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
		r.Header.Set("Sec-WebSocket-Protocol", "jcode-auth, tok-xyz")
		if got := extractToken(r); got != "tok-xyz" {
			t.Fatalf("got %q, want tok-xyz", got)
		}
	})
	t.Run("query fallback", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/ws?token=qq", nil)
		if got := extractToken(r); got != "qq" {
			t.Fatalf("got %q, want qq", got)
		}
	})
	t.Run("header beats query", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/x?token=qq", nil)
		r.Header.Set("Authorization", "Bearer hdr")
		if got := extractToken(r); got != "hdr" {
			t.Fatalf("got %q, want hdr", got)
		}
	})
	t.Run("none", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		if got := extractToken(r); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
	t.Run("query ignored on non-ws endpoint", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/chat?token=qq", nil)
		if got := extractToken(r); got != "" {
			t.Fatalf("got %q, want empty (query token only allowed on ws endpoints)", got)
		}
	})
	t.Run("query allowed on pty ws", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/pty/pty_1/ws?token=pp", nil)
		if got := extractToken(r); got != "pp" {
			t.Fatalf("got %q, want pp", got)
		}
	})
}

func TestValidToken(t *testing.T) {
	if !validToken("s3cret", "s3cret") {
		t.Error("matching tokens should validate")
	}
	if validToken("wrong", "s3cret") {
		t.Error("mismatched tokens must not validate")
	}
	if validToken("", "s3cret") {
		t.Error("empty provided token must not validate")
	}
	if validToken("x", "") {
		t.Error("empty expected token must not validate")
	}
}

func TestIsValidWSSubprotocolToken(t *testing.T) {
	valid := []string{"abc123", "aZ0-_", "Zm9vYmFy", "x"} // base64url-style tokens
	for _, s := range valid {
		if !IsValidWSSubprotocolToken(s) {
			t.Errorf("IsValidWSSubprotocolToken(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "has space", "has,comma", "semi;colon", "a/b", "quote\"x", "ctrl\tx", "ünïcode"}
	for _, s := range invalid {
		if IsValidWSSubprotocolToken(s) {
			t.Errorf("IsValidWSSubprotocolToken(%q) = true, want false", s)
		}
	}
}

func TestIsAuthExempt(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodGet, "/api/health", true},
		{http.MethodPost, "/api/auth/verify", true},
		{http.MethodOptions, "/api/chat", true}, // preflight defensively exempt
		{http.MethodGet, "/", true},
		{http.MethodGet, "/assets/app.js", true},
		{http.MethodGet, "/index.html", true},
		{http.MethodPost, "/api/chat", false},
		{http.MethodPost, "/api/mcp/servers", false},
		{http.MethodGet, "/api/ws", false},
		{http.MethodGet, "/api/pty/pty_1/ws", false},
		{http.MethodPost, "/api/health", false},     // wrong method → not exempt
		{http.MethodGet, "/api/auth/verify", false}, // wrong method → not exempt
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		if got := isAuthExempt(r); got != c.want {
			t.Errorf("isAuthExempt(%s %s) = %v, want %v", c.method, c.path, got, c.want)
		}
	}
}

func TestAuthMiddleware(t *testing.T) {
	// The inner handler returns 418 so we can tell "passed through" (418) apart
	// from "blocked by middleware" (401).
	const passed = http.StatusTeapot
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(passed) })

	serve := func(s *Server, r *http.Request) int {
		rec := httptest.NewRecorder()
		s.authMiddleware(inner).ServeHTTP(rec, r)
		return rec.Code
	}

	t.Run("auth disabled passes through", func(t *testing.T) {
		s := &Server{requireAuth: false}
		if code := serve(s, httptest.NewRequest(http.MethodPost, "/api/chat", nil)); code != passed {
			t.Fatalf("got %d, want passthrough %d", code, passed)
		}
	})

	s := &Server{requireAuth: true, authToken: "s3cret"}

	t.Run("protected without token → 401", func(t *testing.T) {
		if code := serve(s, httptest.NewRequest(http.MethodPost, "/api/chat", nil)); code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", code)
		}
	})
	t.Run("protected with wrong token → 401", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/chat", nil)
		r.Header.Set("Authorization", "Bearer nope")
		if code := serve(s, r); code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", code)
		}
	})
	t.Run("protected with correct token → passes", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/chat", nil)
		r.Header.Set("Authorization", "Bearer s3cret")
		if code := serve(s, r); code != passed {
			t.Fatalf("got %d, want passthrough %d", code, passed)
		}
	})
	t.Run("websocket subprotocol token → passes", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
		r.Header.Set("Sec-WebSocket-Protocol", "jcode-auth, s3cret")
		if code := serve(s, r); code != passed {
			t.Fatalf("got %d, want passthrough %d", code, passed)
		}
	})
	t.Run("health exempt even with auth on", func(t *testing.T) {
		if code := serve(s, httptest.NewRequest(http.MethodGet, "/api/health", nil)); code != passed {
			t.Fatalf("got %d, want passthrough %d", code, passed)
		}
	})
	t.Run("SPA asset exempt", func(t *testing.T) {
		if code := serve(s, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)); code != passed {
			t.Fatalf("got %d, want passthrough %d", code, passed)
		}
	})
}
