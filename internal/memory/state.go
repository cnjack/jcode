package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// State is the per-scope coordination file (state.json). It replaces the
// SQLite database Codex uses: entry counts are in the thousands at most, and
// flock + atomic rename matches the concurrency conventions of
// internal/session and internal/automation.
type State struct {
	Version int `json:"version"`
	// Files tracks read-usage per memory file (scope-root-relative path).
	// Consolidation ranks by usage and expires long-unused entries.
	Files map[string]*FileUsage `json:"files,omitempty"`
	// Extracted tracks phase-1 work per source session UUID (M2).
	Extracted map[string]*ExtractRecord `json:"extracted,omitempty"`
	// Budget is the pipeline token ledger per day ("2026-07-04" → tokens).
	Budget map[string]int64 `json:"budget,omitempty"`
	// LastConsolidation records the most recent phase-2 outcome (M3).
	LastConsolidation *ConsolidationRecord `json:"last_consolidation,omitempty"`
	// LastPipelineAt is when the pipeline last ran (cooldown gate). RFC3339.
	LastPipelineAt string `json:"last_pipeline_at,omitempty"`
}

// FileUsage is the usage-feedback loop: bumped whenever the agent reads a
// memory file (see UsageMiddleware), consumed by consolidation ranking.
type FileUsage struct {
	UsageCount int    `json:"usage_count"`
	LastUsage  string `json:"last_usage,omitempty"` // RFC3339
}

// ExtractRecord tracks one extracted session (phase 1, M2).
type ExtractRecord struct {
	At          string `json:"at"` // RFC3339
	SummaryFile string `json:"summary_file,omitempty"`
	UsageCount  int    `json:"usage_count"`
	LastUsage   string `json:"last_usage,omitempty"`
	Failed      bool   `json:"failed,omitempty"`
	FailCount   int    `json:"fail_count,omitempty"` // consecutive extraction failures (backoff)
	Error       string `json:"error,omitempty"`
}

// ConsolidationRecord summarizes a phase-2 run (M3). Decisions holds the
// ADD/UPDATE/DELETE/NOOP protocol output so runs are assertable.
type ConsolidationRecord struct {
	At           string         `json:"at"` // RFC3339
	NoopFastPath bool           `json:"noop_fast_path"`
	Decisions    map[string]int `json:"decisions,omitempty"` // op → count
	Commit       string         `json:"commit,omitempty"`
}

func statePath(scopeRoot string) string { return filepath.Join(scopeRoot, StateFile) }
func lockPath(scopeRoot string) string  { return filepath.Join(scopeRoot, ".state.lock") }

// TryLockPipeline takes the scope's non-blocking pipeline lock. Returns a
// release func and whether the lock was acquired (false = another process is
// already running the pipeline).
func TryLockPipeline(scopeRoot string) (func(), bool, error) {
	if err := os.MkdirAll(scopeRoot, 0o755); err != nil {
		return nil, false, err
	}
	l, ok, err := tryAcquireLock(filepath.Join(scopeRoot, ".pipeline.lock"))
	if err != nil || !ok {
		return func() {}, ok, err
	}
	return l.release, true, nil
}

// ClearScope removes a scope's memory directory, coordinating with the pipeline
// lock so a running distillation cannot resurrect a half-cleared scope.
//
// It reports busy=true (deleting nothing) if the pipeline currently holds the
// lock — the caller should ask the user to retry. Otherwise it holds the lock
// across the delete (a concurrent pipeline's non-blocking TryLockPipeline keeps
// failing), which closes the release-then-delete race the naive version had.
// On Windows RemoveAll can hit a sharing violation on the still-open lock file;
// once the handle is released a retry succeeds, so we release then retry.
func ClearScope(scopeRoot string) (busy bool, err error) {
	release, ok, lerr := TryLockPipeline(scopeRoot)
	if lerr == nil && !ok {
		return true, nil
	}
	err = os.RemoveAll(scopeRoot)
	if release != nil {
		release()
	}
	if err != nil {
		// Retry after the lock handle is closed (Windows).
		err = os.RemoveAll(scopeRoot)
	}
	return false, err
}

// LoadState reads state.json without locking (callers that mutate must use
// UpdateState). A missing or corrupt file yields a fresh state rather than an
// error: memory must never take the agent down.
func LoadState(scopeRoot string) *State {
	st := &State{Version: 1}
	data, err := os.ReadFile(statePath(scopeRoot))
	if err == nil {
		_ = json.Unmarshal(data, st)
	}
	if st.Version == 0 {
		st.Version = 1
	}
	if st.Files == nil {
		st.Files = map[string]*FileUsage{}
	}
	return st
}

// UpdateState applies fn to the scope's state under an exclusive file lock
// and persists the result atomically. Lost updates are prevented by
// re-reading inside the lock.
func UpdateState(scopeRoot string, fn func(*State) error) error {
	if err := os.MkdirAll(scopeRoot, 0o755); err != nil {
		return err
	}
	lock, err := acquireLock(lockPath(scopeRoot))
	if err != nil {
		return err
	}
	defer lock.release()

	st := LoadState(scopeRoot)
	if err := fn(st); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(statePath(scopeRoot), data)
}

// RecordUsage bumps the usage counter for a memory file. absPath must be an
// absolute path somewhere under Root(); anything else is silently ignored so
// the middleware can call this unconditionally.
func RecordUsage(absPath string) {
	root := Root()
	rel, err := filepath.Rel(root, absPath)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) ||
		len(rel) > 0 && rel[0] == '.' {
		return
	}
	// rel is like "projects/<slug>/notes/x.md" or "global/MEMORY.md":
	// scope root is the first path element (plus slug for projects).
	parts := splitPath(rel)
	var scopeRoot, inScope string
	switch {
	case len(parts) >= 3 && parts[0] == "projects":
		scopeRoot = filepath.Join(root, parts[0], parts[1])
		inScope = filepath.Join(parts[2:]...)
	case len(parts) >= 2 && parts[0] == "global":
		scopeRoot = filepath.Join(root, parts[0])
		inScope = filepath.Join(parts[1:]...)
	default:
		return
	}
	if inScope == StateFile || inScope == filepath.Base(lockPath("")) {
		return
	}
	now := time.Now().Format(time.RFC3339)
	_ = UpdateState(scopeRoot, func(st *State) error {
		u := st.Files[inScope]
		if u == nil {
			u = &FileUsage{}
			st.Files[inScope] = u
		}
		u.UsageCount++
		u.LastUsage = now
		// Consolidation ranking joins this st.Files entry back to its source
		// session via ExtractRecord.SummaryFile (see pipeline.expireAndRank);
		// no separate write to st.Extracted is needed.
		return nil
	})
}

func splitPath(p string) []string {
	var parts []string
	for _, seg := range splitSlash(filepath.ToSlash(p)) {
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	return parts
}

func splitSlash(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
