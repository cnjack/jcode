package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// helper: creates an in-memory-only EnhancedTodoStore.
func newTestEnhancedStore(t *testing.T) *EnhancedTodoStore {
	t.Helper()
	return NewEnhancedTodoStore("test-session", nil)
}

// helper: creates an EnhancedTodoStore backed by a real StorageManager in a temp dir.
func newTestEnhancedStoreWithStorage(t *testing.T) (*EnhancedTodoStore, *StorageManager) {
	t.Helper()
	base := filepath.Join(t.TempDir(), "storage")
	subdirs := []string{"file-history", "tool-results", "todos", "plans", "tasks", "oauth"}
	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sm := &StorageManager{
		baseDir:    base,
		sessionID:  "test-session",
		writeQueue: NewWriteQueue(10 * time.Millisecond),
	}
	store := NewEnhancedTodoStore("test-session", sm)
	return store, sm
}

// enumStrings extracts a []string from a JSON-schema enum ([]any) for
// comparison.
func enumStrings(t *testing.T, enum []any) []string {
	t.Helper()
	out := make([]string, 0, len(enum))
	for _, v := range enum {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("expected string enum value, got %T (%v)", v, v)
		}
		out = append(out, s)
	}
	return out
}

func assertEnumEquals(t *testing.T, got []any, want []string) {
	t.Helper()
	gotStrs := enumStrings(t, got)
	if len(gotStrs) != len(want) {
		t.Fatalf("enum length mismatch: got %v, want %v", gotStrs, want)
	}
	set := make(map[string]bool, len(gotStrs))
	for _, s := range gotStrs {
		set[s] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Fatalf("enum missing %q: got %v", w, gotStrs)
		}
	}
}

// TD-S1: legacy todowrite declares an item schema for todos with the status
// enum (#29).
func TestTodoWriteSchema_TodosItemSchema(t *testing.T) {
	env := NewEnv(t.TempDir(), "linux/amd64")
	tool := env.NewTodoWriteTool()

	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}

	todos := js.Properties.Value("todos")
	if todos == nil {
		t.Fatal("expected todos property in schema")
	}
	if todos.Items == nil {
		t.Fatal("expected todos to declare an item schema (items)")
	}
	if todos.Items.Properties == nil {
		t.Fatal("expected todos item schema to declare properties")
	}
	for _, name := range []string{"id", "title", "status"} {
		if todos.Items.Properties.Value(name) == nil {
			t.Fatalf("expected todos item schema to declare %q", name)
		}
	}
	required := strings.Join(todos.Items.Required, ",")
	for _, name := range []string{"id", "title", "status"} {
		if !strings.Contains(required, name) {
			t.Fatalf("expected %q required, got: %v", name, todos.Items.Required)
		}
	}
	status := todos.Items.Properties.Value("status")
	assertEnumEquals(t, status.Enum, []string{"pending", "in_progress", "completed", "cancelled"})
}

// TD-S2: enhanced todowrite declares an item schema for items with the
// enhanced status enum (#29).
func TestEnhancedTodoWriteSchema_ItemsSchema(t *testing.T) {
	tool := NewEnhancedTodoWriteTool(newTestEnhancedStore(t))

	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}

	items := js.Properties.Value("items")
	if items == nil {
		t.Fatal("expected items property in schema")
	}
	if items.Items == nil {
		t.Fatal("expected items to declare an item schema (items)")
	}
	if items.Items.Properties == nil {
		t.Fatal("expected items item schema to declare properties")
	}
	for _, name := range []string{"id", "title", "status", "blocked_by", "summary"} {
		if items.Items.Properties.Value(name) == nil {
			t.Fatalf("expected items item schema to declare %q", name)
		}
	}
	required := strings.Join(items.Items.Required, ",")
	for _, name := range []string{"id", "title"} {
		if !strings.Contains(required, name) {
			t.Fatalf("expected %q required, got: %v", name, items.Items.Required)
		}
	}
	status := items.Items.Properties.Value("status")
	assertEnumEquals(t, status.Enum, []string{"not_started", "in_progress", "completed", "skipped"})

	// Legacy todos array also carries an item schema.
	todos := js.Properties.Value("todos")
	if todos == nil || todos.Items == nil {
		t.Fatal("expected legacy todos to declare an item schema (items)")
	}
}

// TD-01: legacy compat (todos array → update)
func TestTD01_LegacyCompat(t *testing.T) {
	store := newTestEnhancedStore(t)
	tw := NewEnhancedTodoWriteTool(store)

	input := `{"todos": [{"id": 1, "title": "task one", "status": "pending"}, {"id": 2, "title": "task two", "status": "in_progress"}]}`
	result, err := tw.InvokableRun(context.TODO(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Updated") {
		t.Fatalf("expected Updated in result, got: %s", result)
	}
	items := store.Snapshot()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	// Legacy "pending" maps to "not_started"
	if items[0].Status != StatusNotStarted {
		t.Errorf("expected not_started, got %s", items[0].Status)
	}
	if items[1].Status != StatusInProgress {
		t.Errorf("expected in_progress, got %s", items[1].Status)
	}
}

// TD-02: update action replaces all
func TestTD02_UpdateReplacesAll(t *testing.T) {
	store := newTestEnhancedStore(t)

	// Seed with initial items.
	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "old", Status: StatusNotStarted},
	})

	tw := NewEnhancedTodoWriteTool(store)
	input := `{"action": "update", "items": [{"id": "x", "title": "new", "status": "not_started"}]}`
	_, err := tw.InvokableRun(context.TODO(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := store.Snapshot()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].ID != "x" {
		t.Errorf("expected id 'x', got %q", items[0].ID)
	}
}

// TD-03: add action appends
func TestTD03_AddAppends(t *testing.T) {
	store := newTestEnhancedStore(t)
	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "first", Status: StatusNotStarted},
	})

	tw := NewEnhancedTodoWriteTool(store)
	input := `{"action": "add", "items": [{"id": "b", "title": "second", "status": "not_started"}]}`
	result, err := tw.InvokableRun(context.TODO(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Added 1") {
		t.Errorf("expected 'Added 1' in result, got: %s", result)
	}
	if len(store.Snapshot()) != 2 {
		t.Fatalf("expected 2 items, got %d", len(store.Snapshot()))
	}
}

// TD-04: modify action changes status
func TestTD04_ModifyStatus(t *testing.T) {
	store := newTestEnhancedStore(t)
	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "task", Status: StatusNotStarted},
	})

	tw := NewEnhancedTodoWriteTool(store)
	input := `{"action": "modify", "id": "a", "status": "in_progress"}`
	_, err := tw.InvokableRun(context.TODO(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.Snapshot()[0].Status != StatusInProgress {
		t.Errorf("expected in_progress, got %s", store.Snapshot()[0].Status)
	}
}

// TD-05: remove action deletes
func TestTD05_Remove(t *testing.T) {
	store := newTestEnhancedStore(t)
	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "first", Status: StatusCompleted},
		{ID: "b", Title: "second", Status: StatusNotStarted},
	})

	tw := NewEnhancedTodoWriteTool(store)
	input := `{"action": "remove", "id": "a"}`
	_, err := tw.InvokableRun(context.TODO(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := store.Snapshot()
	if len(items) != 1 || items[0].ID != "b" {
		t.Fatalf("expected only item b, got %v", items)
	}
}

// TD-06: read action returns summary
func TestTD06_ReadAction(t *testing.T) {
	store := newTestEnhancedStore(t)
	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "task", Status: StatusCompleted},
	})

	tw := NewEnhancedTodoWriteTool(store)
	result, err := tw.InvokableRun(context.TODO(), `{"action": "read"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "1 todos") {
		t.Errorf("expected summary in result, got: %s", result)
	}
}

// TD-07: blocked_by referencing non-existent ID → error
func TestTD07_BlockedByNonExistent(t *testing.T) {
	store := newTestEnhancedStore(t)
	err := store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "task", Status: StatusNotStarted, BlockedBy: []string{"z"}},
	})
	if err == nil {
		t.Fatal("expected error for non-existent blocked_by")
	}
	if !strings.Contains(err.Error(), "non-existent") {
		t.Errorf("expected non-existent mention, got: %v", err)
	}
}

// TD-08: circular dependency detected
func TestTD08_CircularDependency(t *testing.T) {
	store := newTestEnhancedStore(t)
	err := store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "t1", Status: StatusNotStarted, BlockedBy: []string{"b"}},
		{ID: "b", Title: "t2", Status: StatusNotStarted, BlockedBy: []string{"a"}},
	})
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("expected circular mention, got: %v", err)
	}
}

// TD-09: blocked_by not completed → cannot start
func TestTD09_BlockedByNotCompleted(t *testing.T) {
	store := newTestEnhancedStore(t)
	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "blocker", Status: StatusNotStarted},
		{ID: "b", Title: "blocked", Status: StatusNotStarted, BlockedBy: []string{"a"}},
	})
	err := store.Modify("b", StatusInProgress, "")
	if err == nil {
		t.Fatal("expected error when starting blocked item")
	}
	if !strings.Contains(err.Error(), "blocked by") {
		t.Errorf("expected 'blocked by' mention, got: %v", err)
	}
}

// TD-10: dependencies all completed → can start
func TestTD10_DependenciesCompleted(t *testing.T) {
	store := newTestEnhancedStore(t)
	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "blocker", Status: StatusCompleted},
		{ID: "b", Title: "blocked", Status: StatusNotStarted, BlockedBy: []string{"a"}},
	})
	err := store.Modify("b", StatusInProgress, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.Snapshot()[1].Status != StatusInProgress {
		t.Errorf("expected in_progress, got %s", store.Snapshot()[1].Status)
	}
}

// TD-11: cannot remove item that others depend on
func TestTD11_CannotRemoveDependency(t *testing.T) {
	store := newTestEnhancedStore(t)
	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "blocker", Status: StatusCompleted},
		{ID: "b", Title: "blocked", Status: StatusNotStarted, BlockedBy: []string{"a"}},
	})
	err := store.Remove("a")
	if err == nil {
		t.Fatal("expected error removing depended item")
	}
	if !strings.Contains(err.Error(), "depends on it") {
		t.Errorf("expected 'depends on' mention, got: %v", err)
	}
}

// TD-12: save format is correct JSON with version=2
func TestTD12_SaveFormat(t *testing.T) {
	store, sm := newTestEnhancedStoreWithStorage(t)
	defer func() { _ = sm.Close() }()

	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "task", Status: StatusNotStarted},
	})

	// Flush async writes.
	sm.writeQueue.DrainSync()

	path := filepath.Join(sm.TodosDir(), "test-session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	var file TodoFileFormat
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if file.Version != 2 {
		t.Errorf("expected version 2, got %d", file.Version)
	}
	if file.SessionID != "test-session" {
		t.Errorf("expected session_id test-session, got %q", file.SessionID)
	}
	if len(file.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(file.Items))
	}
}

// TD-14: Load from disk
func TestTD14_LoadFromDisk(t *testing.T) {
	base := filepath.Join(t.TempDir(), "storage")
	subdirs := []string{"file-history", "tool-results", "todos", "plans", "tasks", "oauth"}
	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sm := &StorageManager{
		baseDir:    base,
		sessionID:  "load-test",
		writeQueue: NewWriteQueue(10 * time.Millisecond),
	}
	defer func() { _ = sm.Close() }()

	// Write a file to disk manually.
	file := TodoFileFormat{
		Version:   2,
		SessionID: "load-test",
		UpdatedAt: time.Now(),
		Items: []EnhancedTodoItem{
			{ID: "x", Title: "loaded", Status: StatusNotStarted},
		},
	}
	data, _ := json.Marshal(file)
	path := filepath.Join(sm.TodosDir(), "load-test.json")
	_ = os.WriteFile(path, data, 0o600)

	// Create store — should load automatically.
	store := NewEnhancedTodoStore("load-test", sm)
	items := store.Snapshot()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].ID != "x" || items[0].Title != "loaded" {
		t.Errorf("unexpected item: %+v", items[0])
	}
}

// TD-15: corrupted JSON doesn't panic
func TestTD15_CorruptedJSON(t *testing.T) {
	base := filepath.Join(t.TempDir(), "storage")
	subdirs := []string{"file-history", "tool-results", "todos", "plans", "tasks", "oauth"}
	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sm := &StorageManager{
		baseDir:    base,
		sessionID:  "corrupt-test",
		writeQueue: NewWriteQueue(10 * time.Millisecond),
	}
	defer func() { _ = sm.Close() }()

	// Write garbage.
	path := filepath.Join(sm.TodosDir(), "corrupt-test.json")
	_ = os.WriteFile(path, []byte("{invalid json!!!"), 0o600)

	// Should not panic.
	store := NewEnhancedTodoStore("corrupt-test", sm)
	if store.HasItems() {
		t.Error("expected no items after corrupt load")
	}
}

// TD-17: skipped is valid status
func TestTD17_SkippedStatus(t *testing.T) {
	store := newTestEnhancedStore(t)
	err := store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "task", Status: StatusSkipped},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.Snapshot()[0].Status != StatusSkipped {
		t.Errorf("expected skipped, got %s", store.Snapshot()[0].Status)
	}
	// Skipped should count as complete for HasIncomplete.
	if store.HasIncomplete() {
		t.Error("expected no incomplete items")
	}
}

// TD-18: at most 1 in_progress
func TestTD18_AtMostOneInProgress(t *testing.T) {
	store := newTestEnhancedStore(t)
	err := store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "t1", Status: StatusInProgress},
		{ID: "b", Title: "t2", Status: StatusInProgress},
	})
	if err == nil {
		t.Fatal("expected error for multiple in_progress")
	}
	if !strings.Contains(err.Error(), "at most 1") {
		t.Errorf("expected 'at most 1' mention, got: %v", err)
	}
}

// TD-19: duplicate ID returns error
func TestTD19_DuplicateID(t *testing.T) {
	store := newTestEnhancedStore(t)
	err := store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "t1", Status: StatusNotStarted},
		{ID: "a", Title: "t2", Status: StatusNotStarted},
	})
	if err == nil {
		t.Fatal("expected error for duplicate IDs")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected 'duplicate' mention, got: %v", err)
	}
}

// --- Additional edge-case tests ---

func TestEnhanced_ReadToolEmpty(t *testing.T) {
	store := newTestEnhancedStore(t)
	tr := NewEnhancedTodoReadTool(store)
	result, err := tr.InvokableRun(context.TODO(), "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No todos yet." {
		t.Errorf("expected 'No todos yet.', got: %s", result)
	}
}

func TestEnhanced_AddConflictingID(t *testing.T) {
	store := newTestEnhancedStore(t)
	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "first", Status: StatusNotStarted},
	})
	err := store.Add([]EnhancedTodoItem{
		{ID: "a", Title: "dup", Status: StatusNotStarted},
	})
	if err == nil {
		t.Fatal("expected error for conflicting add")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists', got: %v", err)
	}
}

func TestEnhanced_ItemsBackwardCompat(t *testing.T) {
	store := newTestEnhancedStore(t)
	_ = store.Update([]EnhancedTodoItem{
		{ID: "1", Title: "task", Status: StatusNotStarted},
		{ID: "2", Title: "done", Status: StatusSkipped},
	})
	legacyItems := store.Items()
	if len(legacyItems) != 2 {
		t.Fatalf("expected 2 items, got %d", len(legacyItems))
	}
	if legacyItems[0].Status != TodoPending {
		t.Errorf("expected pending, got %s", legacyItems[0].Status)
	}
	if legacyItems[1].Status != TodoCancelled {
		t.Errorf("expected cancelled, got %s", legacyItems[1].Status)
	}
}

func TestEnhanced_IncompleteSummary(t *testing.T) {
	store := newTestEnhancedStore(t)
	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "pending task", Status: StatusNotStarted},
		{ID: "b", Title: "done task", Status: StatusCompleted},
	})
	summary := store.IncompleteSummary()
	if !strings.Contains(summary, "pending task") {
		t.Errorf("expected 'pending task' in summary, got: %s", summary)
	}
	if strings.Contains(summary, "done task") {
		t.Errorf("did not expect 'done task' in summary, got: %s", summary)
	}
}

func TestEnhanced_OnChangeCallback(t *testing.T) {
	store := newTestEnhancedStore(t)
	called := false
	store.SetOnChange(func(items []EnhancedTodoItem) {
		called = true
	})
	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "task", Status: StatusNotStarted},
	})
	if !called {
		t.Error("expected onChange callback to be called")
	}
}

func TestEnhanced_SaveSync(t *testing.T) {
	store, sm := newTestEnhancedStoreWithStorage(t)
	defer func() { _ = sm.Close() }()

	_ = store.Update([]EnhancedTodoItem{
		{ID: "a", Title: "task", Status: StatusNotStarted},
	})

	if err := store.SaveSync(); err != nil {
		t.Fatalf("SaveSync failed: %v", err)
	}

	path := filepath.Join(sm.TodosDir(), "test-session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	var file TodoFileFormat
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if file.Version != 2 {
		t.Errorf("expected version 2, got %d", file.Version)
	}
}
