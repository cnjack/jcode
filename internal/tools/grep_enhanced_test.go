package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// helper: creates a grepTool with a local env for testing.
func newTestGrepTool(t *testing.T) *grepTool {
	t.Helper()
	env := NewEnv(t.TempDir(), "linux")
	return &grepTool{env: env}
}

// G-01: basic search returns matches in rg args
func TestG01_BasicSearch(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "TODO", Path: "/src"}
	args := g.buildRgArgs(input)

	if !containsArg(args, "TODO") {
		t.Fatal("expected pattern in args")
	}
	if !containsArg(args, "/src") {
		t.Fatal("expected path in args")
	}
	if !containsArg(args, "--no-heading") {
		t.Fatal("expected --no-heading")
	}
	if !containsArg(args, "--line-number") {
		t.Fatal("expected --line-number")
	}
	if !containsArg(args, "--hidden") {
		t.Fatal("expected --hidden for hidden file support")
	}
}

// G-02: files_with_matches is the default mode
func TestG02_FilesWithMatchesModeDefault(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "files_with_matches"}
	args := g.buildRgArgs(input)

	if !containsArg(args, "--files-with-matches") {
		t.Fatal("expected --files-with-matches in default mode")
	}
	if containsArg(args, "--count") {
		t.Fatal("default mode should not have --count")
	}
}

// G-03: content mode must not use rg's per-file --max-count — it silently
// drops matches beyond the cap within a single file, making them unreachable
// via offset. Limiting is done globally in Go instead.
func TestG03_ContentMode(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "content"}
	args := g.buildRgArgs(input)

	if containsArg(args, "--files-with-matches") {
		t.Fatal("content mode should not have --files-with-matches")
	}
	if containsArgPrefix(args, "--max-count") {
		t.Fatalf("content mode must not use per-file --max-count, got: %v", args)
	}
}

// G-04: count mode
func TestG04_CountMode(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "count"}
	args := g.buildRgArgs(input)

	if !containsArg(args, "--count") {
		t.Fatal("expected --count in args")
	}
}

// G-05: before_context adds --before-context flag
func TestG05_BeforeContext(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "content", BeforeContext: 3}
	args := g.buildRgArgs(input)

	if !containsArg(args, "--before-context=3") {
		t.Fatalf("expected --before-context=3, got: %v", args)
	}
}

// G-06: after_context adds --after-context flag
func TestG06_AfterContext(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "content", AfterContext: 5}
	args := g.buildRgArgs(input)

	if !containsArg(args, "--after-context=5") {
		t.Fatalf("expected --after-context=5, got: %v", args)
	}
}

// G-07: context adds --context flag and overrides before/after
func TestG07_Context(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "content", Context: 2, BeforeContext: 10, AfterContext: 10}
	args := g.buildRgArgs(input)

	if !containsArg(args, "--context=2") {
		t.Fatalf("expected --context=2, got: %v", args)
	}
	if containsArgPrefix(args, "--before-context") {
		t.Fatal("context should override before_context")
	}
	if containsArgPrefix(args, "--after-context") {
		t.Fatal("context should override after_context")
	}
}

// G-08: offset pagination in content mode
func TestG08_OffsetPagination(t *testing.T) {
	g := newTestGrepTool(t)

	// Build sample content lines
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, "file.go:"+string(rune('0'+i))+": match")
	}

	result := g.formatContent(lines, "", 4, 3, false)
	// Should contain line index 3
	if !strings.Contains(result, "file.go:3") {
		t.Fatalf("expected line 3 in output, got: %s", result)
	}
	// Should NOT contain line index 0
	if strings.Contains(result, "file.go:0") {
		t.Fatalf("offset should have skipped line 0, got: %s", result)
	}
	// Should contain pagination hint
	if !strings.Contains(result, "offset") {
		t.Fatalf("expected offset info in result, got: %s", result)
	}
}

// G-09: multiline adds --multiline and --multiline-dotall flags
func TestG09_Multiline(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo\\nbar", Path: "/src", Multiline: true}
	args := g.buildRgArgs(input)

	if !containsArg(args, "--multiline") {
		t.Fatalf("expected --multiline in args, got: %v", args)
	}
	if !containsArg(args, "--multiline-dotall") {
		t.Fatalf("expected --multiline-dotall in args, got: %v", args)
	}
}

// G-10: file_type adds --type flag
func TestG10_FileType(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", FileType: "go"}
	args := g.buildRgArgs(input)

	found := false
	for i, a := range args {
		if a == "--type" && i+1 < len(args) && args[i+1] == "go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --type go in args, got: %v", args)
	}
}

// G-11: default max is 250
func TestG11_DefaultMax(t *testing.T) {
	if grepDefaultMax != 250 {
		t.Fatalf("expected default max 250, got %d", grepDefaultMax)
	}
	if grepAbsoluteMax != 1000 {
		t.Fatalf("expected absolute max 1000, got %d", grepAbsoluteMax)
	}
}

// G-12: line length limit (--max-columns=500 in rg args)
func TestG12_LineLengthLimit(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src"}
	args := g.buildRgArgs(input)

	if !containsArg(args, "--max-columns=500") {
		t.Fatalf("expected --max-columns=500, got: %v", args)
	}
	if !containsArg(args, "--max-columns-preview") {
		t.Fatalf("expected --max-columns-preview, got: %v", args)
	}
}

// G-13: VCS exclusions (all 10 dirs excluded)
func TestG13_VCSExclusions(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src"}

	rgArgs := g.buildRgArgs(input)
	expectedDirs := []string{".git", "node_modules", "vendor", "__pycache__", ".venv", ".svn", ".hg", ".bzr", ".jj", ".sl"}
	for _, dir := range expectedDirs {
		exclusion := "!" + dir
		found := false
		for _, a := range rgArgs {
			if a == exclusion {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("rg args missing exclusion for %s: %v", dir, rgArgs)
		}
	}
}

// G-14: dash-prefixed patterns use -e flag
func TestG14_DashPattern(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "-verbose", Path: "/src"}
	args := g.buildRgArgs(input)

	found := false
	for i, a := range args {
		if a == "-e" && i+1 < len(args) && args[i+1] == "-verbose" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected -e flag for dash-prefixed pattern, got: %v", args)
	}
}

// G-15: basic input fields still work
func TestG15_LegacyCompat(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{
		Pattern:         "hello",
		Path:            "/home",
		Include:         "*.go",
		CaseInsensitive: true,
		MaxResults:      100,
	}

	args := g.buildRgArgs(input)
	if !containsArg(args, "--ignore-case") {
		t.Fatal("expected --ignore-case for basic compat")
	}
	if !containsArg(args, "*.go") {
		t.Fatal("expected include glob for basic compat")
	}
	if !containsArg(args, "hello") {
		t.Fatal("expected pattern for basic compat")
	}
	if containsArg(args, "--multiline") {
		t.Fatal("basic input should not trigger --multiline")
	}
}

// G-16: relative path conversion
func TestG16_RelativePath(t *testing.T) {
	rel := toRelativePath("/home/user/project/src/main.go", "/home/user/project")
	if rel != "src/main.go" {
		t.Fatalf("expected src/main.go, got %s", rel)
	}

	// Path outside pwd should stay absolute
	rel = toRelativePath("/other/path/file.go", "/home/user/project")
	if !strings.Contains(rel, "other") {
		t.Fatalf("expected path to stay relative, got %s", rel)
	}
}

// G-17: relativizeLine converts grep output lines
func TestG17_RelativizeLine(t *testing.T) {
	line := relativizeLine("/home/user/project/src/main.go:42:func main() {", "/home/user/project")
	if !strings.HasPrefix(line, "src/main.go:42:") {
		t.Fatalf("expected relative path in line, got: %s", line)
	}
}

// G-18: context lines are only added in content mode
func TestG18_ContextOnlyInContentMode(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "files_with_matches", Context: 3}
	args := g.buildRgArgs(input)

	if containsArgPrefix(args, "--context") {
		t.Fatal("context flags should not appear in files_with_matches mode")
	}
}

// G-19: capped content output uses an honest footer instead of claiming an
// exact total that was never fully counted.
func TestG19_ContentCappedFooter(t *testing.T) {
	g := newTestGrepTool(t)

	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, fmt.Sprintf("file.go:%d: match", i+1))
	}

	// capped: rg was stopped early — total is unknown
	result := g.formatContent(lines, "", 4, 0, true)
	if !strings.Contains(result, "more results available") {
		t.Fatalf("expected 'more results available' in capped footer, got: %s", result)
	}
	if !strings.Contains(result, "offset=4") {
		t.Fatalf("expected next-page hint offset=4 in capped footer, got: %s", result)
	}
	if strings.Contains(result, "total") {
		t.Fatalf("capped footer must not claim an exact total, got: %s", result)
	}

	// uncapped: totals are exact and may be reported
	result = g.formatContent(lines, "", 4, 0, false)
	if !strings.Contains(result, "5 total") {
		t.Fatalf("expected exact '5 total' in uncapped truncated footer, got: %s", result)
	}

	// uncapped, not truncated: plain match count
	result = g.formatContent(lines[:3], "", 4, 0, false)
	if !strings.Contains(result, "3 matches found") {
		t.Fatalf("expected '(3 matches found)', got: %s", result)
	}
}

// G-20: e2e — a single file with more matches than max_results is paginated
// globally. The old per-file --max-count made matches beyond the cap
// permanently unreachable and reported a bogus total.
func TestG20_LocalEarlyStopSingleFile(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}

	g := newTestGrepTool(t)
	dir := t.TempDir()
	var content strings.Builder
	for i := 1; i <= 300; i++ {
		fmt.Fprintf(&content, "match %03d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	// First page: exactly 10 results, honest capped footer
	args := fmt.Sprintf(`{"pattern":"match","path":%q,"output_mode":"content","max_results":10}`, dir)
	result, err := g.InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	resultLines := 0
	for _, line := range strings.Split(result, "\n") {
		if strings.Contains(line, "match 0") {
			resultLines++
		}
	}
	if resultLines != 10 {
		t.Fatalf("expected exactly 10 result lines, got %d: %s", resultLines, result)
	}
	if !strings.Contains(result, "more results available") {
		t.Fatalf("expected capped footer, got: %s", result)
	}
	if !strings.Contains(result, "offset=10") {
		t.Fatalf("expected next-page hint offset=10, got: %s", result)
	}
	if strings.Contains(result, "11 total") {
		t.Fatalf("must not claim bogus '11 total' (old per-file cap artifact), got: %s", result)
	}

	// Deep page: offset=290 reaches matches 291-300 (unreachable under the old
	// per-file cap of max_results+offset+1 applied at page one) with exact total
	args = fmt.Sprintf(`{"pattern":"match","path":%q,"output_mode":"content","max_results":10,"offset":290}`, dir)
	result, err = g.InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "match 291") || !strings.Contains(result, "match 300") {
		t.Fatalf("expected matches 291-300 on deep page, got: %s", result)
	}
	if !strings.Contains(result, "300 total") {
		t.Fatalf("expected exact '300 total' when uncapped, got: %s", result)
	}
}

// G-21: e2e — when the scan completes without hitting the cap, every file's
// matches are present and the total is exact.
func TestG21_ExactTotalWhenUncapped(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}

	g := newTestGrepTool(t)
	dir := t.TempDir()
	var a strings.Builder
	for i := 1; i <= 300; i++ {
		fmt.Fprintf(&a, "needle a%03d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "aaa.txt"), []byte(a.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&b, "needle b%03d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "bbb.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	args := fmt.Sprintf(`{"pattern":"needle","path":%q,"output_mode":"content","max_results":500}`, dir)
	result, err := g.InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "needle b010") {
		t.Fatalf("expected second file's matches present when uncapped, got: %s", result)
	}
	if !strings.Contains(result, "310 matches found") {
		t.Fatalf("expected exact '(310 matches found)', got: %s", result)
	}
}

// G-22: remote content mode guards output volume with head instead of the
// per-file --max-count.
func TestG22_RemoteCmdHeadGuard(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "content", Offset: 20}
	cmd := g.buildRemoteCmd(input, 100)

	if !strings.Contains(cmd, "| head -n 121") {
		t.Fatalf("expected '| head -n 121' (offset+max+1) in remote cmd: %s", cmd)
	}
	if strings.Contains(cmd, "--max-count") {
		t.Fatalf("remote content cmd must not use per-file --max-count: %s", cmd)
	}
}

// G-23: unknown output_mode values are rejected instead of silently running
// in content mode.
func TestG23_InvalidOutputModeRejected(t *testing.T) {
	g := newTestGrepTool(t)
	dir := t.TempDir()

	for _, mode := range []string{"files", "matches"} {
		args := fmt.Sprintf(`{"pattern":"x","path":%q,"output_mode":%q}`, dir, mode)
		_, err := g.InvokableRun(context.Background(), args)
		if err == nil {
			t.Fatalf("expected error for output_mode %q", mode)
		}
		if !strings.Contains(err.Error(), "invalid output_mode") {
			t.Fatalf("expected 'invalid output_mode' in error, got: %v", err)
		}
		for _, valid := range []string{"files_with_matches", "content", "count"} {
			if !strings.Contains(err.Error(), valid) {
				t.Fatalf("expected error to list valid value %q, got: %v", valid, err)
			}
		}
	}
}

// G-24: all valid output modes (and the empty default) are accepted.
func TestG24_ValidOutputModesAccepted(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}

	g := newTestGrepTool(t)
	dir := t.TempDir() // empty dir: no matches, but no validation error either

	for _, mode := range []string{"files_with_matches", "content", "count", ""} {
		args := fmt.Sprintf(`{"pattern":"x","path":%q,"output_mode":%q}`, dir, mode)
		_, err := g.InvokableRun(context.Background(), args)
		if err != nil {
			t.Fatalf("output_mode %q should be accepted, got error: %v", mode, err)
		}
	}
}

// --- helpers ---

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func containsArgPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}
