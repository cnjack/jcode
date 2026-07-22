package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRoleFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAgentRolesPrecedenceAndValidation(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	userRole := filepath.Join(home, ".jcode", "agents", "reviewer.json")
	writeRoleFile(t, userRole, `{"description":"user reviewer","profile":"explore","instructions":"read only"}`)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "reviewer.json"),
		`{"description":"project reviewer","profile":"general","instructions":"project rules"}`)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "broken.json"), `{"profile":"general"}`)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "huge.json"),
		`{"description":"huge","instructions":"`+strings.Repeat("x", maxAgentRoleFileBytes)+`"}`)
	if err := os.Symlink(userRole, filepath.Join(project, ".jcode", "agents", "linked.json")); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{Agents: map[string]*AgentRoleConfig{
		"reviewer": {Description: "inline reviewer", Profile: "coordinator", Instructions: "inline rules", Model: "small"},
		"general":  {Description: "reserved", Instructions: "bad"},
	}}
	roles := LoadAgentRoles(project, cfg)
	role, ok := roles["reviewer"]
	if !ok || role.Description != "inline reviewer" || role.Profile != "coordinator" || role.Model != "small" {
		t.Fatalf("reviewer = %+v, ok=%v", role, ok)
	}
	for _, rejected := range []string{"broken", "huge", "linked", "general"} {
		if _, ok := roles[rejected]; ok {
			t.Errorf("unsafe/malformed role %q was loaded", rejected)
		}
	}
}

func TestLoadAgentRolesProjectOverridesUser(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	writeRoleFile(t, filepath.Join(home, ".jcode", "agents", "audit.json"),
		`{"description":"user","instructions":"user"}`)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "audit.json"),
		`{"description":"project","profile":"general","instructions":"project"}`)
	role := LoadAgentRoles(project, &Config{})["audit"]
	if role.Description != "project" || role.Profile != "general" {
		t.Fatalf("project precedence lost: %+v", role)
	}
}
