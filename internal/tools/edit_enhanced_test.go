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

// trackFile registers path in the env's FileTracker as if the agent had read
// it, satisfying the read-before-edit guard for tests that pre-seed files on
// disk with os.WriteFile.
func trackFile(t *testing.T, env *Env, path string) {
	t.Helper()
	if env.FileTracker == nil {
		t.Fatal("env.FileTracker is nil; NewEnv must wire a FileTracker")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	env.FileTracker.TrackRead(path, content, info.ModTime())
}

// E-01: Backward compatibility — single old_string/new_string edit.
func TestEdit_SingleEdit(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(file, []byte("hello world\n"), 0644)
	trackFile(t, env, file)

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
	trackFile(t, env, file)

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
	trackFile(t, env, file)

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
	trackFile(t, env, file)

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

// E-10: Editing an existing file that was never read is rejected (#7).
func TestEdit_RejectsUnreadExistingFile(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "unread.txt")
	_ = os.WriteFile(file, []byte("hello world\n"), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","old_string":"hello","new_string":"goodbye"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "has not been read") {
		t.Fatalf("expected read-before-edit rejection, got: %s", result)
	}

	content, _ := os.ReadFile(file)
	if string(content) != "hello world\n" {
		t.Fatalf("file must not be modified, got: %q", content)
	}
}

// E-11: A file created via edit's create mode can be edited immediately
// without an intervening read (createFile must register in the tracker).
func TestEdit_AllowsEditAfterCreate(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "created.txt")
	if _, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","old_string":"","new_string":"alpha beta\n"}`); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","old_string":"alpha","new_string":"gamma"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Successfully replaced 1 occurrence") {
		t.Fatalf("expected successful edit after create, got: %s", result)
	}

	content, _ := os.ReadFile(file)
	if string(content) != "gamma beta\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

// E-12: Running the read tool first satisfies the read-before-edit guard.
func TestEdit_AllowsEditAfterRead(t *testing.T) {
	env, dir := newTestEnv(t)
	readTool := env.NewReadTool()
	editTool := env.NewEditTool()

	file := filepath.Join(dir, "readfirst.txt")
	_ = os.WriteFile(file, []byte("hello world\n"), 0644)

	if _, err := readTool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`"}`); err != nil {
		t.Fatalf("read failed: %v", err)
	}

	result, err := editTool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","old_string":"hello","new_string":"goodbye"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Successfully replaced 1 occurrence") {
		t.Fatalf("expected successful edit after read, got: %s", result)
	}
}

// E-13: Multi-edit rejects an ambiguous old_string (multiple matches, no
// replace_all) instead of silently replacing the first occurrence (#23).
func TestEdit_MultiEditAmbiguousOldString(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "ambig.txt")
	_ = os.WriteFile(file, []byte("foo bar foo\n"), 0644)
	trackFile(t, env, file)

	_, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","edits":[{"old_string":"foo","new_string":"qux"}]}`)
	if err == nil {
		t.Fatal("expected error for ambiguous old_string")
	}
	if !strings.Contains(err.Error(), "appears 2 times") {
		t.Fatalf("expected ambiguity error with count, got: %s", err.Error())
	}

	content, _ := os.ReadFile(file)
	if string(content) != "foo bar foo\n" {
		t.Fatalf("file must not be modified on failure, got: %q", content)
	}
}

// E-14: Multi-edit honours per-op replace_all.
func TestEdit_MultiEditPerOpReplaceAll(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "replall.txt")
	_ = os.WriteFile(file, []byte("foo bar foo\n"), 0644)
	trackFile(t, env, file)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","edits":[{"old_string":"foo","new_string":"qux","replace_all":true}]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Successfully applied 1 edit(s)") {
		t.Fatalf("unexpected result: %s", result)
	}

	content, _ := os.ReadFile(file)
	if string(content) != "qux bar qux\n" {
		t.Fatalf("expected both occurrences replaced, got: %q", content)
	}
}

// E-15: Multi-edit rejects an op whose old_string only matches text inserted
// by an earlier op's new_string (overlap guard).
func TestEdit_MultiEditOverlapRejected(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "overlap.txt")
	_ = os.WriteFile(file, []byte("hello world\n"), 0644)
	trackFile(t, env, file)

	_, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","edits":[{"old_string":"world","new_string":"beautiful world"},{"old_string":"beautiful","new_string":"ugly"}]}`)
	if err == nil {
		t.Fatal("expected error for overlapping edits")
	}
	if !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("expected overlap error, got: %s", err.Error())
	}

	content, _ := os.ReadFile(file)
	if string(content) != "hello world\n" {
		t.Fatalf("file must not be modified on failure, got: %q", content)
	}
}

// E-16: Multi-edit rejects an empty old_string op.
func TestEdit_MultiEditEmptyOldString(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewEditTool()

	file := filepath.Join(dir, "emptyold.txt")
	_ = os.WriteFile(file, []byte("content\n"), 0644)
	trackFile(t, env, file)

	_, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","edits":[{"old_string":"","new_string":"x"}]}`)
	if err == nil {
		t.Fatal("expected error for empty old_string in multi-edit")
	}
	if !strings.Contains(err.Error(), "old_string must not be empty") {
		t.Fatalf("expected empty old_string error, got: %s", err.Error())
	}

	content, _ := os.ReadFile(file)
	if string(content) != "content\n" {
		t.Fatalf("file must not be modified on failure, got: %q", content)
	}
}

// E-17: The edits array parameter declares a machine-readable item schema
// with old_string/new_string/replace_all (#29).
func TestEditToolSchema_EditsItemSchema(t *testing.T) {
	env, _ := newTestEnv(t)
	tool := env.NewEditTool()

	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	js, err := info.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}

	edits := js.Properties.Value("edits")
	if edits == nil {
		t.Fatal("expected edits property in schema")
	}
	if edits.Items == nil {
		t.Fatal("expected edits to declare an item schema (items)")
	}
	if edits.Items.Properties == nil {
		t.Fatal("expected edits item schema to declare properties")
	}
	for _, name := range []string{"old_string", "new_string", "replace_all"} {
		if edits.Items.Properties.Value(name) == nil {
			t.Fatalf("expected edits item schema to declare %q", name)
		}
	}
	required := strings.Join(edits.Items.Required, ",")
	if !strings.Contains(required, "old_string") || !strings.Contains(required, "new_string") {
		t.Fatalf("expected old_string and new_string required, got: %v", edits.Items.Required)
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
