package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestEnv(t *testing.T) (*Env, string) {
	t.Helper()
	dir := t.TempDir()
	env := NewEnv(dir, "linux/amd64")
	return env, dir
}

// E-01: Backward compatibility — single old_string/new_string edit.
func TestEdit_SingleEdit(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(file, []byte("hello world\n"), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","old_string":"hello","new_string":"goodbye"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Successfully replaced 1 occurrence") {
		t.Fatalf("unexpected result: %s", result)
	}

	content, _ := os.ReadFile(file)
	if string(content) != "goodbye world\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

// E-02: Multi-edit applies edits in order.
func TestEdit_MultiEdit(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "multi.txt")
	_ = os.WriteFile(file, []byte("aaa bbb ccc\n"), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","edits":[{"old_string":"aaa","new_string":"AAA"},{"old_string":"bbb","new_string":"BBB"}]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Successfully applied 2 edit(s)") {
		t.Fatalf("unexpected result: %s", result)
	}

	content, _ := os.ReadFile(file)
	if string(content) != "AAA BBB ccc\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

// E-03: Multi-edit failure reports which edit failed.
func TestEdit_MultiEditFailure(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "fail.txt")
	_ = os.WriteFile(file, []byte("aaa bbb ccc\n"), 0644)

	_, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","edits":[{"old_string":"aaa","new_string":"AAA"},{"old_string":"MISSING","new_string":"XXX"}]}`)
	if err == nil {
		t.Fatal("expected error for missing old_string")
	}
	if !strings.Contains(err.Error(), "edit #2") {
		t.Fatalf("expected error to mention edit #2, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "1 of 2 edits applied successfully") {
		t.Fatalf("expected error to mention edits applied count, got: %s", err.Error())
	}

	// File should NOT have been written (atomic: fail before write).
	content, _ := os.ReadFile(file)
	if string(content) != "aaa bbb ccc\n" {
		t.Fatalf("file should not have been modified on failure, got: %q", content)
	}
}

// E-07: Binary file rejected.
func TestEdit_BinaryRejected(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "image.png")
	_ = os.WriteFile(file, []byte("fake png"), 0644)

	_, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","old_string":"fake","new_string":"real"}`)
	if err == nil {
		t.Fatal("expected error for binary file")
	}
	if !strings.Contains(err.Error(), "binary file") {
		t.Fatalf("expected binary file error, got: %s", err.Error())
	}
}

// E-09: Empty old_string creates new file (existing behavior).
func TestEdit_CreateFile(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "new_file.txt")
	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","old_string":"","new_string":"new content\n"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Created file") {
		t.Fatalf("unexpected result: %s", result)
	}

	content, _ := os.ReadFile(file)
	if string(content) != "new content\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

// E-06: Output includes unified diff format.
func TestEdit_UnifiedDiffOutput(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "diff.txt")
	_ = os.WriteFile(file, []byte("line1\nline2\nline3\n"), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","old_string":"line2","new_string":"LINE_TWO"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "---") || !strings.Contains(result, "+++") {
		t.Fatalf("expected unified diff with --- and +++ headers, got: %s", result)
	}
	if !strings.Contains(result, "@@") {
		t.Fatalf("expected hunk header @@, got: %s", result)
	}
}

// Test mutually exclusive edits and old_string.
func TestEdit_MutuallyExclusive(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "excl.txt")
	_ = os.WriteFile(file, []byte("content\n"), 0644)

	_, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","old_string":"content","new_string":"new","edits":[{"old_string":"a","new_string":"b"}]}`)
	if err == nil {
		t.Fatal("expected error for mutually exclusive params")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got: %s", err.Error())
	}
}
