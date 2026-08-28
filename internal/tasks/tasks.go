// Package tasks implements the persistent, cross-session agent-task registry.
//
// A task is any durable unit of agent work that can outlive the session (or
// even the process) that created it: a background subagent run, or a plain
// work item an agent or the user wants to reference later. Every task gets a
// globally unique, collision-free reference (`task_<16 hex>`), is bound to the
// session, project and machine that created it, and keeps an append-only
// timeline of messages so other sessions can read, follow up on, or take over
// the work via stable `@task` mentions.
//
// Storage layout (mirrors the privacy posture of internal/session):
//
//	~/.jcode/tasks/<project-hash>/<task_ref>.jsonl   (0600, append-only events)
//	~/.jcode/tasks/<project-hash>/.<task_ref>.lock   (cross-process advisory lock)
//
// Visibility is project-scoped: listing and name resolution only ever see the
// caller's own project, and a reference that exists under a different project
// is rejected with an explicit permission error instead of being readable.
package tasks

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Status is the lifecycle state of a task.
type Status string

const (
	// StatusCreated is a durable work item that is not executing.
	StatusCreated Status = "created"
	// StatusPending is accepted for execution but not started yet.
	StatusPending Status = "pending"
	// StatusRunning is executing in a live jcode process.
	StatusRunning Status = "running"
	// StatusCompleted finished successfully.
	StatusCompleted Status = "completed"
	// StatusFailed finished with an error.
	StatusFailed Status = "failed"
	// StatusStopped was cancelled before finishing.
	StatusStopped Status = "stopped"
	// StatusArchived is soft-deleted: kept on disk for audit, no longer usable.
	StatusArchived Status = "archived"
)

// Terminal reports whether status is a final state (no further messages).
func (s Status) Terminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusStopped, StatusArchived:
		return true
	}
	return false
}

// Task kinds.
const (
	KindSubagent = "subagent"
	KindWorkItem = "workitem"
)

// OriginLocal marks records created by this machine (the only origin the
// local registry ever stores; remote/cloud sessions own their own registry).
const OriginLocal = "local"

const (
	refPrefix  = "task_"
	refHexLen  = 16
	maxNameLen = 80
)

var refPattern = regexp.MustCompile(`^task_[0-9a-f]{16}$`)

// NewRef mints a fresh collision-free task reference. 64 bits of entropy from
// crypto/rand makes collisions between sessions and processes practically
// impossible, unlike the process-local bg_subagent_N counters.
func NewRef() string {
	b := make([]byte, refHexLen/2)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is unrecoverable; fall back to time-derived id
		// rather than panicking inside a tool call.
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		b = sum[:refHexLen/2]
	}
	return refPrefix + hex.EncodeToString(b)
}

// ValidateRef reports whether s is a well-formed task reference.
func ValidateRef(s string) bool { return refPattern.MatchString(s) }

// ValidateName normalizes and validates a human-facing task name.
func ValidateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\n", " ")
	name = strings.ReplaceAll(name, "\r", " ")
	if name == "" {
		return "", fmt.Errorf("task name must not be empty")
	}
	if len(name) > maxNameLen {
		name = name[:maxNameLen]
	}
	return name, nil
}

// Event is one persisted JSONL record in a task's log.
type Event struct {
	ID   string    `json:"id"`   // dedup key (uuid); fixed for retries of the same logical event
	Type string    `json:"type"` // created | status | message | archived
	Time time.Time `json:"time"`
	Seq  int       `json:"seq"` // 1-based position in the log

	// created payload
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	Origin      string `json:"origin,omitempty"`
	AgentType   string `json:"agent_type,omitempty"`
	Model       string `json:"model,omitempty"`
	LocalID     string `json:"local_id,omitempty"` // process-local id (bg_subagent_N), debug aid only
	OwnerPID    int    `json:"owner_pid,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	CreateKey   string `json:"create_key,omitempty"` // idempotency key for Create

	// status payload
	Status Status `json:"status,omitempty"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`

	// message payload
	From     string `json:"from,omitempty"`      // sender session id
	FromRole string `json:"from_role,omitempty"` // user | agent | system
	Body     string `json:"body,omitempty"`
	MsgKey   string `json:"msg_key,omitempty"` // idempotency key: retry-safe delivery
}

// Event types.
const (
	EventCreated  = "created"
	EventStatus   = "status"
	EventMessage  = "message"
	EventArchived = "archived"
)

// Record is the folded snapshot of a task log at read time.
type Record struct {
	Ref         string    `json:"ref"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	Origin      string    `json:"origin,omitempty"`
	AgentType   string    `json:"agent_type,omitempty"`
	Model       string    `json:"model,omitempty"`
	LocalID     string    `json:"local_id,omitempty"`
	Status      Status    `json:"status"`
	Output      string    `json:"output,omitempty"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	EndedAt     time.Time `json:"ended_at,omitempty"`
	OwnerPID    int       `json:"owner_pid,omitempty"`
	Hostname    string    `json:"hostname,omitempty"`
	// Timeline holds message events in order (the audit trail of who said
	// what to this task, exactly once per logical message).
	Timeline []Event `json:"timeline,omitempty"`
	// Zombie is set when the snapshot corrected a "running" status because
	// the owning process is gone (crash / disconnect recovery).
	Zombie bool `json:"zombie,omitempty"`

	// createKeyCache carries the idempotency key from the created event;
	// internal only, never serialized.
	createKeyCache string `json:"-"`
}

// CreateInput carries the immutable identity of a new task.
type CreateInput struct {
	Name        string
	Description string
	Kind        string
	SessionID   string
	Origin      string
	AgentType   string
	Model       string
	LocalID     string
	OwnerPID    int
	Hostname    string
	// Key optionally makes Create idempotent: a retry with the same key
	// returns the original record instead of minting a duplicate.
	Key string
}

func fold(events []Event) *Record {
	rec := &Record{Status: StatusCreated}
	for i := range events {
		ev := events[i]
		switch ev.Type {
		case EventCreated:
			rec.Ref = ev.ID
			rec.Name = ev.Name
			rec.Description = ev.Description
			rec.Kind = ev.Kind
			rec.SessionID = ev.SessionID
			rec.Origin = ev.Origin
			rec.AgentType = ev.AgentType
			rec.Model = ev.Model
			rec.LocalID = ev.LocalID
			rec.OwnerPID = ev.OwnerPID
			rec.Hostname = ev.Hostname
			rec.CreatedAt = ev.Time
			rec.createKeyCache = ev.CreateKey
			if rec.Kind == "" {
				rec.Kind = KindWorkItem
			}
			if rec.Origin == "" {
				rec.Origin = OriginLocal
			}
		case EventStatus:
			rec.Status = ev.Status
			if ev.Output != "" {
				rec.Output = ev.Output
			}
			if ev.Error != "" {
				rec.Error = ev.Error
			}
			if ev.Status.Terminal() {
				rec.EndedAt = ev.Time
			}
		case EventMessage:
			rec.Timeline = append(rec.Timeline, ev)
		case EventArchived:
			rec.Status = StatusArchived
			rec.EndedAt = ev.Time
		}
		if ev.Time.After(rec.UpdatedAt) {
			rec.UpdatedAt = ev.Time
		}
	}
	return rec
}

func sha256Sum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
