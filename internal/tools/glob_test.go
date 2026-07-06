package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// GL-01: basic pattern builds an rg --files command (tests buildRgGlobCmd)
func TestGL01_BasicPattern(t *testing.T) {
	cmd := buildRgGlobCmd("*.go", "/src", 0, 100)

	if !strings.Contains(cmd, "'/src'") {
		t.Fatalf("expected quoted search path in cmd: %s", cmd)
	}
	if !strings.Contains(cmd, "rg --files") {
		t.Fatalf("expected rg --files in cmd: %s", cmd)
	}
	if !strings.Contains(cmd, "--hidden") {
		t.Fatalf("expected --hidden in cmd: %s", cmd)
	}
	if !strings.Contains(cmd, "--glob '*.go'") {
		t.Fatalf("expected quoted --glob pattern in cmd: %s", cmd)
	}
	if strings.Contains(cmd, "-name") || strings.Contains(cmd, "-type f") {
		t.Fatalf("cmd should no longer use find primitives: %s", cmd)
	}
	// Verify VCS dirs are excluded via negated globs
	for _, dir := range globExclusions {
		if !strings.Contains(cmd, "--glob '!"+dir+"'") {
			t.Errorf("expected exclusion --glob '!%s' in cmd: %s", dir, cmd)
		}
	}
}

// GL-02: default limit is 100
func TestGL02_DefaultLimit(t *testing.T) {
	if globDefaultLimit != 100 {
		t.Fatalf("expected default limit 100, got %d", globDefaultLimit)
	}
	if globMaxLimit != 500 {
		t.Fatalf("expected max limit 500, got %d", globMaxLimit)
	}

	// buildRgGlobCmd with limit 100 should head -n 101 (limit+1 to detect truncation)
	cmd := buildRgGlobCmd("*.txt", "/src", 0, 100)
	if !strings.Contains(cmd, "head -n 101") {
		t.Fatalf("expected head -n 101 for limit 100, got: %s", cmd)
	}
}

// GL-04: max_depth limits depth
func TestGL04_MaxDepth(t *testing.T) {
	cmd := buildRgGlobCmd("*.go", "/src", 3, 100)
	if !strings.Contains(cmd, "--max-depth 3") {
		t.Fatalf("expected --max-depth 3 in cmd: %s", cmd)
	}

	// Without max_depth
	cmdNoDepth := buildRgGlobCmd("*.go", "/src", 0, 100)
	if strings.Contains(cmdNoDepth, "--max-depth") {
		t.Fatalf("no max_depth should not include --max-depth: %s", cmdNoDepth)
	}
}

// GL-05: '**' recursion and slash-anchored globs work end-to-end on a real tree.
// The old find -name implementation treated '/' and '**' literally and returned
// nothing for these patterns.
func TestGL05_RecursiveGlobE2E(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "top.go"), "package top\n")
	mustWriteFile(t, filepath.Join(dir, "src", "b.test.ts"), "b\n")
	mustWriteFile(t, filepath.Join(dir, "src", "deep", "c.test.ts"), "c\n")
	mustWriteFile(t, filepath.Join(dir, "src", "x.go"), "package x\n")
	mustWriteFile(t, filepath.Join(dir, "src", "deep", "y.go"), "package y\n")

	// '**/*.test.ts' must match at every depth
	stdout, stderr, err := execLocal(context.Background(), buildRgGlobCmd("**/*.test.ts", dir, 0, 100))
	if err != nil {
		t.Fatalf("execLocal failed: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "src/b.test.ts") {
		t.Errorf("expected src/b.test.ts in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "src/deep/c.test.ts") {
		t.Errorf("expected src/deep/c.test.ts in output, got: %q", stdout)
	}

	// 'src/**/*.go' is anchored to the search dir: matches src/ subtree only
	stdout, stderr, err = execLocal(context.Background(), buildRgGlobCmd("src/**/*.go", dir, 0, 100))
	if err != nil {
		t.Fatalf("execLocal failed: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "src/x.go") {
		t.Errorf("expected src/x.go in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "src/deep/y.go") {
		t.Errorf("expected src/deep/y.go in output, got: %q", stdout)
	}
	if strings.Contains(stdout, "top.go") {
		t.Errorf("anchored pattern must not match top.go outside src/, got: %q", stdout)
	}
}

// GL-06: patterns starting with '!' are rejected (rg would invert them into
// an exclusion glob, silently reversing the match semantics).
func TestGL06_BangPatternRejected(t *testing.T) {
	env := NewEnv(t.TempDir(), "linux")
	gt := env.NewGlobTool()

	_, err := gt.InvokableRun(context.Background(), `{"pattern":"!foo"}`)
	if err == nil {
		t.Fatal("expected error for pattern starting with '!'")
	}
	if !strings.Contains(err.Error(), "!") {
		t.Fatalf("expected error to mention the '!' prefix, got: %v", err)
	}
}

// GL-07: results are sorted newest-first before truncation
func TestGL07_SortFlag(t *testing.T) {
	cmd := buildRgGlobCmd("*.go", "/src", 0, 100)

	sortIdx := strings.Index(cmd, "--sortr=modified")
	if sortIdx < 0 {
		t.Fatalf("expected --sortr=modified in cmd: %s", cmd)
	}
	headIdx := strings.Index(cmd, "| head -n")
	if headIdx < 0 {
		t.Fatalf("expected | head -n in cmd: %s", cmd)
	}
	if sortIdx > headIdx {
		t.Fatalf("sorting must happen before head truncation: %s", cmd)
	}
}

// GL-08: mtime sorting is observable end-to-end — newest file first, and
// truncation keeps the newest files.
func TestGL08_SortedByMtimeE2E(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package a\n")
	mustWriteFile(t, filepath.Join(dir, "b.go"), "package b\n")
	// Explicit mtimes: on filesystems with coarse timestamp granularity the
	// two writes could land in the same tick and the sort would fall back to
	// path order, flipping the newest-first assertion.
	now := time.Now()
	if err := os.Chtimes(filepath.Join(dir, "a.go"), now.Add(-2*time.Second), now.Add(-2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "b.go"), now, now); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := execLocal(context.Background(), buildRgGlobCmd("*.go", dir, 0, 100))
	if err != nil {
		t.Fatalf("execLocal failed: %v (stderr: %s)", err, stderr)
	}
	aIdx := strings.Index(stdout, "a.go")
	bIdx := strings.Index(stdout, "b.go")
	if aIdx < 0 || bIdx < 0 {
		t.Fatalf("expected both a.go and b.go in output, got: %q", stdout)
	}
	if bIdx > aIdx {
		t.Fatalf("expected newest file b.go before a.go, got: %q", stdout)
	}

	// limit=1: truncation keeps the newest file
	stdout, stderr, err = execLocal(context.Background(), buildRgGlobCmd("*.go", dir, 0, 1))
	if err != nil {
		t.Fatalf("execLocal failed: %v (stderr: %s)", err, stderr)
	}
	result, err := formatGlobOutput(stdout, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "b.go") {
		t.Fatalf("truncated result should keep newest file b.go, got: %s", result)
	}
	if strings.Contains(result, "a.go") {
		t.Fatalf("truncated result should not contain older file a.go, got: %s", result)
	}
}

// mustWriteFile creates a file (and parent dirs) with the given content.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Test formatGlobOutput
func TestGlob_FormatOutput(t *testing.T) {
	t.Run("empty output", func(t *testing.T) {
		result, err := formatGlobOutput("", 100, 10*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		if result != "No files found." {
			t.Fatalf("expected no files message, got: %s", result)
		}
	})

	t.Run("normal output", func(t *testing.T) {
		output := "a.go\nb.go\nc.go"
		result, err := formatGlobOutput(output, 100, 5*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result, "3 files found") {
			t.Fatalf("expected 3 files found, got: %s", result)
		}
	})

	t.Run("truncated output", func(t *testing.T) {
		// 4 lines but limit=3 → truncated
		output := "a.go\nb.go\nc.go\nd.go"
		result, err := formatGlobOutput(output, 3, 5*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result, "3 files shown") {
			t.Fatalf("expected truncation message, got: %s", result)
		}
		if strings.Contains(result, "d.go") {
			t.Fatalf("truncated result should not contain d.go: %s", result)
		}
	})
}
