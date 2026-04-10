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
}

// G-02: content mode is default
func TestG02_ContentModeDefault(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src"}
	args := g.buildRgArgs(input, grepDefaultMax)

	// content mode uses --max-count, not --files-with-matches or --count
	if containsArg(args, "--files-with-matches") {
		t.Fatal("content mode should not have --files-with-matches")
	}
	if containsArg(args, "--count") {
		t.Fatal("content mode should not have --count")
	}
	if !containsArgPrefix(args, "--max-count") {
		t.Fatal("expected --max-count in content mode")
	}
}

// G-03: files_with_matches mode
func TestG03_FilesWithMatchesMode(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", OutputMode: "files_with_matches"}
	args := g.buildRgArgs(input, grepDefaultMax)

	if !containsArg(args, "--files-with-matches") {
		t.Fatal("expected --files-with-matches in args")
	}

	// grep fallback should use -l
	grepArgs := g.buildGrepArgs(input)
	if !containsArg(grepArgs, "-l") {
		t.Fatal("expected -l in grep fallback args")
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

	grepArgs := g.buildGrepArgs(input)
	if !containsArg(grepArgs, "-c") {
		t.Fatal("expected -c in grep fallback args")
	}
}

// G-05: before_context adds -B flag
func TestG05_BeforeContext(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", BeforeContext: 3}
	args := g.buildRgArgs(input, grepDefaultMax)

	if !containsArg(args, "--before-context=3") {
		t.Fatalf("expected --before-context=3, got: %v", args)
	}

	grepArgs := g.buildGrepArgs(input)
	if !containsArg(grepArgs, "-B3") {
		t.Fatalf("expected -B3 in grep args, got: %v", grepArgs)
	}
}

// G-06: after_context adds -A flag
func TestG06_AfterContext(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", AfterContext: 5}
	args := g.buildRgArgs(input, grepDefaultMax)

	if !containsArg(args, "--after-context=5") {
		t.Fatalf("expected --after-context=5, got: %v", args)
	}

	grepArgs := g.buildGrepArgs(input)
	if !containsArg(grepArgs, "-A5") {
		t.Fatalf("expected -A5 in grep args, got: %v", grepArgs)
	}
}

// G-07: context adds -C flag and overrides before/after
func TestG07_Context(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", Context: 2, BeforeContext: 10, AfterContext: 10}
	args := g.buildRgArgs(input, grepDefaultMax)

	if !containsArg(args, "--context=2") {
		t.Fatalf("expected --context=2, got: %v", args)
	}
	// When context is set, before/after should NOT appear
	if containsArgPrefix(args, "--before-context") {
		t.Fatal("context should override before_context")
	}
	if containsArgPrefix(args, "--after-context") {
		t.Fatal("context should override after_context")
	}

	grepArgs := g.buildGrepArgs(input)
	if !containsArg(grepArgs, "-C2") {
		t.Fatalf("expected -C2 in grep args, got: %v", grepArgs)
	}
}

// G-08: offset pagination
func TestG08_OffsetPagination(t *testing.T) {
	// Build sample output: 10 lines
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, "file.go:"+string(rune('0'+i))+": match")
	}
	output := strings.Join(lines, "\n")

	// offset=3, maxResults=4 → show lines[3..6]
	result, err := formatGrepOutput(output, 4, 3)
	if err != nil {
		t.Fatal(err)
	}
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

// G-09: multiline adds --multiline flag
func TestG09_Multiline(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo\\nbar", Path: "/src", Multiline: true}
	args := g.buildRgArgs(input, grepDefaultMax)

	if !containsArg(args, "--multiline") {
		t.Fatalf("expected --multiline in args, got: %v", args)
	}
}

// G-10: file_type adds --type flag
func TestG10_FileType(t *testing.T) {
	g := newTestGrepTool(t)
	input := GrepInput{Pattern: "foo", Path: "/src", FileType: "go"}
	args := g.buildRgArgs(input, grepDefaultMax)

	// Should have --type followed by go
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

	// grep fallback uses --include=*.go
	grepArgs := g.buildGrepArgs(input)
	if !containsArg(grepArgs, "--include=*.go") {
		t.Fatalf("expected --include=*.go in grep args, got: %v", grepArgs)
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

	// Check rg args have glob exclusions
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

	// Check grep args have --exclude-dir
	grepArgs := g.buildGrepArgs(input)
	for _, dir := range expectedDirs {
		exclusion := "--exclude-dir=" + dir
		if !containsArg(grepArgs, exclusion) {
			t.Errorf("grep args missing %s: %v", exclusion, grepArgs)
		}
	}
}

// G-15: basic input fields still work
func TestG15_LegacyCompat(t *testing.T) {
	g := newTestGrepTool(t)
	// Simulate basic input: only pattern, path, include, case_insensitive, max_results
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
	// Extended flags should not appear for basic input
	if containsArg(args, "--multiline") {
		t.Fatal("basic input should not trigger --multiline")
	}
	if containsArg(args, "--files-with-matches") {
		t.Fatal("basic input should not trigger --files-with-matches")
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
