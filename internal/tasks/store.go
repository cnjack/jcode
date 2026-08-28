package tasks

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cnjack/jcode/internal/config"
)

// Sentinel errors callers can classify with errors.Is.
var (
	// ErrNotFound: no task with that reference exists on this machine.
	ErrNotFound = errors.New("task not found")
	// ErrCrossProject: the reference exists but belongs to a different
	// project — reading it would cross the project boundary.
	ErrCrossProject = errors.New("task belongs to a different project")
	// ErrArchived: soft-deleted task; readable metadata, no mutations.
	ErrArchived = errors.New("task is archived")
	// ErrTerminal: the task already reached a final state.
	ErrTerminal = errors.New("task already finished")
	// ErrAmbiguous: a name matched more than one visible task.
	ErrAmbiguous = errors.New("ambiguous task name")
)

const (
	privateTaskDirMode  os.FileMode = 0o700
	privateTaskFileMode os.FileMode = 0o600
)

// Store is a project-scoped view over the persistent task registry root.
// All List/Resolve operations only see this project's tasks; Get refuses
// references that live under other projects.
type Store struct {
	root    string
	project string
	dir     string // root/<projectHash>

	host string
	pid  int

	mu sync.Mutex
}

// NewStore opens (creating if needed) the registry for a project rooted at
// root. project must be the absolute workspace path.
func NewStore(root, project string) (*Store, error) {
	if strings.TrimSpace(project) == "" {
		return nil, fmt.Errorf("tasks: project path must not be empty")
	}
	dir := filepath.Join(root, projectDirName(project))
	if err := os.MkdirAll(dir, privateTaskDirMode); err != nil {
		return nil, fmt.Errorf("tasks: create registry dir: %w", err)
	}
	if err := os.Chmod(dir, privateTaskDirMode); err != nil {
		return nil, fmt.Errorf("tasks: secure registry dir: %w", err)
	}
	host, _ := os.Hostname()
	return &Store{
		root:    root,
		project: project,
		dir:     dir,
		host:    host,
		pid:     os.Getpid(),
	}, nil
}

// OpenDefault opens the default registry (~/.jcode/tasks) for a project.
func OpenDefault(project string) (*Store, error) {
	root, err := config.TasksDir()
	if err != nil {
		return nil, err
	}
	return NewStore(root, project)
}

// Project returns the project path the store is scoped to.
func (s *Store) Project() string { return s.project }

// Hostname returns the machine identity stamped into records this store
// creates (used for crash detection and remote-origin display).
func (s *Store) Hostname() string { return s.host }

// PID returns the process id stamped into records this store creates.
func (s *Store) PID() int { return s.pid }

func projectDirName(project string) string {
	return sha256Sum(project)[:16]
}

// Create mints a new durable task record and returns its snapshot.
// When in.Key is set and a task with the same create key already exists in
// this project, the original record is returned (idempotent create).
func (s *Store) Create(in CreateInput) (*Record, error) {
	name, err := ValidateName(in.Name)
	if err != nil {
		return nil, err
	}
	if in.Kind == "" {
		in.Kind = KindWorkItem
	}
	if in.Origin == "" {
		in.Origin = OriginLocal
	}
	if in.Hostname == "" {
		in.Hostname = s.host
	}
	if in.OwnerPID == 0 {
		in.OwnerPID = s.pid
	}

	if in.Key != "" {
		s.mu.Lock()
		rec, keyErr := s.findByCreateKeyLocked(in.Key)
		s.mu.Unlock()
		if keyErr == nil {
			return rec, nil
		}
	}

	ref := NewRef()
	if !ValidateRef(ref) { // unreachable; guards a broken rand fallback
		return nil, fmt.Errorf("tasks: generated invalid ref %q", ref)
	}
	ev := Event{
		ID:          ref,
		Type:        EventCreated,
		Time:        time.Now().UTC(),
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		Kind:        in.Kind,
		SessionID:   in.SessionID,
		Origin:      in.Origin,
		AgentType:   in.AgentType,
		Model:       in.Model,
		LocalID:     in.LocalID,
		OwnerPID:    in.OwnerPID,
		Hostname:    in.Hostname,
		CreateKey:   in.Key,
	}
	if err := s.mutate(ref, func() error {
		return s.appendEventLocked(ref, ev)
	}); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.readLocked(ref)
	if err != nil {
		return nil, err
	}
	return s.correctZombie(rec), nil
}

// Get returns the snapshot for ref, enforcing the project boundary.
func (s *Store) Get(ref string) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.readLocked(ref)
	if err == nil {
		return s.correctZombie(rec), nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// Not in this project: refuse loudly if it exists elsewhere on this
	// machine — never silently serve cross-project data.
	if s.refExistsOutsideProject(ref) {
		return nil, fmt.Errorf("%w: %s (visible to its own project only)", ErrCrossProject, ref)
	}
	return nil, fmt.Errorf("%w: %s (it may belong to another session, a different machine, or a remote/cloud executor)", ErrNotFound, ref)
}

// List returns this project's tasks, newest activity first. An empty filter
// returns all tasks.
func (s *Store) List(filter Status) ([]*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.listLocked(filter)
	if err != nil {
		return nil, fmt.Errorf("tasks: read registry: %w", err)
	}
	return out, nil
}

// Message appends a message to a task's timeline and returns the updated
// snapshot. Delivery is exactly-once for a given key: concurrent or retried
// calls with the same key (even from different jcode processes) append a
// single event.
func (s *Store) Message(ref, from, fromRole, body, key string) (*Record, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("tasks: message body must not be empty")
	}
	if len(body) > 64*1024 {
		body = body[:64*1024]
	}

	var out *Record
	err := s.mutate(ref, func() error {
		rec, err := s.readLocked(ref)
		if err != nil {
			if errors.Is(err, ErrNotFound) && s.refExistsOutsideProject(ref) {
				return fmt.Errorf("%w: %s", ErrCrossProject, ref)
			}
			return err
		}
		if rec.Status == StatusArchived {
			return fmt.Errorf("%w: %s was archived", ErrArchived, ref)
		}
		if rec.Status.Terminal() {
			return fmt.Errorf("%w: %s is %s (ended %s); create a new task instead",
				ErrTerminal, ref, rec.Status, formatTime(rec.EndedAt))
		}
		if key != "" {
			for _, ev := range rec.Timeline {
				if ev.MsgKey == key {
					// Idempotent retry: the message is already in the
					// timeline exactly once — do not append a duplicate.
					out = s.correctZombie(rec)
					return nil
				}
			}
		}
		ev := Event{
			ID:       uuid.NewString(),
			Type:     EventMessage,
			Time:     time.Now().UTC(),
			From:     from,
			FromRole: fromRole,
			Body:     body,
			MsgKey:   key,
		}
		if err := s.appendEventLocked(ref, ev); err != nil {
			return err
		}
		rec, err = s.readLocked(ref)
		if err != nil {
			return err
		}
		out = s.correctZombie(rec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SetStatus transitions a task (used by the live task-manager bridge to
// mirror in-process lifecycle into the durable record).
func (s *Store) SetStatus(ref string, status Status, output, errMsg string) error {
	if status == "" {
		return fmt.Errorf("tasks: status must not be empty")
	}
	return s.mutate(ref, func() error {
		if _, err := s.readLocked(ref); err != nil {
			return err
		}
		ev := Event{
			ID:     uuid.NewString(),
			Type:   EventStatus,
			Time:   time.Now().UTC(),
			Status: status,
			Output: output,
			Error:  errMsg,
		}
		return s.appendEventLocked(ref, ev)
	})
}

// Archive soft-deletes a task: the log is kept for audit, but reads surface
// the archived state and messages return explicit errors from then on.
func (s *Store) Archive(ref string) (*Record, error) {
	var out *Record
	err := s.mutate(ref, func() error {
		rec, err := s.readLocked(ref)
		if err != nil {
			if errors.Is(err, ErrNotFound) && s.refExistsOutsideProject(ref) {
				return fmt.Errorf("%w: %s", ErrCrossProject, ref)
			}
			return err
		}
		if rec.Status == StatusArchived {
			out = rec
			return nil
		}
		if rec.Status == StatusRunning || rec.Status == StatusPending {
			return fmt.Errorf("task %s is %s; stop it before archiving", ref, rec.Status)
		}
		ev := Event{
			ID:   uuid.NewString(),
			Type: EventArchived,
			Time: time.Now().UTC(),
		}
		if err := s.appendEventLocked(ref, ev); err != nil {
			return err
		}
		rec, err = s.readLocked(ref)
		if err != nil {
			return err
		}
		out = rec
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Resolve resolves a user- or agent-supplied reference: a full task ref, a
// ref without the task_ prefix, or a unique task name within this project.
// Ambiguous names return ErrAmbiguous together with the candidate refs.
func (s *Store) Resolve(query string) (*Record, error) {
	q := strings.TrimSpace(query)
	q = strings.TrimPrefix(q, "@")
	if q == "" {
		return nil, fmt.Errorf("task reference must not be empty")
	}
	if ValidateRef(q) {
		return s.Get(q)
	}
	if short := "task_" + q; ValidateRef(short) {
		return s.Get(short)
	}
	// Name lookup within the visible project only.
	recs, err := s.List("")
	if err != nil {
		return nil, err
	}
	var matches []*Record
	// Archived tasks stay resolvable so callers can report a precise
	// "archived" error instead of a bare not-found.
	for _, rec := range recs {
		if strings.EqualFold(rec.Name, q) {
			matches = append(matches, rec)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%w: no task named %q in this project", ErrNotFound, q)
	case 1:
		return matches[0], nil
	default:
		var names []string
		for _, rec := range matches {
			names = append(names, rec.Ref+" ("+rec.Name+")")
		}
		return nil, fmt.Errorf("%w: %q matches %d tasks: %s", ErrAmbiguous, q, len(matches), strings.Join(names, ", "))
	}
}

// --- internals ---

// findByCreateKeyLocked scans this project's records for an idempotent
// create-key match. Caller holds s.mu.
func (s *Store) findByCreateKeyLocked(key string) (*Record, error) {
	recs, err := s.listLocked("")
	if err != nil {
		return nil, err
	}
	for _, rec := range recs {
		if rec.createKeyCache == key {
			return rec, nil
		}
	}
	return nil, ErrNotFound
}

// listLocked lists (and zombie-corrects) records. Caller holds s.mu.
func (s *Store) listLocked(filter Status) ([]*Record, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Record
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		ref := strings.TrimSuffix(name, ".jsonl")
		if !ValidateRef(ref) {
			continue
		}
		rec, err := s.readLocked(ref)
		if err != nil {
			continue // skip unreadable/partial logs rather than failing the list
		}
		rec = s.correctZombie(rec)
		if filter != "" && rec.Status != filter {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].Ref < out[j].Ref
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *Store) taskPath(ref string) string {
	return filepath.Join(s.dir, ref+".jsonl")
}

// readLocked loads and folds a task log. Missing file → ErrNotFound.
// Caller holds s.mu.
func (s *Store) readLocked(ref string) (*Record, error) {
	if !ValidateRef(ref) {
		return nil, fmt.Errorf("%w: %q is not a valid task reference (expected task_<16 hex>)", ErrNotFound, ref)
	}
	f, err := os.Open(s.taskPath(ref))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, ref)
		}
		return nil, fmt.Errorf("tasks: open %s: %w", ref, err)
	}
	defer func() { _ = f.Close() }()

	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// A torn trailing line can only come from a crash mid-append;
			// ignore it instead of failing the whole task.
			continue
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("tasks: read %s: %w", ref, err)
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, ref)
	}
	for i := range events {
		events[i].Seq = i + 1
	}
	return fold(events), nil
}

// mutate runs fn while holding both the in-process mutex and the
// cross-process task lock, so a read-check-append sequence (dedup,
// transition validation) is atomic against every other jcode process.
func (s *Store) mutate(ref string, fn func() error) error {
	if !ValidateRef(ref) {
		return fmt.Errorf("%w: %q is not a valid task reference", ErrNotFound, ref)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := acquireTaskLock(filepath.Join(s.dir, "."+ref+".lock"))
	if err != nil {
		return fmt.Errorf("tasks: lock %s: %w", ref, err)
	}
	defer func() { _ = lock.release() }()
	return fn()
}

// appendEventLocked appends one event. Caller must hold s.mu and the task lock.
func (s *Store) appendEventLocked(ref string, ev Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("tasks: encode event: %w", err)
	}
	f, err := os.OpenFile(s.taskPath(ref), os.O_APPEND|os.O_CREATE|os.O_WRONLY, privateTaskFileMode)
	if err != nil {
		return fmt.Errorf("tasks: open log %s: %w", ref, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("tasks: append event to %s: %w", ref, err)
	}
	return nil
}

// refExistsOutsideProject reports whether ref exists under a sibling project
// directory. Used to turn a bare "not found" into an explicit permission
// error instead of leaking data across the project boundary.
func (s *Store) refExistsOutsideProject(ref string) bool {
	if !ValidateRef(ref) {
		return false
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return false
	}
	own := projectDirName(s.project)
	for _, ent := range entries {
		if !ent.IsDir() || ent.Name() == own {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.root, ent.Name(), ref+".jsonl")); err == nil {
			return true
		}
	}
	return false
}

// correctZombie rewrites the snapshot (never the log) when a "running" task's
// owning process is gone, so crashed sessions surface as clear failures
// instead of eternal spinners.
func (s *Store) correctZombie(rec *Record) *Record {
	if rec == nil || rec.Status != StatusRunning {
		return rec
	}
	if rec.OwnerPID <= 0 || rec.Hostname == "" || rec.Hostname != s.host {
		return rec // remote host or unknown owner: cannot verify, keep as-is
	}
	if processAlive(rec.OwnerPID) {
		return rec
	}
	cp := *rec
	cp.Status = StatusFailed
	cp.Error = fmt.Sprintf("owning process (pid %d) exited before the task completed; the task did not finish", rec.OwnerPID)
	cp.Zombie = true
	cp.EndedAt = rec.UpdatedAt
	return &cp
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.Format(time.RFC3339)
}
