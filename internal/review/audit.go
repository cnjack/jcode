package review

import (
	"encoding/json"
	"os"
	"regexp"
	"sync"
	"time"
)

// auditRecord is one line in the approval-review audit log. It records every
// reviewer verdict (including failures that fall back to the user) so decisions
// can be replayed and debugged, and so tests can assert on real behavior rather
// than the agent's self-report.
type auditRecord struct {
	TS         string `json:"ts"`
	Tool       string `json:"tool"`
	Args       string `json:"args"`
	Cwd        string `json:"cwd"`
	IsExternal bool   `json:"is_external"`
	Decision   string `json:"decision"` // allow | deny | escalate
	Risk       string `json:"risk,omitempty"`
	UserAuth   string `json:"user_auth,omitempty"`
	Rationale  string `json:"rationale,omitempty"`
	Failed     bool   `json:"failed,omitempty"` // review could not complete
	FailReason string `json:"fail_reason,omitempty"`
	Model      string `json:"model,omitempty"`
	LatencyMS  int64  `json:"latency_ms"`
	// Cache observability (V3): reviewer-call token accounting, used to confirm
	// the reviewer's own prompt cache is being hit without touching the main
	// conversation's cache.
	PromptTokens int64 `json:"prompt_tokens,omitempty"`
	CachedTokens int64 `json:"cached_tokens,omitempty"`
	CacheSeen    bool  `json:"cache_seen,omitempty"`
	Investigated bool  `json:"investigated,omitempty"` // V2: used read-only tools
	ReviewCalls  int64 `json:"review_calls,omitempty"` // LLM calls this review consumed
	// Prefilter names the deterministic rule that decided this call without
	// consulting the model (see ssrf.go); empty for normal model verdicts.
	Prefilter string `json:"prefilter,omitempty"`
}

// auditArgsCap bounds how much of the tool arguments the audit log stores. The
// leading portion is what matters for a shell command or file path; a giant
// pasted blob is not worth the disk.
const auditArgsCap = 2000

// redactedPlaceholder replaces a matched secret value.
const redactedPlaceholder = "«redacted»"

// secretPatterns match credential material that commonly appears inline in the
// very commands this reviewer exists to adjudicate (curl with an auth header, a
// CLI --password flag, an exported token). The audit log is append-only and
// long-lived, so writing those verbatim would turn the debugging trail into a
// durable plaintext credential store — 0600 perms do not survive a backup, a
// synced config dir, or another process running as the same user.
//
// This is best-effort defense-in-depth, not a guarantee: it cannot catch an
// arbitrary opaque secret with no surrounding syntax. The value of the audit log
// is the decision and the shape of the command, not the secret itself.
var secretPatterns = []*regexp.Regexp{
	// Authorization: Bearer <token> / Basic <blob>, in a header or flag.
	regexp.MustCompile(`(?i)(authorization\s*:\s*(?:bearer|basic|token)\s+)\S+`),
	// -H 'X-...-Token: v' / api-key: v / x-api-key: v
	regexp.MustCompile(`(?i)((?:x-)?(?:api[-_]?key|auth[-_]?token|access[-_]?token|secret)\s*[:=]\s*)\S+`),
	// --password=v, --token v, -p v (long-form flags only; -p alone is too noisy)
	regexp.MustCompile(`(?i)(--(?:password|passwd|token|secret|api[-_]?key)(?:[=\s]+))\S+`),
	// KEY=value env assignments for secret-ish names.
	regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:PASSWORD|SECRET|TOKEN|APIKEY|API_KEY|ACCESS_KEY)[A-Z0-9_]*\s*=\s*)\S+`),
	// Well-known key prefixes (GitHub, Slack, AWS, OpenAI-style, private keys).
	regexp.MustCompile(`\b(gh[pousr]_)[A-Za-z0-9]{16,}`),
	regexp.MustCompile(`\b(xox[baprs]-)[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\b(AKIA)[A-Z0-9]{12,}`),
	regexp.MustCompile(`\b(sk-)[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`(-----BEGIN [A-Z ]*PRIVATE KEY-----)[\s\S]*?(-----END [A-Z ]*PRIVATE KEY-----)`),
}

// redactSecrets masks credential-shaped values while preserving the surrounding
// command so the audit trail stays readable.
func redactSecrets(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "${1}"+redactedPlaceholder)
	}
	return s
}

// auditLog appends verdict records to a JSONL file. Writes are serialized and
// best-effort: an audit failure must never change an approval outcome.
type auditLog struct {
	mu   sync.Mutex
	path string
}

func newAuditLog(path string) *auditLog {
	return &auditLog{path: path}
}

func (a *auditLog) write(rec auditRecord) {
	if a == nil || a.path == "" {
		return
	}
	// Redact before truncating: truncation could otherwise slice a secret in half
	// and leave the leading portion unmatched by the patterns.
	rec.Args = redactSecrets(rec.Args)
	// The rationale is model-authored and can quote the command it judged.
	rec.Rationale = redactSecrets(rec.Rationale)
	if len(rec.Args) > auditArgsCap {
		rec.Args = rec.Args[:auditArgsCap] + "…"
	}
	if rec.TS == "" {
		rec.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(line, '\n'))
}
