package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	appconfig "github.com/cnjack/jcode/internal/config"
)

// ---------------------------------------------------------------------------
// EnhancedTodoItem
// ---------------------------------------------------------------------------

// EnhancedTodoItem represents a single todo entry with dependency support.
type EnhancedTodoItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	BlockedBy []string  `json:"blocked_by,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

const (
	StatusNotStarted = "not_started"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusSkipped    = "skipped"
)

var validEnhancedStatuses = map[string]bool{
	StatusNotStarted: true,
	StatusInProgress: true,
	StatusCompleted:  true,
	StatusSkipped:    true,
}

// ---------------------------------------------------------------------------
// EnhancedTodoStore
// ---------------------------------------------------------------------------

// EnhancedTodoStore is a concurrency-safe store with dependency tracking and optional persistence.
type EnhancedTodoStore struct {
	items     []EnhancedTodoItem
	sessionID string
	storage   *StorageManager
	mu        sync.RWMutex
	dirty     bool
	onChange  func([]EnhancedTodoItem)
}

// NewEnhancedTodoStore creates a store, loading persisted data if storage is non-nil.
func NewEnhancedTodoStore(sessionID string, storage *StorageManager) *EnhancedTodoStore {
	s := &EnhancedTodoStore{
		sessionID: sessionID,
		storage:   storage,
	}
	if storage != nil {
		if err := s.Load(); err != nil {
			appconfig.Logger().Printf("todo_enhanced: load failed: %v", err)
		}
	}
	return s
}

// SetOnChange registers a callback invoked after every mutation.
func (s *EnhancedTodoStore) SetOnChange(fn func([]EnhancedTodoItem)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = fn
}

// ---------------------------------------------------------------------------
// Mutations
// ---------------------------------------------------------------------------

// Update replaces the entire list (legacy-compatible full-replacement semantics).
func (s *EnhancedTodoStore) Update(items []EnhancedTodoItem) error {
	if err := validate(items); err != nil {
		return err
	}
	now := time.Now()
	for i := range items {
		if items[i].CreatedAt.IsZero() {
			items[i].CreatedAt = now
		}
		items[i].UpdatedAt = now
	}

	s.mu.Lock()
	s.items = make([]EnhancedTodoItem, len(items))
	copy(s.items, items)
	s.dirty = true
	cb := s.onChange
	s.mu.Unlock()

	s.saveAsync()
	s.notifyChange(cb)
	return nil
}

// Add appends new items, checking for ID conflicts with existing items.
func (s *EnhancedTodoStore) Add(items []EnhancedTodoItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := make(map[string]bool, len(s.items))
	for _, it := range s.items {
		existing[it.ID] = true
	}
	for _, it := range items {
		if existing[it.ID] {
			return fmt.Errorf("todo id %q already exists", it.ID)
		}
	}

	merged := make([]EnhancedTodoItem, len(s.items), len(s.items)+len(items))
	copy(merged, s.items)

	now := time.Now()
	for _, it := range items {
		if it.CreatedAt.IsZero() {
			it.CreatedAt = now
		}
		it.UpdatedAt = now
		merged = append(merged, it)
	}

	if err := validate(merged); err != nil {
		return err
	}

	s.items = merged
	s.dirty = true
	cb := s.onChange

	s.mu.Unlock()
	s.saveAsync()
	s.notifyChange(cb)
	s.mu.Lock() // re-lock for deferred unlock
	return nil
}

// Modify changes a single item by ID.
func (s *EnhancedTodoStore) Modify(id, status, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, it := range s.items {
		if it.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("todo id %q not found", id)
	}

	if status != "" {
		if !validEnhancedStatuses[status] {
			return fmt.Errorf("invalid status %q", status)
		}
		// Check dependency constraint when moving to in_progress.
		if status == StatusInProgress {
			if err := checkDependencies(s.items[idx], s.items); err != nil {
				return err
			}
			// At most 1 in_progress: count existing (exclude current item).
			for _, it := range s.items {
				if it.ID != id && it.Status == StatusInProgress {
					return fmt.Errorf("at most 1 todo can be in_progress at a time")
				}
			}
		}
		s.items[idx].Status = status
	}
	if title != "" {
		s.items[idx].Title = title
	}
	s.items[idx].UpdatedAt = time.Now()
	s.dirty = true
	cb := s.onChange

	s.mu.Unlock()
	s.saveAsync()
	s.notifyChange(cb)
	s.mu.Lock()
	return nil
}

// Remove deletes an item by ID. Fails if other items depend on it.
func (s *EnhancedTodoStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := false
	for _, it := range s.items {
		if it.ID == id {
			found = true
			continue
		}
		for _, dep := range it.BlockedBy {
			if dep == id {
				return fmt.Errorf("cannot remove %q: item %q depends on it", id, it.ID)
			}
		}
	}
	if !found {
		return fmt.Errorf("todo id %q not found", id)
	}

	newItems := make([]EnhancedTodoItem, 0, len(s.items)-1)
	for _, it := range s.items {
		if it.ID != id {
			newItems = append(newItems, it)
		}
	}
	s.items = newItems
	s.dirty = true
	cb := s.onChange

	s.mu.Unlock()
	s.saveAsync()
	s.notifyChange(cb)
	s.mu.Lock()
	return nil
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// Snapshot returns a deep copy of items.
func (s *EnhancedTodoStore) Snapshot() []EnhancedTodoItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EnhancedTodoItem, len(s.items))
	for i, it := range s.items {
		out[i] = it
		if it.BlockedBy != nil {
			out[i].BlockedBy = make([]string, len(it.BlockedBy))
			copy(out[i].BlockedBy, it.BlockedBy)
		}
	}
	return out
}

// HasItems returns true if there are any items.
func (s *EnhancedTodoStore) HasItems() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items) > 0
}

// HasIncomplete returns true if any items are not_started or in_progress.
func (s *EnhancedTodoStore) HasIncomplete() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, it := range s.items {
		if it.Status != StatusCompleted && it.Status != StatusSkipped {
			return true
		}
	}
	return false
}

// Summary returns a human-readable summary string.
func (s *EnhancedTodoStore) Summary() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var notStarted, inProgress, completed, skipped int
	for _, it := range s.items {
		switch it.Status {
		case StatusNotStarted:
			notStarted++
		case StatusInProgress:
			inProgress++
		case StatusCompleted:
			completed++
		case StatusSkipped:
			skipped++
		}
	}
	total := len(s.items)
	return fmt.Sprintf("%d todos (%d completed, %d in_progress, %d not_started, %d skipped)",
		total, completed, inProgress, notStarted, skipped)
}

// IncompleteSummary returns a message listing incomplete items.
func (s *EnhancedTodoStore) IncompleteSummary() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var lines []string
	for _, it := range s.items {
		if it.Status != StatusCompleted && it.Status != StatusSkipped {
			lines = append(lines, fmt.Sprintf("  - [%s] %s: %s", it.Status, it.ID, it.Title))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	msg := "You still have incomplete todos:\n"
	for _, l := range lines {
		msg += l + "\n"
	}
	msg += "Please complete or skip all remaining todos before finishing."
	return msg
}

// Items converts enhanced items to legacy TodoItem format for backward TUI compatibility.
func (s *EnhancedTodoStore) Items() []TodoItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TodoItem, len(s.items))
	for i, it := range s.items {
		var id int
		_, _ = fmt.Sscanf(it.ID, "%d", &id)
		status := TodoStatus(it.Status)
		switch it.Status {
		case StatusNotStarted:
			status = TodoPending
		case StatusSkipped:
			status = TodoCancelled
		}
		out[i] = TodoItem{
			ID:     id,
			Title:  it.Title,
			Status: status,
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func validate(items []EnhancedTodoItem) error {
	ids := make(map[string]bool, len(items))
	inProgressCount := 0
	for _, it := range items {
		if it.ID == "" {
			return fmt.Errorf("todo item has empty id")
		}
		if it.Title == "" {
			return fmt.Errorf("todo item %q has empty title", it.ID)
		}
		if ids[it.ID] {
			return fmt.Errorf("duplicate todo id: %q", it.ID)
		}
		ids[it.ID] = true
		if !validEnhancedStatuses[it.Status] {
			return fmt.Errorf("invalid status %q for todo %q, must be not_started/in_progress/completed/skipped", it.Status, it.ID)
		}
		if it.Status == StatusInProgress {
			inProgressCount++
		}
	}
	if inProgressCount > 1 {
		return fmt.Errorf("at most 1 todo can be in_progress at a time, found %d", inProgressCount)
	}
	// Validate blocked_by references exist.
	for _, it := range items {
		for _, dep := range it.BlockedBy {
			if !ids[dep] {
				return fmt.Errorf("todo %q blocked_by non-existent id %q", it.ID, dep)
			}
		}
	}
	// Cycle detection.
	if hasCycle(items) {
		return fmt.Errorf("circular dependency detected among todos")
	}
	return nil
}

// checkDependencies returns an error if any blocked_by item is not completed.
func checkDependencies(item EnhancedTodoItem, all []EnhancedTodoItem) error {
	if len(item.BlockedBy) == 0 {
		return nil
	}
	statusMap := make(map[string]string, len(all))
	for _, it := range all {
		statusMap[it.ID] = it.Status
	}
	var notDone []string
	for _, dep := range item.BlockedBy {
		if statusMap[dep] != StatusCompleted {
			notDone = append(notDone, dep)
		}
	}
	if len(notDone) > 0 {
		return fmt.Errorf("cannot start %q: blocked by incomplete items: %s", item.ID, strings.Join(notDone, ", "))
	}
	return nil
}

// hasCycle uses Kahn's algorithm to detect cycles.
func hasCycle(items []EnhancedTodoItem) bool {
	idSet := make(map[string]bool, len(items))
	inDegree := make(map[string]int, len(items))
	dependents := make(map[string][]string, len(items))
	for _, it := range items {
		idSet[it.ID] = true
		inDegree[it.ID] = 0
	}
	for _, it := range items {
		for _, dep := range it.BlockedBy {
			if idSet[dep] {
				inDegree[it.ID]++
				dependents[dep] = append(dependents[dep], it.ID)
			}
		}
	}
	queue := make([]string, 0, len(items))
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}
	return visited != len(items)
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

// TodoFileFormat is the on-disk JSON structure.
type TodoFileFormat struct {
	Version   int                `json:"version"`
	SessionID string             `json:"session_id"`
	UpdatedAt time.Time          `json:"updated_at"`
	Items     []EnhancedTodoItem `json:"items"`
}

func (s *EnhancedTodoStore) todoFilePath() string {
	if s.storage == nil {
		return ""
	}
	return filepath.Join(s.storage.TodosDir(), s.sessionID+".json")
}

// Load reads persisted state from disk. Errors from missing or corrupt files are ignored.
func (s *EnhancedTodoStore) Load() error {
	path := s.todoFilePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file TodoFileFormat
	if err := json.Unmarshal(data, &file); err != nil {
		appconfig.Logger().Printf("todo_enhanced: ignoring corrupt file %s: %v", path, err)
		return nil
	}
	s.mu.Lock()
	s.items = file.Items
	if s.items == nil {
		s.items = nil
	}
	s.dirty = false
	s.mu.Unlock()
	return nil
}

func (s *EnhancedTodoStore) saveAsync() {
	if s.storage == nil {
		return
	}
	data, err := s.marshalFile()
	if err != nil {
		appconfig.Logger().Printf("todo_enhanced: marshal failed: %v", err)
		return
	}
	s.storage.WriteAsync(s.todoFilePath(), data, 0o600)
}

// SaveSync synchronously writes state to disk (for graceful shutdown).
// It drains any pending async writes first, then writes the current state.
func (s *EnhancedTodoStore) SaveSync() error {
	if s.storage == nil {
		return nil
	}
	// Drain any pending async writes first to avoid a race where an older
	// queued write lands on disk after our synchronous write.
	s.storage.writeQueue.DrainSync()

	s.mu.RLock()
	hasItems := len(s.items) > 0
	s.mu.RUnlock()

	if !hasItems {
		return nil
	}

	data, err := s.marshalFile()
	if err != nil {
		return err
	}
	return s.storage.Write(s.todoFilePath(), data, 0o600)
}

func (s *EnhancedTodoStore) marshalFile() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	file := TodoFileFormat{
		Version:   2,
		SessionID: s.sessionID,
		UpdatedAt: time.Now(),
		Items:     s.items,
	}
	return json.MarshalIndent(file, "", "  ")
}

func (s *EnhancedTodoStore) notifyChange(cb func([]EnhancedTodoItem)) {
	if cb == nil {
		return
	}
	cb(s.Snapshot())
}

// ---------------------------------------------------------------------------
// TodoAction & EnhancedTodoInput
// ---------------------------------------------------------------------------

// TodoAction identifies the operation to perform.
type TodoAction string

const (
	TodoActionUpdate TodoAction = "update"
	TodoActionAdd    TodoAction = "add"
	TodoActionModify TodoAction = "modify"
	TodoActionRemove TodoAction = "remove"
	TodoActionRead   TodoAction = "read"
)

// EnhancedTodoInput is the unified JSON input for the todowrite enhanced tool.
type EnhancedTodoInput struct {
	Action TodoAction         `json:"action,omitempty"`
	Items  []EnhancedTodoItem `json:"items,omitempty"`
	ID     string             `json:"id,omitempty"`
	Status string             `json:"status,omitempty"`
	Title  string             `json:"title,omitempty"`
	// legacy compat
	Todos []TodoItem `json:"todos,omitempty"`
}

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

// NewEnhancedTodoWriteTool creates the todowrite tool backed by an EnhancedTodoStore.
func NewEnhancedTodoWriteTool(store *EnhancedTodoStore) tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "todowrite",
		Desc: `Use this tool to create and manage a structured task list for your current coding session.

## Actions
- **update**: Replace the entire todo list (default). Send "items" array.
- **add**: Append new items. Send "items" array.
- **modify**: Change a single item. Send "id" plus optional "status" and/or "title".
- **remove**: Delete a single item by "id".
- **read**: Read the current list (same as todoread).

## Item Fields
- id (string, required): unique identifier
- title (string, required): description
- status: not_started | in_progress | completed | skipped
- blocked_by: array of item IDs this task depends on
- summary: optional completion note

## Dependency Rules
- blocked_by IDs must reference existing items
- Cannot start (in_progress) until all blocked_by items are completed
- Cannot remove an item others depend on
- Circular dependencies are rejected

## Task Management Rules
- At most ONE task in in_progress at a time
- Complete or skip current tasks before starting new ones`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {
				Type: schema.String,
				Desc: `Action to perform: "update" (default), "add", "modify", "remove", "read".`,
			},
			"items": {
				Type: schema.Array,
				Desc: `Array of todo items for "update" or "add" actions.`,
			},
			"id": {
				Type: schema.String,
				Desc: `Item ID for "modify" or "remove" actions.`,
			},
			"status": {
				Type: schema.String,
				Desc: `New status for "modify" action.`,
			},
			"title": {
				Type: schema.String,
				Desc: `New title for "modify" action.`,
			},
			"todos": {
				Type: schema.Array,
				Desc: `Legacy compatibility: array of legacy-format todos. If present without action, treated as update.`,
			},
		}),
	}
	return &enhancedTodoWriteTool{store: store, info: info}
}

type enhancedTodoWriteTool struct {
	store *EnhancedTodoStore
	info  *schema.ToolInfo
}

func (t *enhancedTodoWriteTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *enhancedTodoWriteTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input EnhancedTodoInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse todowrite input: %w", err)
	}

	// Legacy compat: if todos field is present and no action, convert to update.
	if len(input.Todos) > 0 && input.Action == "" {
		input.Action = TodoActionUpdate
		input.Items = convertLegacyToEnhanced(input.Todos)
	}

	if input.Action == "" {
		input.Action = TodoActionUpdate
	}

	switch input.Action {
	case TodoActionUpdate:
		if err := t.store.Update(input.Items); err != nil {
			return "", err
		}
		result, _ := json.Marshal(t.store.Snapshot())
		return fmt.Sprintf("Updated. %s\n%s", t.store.Summary(), string(result)), nil

	case TodoActionAdd:
		if err := t.store.Add(input.Items); err != nil {
			return "", err
		}
		result, _ := json.Marshal(t.store.Snapshot())
		return fmt.Sprintf("Added %d item(s). %s\n%s", len(input.Items), t.store.Summary(), string(result)), nil

	case TodoActionModify:
		if input.ID == "" {
			return "", fmt.Errorf("modify action requires 'id'")
		}
		if err := t.store.Modify(input.ID, input.Status, input.Title); err != nil {
			return "", err
		}
		result, _ := json.Marshal(t.store.Snapshot())
		return fmt.Sprintf("Modified %q. %s\n%s", input.ID, t.store.Summary(), string(result)), nil

	case TodoActionRemove:
		if input.ID == "" {
			return "", fmt.Errorf("remove action requires 'id'")
		}
		if err := t.store.Remove(input.ID); err != nil {
			return "", err
		}
		result, _ := json.Marshal(t.store.Snapshot())
		return fmt.Sprintf("Removed %q. %s\n%s", input.ID, t.store.Summary(), string(result)), nil

	case TodoActionRead:
		return t.readSummary(), nil

	default:
		return "", fmt.Errorf("unknown action %q", input.Action)
	}
}

func (t *enhancedTodoWriteTool) readSummary() string {
	items := t.store.Snapshot()
	if len(items) == 0 {
		return "No todos yet."
	}
	result, _ := json.Marshal(items)
	return fmt.Sprintf("%s\n%s", t.store.Summary(), string(result))
}

// NewEnhancedTodoReadTool creates the todoread tool backed by an EnhancedTodoStore.
func NewEnhancedTodoReadTool(store *EnhancedTodoStore) tool.InvokableTool {
	info := &schema.ToolInfo{
		Name:        "todoread",
		Desc:        `Read the current todo list. Returns summary and full item listing.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}
	return &enhancedTodoReadTool{store: store, info: info}
}

type enhancedTodoReadTool struct {
	store *EnhancedTodoStore
	info  *schema.ToolInfo
}

func (t *enhancedTodoReadTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *enhancedTodoReadTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	items := t.store.Snapshot()
	if len(items) == 0 {
		return "No todos yet.", nil
	}
	result, _ := json.Marshal(items)
	return fmt.Sprintf("%s\n%s", t.store.Summary(), string(result)), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func convertLegacyToEnhanced(legacy []TodoItem) []EnhancedTodoItem {
	out := make([]EnhancedTodoItem, len(legacy))
	now := time.Now()
	for i, it := range legacy {
		status := string(it.Status)
		switch it.Status {
		case TodoPending:
			status = StatusNotStarted
		case TodoCancelled:
			status = StatusSkipped
		}
		out[i] = EnhancedTodoItem{
			ID:        fmt.Sprintf("%d", it.ID),
			Title:     it.Title,
			Status:    status,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
	return out
}
