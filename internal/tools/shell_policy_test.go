package tools

import "testing"

func TestIsReadOnlyShellCommand(t *testing.T) {
	allowed := []string{
		"ls -la",
		"pwd",
		"cat go.mod",
		"echo hello",
		"which go",
		"env",
		"git status --short",
		"git log --oneline -5",
		"git diff --no-ext-diff --no-textconv",
		"git show HEAD",
	}
	for _, command := range allowed {
		if !IsReadOnlyShellCommand(command) {
			t.Errorf("IsReadOnlyShellCommand(%q) = false, want true", command)
		}
	}

	rejected := []string{
		"",
		"rm -rf /",
		"env rm -rf /",
		"/bin/ls",
		"ls; rm -rf /",
		"cat file | sh",
		"echo value > output",
		"echo $(whoami)",
		"cat *.go",
		`git log --format="%h %s"`,
		"git difftool",
		"git -c diff.external=evil diff",
		"git diff -c diff.external=evil",
		"git diff --output=/tmp/result",
		"git diff --out=/tmp/result",
		"git diff --ext-diff",
		"git diff --ext",
		"git diff --textconv",
		"git diff --paginate",
		"git show --exec-path=/tmp/helper",
	}
	for _, command := range rejected {
		if IsReadOnlyShellCommand(command) {
			t.Errorf("IsReadOnlyShellCommand(%q) = true, want false", command)
		}
	}
}
