// Package ssrf is a dependency-free leaf holding the canonical list of cloud
// instance-metadata host indicators and the host-only matcher built from it.
//
// It exists so the two packages that need to recognize metadata destinations —
// the approval reviewer (internal/review) and the WebFetch tool's dial guard
// (internal/tools) — share one list instead of drifting, WITHOUT introducing an
// import cycle (internal/review already imports internal/tools, so tools cannot
// import review). Nothing in this package may import either of them.
package ssrf

import "strings"

// MetadataHosts are literal needles for cloud instance-metadata endpoints,
// including the common numeric obfuscations of 169.254.169.254 that bypass a
// naive dotted-quad match. Matched case-insensitively as substrings.
var MetadataHosts = []string{
	"169.254.",      // IPv4 link-local: IMDS (…169.254), ECS task (…170.2)
	"fd00:ec2::254", // AWS IMDS over IPv6
	"metadata.google.internal",
	"metadata.goog",
	"100.100.100.200", // Alibaba Cloud
	"192.0.0.192",     // Oracle Cloud
	// Numeric encodings of 169.254.169.254 — curl/wget accept all of these.
	"2852039166",          // decimal
	"0xa9fea9fe",          // hex
	"0xa9.0xfe.0xa9.0xfe", // dotted hex
	"0251.0376.0251.0376", // dotted octal
	"0251.0376.43518",     // mixed octal/decimal
}

// IsMetadataHost reports whether host is a known cloud instance-metadata host
// (or a numeric obfuscation of 169.254.169.254). host is matched
// case-insensitively; pass the bare host (no scheme/port).
func IsMetadataHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return false
	}
	for _, n := range MetadataHosts {
		if strings.Contains(h, n) {
			return true
		}
	}
	return false
}
