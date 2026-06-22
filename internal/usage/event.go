// Package usage records and aggregates token-usage statistics across all jcode
// surfaces (TUI, web, ACP). It uses an append-only JSON-lines event log
// (~/.jcode/usage/events.jsonl), one line per agent turn. Append-only writes
// are atomic for small records under O_APPEND, so multiple jcode processes can
// record concurrently without a read-modify-write race; all derived metrics are
// computed at read time by Aggregate.
package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

// Event is a single recorded agent turn's token usage. Token fields are the
// per-turn delta (not cumulative).
type Event struct {
	TS         int64  `json:"ts"`   // unix seconds
	Date       string `json:"date"` // YYYY-MM-DD, local time
	Project    string `json:"project,omitempty"`
	Session    string `json:"session,omitempty"` // session UUID
	Model      string `json:"model,omitempty"`
	Prompt     int    `json:"prompt"`
	Completion int    `json:"completion"`
	Cached     int    `json:"cached"`
	Reasoning  int    `json:"reasoning,omitempty"`
	CacheWrite int    `json:"cache_write,omitempty"`
	Total      int    `json:"total"`
	Calls      int    `json:"calls,omitempty"` // API calls in this turn
	// CacheSeen is true when the provider reported a prompt_tokens_details object
	// during the turn (caching supported), even if cached==0. Lets stats show "—"
	// vs a real 0% without conflating "unsupported" with "supported, no hit".
	// Absent in events written before this field existed (defaults to false, with
	// a Cached>0 fallback in Aggregate).
	CacheSeen bool `json:"cache_seen,omitempty"`
}

// RecordEvent stamps ev with the current time and appends it to the default
// store. Callers fill in the session/project/model + token deltas; TS/Date are
// set here. Best-effort: errors are swallowed so stats never break a run.
func RecordEvent(ev Event) {
	ts := time.Now()
	ev.TS = ts.Unix()
	ev.Date = ts.Format(dateLayout)
	_ = Default().Record(ev)
}

// Store is an append-only event-log writer/reader.
type Store struct {
	path string
	mu   sync.Mutex // serialises in-process appends
}

// NewStore returns a Store backed by the given file path.
func NewStore(path string) *Store { return &Store{path: path} }

var (
	defaultStore *Store
	defaultOnce  sync.Once
)

// Default returns the process-wide store bound to ~/.jcode/usage/events.jsonl.
// If the path cannot be resolved, the returned store is a no-op.
func Default() *Store {
	defaultOnce.Do(func() {
		path, err := config.UsageEventsPath()
		if err != nil {
			path = ""
		}
		defaultStore = &Store{path: path}
	})
	return defaultStore
}

// Record appends one event. Turns with no token usage are dropped. A nil or
// pathless store is a no-op so callers never need to guard.
func (s *Store) Record(ev Event) error {
	if s == nil || s.path == "" {
		return nil
	}
	if ev.Total <= 0 && ev.Prompt <= 0 && ev.Completion <= 0 {
		return nil
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(line)
	return err
}

// Load reads all events with Date >= since (YYYY-MM-DD). An empty since loads
// everything. A missing log file yields an empty slice, not an error.
// Malformed lines are skipped so a single bad write can't break stats.
func (s *Store) Load(since string) ([]Event, error) {
	if s == nil || s.path == "" {
		return nil, nil
	}
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev Event
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if since != "" && ev.Date < since {
			continue
		}
		out = append(out, ev)
	}
	return out, sc.Err()
}
