package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMCPFile_NotExist(t *testing.T) {
	servers, err := LoadMCPFile("/nonexistent/mcp.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if servers != nil {
		t.Errorf("expected nil, got %v", servers)
	}
}

func TestLoadMCPFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	content := `{
		"mcpServers": {
			"filesystem": {
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
			},
			"remote": {
				"url": "http://localhost:3000/mcp",
				"timeout_seconds": 30
			}
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := LoadMCPFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
	fs := servers["filesystem"]
	if fs.Command != "npx" {
		t.Errorf("filesystem.Command = %q, want npx", fs.Command)
	}
	if len(fs.Args) != 3 {
		t.Errorf("filesystem.Args = %v, want 3 args", fs.Args)
	}
	remote := servers["remote"]
	if remote.URL != "http://localhost:3000/mcp" {
		t.Errorf("remote.URL = %q", remote.URL)
	}
	if remote.TimeoutSeconds != 30 {
		t.Errorf("remote.TimeoutSeconds = %d, want 30", remote.TimeoutSeconds)
	}
}

func TestLoadMCPFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadMCPFile(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadMCPFiles_ProjectRoot(t *testing.T) {
	// Isolate HOME so global ~/.jcode/mcp.json doesn't interfere.
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	content := `{"mcpServers": {"local-tool": {"command": "/usr/bin/local"}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := LoadMCPFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if servers == nil {
		t.Fatal("expected non-nil servers")
	}
	if servers["local-tool"] == nil {
		t.Fatal("expected local-tool server")
	}
	if servers["local-tool"].Command != "/usr/bin/local" {
		t.Errorf("Command = %q", servers["local-tool"].Command)
	}
}

func TestLoadMCPFiles_DotJcodeDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	jcodeDir := filepath.Join(dir, ".jcode")
	if err := os.MkdirAll(jcodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"mcpServers": {"dot-tool": {"command": "/usr/bin/dot"}}}`
	if err := os.WriteFile(filepath.Join(jcodeDir, "mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := LoadMCPFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if servers["dot-tool"] == nil {
		t.Fatal("expected dot-tool server from .jcode/mcp.json")
	}
}

func TestLoadMCPFiles_MergePrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	jcodeDir := filepath.Join(dir, ".jcode")
	if err := os.MkdirAll(jcodeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// .jcode/mcp.json defines a server with args.
	dotContent := `{"mcpServers": {"tool": {"command": "/usr/bin/tool", "args": ["--from-dot"]}}}`
	if err := os.WriteFile(filepath.Join(jcodeDir, "mcp.json"), []byte(dotContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Root mcp.json overrides args (higher precedence).
	rootContent := `{"mcpServers": {"tool": {"args": ["--from-root"]}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(rootContent), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := LoadMCPFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tool := servers["tool"]
	if tool == nil {
		t.Fatal("expected tool server")
	}
	// Command from .jcode/mcp.json (first definition), args from root (override).
	if tool.Command != "/usr/bin/tool" {
		t.Errorf("Command = %q, want /usr/bin/tool", tool.Command)
	}
	if len(tool.Args) != 1 || tool.Args[0] != "--from-root" {
		t.Errorf("Args = %v, want [--from-root]", tool.Args)
	}
}

func TestMergeMCPServers_NilConfig(t *testing.T) {
	// Should not panic.
	MergeMCPServers(nil, map[string]*MCPServer{"x": {Command: "y"}})
}

func TestMergeMCPServers_EmptyServers(t *testing.T) {
	cfg := &Config{}
	MergeMCPServers(cfg, nil)
	if cfg.MCPServers != nil {
		t.Error("expected nil MCPServers for empty input")
	}
}
