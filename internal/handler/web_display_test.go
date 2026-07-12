package handler

import (
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/tools"
)

// TestExtractToolDisplayInfoMCP pins the codex-style MCP rendering: a
// registered MCP tool gets "server.tool" as its title and its compacted args
// as subtitle; unknown tools keep the raw-name fallback.
func TestExtractToolDisplayInfoMCP(t *testing.T) {
	tools.RegisterMCPToolServer("resolve_library_id", "context7")

	info := extractToolDisplayInfo("resolve_library_id", "{ \"library\": \"react\",\n  \"version\": 18 }")
	if info.Title != "context7.resolve_library_id" {
		t.Errorf("MCP title = %q, want context7.resolve_library_id", info.Title)
	}
	if info.Subtitle != `{"library":"react","version":18}` {
		t.Errorf("MCP subtitle not compacted: %q", info.Subtitle)
	}
	if info.Icon != "mcp" {
		t.Errorf("MCP icon = %q, want mcp", info.Icon)
	}

	// Unregistered unknown tool: unchanged fallback.
	info = extractToolDisplayInfo("mystery_tool", "{}")
	if info.Title != "mystery_tool" || info.Icon != "tool" || info.Subtitle != "" {
		t.Errorf("unknown-tool fallback changed: %+v", info)
	}
}

// TestCompactToolArgs covers empty/no-op payloads, compaction, rune-safe
// truncation, and the invalid-JSON passthrough.
func TestCompactToolArgs(t *testing.T) {
	for _, empty := range []string{"", "{}", "null", "  {}  "} {
		if got := compactToolArgs(empty, 80); got != "" {
			t.Errorf("compactToolArgs(%q) = %q, want empty", empty, got)
		}
	}
	if got := compactToolArgs("{\n  \"a\": 1\n}", 80); got != `{"a":1}` {
		t.Errorf("compaction wrong: %q", got)
	}
	long := `{"q":"` + strings.Repeat("界", 100) + `"}`
	got := compactToolArgs(long, 40)
	if r := []rune(got); len(r) != 41 || !strings.HasSuffix(got, "…") {
		t.Errorf("truncation wrong (%d runes): %q", len([]rune(got)), got)
	}
	if got := compactToolArgs("not json\nline", 80); got != "not json line" {
		t.Errorf("invalid-JSON passthrough wrong: %q", got)
	}
}

// TestTodoWriteSubtitle pins the compact call-line summary for todowrite:
// "completed/total · current task" instead of the raw todos array.
func TestTodoWriteSubtitle(t *testing.T) {
	info := extractToolDisplayInfo("todowrite",
		`{"todos":[{"id":1,"title":"a","status":"completed"},`+
			`{"id":2,"title":"fix parser","status":"in_progress"},`+
			`{"id":3,"title":"c","status":"pending"}]}`)
	if info.Subtitle != "1/3 · fix parser" {
		t.Errorf("todowrite subtitle = %q, want \"1/3 · fix parser\"", info.Subtitle)
	}

	// Enhanced single-item actions summarize as the action name.
	info = extractToolDisplayInfo("todowrite", `{"action":"modify","id":"t1","status":"completed"}`)
	if info.Subtitle != "modify" {
		t.Errorf("modify subtitle = %q, want modify", info.Subtitle)
	}

	// Enhanced items payload counts like the legacy one.
	info = extractToolDisplayInfo("todowrite",
		`{"action":"update","items":[{"id":"a","title":"x","status":"completed"},{"id":"b","title":"y","status":"not_started"}]}`)
	if info.Subtitle != "1/2" {
		t.Errorf("items subtitle = %q, want 1/2", info.Subtitle)
	}

	// Unparseable args stay quiet rather than dumping JSON.
	info = extractToolDisplayInfo("todowrite", "")
	if info.Subtitle != "" {
		t.Errorf("empty-args subtitle = %q, want empty", info.Subtitle)
	}
}
