package web

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

// wsAuthSubprotocol is the WebSocket subprotocol name under which the bearer
// token rides on handshakes. Browsers cannot set custom headers on WebSocket
// connections, so the frontend sends ["jcode-auth", "<token>"] and the token is
// read from the second value. The server also advertises this subprotocol on
// the upgrader so gorilla echoes the protocol name back and the handshake
// completes cleanly.
const wsAuthSubprotocol = "jcode-auth"

// IsLoopbackBind reports whether the given bind host is loopback-only.
//
// An empty host, "0.0.0.0", or "::" binds all interfaces and is treated as
// exposed (non-loopback). "localhost" and any IP whose IsLoopback() is true are
// loopback. A hostname we cannot resolve statically is conservatively treated as
// exposed, so we fail safe (require auth) rather than fail open.
func IsLoopbackBind(host string) bool {
	switch host {
	case "", "0.0.0.0", "::":
		return false
	case "localhost":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false // unknown hostname → assume exposed, require auth
}

// extractToken pulls the bearer token from a request, in priority order:
//  1. Authorization: Bearer <token>
//  2. Sec-WebSocket-Protocol: jcode-auth, <token>  (browser WebSocket handshakes
//     can't carry custom headers, so the token rides as the second subprotocol)
//  3. ?token=<token>  (fallback for non-browser ws clients; discouraged because
//     it lands in access logs, proxy logs and browser history)
func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if protos := r.Header.Get("Sec-WebSocket-Protocol"); protos != "" {
		parts := strings.Split(protos, ",")
		for i, p := range parts {
			if strings.TrimSpace(p) == wsAuthSubprotocol && i+1 < len(parts) {
				return strings.TrimSpace(parts[i+1])
			}
		}
	}
	// The ?token= fallback exists only for non-browser WebSocket clients that can
	// set neither a header nor a subprotocol. Restrict it to the WS endpoints so
	// bearer tokens for normal HTTP APIs never land in access/proxy logs, the
	// Referer header, or browser history.
	if acceptsQueryToken(r.URL.Path) {
		return r.URL.Query().Get("token")
	}
	return ""
}

// acceptsQueryToken reports whether the ?token= fallback is allowed for a path —
// only the WebSocket endpoints (the main event stream and the PTY sockets).
func acceptsQueryToken(path string) bool {
	return path == "/api/ws" || (strings.HasPrefix(path, "/api/pty/") && strings.HasSuffix(path, "/ws"))
}

// IsValidWSSubprotocolToken reports whether s is usable as a WebSocket
// subprotocol value (RFC 6455 / RFC 7230 token): non-empty, printable ASCII with
// no spaces or separators. The frontend sends the token as the second WS
// subprotocol, so an explicit token containing such characters would make the
// browser's WebSocket constructor throw and break the connection at startup.
func IsValidWSSubprotocolToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 0x21 || r > 0x7e {
			return false // control chars, space, and non-ASCII
		}
		switch r {
		case '(', ')', '<', '>', '@', ',', ';', ':', '\\', '"', '/', '[', ']', '?', '=', '{', '}':
			return false // RFC 7230 separators
		}
	}
	return true
}

// validToken compares the provided token against the expected one in constant
// time. An empty expected or provided token never validates.
func validToken(provided, expected string) bool {
	if expected == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

// isAuthExempt reports whether a request may proceed without a token even when
// auth is required. It is a POSITIVE allowlist rather than a "/api/* needs auth"
// rule, so an unregistered /api path that falls through to the SPA handler can't
// sneak past: anything under /api/ requires a token unless explicitly listed.
func isAuthExempt(r *http.Request) bool {
	if r.Method == http.MethodOptions {
		return true // defensive: preflight is normally short-circuited by corsMiddleware
	}
	p := r.URL.Path
	if r.Method == http.MethodGet && p == "/api/health" {
		return true // the frontend probes this before it has a token
	}
	if r.Method == http.MethodPost && p == "/api/auth/verify" {
		return true // the endpoint the login page calls to validate a typed token
	}
	// Everything outside /api/ is the SPA shell + embedded static assets: the
	// login page itself must load before the user has a token.
	return !strings.HasPrefix(p, "/api/")
}

// authMiddleware enforces token auth when requireAuth is set. corsMiddleware
// MUST wrap this — corsMiddleware(s.authMiddleware(mux)) — so OPTIONS preflights
// are answered by cors and never reach here without an Authorization header.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.requireAuth || isAuthExempt(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !validToken(extractToken(r), s.authToken) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleAuthVerify lets the login page check a token the user typed in. It reads
// the token the same way the middleware does and returns 200 on a match, 401
// otherwise. When auth is not required it always succeeds.
func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth || validToken(extractToken(r), s.authToken) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
}
