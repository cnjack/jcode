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
	EntrySubagentAsync  EntryType = "subagent_async"
	EntryModeChange     EntryType = "mode_change"
	EntryCompact        EntryType = "compact"
	EntryBudgetWarning  EntryType = "budget_warning"
	EntrySystemPrompt   EntryType = "system_prompt"
	EntryGoalUpdate     EntryType = "goal_update"
)

// TodoSnapshotItem is a single todo entry stored in a todo_snapshot event.
type TodoSnapshotItem struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// EntryImage stores a single image attached to a user message.
type EntryImage struct {
	MimeType string `json:"media_type"`
	Data     string `json:"data"` // base64-encoded
}

// Entry is one line of the JSONL session file.
type Entry struct {
	Type       EntryType `json:"type"`
	UUID       string    `json:"uuid,omitempty"`
	Project    string    `json:"project,omitempty"`
	Provider   string    `json:"provider,omitempty"`
	Model      string    `json:"model,omitempty"`
	Content    string    `json:"content,omitempty"`
	Name       string    `json:"name,omitempty"`         // tool name
	Args       string    `json:"args,omitempty"`         // tool args JSON
	Output     string    `json:"output,omitempty"`       // tool output
	Error      string    `json:"error,omitempty"`        // tool error
	ToolCallID string    `json:"tool_call_id,omitempty"` // links tool_call ↔ tool_result
	Timestamp  string    `json:"timestamp"`

	// Images attached to a user message.
	Images []EntryImage `json:"images,omitempty"`

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

	// system_prompt fields
	EnvInfo string `json:"env_info,omitempty"` // serialized environment snapshot

	// goal_update fields. GoalStatus == "cleared" marks goal removal.
	GoalObjective  string `json:"goal_objective,omitempty"`
	GoalStatus     string `json:"goal_status,omitempty"`
	GoalTokensUsed int64  `json:"goal_tokens_used,omitempty"`
}

// SessionMeta is stored in the index for fast listing.
type SessionMeta struct {
	UUID      string `json:"uuid"`
	Project   string `json:"project"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	StartTime string `json:"start_time"` // RFC3339
	Title     string `json:"title,omitempty"`
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
	// Per-teammate fields (empty for leader recorder).
	customDir string // leader UUID for subagent path
	agentID   string // teammate agent ID
	// resuming is true when loading an existing session via --resume.
	// In this mode, ensureFile opens the existing file for append instead of creating new.
	resuming bool
	title    string
	hasTitle bool // true after title has been generated or when resuming
	// pendingSystemPrompt / pendingEnvInfo buffer the system prompt until the
	// first real message is recorded so that opening and immediately closing
	// jcode does not leave an empty session on disk.
	pendingSystemPrompt string
	pendingEnvInfo      string
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

// ValidateSessionID checks that a session ID is safe for use as a filename.
// It rejects empty IDs, path traversal sequences, and path separators.
func ValidateSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("session ID must not be empty")
	}
	if id == "." || id == ".." {
		return fmt.Errorf("invalid session ID: %q", id)
	}
	if strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("session ID must not contain path separators: %q", id)
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("session ID must not contain path traversal: %q", id)
	}
	return nil
}

// SetUUID overrides the session identifier. Used when resuming an existing session
// so that new messages are appended to the same session file.
// If the session file does not yet exist on disk, the recorder is NOT marked as
// resuming, so the first user message will still generate a title.
func (r *Recorder) SetUUID(id string) {
	if err := ValidateSessionID(id); err != nil {
		config.Logger().Printf("[session] SetUUID rejected: %v", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.uuid = id
	// Only mark as resuming if the session file already exists on disk.
	// For brand-new sessions with a client-provided UUID, we still want
	// title generation on the first user message.
	dir, err := config.SessionsDir()
	if err == nil {
		filePath := filepath.Join(dir, id+".json")
		if _, statErr := os.Stat(filePath); statErr == nil {
			r.resuming = true
		}
	}
	// If a file was already opened with the old UUID, close it so the next
	// write opens the correct file for append.
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
}

// HasRecording reports whether any message has been recorded (i.e. the
// session file has been created).  Returns false for sessions where the
// user quit without any conversation.
func (r *Recorder) HasRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file != nil
}

// RecordUser appends a user message entry.
// On the first user message, the title is auto-generated from the content.
// Optional images are persisted alongside the text so they survive session restore.
func (r *Recorder) RecordUser(content string, images ...EntryImage) {
	r.mu.Lock()
	needsTitle := !r.hasTitle && !r.resuming
	r.mu.Unlock()
	_ = r.writeEntry(Entry{Type: EntryUser, Content: content, Images: images})
	if needsTitle {
		title := generateTitle(content)
		r.mu.Lock()
		r.title = title
		r.hasTitle = true
		r.mu.Unlock()
		_ = updateIndexTitle(r.project, r.uuid, title)
	}
}

// RecordAssistant appends an assistant message entry.
func (r *Recorder) RecordAssistant(content string) {
	_ = r.writeEntry(Entry{Type: EntryAssistant, Content: content})
}

// RecordToolCall appends a tool-call entry.
func (r *Recorder) RecordToolCall(name, args, toolCallID string) {
	_ = r.writeEntry(Entry{Type: EntryToolCall, Name: name, Args: args, ToolCallID: toolCallID})
}

// RecordToolResult appends a tool-result entry.
// Large outputs are automatically truncated (head+tail preserved) and the
// full content is saved to an overflow file on disk.
func (r *Recorder) RecordToolResult(name, output, toolCallID string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	output = TruncateToolOutput(output, r.uuid, toolCallID)
	_ = r.writeEntry(Entry{Type: EntryToolResult, Name: name, Output: output, ToolCallID: toolCallID, Error: errStr})
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

// RecordGoalUpdate appends a goal state change entry. An empty status records
// a "cleared" marker so resume knows the goal was removed.
func (r *Recorder) RecordGoalUpdate(objective, status string, tokensUsed int64) {
	if status == "" {
		status = "cleared"
	}
	_ = r.writeEntry(Entry{
		Type:           EntryGoalUpdate,
		GoalObjective:  objective,
		GoalStatus:     status,
		GoalTokensUsed: tokensUsed,
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

// RecordSubagentAsync appends an async subagent launch entry with the task ID.
func (r *Recorder) RecordSubagentAsync(name, taskID, agentType string) {
	_ = r.writeEntry(Entry{Type: EntrySubagentAsync, SubagentName: name, Output: taskID, SubagentType: agentType})
}

// RecordModeChange appends a mode transition entry.
func (r *Recorder) RecordModeChange(mode string) {
	_ = r.writeEntry(Entry{Type: EntryModeChange, Mode: mode})
}

// RecordCompact appends a compact/summarization event entry.
func (r *Recorder) RecordCompact(summary string, compactedN int) {
	_ = r.writeEntry(Entry{Type: EntryCompact, Summary: summary, CompactedN: compactedN})
}

// RecordSystemPrompt buffers the system prompt so it can be written together
// with the first real message. This avoids creating a session file when the
// user opens and immediately closes jcode without any conversation.
func (r *Recorder) RecordSystemPrompt(prompt, envInfo string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendingSystemPrompt = prompt
	r.pendingEnvInfo = envInfo
}

// TruncateAtUserMessage rewrites the session file keeping only entries that
// appear before the (beforeCount)th user message (0-indexed).
// If beforeCount == 0, the file is truncated to the session_start header only.
// The recorder is reset to append mode on the (now shorter) file.
// This preserves the session UUID and index entry — no new session is created.
func (r *Recorder) TruncateAtUserMessage(beforeCount int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.agentID != "" {
		return fmt.Errorf("TruncateAtUserMessage not supported for teammate recorders")
	}

	// Close current file handle before rewriting.
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}

	dir, err := config.SessionsDir()
	if err != nil {
		return err
	}
	filePath := filepath.Join(dir, r.uuid+".json")

	// Load existing entries.
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing to truncate.
			return nil
		}
		return fmt.Errorf("read session file: %w", err)
	}

	// Collect entries to keep: session_start always, then everything before
	// the beforeCount-th user entry. When beforeCount == 0 we keep nothing
	// except the session_start header.
	var keep []string
	userCount := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		// Always keep the session_start header.
		if e.Type == EntrySessionStart {
			keep = append(keep, line)
			continue
		}
		// Stop as soon as we reach the Nth user message.
		if e.Type == EntryUser {
			if userCount >= beforeCount {
				break
			}
			userCount++
		}
		// For beforeCount == 0 we must not keep any non-session_start entry.
		if beforeCount == 0 {
			break
		}
		keep = append(keep, line)
	}

	// Atomically rewrite the file.
	tmpPath := filePath + ".tmp"
	content := strings.Join(keep, "\n")
	if len(keep) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write truncated session: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("rename truncated session: %w", err)
	}

	// Reopen for append so subsequent writes go to the correct file.
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("reopen session for append: %w", err)
	}
	r.file = f
	r.resuming = true
	return nil
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

// NewTeammateRecorder creates a Recorder that stores its JSONL transcript
// under the leader session's subagents directory:
//
//	~/.jcode/sessions/{leaderUUID}/subagents/agent-{agentID}.jsonl
//
// This mirrors Claude Code's per-agent transcript pattern.
func NewTeammateRecorder(leaderUUID, agentID, model string) (*Recorder, error) {
	r := &Recorder{
		uuid:      leaderUUID + "/" + agentID,
		project:   agentID,
		provider:  "teammate",
		model:     model,
		startTime: time.Now(),
	}
	// Override ensureFile to write to subagent path.
	r.customDir = leaderUUID
	r.agentID = agentID
	return r, nil
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

	var filePath string
	if r.agentID != "" {
		// Teammate recorder: ~/.jcode/sessions/{leaderUUID}/subagents/agent-{agentID}.jsonl
		subDir := filepath.Join(dir, r.customDir, "subagents")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			return fmt.Errorf("create subagents dir: %w", err)
		}
		filePath = filepath.Join(subDir, "agent-"+r.agentID+".jsonl")
	} else {
		// Leader recorder: ~/.jcode/sessions/{uuid}.json
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create sessions dir: %w", err)
		}
		filePath = filepath.Join(dir, r.uuid+".json")
	}

	// When resuming an existing session, open the file for append instead of creating new.
	if r.resuming {
		f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open session file for append: %w", err)
		}
		r.file = f
		return nil
	}

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

	// Update the shared index (non-fatal, skip for teammate recorders).
	if r.agentID == "" {
		_ = addToIndex(r.project, SessionMeta{
			UUID:      r.uuid,
			Project:   r.project,
			Provider:  r.provider,
			Model:     r.model,
			StartTime: r.startTime.Format(time.RFC3339),
		})
	}
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
	// Flush buffered system prompt before the first real entry.
	if r.pendingSystemPrompt != "" {
		sp := Entry{
			Type:      EntrySystemPrompt,
			Content:   r.pendingSystemPrompt,
			EnvInfo:   r.pendingEnvInfo,
			Timestamp: e.Timestamp,
		}
		r.pendingSystemPrompt = ""
		r.pendingEnvInfo = ""
		if spData, spErr := json.Marshal(sp); spErr == nil {
			_, _ = r.file.WriteString(string(spData) + "\n")
		}
	}
	if _, err = r.file.WriteString(string(data) + "\n"); err != nil {
		return err
	}
	// Sync to disk so entries survive a crash.
	return r.file.Sync()
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
	// Atomic write: write to temp file then rename to prevent corruption
	// from concurrent writes or interrupted I/O.
	tmpPath := indexPath + ".tmp"
	if err := os.WriteFile(tmpPath, newData, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, indexPath)
}

// generateTitle creates a human-readable session title from the first user message.
// It truncates to a reasonable length and strips newlines.
func generateTitle(content string) string {
	// Take first line only
	title := content
	if idx := strings.IndexAny(title, "\n\r"); idx >= 0 {
		title = title[:idx]
	}
	title = strings.TrimSpace(title)
	if len(title) == 0 {
		return "New session"
	}
	// Truncate to 80 chars
	runes := []rune(title)
	if len(runes) > 80 {
		title = string(runes[:80]) + "…"
	}
	return title
}

// updateIndexTitle updates the title of a session in the shared index file.
func updateIndexTitle(project, uuid, title string) error {
	indexPath, err := config.SessionsIndexPath()
	if err != nil {
		return err
	}
	data, readErr := os.ReadFile(indexPath)
	if readErr != nil {
		return readErr
	}
	var idx sessionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}
	metas := idx.Sessions[project]
	for i := range metas {
		if metas[i].UUID == uuid {
			metas[i].Title = title
			break
		}
	}
	idx.Sessions[project] = metas
	newData, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := indexPath + ".tmp"
	if err := os.WriteFile(tmpPath, newData, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, indexPath)
}

// DeleteSession removes a session file and its index entry.
func DeleteSession(project, uuid string) error {
	// 1. Remove from index.
	if err := removeFromIndex(project, uuid); err != nil {
		return fmt.Errorf("remove index entry: %w", err)
	}
	// 2. Delete the JSONL file.
	dir, err := config.SessionsDir()
	if err != nil {
		return err
	}
	filePath := filepath.Join(dir, uuid+".json")
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session file: %w", err)
	}
	return nil
}

func removeFromIndex(project, uuid string) error {
	indexPath, err := config.SessionsIndexPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var idx sessionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}
	metas := idx.Sessions[project]
	filtered := make([]SessionMeta, 0, len(metas))
	for _, m := range metas {
		if m.UUID != uuid {
			filtered = append(filtered, m)
		}
	}
	idx.Sessions[project] = filtered
	newData, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := indexPath + ".tmp"
	if err := os.WriteFile(tmpPath, newData, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, indexPath)
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

// ListAllSessions returns all sessions across all projects, keyed by project path.
func ListAllSessions() (map[string][]SessionMeta, error) {
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
	return idx.Sessions, nil
}

// LoadSession reads all entries from a session JSONL file identified by uuid.
func LoadSession(id string) ([]Entry, error) {
	if err := ValidateSessionID(id); err != nil {
		return nil, err
	}
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
	var skipped int
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			skipped++
			preview := line
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			config.Logger().Printf("[session] corrupted line in %s (skipped %d): %v — %s", id, skipped, err, preview)
			continue
		}
		entries = append(entries, e)
	}
	if skipped > 0 {
		config.Logger().Printf("[session] loaded %s with %d corrupted lines skipped", id, skipped)
	}
	return entries, nil
}
