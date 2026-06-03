package handler

import (
	"path/filepath"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestACPToolPresentationReadUsesFriendlyTitleAndAbsoluteLocation(t *testing.T) {
	workDir := filepath.Join(string(filepath.Separator), "tmp", "jcode-work")
	h := NewACPHandler(nil, "sess", workDir)

	p := h.presentationForTool("read", `{"file_path":"internal/handler/acp.go","offset":10,"limit":5}`)

	if p.Title != "Read internal/handler/acp.go (10-14)" {
		t.Fatalf("title = %q", p.Title)
	}
	if p.Kind != acp.ToolKindRead {
		t.Fatalf("kind = %q", p.Kind)
	}
	if len(p.Locations) != 1 {
		t.Fatalf("locations len = %d", len(p.Locations))
	}
	wantPath := filepath.Join(workDir, "internal", "handler", "acp.go")
	if p.Locations[0].Path != wantPath {
		t.Fatalf("location path = %q, want %q", p.Locations[0].Path, wantPath)
	}
	if p.Locations[0].Line == nil || *p.Locations[0].Line != 10 {
		t.Fatalf("location line = %v, want 10", p.Locations[0].Line)
	}
}

func TestACPToolPresentationSearchAndExecute(t *testing.T) {
	h := NewACPHandler(nil, "sess", "/repo")

	grep := h.presentationForTool("grep", `{"pattern":"ToolCall","path":"internal"}`)
	if grep.Title != `Search "ToolCall" in internal` {
		t.Fatalf("grep title = %q", grep.Title)
	}
	if grep.Kind != acp.ToolKindSearch {
		t.Fatalf("grep kind = %q", grep.Kind)
	}

	exec := h.presentationForTool("execute", `{"command":"go test ./...","description":"Run all tests"}`)
	if exec.Title != "Run all tests" {
		t.Fatalf("execute title = %q", exec.Title)
	}
	if exec.Kind != acp.ToolKindExecute {
		t.Fatalf("execute kind = %q", exec.Kind)
	}
}

func TestACPToolPresentationWriteIncludesDiffContent(t *testing.T) {
	h := NewACPHandler(nil, "sess", "/repo")

	p := h.presentationForTool("write", `{"file_path":"README.md","content":"hello"}`)

	if p.Title != "Write README.md" {
		t.Fatalf("title = %q", p.Title)
	}
	if len(p.Content) != 1 || p.Content[0].Diff == nil {
		t.Fatalf("expected one diff content item, got %#v", p.Content)
	}
	if p.Content[0].Diff.Path != "README.md" {
		t.Fatalf("diff path = %q", p.Content[0].Diff.Path)
	}
}

func TestACPToolFailureOutputDetection(t *testing.T) {
	cases := []string{
		"Tool execution failed: exit status 1",
		"partial output\n\nTool execution failed: exit status 1",
		"Tool execution panicked: boom",
	}
	for _, tc := range cases {
		if !isToolFailureOutput(tc) {
			t.Fatalf("expected failure output for %q", tc)
		}
	}
	if isToolFailureOutput(strings.TrimSpace("command completed")) {
		t.Fatal("did not expect normal output to be treated as failure")
	}
}
