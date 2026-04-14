package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-01: Line numbers in output.
func TestRead_LineNumbers(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	file := filepath.Join(dir, "lines.txt")
	_ = os.WriteFile(file, []byte("aaa\nbbb\nccc\n"), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Check line numbers with │ separator.
	if !strings.Contains(result, "1 │ aaa") {
		t.Fatalf("expected line 1 with │ separator, got: %s", result)
	}
	if !strings.Contains(result, "2 │ bbb") {
		t.Fatalf("expected line 2 with │ separator, got: %s", result)
	}
	if !strings.Contains(result, "3 │ ccc") {
		t.Fatalf("expected line 3 with │ separator, got: %s", result)
	}
}

// R-02: Offset/limit works.
func TestRead_OffsetLimit(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	file := filepath.Join(dir, "offset.txt")
	var content strings.Builder
	for i := 1; i <= 100; i++ {
		content.WriteString("line" + strings.Repeat("x", i) + "\n")
	}
	_ = os.WriteFile(file, []byte(content.String()), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","offset":10,"limit":5}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Line 11 (0-indexed offset=10 means starting at the 11th line).
	if !strings.Contains(result, "11 │") {
		t.Fatalf("expected line 11, got: %s", result)
	}
	// Should show 5 lines: 11-15.
	if !strings.Contains(result, "15 │") {
		t.Fatalf("expected line 15, got: %s", result)
	}
	// Should not contain line 16.
	if strings.Contains(result, "16 │") {
		t.Fatalf("should not contain line 16, got: %s", result)
	}
	// Should have truncation message.
	if !strings.Contains(result, "more lines") {
		t.Fatalf("expected truncation message, got: %s", result)
	}
}

// R-03: Default limit (2000 lines).
func TestRead_DefaultLimit(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	file := filepath.Join(dir, "big.txt")
	var content strings.Builder
	for i := 0; i < 3000; i++ {
		content.WriteString("line\n")
	}
	_ = os.WriteFile(file, []byte(content.String()), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have a truncation message since 3000 > 2000.
	if !strings.Contains(result, "more lines") {
		t.Fatalf("expected truncation message for default limit, got tail: %s", result[max(0, len(result)-200):])
	}
	// Should contain line 2000 but not line 2001.
	if !strings.Contains(result, "2000 │") {
		t.Fatalf("expected line 2000 in output")
	}
	if strings.Contains(result, "2001 │") {
		t.Fatalf("should not contain line 2001 with default limit")
	}
}

// R-05: Binary extension detected.
func TestRead_BinaryExtension(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	file := filepath.Join(dir, "image.png")
	_ = os.WriteFile(file, []byte("fake png"), 0644)

	_, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`"}`)
	if err == nil {
		t.Fatal("expected error for binary extension")
	}
	if !strings.Contains(err.Error(), "binary file") {
		t.Fatalf("expected binary file error, got: %s", err.Error())
	}
}

// R-07: Directory returns listing.
func TestRead_DirectoryListing(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	// Create a file inside the dir so listing shows something.
	_ = os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+dir+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "directory") {
		t.Fatalf("expected directory listing, got: %s", result)
	}
	if !strings.Contains(result, "hello.txt") {
		t.Fatalf("expected hello.txt in listing, got: %s", result)
	}
}
