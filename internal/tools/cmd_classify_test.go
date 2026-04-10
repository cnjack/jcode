package tools

import "testing"

func TestClassify_SearchCommands(t *testing.T) {
	// X-23: grep, rg, find → search
	for _, cmd := range []string{"grep -r foo .", "rg pattern", "find . -name '*.go'", "ag something"} {
		if cat := classifyCommand(cmd); cat != CmdSearch {
			t.Errorf("expected %q → search, got %s", cmd, cat)
		}
	}
}

func TestClassify_ReadCommands(t *testing.T) {
	for _, cmd := range []string{"cat file.txt", "head -20 file.go", "tail -f log", "jq .name", "wc -l"} {
		if cat := classifyCommand(cmd); cat != CmdRead {
			t.Errorf("expected %q → read, got %s", cmd, cat)
		}
	}
}

func TestClassify_ListCommands(t *testing.T) {
	// X-24 (partial): ls → list
	for _, cmd := range []string{"ls -la", "tree", "du -sh .", "df -h"} {
		if cat := classifyCommand(cmd); cat != CmdList {
			t.Errorf("expected %q → list, got %s", cmd, cat)
		}
	}
}

func TestClassify_SafeCommands(t *testing.T) {
	// X-24 (partial): pwd, echo → safe
	for _, cmd := range []string{"pwd", "echo hello", "date", "whoami", "uname -a", "which go", "env", "printenv HOME"} {
		if cat := classifyCommand(cmd); cat != CmdSafe {
			t.Errorf("expected %q → safe, got %s", cmd, cat)
		}
	}
}

func TestClassify_GitReadCommands(t *testing.T) {
	// X-25: git status, git log → git
	for _, cmd := range []string{
		"git status", "git log --oneline", "git diff HEAD",
		"git show abc123", "git branch -a", "git tag",
		"git remote -v", "git config user.name",
	} {
		if cat := classifyCommand(cmd); cat != CmdGit {
			t.Errorf("expected %q → git, got %s", cmd, cat)
		}
	}
}

func TestClassify_GitMutatingCommands(t *testing.T) {
	// git push, git commit, etc. should be mutating
	for _, cmd := range []string{"git push", "git commit -m 'msg'", "git checkout main", "git merge feature"} {
		if cat := classifyCommand(cmd); cat != CmdMutating {
			t.Errorf("expected %q → mutating, got %s", cmd, cat)
		}
	}
}

func TestClassify_Unknown(t *testing.T) {
	// X-27: unknown → mutating
	for _, cmd := range []string{"npm install", "go build ./...", "docker run alpine", "make build"} {
		if cat := classifyCommand(cmd); cat != CmdMutating {
			t.Errorf("expected %q → mutating, got %s", cmd, cat)
		}
	}
}

func TestClassify_Empty(t *testing.T) {
	if cat := classifyCommand(""); cat != CmdMutating {
		t.Errorf("expected empty → mutating, got %s", cat)
	}
}

func TestClassify_FullPath(t *testing.T) {
	// Commands with full paths should still classify correctly
	if cat := classifyCommand("/usr/bin/grep foo bar"); cat != CmdSearch {
		t.Errorf("expected /usr/bin/grep → search, got %s", cat)
	}
}

func TestClassify_IsCollapsible(t *testing.T) {
	// X-26: search/read/list/git IsCollapsible() = true
	collapsible := []CommandCategory{CmdSearch, CmdRead, CmdList, CmdGit}
	for _, c := range collapsible {
		if !c.IsCollapsible() {
			t.Errorf("expected %s to be collapsible", c)
		}
	}

	nonCollapsible := []CommandCategory{CmdSafe, CmdMutating}
	for _, c := range nonCollapsible {
		if c.IsCollapsible() {
			t.Errorf("expected %s to NOT be collapsible", c)
		}
	}
}
