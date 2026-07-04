package memory

import "regexp"

// Redact masks common credential shapes before anything is persisted to the
// memory store. It runs on memory_note input, phase-1 pipeline input and
// output (see design §6.1). Idempotent: redacted text passes through
// unchanged.
func Redact(s string) string {
	for _, r := range redactRules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

const redacted = "[REDACTED]"

type redactRule struct {
	re   *regexp.Regexp
	repl string
}

// secret-bearing key names, used by both the JSON-quoted and bare assignment
// rules below. Ordering matters only for readability.
const secretKeyNames = `api[_-]?key|apikey|access[_-]?key(?:[_-]?id)?|secret[_-]?access[_-]?key|secret[_-]?key|client[_-]?secret|access[_-]?token|refresh[_-]?token|auth[_-]?token|secret|token|password|passwd|passphrase`

var redactRules = []redactRule{
	// Private key blocks.
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), redacted},
	// URL-embedded credentials: scheme://user:pass@host → keep user, mask pass.
	// The password class allows everything except '@' and whitespace so that
	// passwords containing '/' or ':' are still fully masked.
	{regexp.MustCompile(`\b([a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]+):[^@\s]+@`), "${1}:" + redacted + "@"},
	// Vendor-prefixed tokens. sk- covers OpenAI/Anthropic/Stripe-style keys.
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{10,}\b`), redacted},
	// Classic gh?_ tokens AND the newer fine-grained github_pat_ shape.
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`), redacted},
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{16,}\b`), redacted},
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), redacted},
	{regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`), redacted},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}\b`), redacted},
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`), "Bearer " + redacted},
	// JSON-quoted assignments: "api_key": "value" — the quoted key means no
	// separator sits directly after the key word, so this needs its own rule.
	{regexp.MustCompile(`(?i)("(?:` + secretKeyNames + `)")(\s*:\s*)"[^"]{4,}"`), "${1}${2}\"" + redacted + "\""},
	// Bare assignments: api_key=..., SECRET_ACCESS_KEY: .... Keeps the key
	// name, masks the value. Requires an explicit separator so prose like
	// "token budget" is untouched. Key allows surrounding [A-Z_] segments so
	// AWS_SECRET_ACCESS_KEY etc. match despite the underscore word chars.
	{regexp.MustCompile(`(?i)\b([a-z0-9]*_)?(` + secretKeyNames + `)(\s*[:=]\s*)(["']?)[^\s"']{6,}(["']?)`), "${1}${2}${3}${4}" + redacted + "${5}"},
}
