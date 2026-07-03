package browser

import (
	"net/url"
	"strings"
)

// OriginOf returns the scheme://host[:port] origin of a raw URL, or "" when it
// cannot be parsed (e.g. about:blank).
func OriginOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// IsLocalOrigin reports whether an origin points at the local machine — the
// primary browser-use case (localhost dev-loop). Local targets get lighter
// treatment in some UIs but still follow the same approval tiers.
func IsLocalOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	return strings.HasSuffix(host, ".localhost")
}
