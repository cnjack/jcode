package automation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

const storeVersion = 1

// ErrNotFound is returned (wrapped) when an operation targets an automation id
// that does not exist, so HTTP handlers can map it to 404 rather than 400.
var ErrNotFound = errors.New("automation not found")

// defsFile is the user-edited definitions; stateFile is the volatile scheduler
// bookkeeping; lockFile is the cross-process advisory write lock guarding both.
const (
	defsFile  = "automations.json"
	stateFile = "automation-state.json"
	lockFile  = "automation.lock"
)

type defsDoc struct {
	Version     int           `json:"version"`
	Automations []*Automation `json:"automations"`
}

type stateDoc struct {
	Version int                  `json:"version"`
	State   map[string]*RunState `json:"state"`
}

// Store persists automations across processes. Writes take an OS file lock and
// re-read from disk before mutating, so concurrent jcode processes (web, TUI,
// CLI) never lose updates. Definitions and volatile run-state live in separate
// files so the scheduler's frequent state writes don't collide with human edits.
type Store struct {
	dir       string
	defsPath  string
	statePath string
	lockPath  string

	mu    sync.RWMutex
	defs  map[string]*Automation
	state map[string]*RunState
}

// NewStore opens (and lazily creates) the automation store under ~/.jcode.
func NewStore() (*Store, error) {
	return NewStoreDir(config.ConfigDir())
}

// NewStoreDir opens a store rooted at an explicit directory (used by tests).
func NewStoreDir(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create automation dir: %w", err)
	}
	s := &Store{
		dir:       dir,
		defsPath:  filepath.Join(dir, defsFile),
		statePath: filepath.Join(dir, stateFile),
		lockPath:  filepath.Join(dir, lockFile),
		defs:      map[string]*Automation{},
		state:     map[string]*RunState{},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// loadLocked reads both files from disk into memory. A missing file is empty,
// not an error; an unparseable file is logged and treated as empty so a corrupt
// state file can never block startup.
func (s *Store) loadLocked() error {
	s.defs = map[string]*Automation{}
	s.state = map[string]*RunState{}

	if b, err := os.ReadFile(s.defsPath); err == nil {
		var doc defsDoc
		if jerr := json.Unmarshal(b, &doc); jerr != nil {
			config.Logger().Printf("[automation] corrupt %s, ignoring: %v", defsFile, jerr)
		} else {
			for _, a := range doc.Automations {
				if a != nil && a.ID != "" {
					s.defs[a.ID] = a
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if b, err := os.ReadFile(s.statePath); err == nil {
		var doc stateDoc
		if jerr := json.Unmarshal(b, &doc); jerr != nil {
			config.Logger().Printf("[automation] corrupt %s, rebuilding: %v", stateFile, jerr)
		} else {
			for id, st := range doc.State {
				if st != nil {
					s.state[id] = st
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) persistDefsLocked() error {
	doc := defsDoc{Version: storeVersion, Automations: s.listLocked()}
	return writeJSONAtomic(s.defsPath, doc)
}

func (s *Store) persistStateLocked() error {
	doc := stateDoc{Version: storeVersion, State: s.state}
	return writeJSONAtomic(s.statePath, doc)
}

// withLock runs fn while holding the cross-process write lock with a fresh
// disk-synced view. fn mutates s.defs/s.state in memory; the requested files are
// then persisted atomically.
func (s *Store) withLock(persistDefs, persistState bool, fn func() error) error {
	lock, err := acquireLock(s.lockPath)
	if err != nil {
		return fmt.Errorf("lock automation store: %w", err)
	}
	defer func() { _ = lock.release() }()

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	if persistDefs {
		if err := s.persistDefsLocked(); err != nil {
			return err
		}
	}
	if persistState {
		if err := s.persistStateLocked(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) listLocked() []*Automation {
	out := make([]*Automation, 0, len(s.defs))
	for _, a := range s.defs {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// List returns all automations sorted by creation time.
func (s *Store) List() []*Automation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.listLocked()
	cp := make([]*Automation, len(out))
	for i, a := range out {
		c := *a
		cp[i] = &c
	}
	return cp
}

// Get returns a copy of the automation, or nil if not found.
func (s *Store) Get(id string) *Automation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if a, ok := s.defs[id]; ok {
		c := *a
		return &c
	}
	return nil
}

// State returns a copy of the run-state for an automation (zero value if none).
func (s *Store) State(id string) RunState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if st, ok := s.state[id]; ok {
		return *st
	}
	return RunState{}
}

// Create validates, assigns id/timestamps/defaults, and persists a new
// automation. The input is copied; the stored automation is returned.
func (s *Store) Create(a Automation) (*Automation, error) {
	if a.Mode == "" {
		a.Mode = "full_access"
	}
	if err := ValidateAutomation(&a); err != nil {
		return nil, err
	}
	now := nowFunc().Format(time.RFC3339)
	a.ID = newID()
	a.CreatedAt = now
	a.UpdatedAt = now
	if a.Source == "" {
		a.Source = SourceManual
	}
	a.RunInCloud = false // reserved; never honored in v1

	stored := a
	err := s.withLock(true, false, func() error {
		s.defs[stored.ID] = &stored
		return nil
	})
	if err != nil {
		return nil, err
	}
	c := stored
	return &c, nil
}

// Update applies a mutation to an existing automation under lock, re-validating
// the result. The mutate callback receives a pointer it may modify in place.
//
// Re-enabling (Enabled false -> true) also clears ConsecutiveFails so a
// recovered automation isn't immediately re-disabled by the next single
// failure. Centralizing the reset here means EVERY enable path gets it — the
// web UI's partial-patch PUT (handleUpdateAutomation), the CLI's SetEnabled, and
// any future caller — not just SetEnabled.
func (s *Store) Update(id string, mutate func(*Automation)) (*Automation, error) {
	var out *Automation
	err := s.withLock(true, true, func() error {
		a, ok := s.defs[id]
		if !ok {
			return fmt.Errorf("automation %q: %w", id, ErrNotFound)
		}
		wasEnabled := a.Enabled
		cp := *a
		mutate(&cp)
		cp.ID = id // immutable
		cp.UpdatedAt = nowFunc().Format(time.RFC3339)
		if err := ValidateAutomation(&cp); err != nil {
			return err
		}
		s.defs[id] = &cp
		out = &cp
		if !wasEnabled && cp.Enabled {
			st := s.state[id]
			if st == nil {
				st = &RunState{}
			} else {
				c := *st
				st = &c
			}
			st.ConsecutiveFails = 0
			s.state[id] = st
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	c := *out
	return &c, nil
}

// SetEnabled flips the enabled flag (re-enabling clears ConsecutiveFails via
// Update's shared reset, so a recovered automation isn't immediately
// re-disabled by the next single failure).
func (s *Store) SetEnabled(id string, enabled bool) (*Automation, error) {
	return s.Update(id, func(a *Automation) { a.Enabled = enabled })
}

// Delete removes an automation and its run-state.
func (s *Store) Delete(id string) error {
	return s.withLock(true, true, func() error {
		if _, ok := s.defs[id]; !ok {
			return fmt.Errorf("automation %q: %w", id, ErrNotFound)
		}
		delete(s.defs, id)
		delete(s.state, id)
		return nil
	})
}

// UpdateState mutates only the volatile run-state file (never the definitions),
// so scheduler/run-completion writes can never clobber a concurrent human edit.
func (s *Store) UpdateState(id string, mutate func(*RunState)) error {
	return s.withLock(false, true, func() error {
		st := s.state[id]
		if st == nil {
			st = &RunState{}
		} else {
			cp := *st
			st = &cp
		}
		mutate(st)
		s.state[id] = st
		return nil
	})
}

// TryMarkRunning atomically claims a run for id: it sets LastStatus=running
// (clearing LastError, stamping LastRunAt) only if a run is not ALREADY in
// progress, returning whether the claim succeeded. This is the single
// authoritative guard against overlapping runs across the scheduler, manual
// "Run Now", and other processes — the local in-flight maps are only fast-path
// hints that can't see each other or another process. A crashed run left at
// "running" is cleared by the scheduler's reconcileStale on the next election.
func (s *Store) TryMarkRunning(id string) (bool, error) {
	claimed := false
	err := s.withLock(false, true, func() error {
		st := s.state[id]
		if st != nil && st.LastStatus == StatusRunning {
			return nil // already running; do not clobber the live run's state
		}
		if st == nil {
			st = &RunState{}
		} else {
			cp := *st
			st = &cp
		}
		st.LastStatus = StatusRunning
		st.LastError = ""
		st.LastRunAt = nowFunc().Format(time.RFC3339)
		s.state[id] = st
		claimed = true
		return nil
	})
	return claimed, err
}

// UpdateStateAndMaybeDisable mutates the run-state (e.g. recording a failure and
// bumping ConsecutiveFails) and, in the SAME lock scope, disables the definition
// when ConsecutiveFails has reached AutoDisableThreshold. Folding the disable
// into the run-state mutation closes the TOCTOU window that exists when the
// disable is a separate SetEnabled(false) call: a concurrent successful run can
// no longer reset ConsecutiveFails between the threshold check and the disable.
// Returns whether the definition was disabled by this call.
func (s *Store) UpdateStateAndMaybeDisable(id string, mutate func(*RunState)) (bool, error) {
	disabled := false
	err := s.withLock(true, true, func() error {
		st := s.state[id]
		if st == nil {
			st = &RunState{}
		} else {
			cp := *st
			st = &cp
		}
		mutate(st)
		s.state[id] = st
		if st.ConsecutiveFails >= AutoDisableThreshold {
			if a, ok := s.defs[id]; ok && a.Enabled {
				cp := *a
				cp.Enabled = false
				cp.UpdatedAt = nowFunc().Format(time.RFC3339)
				s.defs[id] = &cp
				disabled = true
			}
		}
		return nil
	})
	return disabled, err
}

// ---- helpers ----

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// extremely unlikely; fall back to a time-derived id
		return strings.ReplaceAll(nowFunc().Format("150405.000000"), ".", "")
	}
	return hex.EncodeToString(b[:])
}
