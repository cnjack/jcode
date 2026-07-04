package pipeline

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cnjack/jcode/internal/memory"
)

// gitEnv strips repo-discovery escape hatches so the baseline repo under the
// memory root can never be confused with an outer repository.
func gitCmd(root string, args ...string) *exec.Cmd {
	base := []string{
		"-C", root,
		"-c", "user.name=jcode-memory",
		"-c", "user.email=memory@jcode.local",
		"-c", "commit.gpgsign=false",
	}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Env = append(os.Environ(), "GIT_DIR="+root+"/.git", "GIT_WORK_TREE="+root)
	return cmd
}

func runGit(root string, args ...string) (string, error) {
	var out, errb bytes.Buffer
	cmd := gitCmd(root, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// gitignoreBody excludes coordination/transient files from the baseline.
// This is what keeps the zero-token no-op fast path alive in steady state:
// without it, every usage-accounting write to state.json (or the pipeline's
// own post-commit state writes) would make `git status` dirty forever and
// force a paid consolidation every cooldown window.
const gitignoreBody = "state.json\n*.lock\n*.tmp\n*.tmp.*\n.state.lock\n.pipeline.lock\n"

// ensureGitignore writes/refreshes the scope's .gitignore.
func ensureGitignore(root string) error {
	p := filepath.Join(root, ".gitignore")
	if b, err := os.ReadFile(p); err == nil && string(b) == gitignoreBody {
		return nil
	}
	return os.WriteFile(p, []byte(gitignoreBody), 0o644)
}

// ensureBaseline initializes the memory git repo if needed and returns true
// when a fresh repo was created.
func ensureBaseline(root string) (bool, error) {
	if err := ensureGitignore(root); err != nil {
		return false, err
	}
	if _, err := os.Stat(root + "/.git"); err == nil {
		// Repo already exists but state.json may have been committed by an
		// older build before .gitignore existed — untrack it so the fast
		// path can recover.
		_, _ = runGit(root, "rm", "-r", "--cached", "-q", "--ignore-unmatch",
			"state.json", ".state.lock", ".pipeline.lock")
		return false, nil
	}
	if _, err := runGit(root, "init", "-q"); err != nil {
		return false, err
	}
	if _, err := runGit(root, "add", "-A"); err != nil {
		return false, err
	}
	// Allow-empty: a brand-new scope may have nothing yet.
	if _, err := runGit(root, "commit", "-q", "--allow-empty", "-m", "memory: baseline"); err != nil {
		return false, err
	}
	return true, nil
}

// workspaceDirty reports whether anything changed since the last baseline
// commit; the diff text (bounded) is returned for the consolidation agent.
func workspaceDirty(root string, maxChars int) (bool, string, error) {
	status, err := runGit(root, "status", "--porcelain")
	if err != nil {
		return false, "", err
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return false, "", nil
	}
	diff, _ := runGit(root, "diff", "HEAD")
	diff = memory.TruncateRunes(diff, maxChars, "\n... (diff truncated)")
	return true, "## Changed files (git status --porcelain)\n" + status + "\n\n## Diff vs baseline\n" + diff, nil
}

func commitBaseline(root, msg string) (string, error) {
	if _, err := runGit(root, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := runGit(root, "commit", "-q", "--allow-empty", "-m", msg); err != nil {
		return "", err
	}
	sha, err := runGit(root, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(sha), err
}
