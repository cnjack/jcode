package tools

import (
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
	args := g.buildRgArgs(input, grepDefaultMax)

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
	args := g.buildRgArgs(input, grepDefaultMax)

	if !containsArg(args, "--files-with-matches") {
		t.Fatal("expected --files-with-matches in default mode")
	}
	if containsArg(args, "--count") {
		t.Fatal("default mode should not have --count")
	}
}

// G-03: content mode
func TestG03_ContentMode(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "content"}
	args := g.buildRgArgs(input, grepDefaultMax)

	if containsArg(args, "--files-with-matches") {
		t.Fatal("content mode should not have --files-with-matches")
	}
	if !containsArgPrefix(args, "--max-count") {
		t.Fatal("expected --max-count in content mode")
	}
}

// G-04: count mode
func TestG04_CountMode(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "count"}
	args := g.buildRgArgs(input, grepDefaultMax)

	if !containsArg(args, "--count") {
		t.Fatal("expected --count in args")
	}
}

// G-05: before_context adds --before-context flag
func TestG05_BeforeContext(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "content", BeforeContext: 3}
	args := g.buildRgArgs(input, grepDefaultMax)

	if !containsArg(args, "--before-context=3") {
		t.Fatalf("expected --before-context=3, got: %v", args)
	}
}

// G-06: after_context adds --after-context flag
func TestG06_AfterContext(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "content", AfterContext: 5}
	args := g.buildRgArgs(input, grepDefaultMax)

	if !containsArg(args, "--after-context=5") {
		t.Fatalf("expected --after-context=5, got: %v", args)
	}
}

// G-07: context adds --context flag and overrides before/after
func TestG07_Context(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "content", Context: 2, BeforeContext: 10, AfterContext: 10}
	args := g.buildRgArgs(input, grepDefaultMax)

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

	result := g.formatContent(lines, "", 4, 3)
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
	args := g.buildRgArgs(input, grepDefaultMax)

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
	args := g.buildRgArgs(input, grepDefaultMax)

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
	args := g.buildRgArgs(input, grepDefaultMax)

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

	rgArgs := g.buildRgArgs(input, grepDefaultMax)
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
	args := g.buildRgArgs(input, grepDefaultMax)

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

	args := g.buildRgArgs(input, 100)
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
	args := g.buildRgArgs(input, grepDefaultMax)

	if containsArgPrefix(args, "--context") {
		t.Fatal("context flags should not appear in files_with_matches mode")
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
