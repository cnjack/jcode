package tools

import (
	"strings"
	"testing"
	"time"
)

// GL-01: basic pattern returns files (tests buildFindCmd)
func TestGL01_BasicPattern(t *testing.T) {
	cmd := buildFindCmd("*.go", "/src", 0, 100)

	if !strings.Contains(cmd, "/src") {
		t.Fatalf("expected search path in cmd: %s", cmd)
	}
	if !strings.Contains(cmd, "*.go") || !strings.Contains(cmd, "-name") {
		t.Fatalf("expected -name with pattern in cmd: %s", cmd)
	}
	if !strings.Contains(cmd, "-type f") {
		t.Fatalf("expected -type f in cmd: %s", cmd)
	}
	// Verify VCS dirs are excluded
	for _, dir := range globExclusions {
		if !strings.Contains(cmd, dir) {
			t.Errorf("expected exclusion for %s in cmd: %s", dir, cmd)
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

	// buildFindCmd with limit 100 should head -n 101 (limit+1 to detect truncation)
	cmd := buildFindCmd("*.txt", "/src", 0, 100)
	if !strings.Contains(cmd, "head -n 101") {
		t.Fatalf("expected head -n 101 for limit 100, got: %s", cmd)
	}
}

// GL-04: max_depth limits depth
func TestGL04_MaxDepth(t *testing.T) {
	cmd := buildFindCmd("*.go", "/src", 3, 100)
	if !strings.Contains(cmd, "-maxdepth 3") {
		t.Fatalf("expected -maxdepth 3 in cmd: %s", cmd)
	}

	// Without max_depth
	cmdNoDepth := buildFindCmd("*.go", "/src", 0, 100)
	if strings.Contains(cmdNoDepth, "-maxdepth") {
		t.Fatalf("no max_depth should not include -maxdepth: %s", cmdNoDepth)
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
