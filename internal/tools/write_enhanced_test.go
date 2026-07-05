package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// W-01: Normal write creates file with correct content.
func TestWrite_NormalWrite(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewWriteTool()

	file := filepath.Join(dir, "new.txt")
	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","content":"hello world\n"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Created") {
		t.Fatalf("expected 'Created' in result, got: %s", result)
	}
	if !strings.Contains(result, file) {
		t.Fatalf("expected file path in result, got: %s", result)
	}

	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(content) != "hello world\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

// W-02: Conflict detection (read file, externally modify, then write → conflict message).
func TestWrite_ConflictDetection(t *testing.T) {
	env, dir := newTestEnv(t)
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()
	ft := NewFileTracker(sm)
	env.FileTracker = ft

	tool := env.NewWriteTool()

	// Create and track the original file.
	file := filepath.Join(dir, "conflict.txt")
	if err := os.WriteFile(file, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)
	ft.TrackRead(file, []byte("original"), info.ModTime())

	// Externally modify the file.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(file, []byte("externally modified"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write should detect the conflict.
	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","content":"new content"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "conflict") {
		t.Fatalf("expected conflict message, got: %s", result)
	}

	// File should not have been overwritten.
	content, _ := os.ReadFile(file)
	if string(content) != "externally modified" {
		t.Fatalf("file should not have been overwritten, got: %q", content)
	}
}

// W-03: Auto backup (write to tracked file → backup exists).
func TestWrite_AutoBackup(t *testing.T) {
	env, dir := newTestEnv(t)
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()
	ft := NewFileTracker(sm)
	env.FileTracker = ft

	tool := env.NewWriteTool()

	// Create, read (track), then overwrite.
	file := filepath.Join(dir, "backup.txt")
	if err := os.WriteFile(file, []byte("original content"), 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)
	ft.TrackRead(file, []byte("original content"), info.ModTime())

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","content":"updated content"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Backup:") {
		t.Fatalf("expected backup path in result, got: %s", result)
	}

	// Verify backup exists in file-history dir.
	entries, err := os.ReadDir(sm.FileHistoryDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one backup file")
	}

	// Verify backup content.
	backupData, err := os.ReadFile(filepath.Join(sm.FileHistoryDir(), entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if string(backupData) != "original content" {
		t.Fatalf("backup content mismatch: %q", backupData)
	}
}

// W-04: Unified diff output (overwrite → output contains diff).
func TestWrite_UnifiedDiff(t *testing.T) {
	env, dir := newTestEnv(t)
	sm := newTestStorageManager(t)
	defer func() { _ = sm.Close() }()
	ft := NewFileTracker(sm)
	env.FileTracker = ft

	tool := env.NewWriteTool()

	file := filepath.Join(dir, "diff.txt")
	if err := os.WriteFile(file, []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(file)
	ft.TrackRead(file, []byte("line1\nline2\nline3\n"), info.ModTime())

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","content":"line1\nmodified\nline3\n"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "diff") {
		t.Fatalf("expected diff in output, got: %s", result)
	}
	if !strings.Contains(result, "-line2") || !strings.Contains(result, "+modified") {
		t.Fatalf("expected diff showing line2→modified change, got: %s", result)
	}
}

// W-06: Overwriting an existing file that was never read is rejected (#7).
func TestWrite_RejectsUnreadExistingFile(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewWriteTool()

	file := filepath.Join(dir, "unread.txt")
	if err := os.WriteFile(file, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","content":"overwritten"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "has not been read") {
		t.Fatalf("expected read-before-write rejection, got: %s", result)
	}

	content, _ := os.ReadFile(file)
	if string(content) != "original" {
		t.Fatalf("file must not be overwritten, got: %q", content)
	}
}

// W-07: Writing a brand-new file requires no prior read.
func TestWrite_CreateNewFile_NoReadRequired(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewWriteTool()

	file := filepath.Join(dir, "brand_new.txt")
	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","content":"fresh content\n"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Created") {
		t.Fatalf("expected file creation, got: %s", result)
	}

	content, _ := os.ReadFile(file)
	if string(content) != "fresh content\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

// W-05: >10MB content returns error.
func TestWrite_ContentTooLarge(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewWriteTool()

	file := filepath.Join(dir, "huge.txt")
	bigContent := strings.Repeat("x", MaxWriteFileSize+1)

	_, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","content":"`+bigContent+`"}`)
	if err == nil {
		t.Fatal("expected error for oversized content")
	}
	if !strings.Contains(err.Error(), "content too large") {
		t.Fatalf("expected 'content too large' error, got: %v", err)
	}

	// File should not have been created.
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("file should not exist after rejected write")
	}
}

// W-06 (#28): Malformed JSON arguments carry a re-emit hint.
func TestWrite_InvalidArgs_Hint(t *testing.T) {
	env, _ := newTestEnv(t)
	tool := env.NewWriteTool()

	_, err := tool.InvokableRun(context.Background(), `{"file_path":`)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Fatalf("expected JSON hint, got: %s", err.Error())
	}
}

// W-07 (#28): A failed write carries a writable-location hint.
func TestWrite_Failed_Hint(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission checks are not enforced")
	}
	env, dir := newTestEnv(t)
	tool := env.NewWriteTool()

	roDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(roDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(roDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0755) })

	_, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+filepath.Join(roDir, "denied.txt")+`","content":"x"}`)
	if err == nil {
		t.Fatal("expected error writing into a read-only directory")
	}
	if !strings.Contains(err.Error(), "failed to write file") {
		t.Fatalf("original error text must be preserved, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "writable") {
		t.Fatalf("expected writable-location hint, got: %s", err.Error())
	}
}
