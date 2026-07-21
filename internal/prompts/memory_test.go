package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryLoader_SingleFile(t *testing.T) {
	dir := t.TempDir()

	// Create a project-level AGENTS.md.
	agentsContent := "# Project Instructions\nDo X and Y."
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsContent), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewMemoryLoader(MemoryConfig{MaxTotalChars: 40000, MaxIncDepth: 5})
	result, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !strings.Contains(result, "Do X and Y") {
		t.Errorf("expected project content in result, got: %s", result)
	}
}

func TestMemoryLoader_MultiLevel(t *testing.T) {
	dir := t.TempDir()

	// Simulate global config dir.
	globalDir := t.TempDir()
	origConfigDir := os.Getenv("HOME")
	// We can't easily override ConfigDir(), so we test project + local only.

	_ = globalDir
	_ = origConfigDir

	// Project AGENTS.md
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("project rules"), 0644); err != nil {
		t.Fatal(err)
	}
	// Local AGENTS.local.md
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.local.md"), []byte("local overrides"), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewMemoryLoader(MemoryConfig{MaxTotalChars: 40000, MaxIncDepth: 5})
	result, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !strings.Contains(result, "project rules") {
		t.Errorf("expected project rules in result, got: %s", result)
	}
	if !strings.Contains(result, "local overrides") {
		t.Errorf("expected local overrides in result, got: %s", result)
	}
}

func TestMemoryLoader_IncludeResolution(t *testing.T) {
	dir := t.TempDir()

	// Create included file.
	includedContent := "## Included Section\nThis was included."
	if err := os.WriteFile(filepath.Join(dir, "extra.md"), []byte(includedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create AGENTS.md with @include directive.
	agentsContent := "# Main\n@include extra.md\nDone."
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsContent), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewMemoryLoader(MemoryConfig{MaxTotalChars: 40000, MaxIncDepth: 5})
	result, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !strings.Contains(result, "This was included") {
		t.Errorf("expected included content, got: %s", result)
	}
	if !strings.Contains(result, "Done.") {
		t.Errorf("expected content after include, got: %s", result)
	}
}

func TestMemoryLoader_CircularReference(t *testing.T) {
	dir := t.TempDir()

	// A includes B, B includes A.
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("@include b.md"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("@include a.md"), 0644); err != nil {
		t.Fatal(err)
	}
	agentsContent := "@include a.md"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsContent), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewMemoryLoader(MemoryConfig{MaxTotalChars: 40000, MaxIncDepth: 5})
	result, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	// Should not hang; circular refs replaced with comment.
	if !strings.Contains(result, "circular include") {
		t.Errorf("expected circular include comment, got: %s", result)
	}
}

func TestMemoryLoader_MaxDepth(t *testing.T) {
	dir := t.TempDir()

	// Create chain: d0.md -> d1.md -> d2.md -> d3.md
	for i := 0; i < 4; i++ {
		var content string
		name := "d" + string(rune('0'+i)) + ".md"
		if i < 3 {
			next := "d" + string(rune('0'+i+1)) + ".md"
			content = "level" + string(rune('0'+i)) + "\n@include " + next
		} else {
			content = "deepest-level"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("@include d0.md"), 0644); err != nil {
		t.Fatal(err)
	}

	// MaxIncDepth=2: should resolve d0 and d1 but stop at d2.
	loader := NewMemoryLoader(MemoryConfig{MaxTotalChars: 40000, MaxIncDepth: 2})
	result, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !strings.Contains(result, "level0") {
		t.Errorf("expected level0 content, got: %s", result)
	}
	// At depth 2, the @include d2.md line should remain unresolved.
	if strings.Contains(result, "deepest-level") {
		t.Errorf("expected max depth to prevent resolving deepest level, got: %s", result)
	}
}

func TestMemoryLoader_FileNotFound(t *testing.T) {
	dir := t.TempDir()

	agentsContent := "@include nonexistent.md\nStill here."
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(agentsContent), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewMemoryLoader(MemoryConfig{MaxTotalChars: 40000, MaxIncDepth: 5})
	result, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !strings.Contains(result, "include not found") {
		t.Errorf("expected 'not found' comment, got: %s", result)
	}
	if !strings.Contains(result, "Still here") {
		t.Errorf("expected remaining content, got: %s", result)
	}
}

func TestMemoryLoader_TotalCharLimit(t *testing.T) {
	dir := t.TempDir()

	// Create a large AGENTS.md.
	bigContent := strings.Repeat("A", 50000)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(bigContent), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewMemoryLoader(MemoryConfig{MaxTotalChars: 1000, MaxIncDepth: 5})
	result, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(result) > 1100 { // 1000 chars + truncation message
		t.Errorf("expected result truncated to ~1000 chars, got %d", len(result))
	}
	if !strings.Contains(result, "truncated") {
		t.Errorf("expected truncation notice, got: %s", result[len(result)-50:])
	}
}

func TestMemoryLoader_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	// Isolate HOME so a real global ~/.jcode/AGENTS.md on the developer
	// machine cannot leak into the loader (it always reads ConfigDir()).
	t.Setenv("HOME", t.TempDir())

	loader := NewMemoryLoader(MemoryConfig{MaxTotalChars: 40000, MaxIncDepth: 5})
	result, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for dir with no AGENTS.md, got: %s", result)
	}
}
