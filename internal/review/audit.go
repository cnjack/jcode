package review

import (
	"encoding/json"
	"os"
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
}

// auditArgsCap bounds how much of the tool arguments the audit log stores. The
// leading portion is what matters for a shell command or file path; a giant
// pasted blob is not worth the disk.
const auditArgsCap = 2000

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
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}
