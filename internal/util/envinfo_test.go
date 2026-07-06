package utils

import (
	"os/exec"
	"strings"
	"testing"
)

// ScrubbedGitEnv must drop every repo-targeting GIT_* variable (git exports
// them to hook subprocesses; inherited, they silently redirect any git command
// at the OUTER repository) while keeping the rest of the environment.
func TestScrubbedGitEnvDropsRepoTargeting(t *testing.T) {
	t.Setenv("GIT_DIR", "/some/outer/repo/.git")
	t.Setenv("GIT_WORK_TREE", "/some/outer/repo")
	t.Setenv("GIT_INDEX_FILE", "/some/outer/repo/.git/index")
	t.Setenv("GIT_AUTHOR_NAME", "keep-me") // non-targeting GIT_* survives

	env := ScrubbedGitEnv()
	joined := "\n" + strings.Join(env, "\n") + "\n"
	for _, banned := range []string{"\nGIT_DIR=", "\nGIT_WORK_TREE=", "\nGIT_INDEX_FILE="} {
		if strings.Contains(joined, banned) {
			t.Fatalf("scrubbed env still contains %q", strings.TrimSpace(banned))
		}
	}
	if !strings.Contains(joined, "\nGIT_AUTHOR_NAME=keep-me\n") {
		t.Fatalf("scrubbed env dropped a non-targeting variable")
	}
	if !strings.Contains(joined, "\nPATH=") {
		t.Fatalf("scrubbed env lost PATH")
	}
}

// gitCommand must operate on the repo selected by -C even when the process
// inherited a GIT_DIR pointing elsewhere (the git-hook scenario: a pre-push
// hook's test/tool subprocesses inherit an absolute GIT_DIR and would
// otherwise read — or with `git init`, corrupt — the outer repository).
func TestGitCommandIgnoresInheritedGitDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	init := exec.Command("git", "-C", repo, "init", "-q")
	init.Env = ScrubbedGitEnv()
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	t.Setenv("GIT_DIR", "/nonexistent/outer/.git")
	if got := gitCommand(repo, "rev-parse", "--is-inside-work-tree"); got != "true" {
		t.Fatalf("gitCommand under inherited GIT_DIR = %q, want %q", got, "true")
	}
}
