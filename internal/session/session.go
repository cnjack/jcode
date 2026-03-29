package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cnjack/jcode/internal/config"
)

// EntryType identifies the kind of JSONL record.
type EntryType string

const (
	EntrySessionStart EntryType = "session_start"
	EntryUser         EntryType = "user"
	EntryAssistant    EntryType = "assistant"
	EntryToolCall     EntryType = "tool_call"
	EntryToolResult   EntryType = "tool_result"

	// Extended entry types for structured state tracking.
	EntryPlanUpdate     EntryType = "plan_update"
	EntryTodoSnapshot   EntryType = "todo_snapshot"
	EntrySubagentStart  EntryType = "subagent_start"
	EntrySubagentResult EntryType = "subagent_result"
	EntryModeChange     EntryType = "mode_change"
	EntryCompact        EntryType = "compact"
)

// TodoSnapshotItem is a single todo entry stored in a todo_snapshot event.
type TodoSnapshotItem struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// Entry is one line of the JSONL session file.
type Entry struct {
	Type      EntryType `json:"type"`
	UUID      string    `json:"uuid,omitempty"`
	Project   string    `json:"project,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
	Content   string    `json:"content,omitempty"`
	Name      string    `json:"name,omitempty"`   // tool name
	Args      string    `json:"args,omitempty"`   // tool args JSON
	Output    string    `json:"output,omitempty"` // tool output
	Error     string    `json:"error,omitempty"`  // tool error
	Timestamp string    `json:"timestamp"`

	// plan_update fields
	PlanStatus  string `json:"plan_status,omitempty"`
	PlanTitle   string `json:"plan_title,omitempty"`
	PlanContent string `json:"plan_content,omitempty"`
	Feedback    string `json:"feedback,omitempty"`

	// todo_snapshot fields
	Todos []TodoSnapshotItem `json:"todos,omitempty"`

	// subagent_start / subagent_result fields
	SubagentName string `json:"subagent_name,omitempty"`
	SubagentType string `json:"subagent_type,omitempty"`

	// mode_change field
	Mode string `json:"mode,omitempty"`

	// compact fields
	Summary    string `json:"summary,omitempty"`
	CompactedN int    `json:"compacted_n,omitempty"`
}

// SessionMeta is stored in the index for fast listing.
type SessionMeta struct {
	UUID      string `json:"uuid"`
	Project   string `json:"project"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	StartTime string `json:"start_time"` // RFC3339
}

// sessionIndex is the on-disk structure of session.json.
type sessionIndex struct {
	Sessions map[string][]SessionMeta `json:"sessions"` // project path → metas
}

// Recorder appends events to a JSONL session file synchronously.
// The file and index entry are created lazily on the first real message so that
// sessions with no conversation are never persisted.
// Call Close() (or defer it) to finalize.
type Recorder struct {
	uuid      string
	project   string
	provider  string
	model     string
	startTime time.Time
	file      *os.File
	mu        sync.Mutex
}

// NewRecorder returns a Recorder that will create the session file only when
// the first message is recorded.  Never returns an error — recording is
// best-effort and must not break normal operation.
func NewRecorder(project, provider, model string) (*Recorder, error) {
	return &Recorder{
		uuid:      uuid.New().String(),
		project:   project,
		provider:  provider,
		model:     model,
		startTime: time.Now(),
	}, nil
}

// UUID returns the session identifier.
func (r *Recorder) UUID() string { return r.uuid }

// RecordUser appends a user message entry.
func (r *Recorder) RecordUser(content string) {
	_ = r.writeEntry(Entry{Type: EntryUser, Content: content})
}

// RecordAssistant appends an assistant message entry.
func (r *Recorder) RecordAssistant(content string) {
	_ = r.writeEntry(Entry{Type: EntryAssistant, Content: content})
}

// RecordToolCall appends a tool-call entry.
func (r *Recorder) RecordToolCall(name, args string) {
	_ = r.writeEntry(Entry{Type: EntryToolCall, Name: name, Args: args})
}

// RecordToolResult appends a tool-result entry.
func (r *Recorder) RecordToolResult(name, output string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	_ = r.writeEntry(Entry{Type: EntryToolResult, Name: name, Output: output, Error: errStr})
}

// RecordPlanUpdate appends a plan state change entry.
func (r *Recorder) RecordPlanUpdate(status, title, content, feedback string) {
	_ = r.writeEntry(Entry{
		Type:        EntryPlanUpdate,
		PlanStatus:  status,
		PlanTitle:   title,
		PlanContent: content,
		Feedback:    feedback,
	})
}

// RecordTodoSnapshot appends a full todo list snapshot entry.
func (r *Recorder) RecordTodoSnapshot(todos []TodoSnapshotItem) {
	_ = r.writeEntry(Entry{Type: EntryTodoSnapshot, Todos: todos})
}

// RecordSubagentStart appends a subagent launch entry.
func (r *Recorder) RecordSubagentStart(name, agentType string) {
	_ = r.writeEntry(Entry{Type: EntrySubagentStart, SubagentName: name, SubagentType: agentType})
}

// RecordSubagentResult appends a subagent completion entry.
func (r *Recorder) RecordSubagentResult(name, output string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	_ = r.writeEntry(Entry{Type: EntrySubagentResult, SubagentName: name, Output: output, Error: errStr})
}

// RecordModeChange appends a mode transition entry.
func (r *Recorder) RecordModeChange(mode string) {
	_ = r.writeEntry(Entry{Type: EntryModeChange, Mode: mode})
}

// RecordCompact appends a compact/summarization event entry.
func (r *Recorder) RecordCompact(summary string, compactedN int) {
	_ = r.writeEntry(Entry{Type: EntryCompact, Summary: summary, CompactedN: compactedN})
}

// Close flushes and closes the underlying file.  Safe to call multiple times.
// If no messages were ever recorded the file is never created.
func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
}

// ensureFile creates the session file and writes the session_start header the
// first time it is called.  Must be called with r.mu held.
func (r *Recorder) ensureFile() error {
	if r.file != nil {
		return nil
	}
	dir, err := config.SessionsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create sessions dir: %w", err)
	}

	filePath := filepath.Join(dir, r.uuid+".json")
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	r.file = f

	// Write the header entry (timestamp already known).
	startEntry := Entry{
		Type:      EntrySessionStart,
		UUID:      r.uuid,
		Project:   r.project,
		Provider:  r.provider,
		Model:     r.model,
		Timestamp: r.startTime.Format(time.RFC3339),
	}
	data, err := json.Marshal(startEntry)
	if err != nil {
		return err
	}
	if _, err = f.WriteString(string(data) + "\n"); err != nil {
		return err
	}

	// Update the shared index (non-fatal).
	_ = addToIndex(r.project, SessionMeta{
		UUID:      r.uuid,
		Project:   r.project,
		Provider:  r.provider,
		Model:     r.model,
		StartTime: r.startTime.Format(time.RFC3339),
	})
	return nil
}

func (r *Recorder) writeEntry(e Entry) error {
	e.Timestamp = time.Now().Format(time.RFC3339)
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Lazily initialise the file on the first real write.
	if err := r.ensureFile(); err != nil {
		return err
	}
	_, err = r.file.WriteString(string(data) + "\n")
	return err
}

// addToIndex adds a SessionMeta to the shared index file.
func addToIndex(project string, meta SessionMeta) error {
	indexPath, err := config.SessionsIndexPath()
	if err != nil {
		return err
	}
	// Ensure parent dir exists
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		return err
	}

	idx := &sessionIndex{Sessions: make(map[string][]SessionMeta)}
	if data, err := os.ReadFile(indexPath); err == nil {
		_ = json.Unmarshal(data, idx)
	}
	if idx.Sessions == nil {
		idx.Sessions = make(map[string][]SessionMeta)
	}

	idx.Sessions[project] = append(idx.Sessions[project], meta)
	newData, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath, newData, 0644)
}

// ListSessions returns all sessions recorded for a given project path, newest last.
func ListSessions(project string) ([]SessionMeta, error) {
	indexPath, err := config.SessionsIndexPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx sessionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return idx.Sessions[project], nil
}

// LoadSession reads all entries from a session JSONL file identified by uuid.
func LoadSession(id string) ([]Entry, error) {
	dir, err := config.SessionsDir()
	if err != nil {
		return nil, err
	}
	filePath := filepath.Join(dir, id+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("session %s not found: %w", id, err)
	}

	var entries []Entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, e)
	}
	return entries, nil
}
