package tools

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
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

// R-08: Total output budget truncation (#5). Also covers #15-read: this test
// calls the tool directly with no reduction middleware attached (newTestEnv
// only builds an Env), proving the read tool self-limits its output even when
// middleware is absent (subagents) or fails open.
func TestRead_TotalBudgetTruncation(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	file := filepath.Join(dir, "huge.txt")
	var content strings.Builder
	line := strings.Repeat("a", 1500)
	for i := 0; i < 2000; i++ {
		content.WriteString(line)
		content.WriteString("\n")
	}
	_ = os.WriteFile(file, []byte(content.String()), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) >= maxReadResultBytes+4096 {
		t.Fatalf("result exceeds output budget: %d bytes", len(result))
	}
	if !strings.Contains(result, "output truncated") {
		t.Fatalf("expected budget truncation message, got tail: %s", result[max(0, len(result)-200):])
	}
	if !strings.Contains(result, "offset=") {
		t.Fatalf("expected continuation offset hint, got tail: %s", result[max(0, len(result)-200):])
	}
	if strings.Contains(result, "2000 │") {
		t.Fatalf("last line should not be emitted when budget is hit")
	}
}

// R-08b: The offset in the budget-truncation message continues exactly at the
// next unread line.
func TestRead_TotalBudgetContinuation(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	file := filepath.Join(dir, "huge2.txt")
	var content strings.Builder
	line := strings.Repeat("b", 1500)
	for i := 0; i < 2000; i++ {
		content.WriteString(line)
		content.WriteString("\n")
	}
	_ = os.WriteFile(file, []byte(content.String()), 0644)

	first, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	idx := strings.LastIndex(first, "offset=")
	if idx < 0 {
		t.Fatalf("expected offset= in truncation message, got tail: %s", first[max(0, len(first)-200):])
	}
	rest := first[idx+len("offset="):]
	numEnd := 0
	for numEnd < len(rest) && rest[numEnd] >= '0' && rest[numEnd] <= '9' {
		numEnd++
	}
	offset, err := strconv.Atoi(rest[:numEnd])
	if err != nil {
		t.Fatalf("failed to parse offset from truncation message: %v", err)
	}

	second, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","offset":`+strconv.Itoa(offset)+`}`)
	if err != nil {
		t.Fatalf("unexpected error on continuation read: %v", err)
	}
	firstLine := strings.SplitN(second, "\n", 2)[0]
	want := strconv.Itoa(offset+1) + " │"
	if !strings.Contains(firstLine, want) {
		t.Fatalf("expected continuation to start at line %d, got first line: %s", offset+1, firstLine)
	}
}

// R-08c: Normal-size files are unaffected by the output budget.
func TestRead_NormalFileNoBudgetHit(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	file := filepath.Join(dir, "normal.txt")
	var content strings.Builder
	for i := 0; i < 100; i++ {
		content.WriteString("hello\n")
	}
	_ = os.WriteFile(file, []byte(content.String()), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "output truncated") {
		t.Fatalf("normal file should not hit budget, got: %s", result)
	}
	if !strings.Contains(result, "1 │") {
		t.Fatalf("expected line 1 in output, got: %s", result)
	}
	if !strings.Contains(result, "100 │") {
		t.Fatalf("expected line 100 in output, got: %s", result)
	}
}

// R-09: A single overlong line is truncated with an inline marker (#13).
func TestRead_LongLineTruncated(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	file := filepath.Join(dir, "longline.txt")
	content := strings.Repeat("x", 3000) + "ZZZ_END\nshort\n"
	_ = os.WriteFile(file, []byte(content), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "1 │") {
		t.Fatalf("expected line 1 in output, got: %s", result[:min(200, len(result))])
	}
	if !strings.Contains(result, "line truncated") {
		t.Fatalf("expected line truncation marker, got: %s", result[:min(200, len(result))])
	}
	if strings.Contains(result, "ZZZ_END") {
		t.Fatalf("tail of overlong line should be cut, got: %s", result[max(0, len(result)-300):])
	}
	if !strings.Contains(result, "2 │ short") {
		t.Fatalf("short line after long line should be intact, got: %s", result[max(0, len(result)-300):])
	}
}

// R-09b: Truncation of a long multi-byte line lands on a UTF-8 rune boundary.
func TestRead_LongLineUTF8Boundary(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	file := filepath.Join(dir, "utf8line.txt")
	// 1000 x 3-byte runes = 3000 bytes; byte 2000 is not a rune boundary.
	content := strings.Repeat("界", 1000) + "\n"
	_ = os.WriteFile(file, []byte(content), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !utf8.ValidString(result) {
		t.Fatalf("result contains invalid UTF-8 after truncation")
	}
	if !strings.Contains(result, "line truncated") {
		t.Fatalf("expected line truncation marker, got: %s", result[:min(200, len(result))])
	}
}

// R-09c: A line exactly at the per-line cap is not truncated.
func TestRead_ShortLineNotTruncated(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	file := filepath.Join(dir, "exactline.txt")
	line := strings.Repeat("y", maxReadLineBytes)
	_ = os.WriteFile(file, []byte(line+"\n"), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "line truncated") {
		t.Fatalf("line exactly at the cap should not be truncated, got: %s", result[max(0, len(result)-300):])
	}
	if !strings.Contains(result, line) {
		t.Fatalf("expected the full line in output")
	}
}

// R-09d: Per-line truncation keeps line numbers aligned with the original
// file when offset/limit are used.
func TestRead_LineTruncationWithOffsetLimit(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	file := filepath.Join(dir, "mixed.txt")
	var content strings.Builder
	for i := 1; i <= 10; i++ {
		if i >= 3 && i <= 5 {
			content.WriteString("LINE" + strconv.Itoa(i) + "_" + strings.Repeat("z", 3000))
		} else {
			content.WriteString("plain" + strconv.Itoa(i))
		}
		content.WriteString("\n")
	}
	_ = os.WriteFile(file, []byte(content.String()), 0644)

	result, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+file+`","offset":2,"limit":3}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Offset 2 (0-indexed) means original lines 3-5.
	if !strings.Contains(result, "3 │ LINE3_") {
		t.Fatalf("expected line number 3 with LINE3 content, got: %s", result)
	}
	if !strings.Contains(result, "5 │ LINE5_") {
		t.Fatalf("expected line number 5 with LINE5 content, got: %s", result)
	}
	if strings.Contains(result, "6 │") {
		t.Fatalf("should not contain line 6, got: %s", result)
	}
	if !strings.Contains(result, "line truncated") {
		t.Fatalf("expected line truncation marker, got: %s", result)
	}
}

// R-10 (#28): Not-found errors carry a curated locate hint.
func TestRead_NotFound_Hint(t *testing.T) {
	env, dir := newTestEnv(t)
	tool := env.NewReadTool()

	_, err := tool.InvokableRun(context.Background(),
		`{"file_path":"`+filepath.Join(dir, "nope.txt")+`"}`)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("original error text must be preserved, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "grep") {
		t.Fatalf("expected locate hint mentioning grep, got: %s", err.Error())
	}
}

// R-11 (#28): Malformed JSON arguments carry a re-emit hint.
func TestRead_InvalidArgs_Hint(t *testing.T) {
	env, _ := newTestEnv(t)
	tool := env.NewReadTool()

	_, err := tool.InvokableRun(context.Background(), `{"file_path":`)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Fatalf("expected JSON hint, got: %s", err.Error())
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
