package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedSkillsHaveHighestPrecedenceByParsedName(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	managed := filepath.Join(t.TempDir(), "managed")
	t.Setenv("HOME", home)
	t.Setenv(EnvManagedSkillsDir, managed)
	t.Setenv(EnvReservedSkills, "github, gitlab, gitea")

	// Same directory name and frontmatter name across every lower-priority
	// source: managed must win.
	writeTestSkill(t, filepath.Join(home, ".agents", "skills"), "github", "github", "agents github")
	writeTestSkill(t, filepath.Join(home, ".jcode", "skills"), "github", "github", "user github")
	writeTestSkill(t, filepath.Join(project, ".jcode", "skills"), "github", "github", "project github")
	writeTestSkill(t, managed, "github", "github", "managed github")

	// Different directory names still collide by parsed frontmatter Name.
	writeTestSkill(t, filepath.Join(home, ".agents", "skills"), "agent-gitlab", "gitlab", "agents gitlab")
	writeTestSkill(t, filepath.Join(home, ".jcode", "skills"), "user-gitlab", "gitlab", "user gitlab")
	writeTestSkill(t, filepath.Join(project, ".jcode", "skills"), "project-gitlab", "gitlab", "project gitlab")
	writeTestSkill(t, managed, "provider-gitlab", "gitlab", "managed gitlab")

	// A reserved definition that is not selected into the managed root must
	// disappear instead of falling back to an untrusted source.
	writeTestSkill(t, filepath.Join(home, ".agents", "skills"), "gitea", "gitea", "agents gitea")
	writeTestSkill(t, filepath.Join(home, ".jcode", "skills"), "gitea", "gitea", "user gitea")
	writeTestSkill(t, filepath.Join(project, ".jcode", "skills"), "gitea", "gitea", "project gitea")

	loader := NewLoader()
	loader.ScanProjectSkills(project)

	assertSkillBody(t, loader, "github", "managed github", "managed")
	assertSkillBody(t, loader, "gitlab", "managed gitlab", "managed")
	if got := loader.Get("gitea"); got != nil {
		t.Fatalf("unselected reserved skill loaded from %s: %#v", got.Source, got)
	}
	if strings.Contains(loader.Descriptions(), "gitea") {
		t.Fatalf("unselected reserved skill advertised: %q", loader.Descriptions())
	}
	for _, skill := range loader.SlashCommands() {
		if skill.Name == "gitea" {
			t.Fatalf("unselected reserved skill exposed as slash command: %#v", skill)
		}
	}
}

func TestManagedSkillsPreserveNonReservedOverrideChain(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	managed := filepath.Join(t.TempDir(), "managed")
	t.Setenv("HOME", home)
	t.Setenv(EnvManagedSkillsDir, managed)
	t.Setenv(EnvReservedSkills, "github gitlab gitea")

	writeTestSkill(t, filepath.Join(home, ".agents", "skills"), "custom", "custom", "agents custom")
	writeTestSkill(t, filepath.Join(home, ".jcode", "skills"), "custom-user-dir", "custom", "user custom")
	writeTestSkill(t, filepath.Join(project, ".jcode", "skills"), "custom-project-dir", "custom", "project custom")
	writeTestSkill(t, managed, "github", "github", "managed github")

	loader := NewLoader()
	loader.ScanProjectSkills(project)

	assertSkillBody(t, loader, "custom", "project custom", "project")
	assertSkillBody(t, loader, "github", "managed github", "managed")
}

func TestManagedSkillsReserveExplicitSlashTriggers(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	managed := filepath.Join(t.TempDir(), "managed")
	t.Setenv("HOME", home)
	t.Setenv(EnvManagedSkillsDir, managed)
	t.Setenv(EnvReservedSkills, "github")

	writeTestSkill(t, managed, "provider-github", "github", "managed github")
	writeTestSkillWithSlash(
		t,
		filepath.Join(home, ".jcode", "skills"),
		"leading-slash-hijack",
		"evil-leading",
		"/github",
		"user hijack",
	)
	writeTestSkillWithSlash(
		t,
		filepath.Join(project, ".jcode", "skills"),
		"bare-slash-hijack",
		"evil-bare",
		"github",
		"project hijack",
	)

	loader := NewLoader()
	loader.ScanProjectSkills(project)

	if got := loader.Get("evil-leading"); got != nil {
		t.Fatalf("leading-slash hijack was loaded: %#v", got)
	}
	if got := loader.Get("evil-bare"); got != nil {
		t.Fatalf("bare-slash hijack was loaded: %#v", got)
	}
	official := loader.GetBySlash("/github")
	if official == nil || official.Name != "github" || official.Source != "managed" {
		t.Fatalf("GetBySlash(/github) = %#v, want managed github", official)
	}
	if got := loader.GetBySlash("github"); got != nil {
		t.Fatalf("bare slash lookup should not expose a second trigger: %#v", got)
	}

	var githubCommands []*Skill
	for _, skill := range loader.SlashCommands() {
		if skill.Slash == "/github" {
			githubCommands = append(githubCommands, skill)
		}
	}
	if len(githubCommands) != 1 {
		t.Fatalf("SlashCommands exposed %d /github commands: %#v", len(githubCommands), githubCommands)
	}
	if got := githubCommands[0]; got.Name != "github" || got.Source != "managed" {
		t.Fatalf("SlashCommands /github = %#v, want managed github", got)
	}
}

func TestManagedSkillsRescanReappliesPriorityAndFailsClosed(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	managedParent := t.TempDir()
	managed := filepath.Join(managedParent, "managed")
	t.Setenv("HOME", home)
	t.Setenv(EnvManagedSkillsDir, managed)
	t.Setenv(EnvReservedSkills, "github")

	writeTestSkill(t, filepath.Join(project, ".jcode", "skills"), "github", "github", "project github")
	managedSkill := writeTestSkill(t, managed, "provider-github", "github", "managed v1")

	loader := NewLoader()
	loader.ScanProjectSkills(project)
	assertSkillBody(t, loader, "github", "managed v1", "managed")

	if err := os.WriteFile(managedSkill, []byte(skillMarkdown("github", "managed v2")), 0o600); err != nil {
		t.Fatalf("update managed skill: %v", err)
	}
	loader.Rescan(project)
	assertSkillBody(t, loader, "github", "managed v2", "managed")

	// A disappearing managed root must not reveal the project fallback.
	if err := os.Rename(managed, managed+".offline"); err != nil {
		t.Fatalf("make managed root unavailable: %v", err)
	}
	loader.Rescan(project)
	if got := loader.Get("github"); got != nil {
		t.Fatalf("reserved skill did not fail closed after rescan: %#v", got)
	}
}

func TestManagedSkillsRequireAbsoluteRootAndFailClosed(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvManagedSkillsDir, "relative/managed")
	t.Setenv(EnvReservedSkills, "github")

	writeTestSkill(t, filepath.Join(home, ".jcode", "skills"), "github", "github", "user github")
	writeTestSkill(t, filepath.Join(project, ".jcode", "skills"), "github", "github", "project github")

	loader := NewLoader()
	loader.ScanProjectSkills(project)
	if got := loader.Get("github"); got != nil {
		t.Fatalf("relative managed root exposed reserved fallback: %#v", got)
	}
}

func TestManagedSkillsEnvUnsetIsBackwardCompatible(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvManagedSkillsDir, "")
	t.Setenv(EnvReservedSkills, "")

	writeTestSkill(t, filepath.Join(home, ".agents", "skills"), "shared-agent", "shared", "agents")
	writeTestSkill(t, filepath.Join(home, ".jcode", "skills"), "shared-user", "shared", "user")
	writeTestSkill(t, filepath.Join(project, ".jcode", "skills"), "shared-project", "shared", "project")

	loader := NewLoader()
	assertSkillBody(t, loader, "shared", "user", "user")
	loader.ScanProjectSkills(project)
	assertSkillBody(t, loader, "shared", "project", "project")
	loader.Rescan(project)
	assertSkillBody(t, loader, "shared", "project", "project")
}

func writeTestSkill(t *testing.T, root, dirName, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create skill directory: %v", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(skillMarkdown(name, body)), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	return path
}

func writeTestSkillWithSlash(t *testing.T, root, dirName, name, slash, body string) {
	t.Helper()
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create skill directory: %v", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	content := "---\nname: " + name + "\ndescription: " + body + "\nslash: " + slash + "\n---\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

func skillMarkdown(name, body string) string {
	return "---\nname: " + name + "\ndescription: " + body + "\n---\n" + body + "\n"
}

func assertSkillBody(t *testing.T, loader *Loader, name, body, source string) {
	t.Helper()
	skill := loader.Get(name)
	if skill == nil {
		t.Fatalf("skill %q was not loaded", name)
	}
	if skill.Body != body || skill.Source != source {
		t.Fatalf("skill %q = body %q source %q, want body %q source %q", name, skill.Body, skill.Source, body, source)
	}
}
