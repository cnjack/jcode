package tools

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// StorageManager
// ---------------------------------------------------------------------------

func TestStorageManagerCreatesDirectories(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()

	subdirs := []string{"file-history", "tool-results", "todos", "plans", "tasks", "oauth"}
	for _, sub := range subdirs {
		dir := filepath.Join(sm.baseDir, sub)
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected dir %s to exist: %v", sub, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", sub)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("expected %s perms 0700, got %o", sub, perm)
		}
	}
}

func TestStorageManagerWrite(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()

	path := filepath.Join(sm.baseDir, "test", "hello.txt")
	if err := sm.Write(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("expected hello, got %s", data)
	}
}

// ---------------------------------------------------------------------------
// WriteQueue
// ---------------------------------------------------------------------------

func TestWriteQueueAsyncAndDrainSync(t *testing.T) {
	dir := t.TempDir()
	wq := NewWriteQueue(50 * time.Millisecond)

	path := filepath.Join(dir, "out.txt")
	wq.Enqueue(path, WriteEntry{Data: []byte("abc"), Mode: 0o644})
	wq.DrainSync()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abc" {
		t.Fatalf("expected abc, got %s", data)
	}

	wq.Close()
}

func TestWriteQueueAppend(t *testing.T) {
	dir := t.TempDir()
	wq := NewWriteQueue(50 * time.Millisecond)

	path := filepath.Join(dir, "append.txt")
	wq.Enqueue(path, WriteEntry{Data: []byte("line1\n"), Mode: 0o644, Append: true})
	wq.Enqueue(path, WriteEntry{Data: []byte("line2\n"), Mode: 0o644, Append: true})
	wq.DrainSync()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "line1\nline2\n" {
		t.Fatalf("unexpected content: %q", data)
	}

	wq.Close()
}

func TestWriteQueueConcurrency(t *testing.T) {
	dir := t.TempDir()
	wq := NewWriteQueue(10 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			p := filepath.Join(dir, itoa(n)+".txt")
			wq.Enqueue(p, WriteEntry{Data: []byte("data"), Mode: 0o644})
		}(i)
	}
	wg.Wait()
	wq.DrainSync()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 50 {
		t.Fatalf("expected 50 files, got %d", len(entries))
	}

	wq.Close()
}

// ---------------------------------------------------------------------------
// FileTracker
// ---------------------------------------------------------------------------

func TestFileTrackerNoConflict(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()
	ft := NewFileTracker(sm)

	// Write a file, track it, check — no conflict.
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	ft.TrackRead(path, []byte("hello"), info.ModTime())

	cr, err := ft.CheckConflict(path)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Status != ConflictNone {
		t.Fatalf("expected ConflictNone, got %d", cr.Status)
	}
}

func TestFileTrackerConflictModified(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()
	ft := NewFileTracker(sm)

	path := filepath.Join(t.TempDir(), "b.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	ft.TrackRead(path, []byte("original"), info.ModTime())

	// Overwrite with different content.
	time.Sleep(10 * time.Millisecond) // ensure mtime differs
	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	cr, err := ft.CheckConflict(path)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Status != ConflictModified {
		t.Fatalf("expected ConflictModified, got %d", cr.Status)
	}
	if cr.OldHash == cr.NewHash {
		t.Fatal("hashes should differ")
	}
}

func TestFileTrackerConflictFileGone(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()
	ft := NewFileTracker(sm)

	path := filepath.Join(t.TempDir(), "c.txt")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	ft.TrackRead(path, []byte("data"), info.ModTime())

	_ = os.Remove(path)
	cr, err := ft.CheckConflict(path)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Status != ConflictFileGone {
		t.Fatalf("expected ConflictFileGone, got %d", cr.Status)
	}
}

func TestFileTrackerTouchOnly(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()
	ft := NewFileTracker(sm)

	path := filepath.Join(t.TempDir(), "d.txt")
	content := []byte("same")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	ft.TrackRead(path, content, info.ModTime())

	// Touch the file (same content, different mtime).
	future := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	cr, err := ft.CheckConflict(path)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Status != ConflictNone {
		t.Fatalf("expected ConflictNone for touch, got %d", cr.Status)
	}
}

func TestFileTrackerCreateBackup(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()
	ft := NewFileTracker(sm)

	path := "/some/project/main.go"
	bp1, err := ft.CreateBackup(path, []byte("v1"))
	if err != nil {
		t.Fatal(err)
	}
	bp2, err := ft.CreateBackup(path, []byte("v2"))
	if err != nil {
		t.Fatal(err)
	}

	if bp1 == bp2 {
		t.Fatal("backup paths should differ between versions")
	}
	if !strings.Contains(bp1, "_v1_") {
		t.Fatalf("first backup should contain _v1_, got %s", bp1)
	}
	if !strings.Contains(bp2, "_v2_") {
		t.Fatalf("second backup should contain _v2_, got %s", bp2)
	}

	// Verify content.
	data, err := os.ReadFile(bp2)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v2" {
		t.Fatalf("expected v2, got %s", data)
	}
}

// ---------------------------------------------------------------------------
// ToolResultStore
// ---------------------------------------------------------------------------

func TestToolResultStoreSmallResult(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()
	ts := NewToolResultStore(sm)

	_, persisted := ts.PersistIfLarge("test", "small output")
	if persisted {
		t.Fatal("small result should not be persisted")
	}
}

func TestToolResultStoreLargeResult(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()
	ts := NewToolResultStore(sm)

	big := strings.Repeat("x", 60000)
	pr, persisted := ts.PersistIfLarge("grep", big)
	if !persisted {
		t.Fatal("large result should be persisted")
	}
	if pr.OriginalSize != 60000 {
		t.Fatalf("expected size 60000, got %d", pr.OriginalSize)
	}
	if len(pr.Preview) != 500 {
		t.Fatalf("expected preview len 500, got %d", len(pr.Preview))
	}
	if !pr.HasMore {
		t.Fatal("expected HasMore=true")
	}

	// Retrieve.
	content, err := ts.Retrieve(pr.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 60000 {
		t.Fatalf("expected retrieved len 60000, got %d", len(content))
	}
}

// ---------------------------------------------------------------------------
// TokenStore
// ---------------------------------------------------------------------------

func TestTokenStoreSaveGetDelete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "oauth")
	ts := NewTokenStore(dir)

	token := OAuthToken{
		AccessToken:  "tok_abc",
		RefreshToken: "ref_xyz",
		Provider:     "github",
		Scopes:       []string{"repo", "read:user"},
		ExpiresAt:    time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := ts.Save("github", token); err != nil {
		t.Fatal(err)
	}

	// Check permissions.
	info, err := os.Stat(filepath.Join(dir, "github.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600, got %o", perm)
	}

	// Get.
	got, err := ts.Get("github")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected token, got nil")
	}
	if got.AccessToken != "tok_abc" {
		t.Fatalf("token mismatch: %s", got.AccessToken)
	}
	if len(got.Scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(got.Scopes))
	}

	// Get non-existent.
	missing, err := ts.Get("gitlab")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatal("expected nil for missing provider")
	}

	// Delete.
	if err := ts.Delete("github"); err != nil {
		t.Fatal(err)
	}
	after, _ := ts.Get("github")
	if after != nil {
		t.Fatal("expected nil after delete")
	}

	// Delete non-existent is not an error.
	if err := ts.Delete("nope"); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestStorageManager creates a StorageManager rooted in a temp directory.
func newTestStorageManager(t *testing.T) *StorageManager {
	t.Helper()
	base := filepath.Join(t.TempDir(), "storage")
	subdirs := []string{"file-history", "tool-results", "todos", "plans", "tasks", "oauth"}
	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return &StorageManager{
		baseDir:    base,
		sessionID:  "test-session",
		writeQueue: NewWriteQueue(10 * time.Millisecond),
	}
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

func TestCleanupTodos(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()

	// Create 25 todo files with distinct modification times.
	for i := 0; i < 25; i++ {
		path := filepath.Join(sm.TodosDir(), "todo_"+itoa(i)+".json")
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		// Set mtime so ordering is deterministic: older files have earlier times.
		mtime := time.Now().Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	if err := sm.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	entries, err := os.ReadDir(sm.TodosDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Fatalf("expected 20 todos after cleanup, got %d", len(entries))
	}

	// Verify the 5 oldest were removed (todo_0 through todo_4).
	remaining := make(map[string]bool)
	for _, e := range entries {
		remaining[e.Name()] = true
	}
	for i := 0; i < 5; i++ {
		name := "todo_" + itoa(i) + ".json"
		if remaining[name] {
			t.Fatalf("expected %s to be deleted (oldest), but it remains", name)
		}
	}
	// The newest 20 should remain.
	for i := 5; i < 25; i++ {
		name := "todo_" + itoa(i) + ".json"
		if !remaining[name] {
			t.Fatalf("expected %s to remain (newest), but it was deleted", name)
		}
	}
}

func TestCleanupTaskLogs(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()

	// Create 55 task log files with distinct modification times.
	for i := 0; i < 55; i++ {
		path := filepath.Join(sm.TasksDir(), "task_"+itoa(i)+".log")
		if err := os.WriteFile(path, []byte("log"), 0o600); err != nil {
			t.Fatal(err)
		}
		mtime := time.Now().Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}

	if err := sm.Cleanup(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	entries, err := os.ReadDir(sm.TasksDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 50 {
		t.Fatalf("expected 50 tasks after cleanup, got %d", len(entries))
	}

	// Verify the 5 oldest were removed.
	remaining := make(map[string]bool)
	for _, e := range entries {
		remaining[e.Name()] = true
	}
	for i := 0; i < 5; i++ {
		name := "task_" + itoa(i) + ".log"
		if remaining[name] {
			t.Fatalf("expected %s to be deleted (oldest), but it remains", name)
		}
	}
}

func TestFileTrackerEviction(t *testing.T) {
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()
	ft := NewFileTracker(sm)

	// Create >100 backups by simulating writes to different "files".
	for i := 0; i < 110; i++ {
		fakePath := "/project/file_" + itoa(i) + ".go"
		_, err := ft.CreateBackup(fakePath, []byte("content_"+itoa(i)))
		if err != nil {
			t.Fatalf("CreateBackup failed at i=%d: %v", i, err)
		}
	}

	// Verify the file-history directory has at most maxSnaps (100) files.
	entries, err := os.ReadDir(sm.FileHistoryDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > ft.maxSnaps {
		t.Fatalf("expected at most %d backups, got %d", ft.maxSnaps, len(entries))
	}
}
