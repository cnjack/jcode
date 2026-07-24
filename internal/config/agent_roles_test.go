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

func roleMarkdown(name, description, model, instructions string) string {
	modelLine := ""
	if model != "" {
		modelLine = "model: " + model + "\n"
	}
	return "---\nname: " + name + "\ndescription: " + description + "\n" +
		modelLine + "---\n\n" + instructions + "\n"
}

func TestLoadAgentRolesPrecedenceAndValidation(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	userRole := filepath.Join(home, ".jcode", "agents", "reviewer.agent.md")
	writeRoleFile(t, userRole, roleMarkdown("reviewer", "user reviewer", "", "User rules."))
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "reviewer.agent.md"),
		roleMarkdown("reviewer", "project reviewer", "small", "Project rules."))
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "missing-name.agent.md"), `---
description: missing name
---

Instructions.`)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "missing-description.agent.md"), `---
name: missing-description
---

Instructions.`)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "empty-body.agent.md"), `---
name: empty-body
description: no instructions
---
`)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "unknown.agent.md"), `---
name: unknown
description: unknown field
tools: read
---

Instructions.`)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "bad-model.agent.md"), `---
name: bad-model
description: invalid model
model: missing-provider
---

Instructions.`)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "huge.agent.md"),
		"---\nname: huge\ndescription: huge\n---\n"+strings.Repeat("x", maxAgentRoleFileBytes))
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "general.agent.md"),
		roleMarkdown("general", "reserved", "", "Instructions."))
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "legacy.json"),
		`{"name":"legacy","description":"legacy","instructions":"ignored"}`)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "plain.md"),
		roleMarkdown("plain", "wrong suffix", "", "Ignored."))
	if err := os.Symlink(userRole, filepath.Join(project, ".jcode", "agents", "linked.agent.md")); err != nil {
		t.Fatal(err)
	}

	roles := LoadAgentRoles(project)
	role, ok := roles["reviewer"]
	if !ok || role.Description != "project reviewer" ||
		role.Instructions != "Project rules." || role.Model != "small" {
		t.Fatalf("reviewer = %+v, ok=%v", role, ok)
	}
	for _, rejected := range []string{
		"missing-name", "missing-description", "empty-body", "unknown", "bad-model",
		"huge", "linked", "general", "legacy", "plain",
	} {
		if _, ok := roles[rejected]; ok {
			t.Errorf("unsafe/malformed role %q was loaded", rejected)
		}
	}
}

func TestLoadAgentRolesProjectOverridesUser(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	writeRoleFile(t, filepath.Join(home, ".jcode", "agents", "audit.agent.md"),
		roleMarkdown("audit", "user", "", "User instructions."))
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "audit.agent.md"),
		roleMarkdown("audit", "project", "", "Project instructions."))
	writeRoleFile(t, filepath.Join(home, ".jcode", "agents", "fallback.agent.md"),
		roleMarkdown("fallback", "user fallback", "", "User fallback instructions."))
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "fallback.agent.md"), `---
name: fallback
description: broken project override
---
`)
	role := LoadAgentRoles(project)["audit"]
	if role.Description != "project" || role.Instructions != "Project instructions." {
		t.Fatalf("project precedence lost: %+v", role)
	}
	fallback := LoadAgentRoles(project)["fallback"]
	if fallback.Description != "user fallback" {
		t.Fatalf("malformed project role hid valid user role: %+v", fallback)
	}
}

func TestLoadAgentRolesUsesFirstValidDuplicateByFilename(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "00-broken.agent.md"), `---
name: duplicate
description: broken
---
`)
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "10-first.agent.md"),
		roleMarkdown("duplicate", "first", "", "First instructions."))
	writeRoleFile(t, filepath.Join(project, ".jcode", "agents", "20-second.agent.md"),
		roleMarkdown("duplicate", "second", "", "Second instructions."))

	role := LoadAgentRoles(project)["duplicate"]
	if role.Description != "first" || role.Instructions != "First instructions." {
		t.Fatalf("duplicate resolution = %+v", role)
	}
}

func TestParseAgentRoleMarkdownSupportsYAMLAndCRLF(t *testing.T) {
	content := []byte("\ufeff---\r\nname: reviewer\r\ndescription: |\r\n  Review correctness\r\n  and security\r\nmodel: provider/model\r\n---\r\n\r\nLead with findings.\r\n")
	meta, body, err := parseAgentRoleMarkdown(content)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "reviewer" || meta.Description != "Review correctness\nand security\n" ||
		meta.Model != "provider/model" {
		t.Fatalf("frontmatter = %+v", meta)
	}
	if body != "Lead with findings." {
		t.Fatalf("body = %q", body)
	}
}
