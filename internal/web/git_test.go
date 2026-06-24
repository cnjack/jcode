package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGit runs git in dir with an isolated config so host/global settings (default
// branch, signing, hooks) can't perturb the test. Fails the test on git error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestGitCheckoutRejectsDashBranch is the regression guard for the argv-injection
// fix: a branch beginning with "-" (e.g. "-f") must be rejected with 400 BEFORE
// any git runs. Previously it flowed straight into the argv as `git checkout -f`,
// which force-switched and silently discarded all uncommitted work — returning a
// 200 OK that masked the data loss.
func TestGitCheckoutRejectsDashBranch(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "t@example.com")
	runGit(t, repo, "config", "user.name", "t")
	file := filepath.Join(repo, "a.txt")
	if err := os.WriteFile(file, []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "a.txt")
	runGit(t, repo, "commit", "-q", "-m", "init")
	// Uncommitted change that a stray `git checkout -f` would revert.
	const dirty = "DIRTY uncommitted\n"
	if err := os.WriteFile(file, []byte(dirty), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{Engine: &Engine{pwd: repo}}
	bg := context.Background()
	s.ctxPtr.Store(&bg)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/git/checkout", strings.NewReader(`{"branch":"-f"}`))
	s.handleGitCheckout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dash branch: want 400, got %d body=%q", rec.Code, rec.Body.String())
	}
	// The uncommitted change must survive untouched.
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != dirty {
		t.Fatalf("uncommitted work was destroyed: got %q want %q", got, dirty)
	}
}

// TestBlockKind covers the tracked/untracked classifier used to pick safe
// recovery options in the branch-switch UI.
func TestBlockKind(t *testing.T) {
	untracked := "error: The following untracked working tree files would be overwritten by checkout:\n\tfoo.txt\n" +
		"Please move or remove them before you switch branches.\nAborting"
	tracked := "error: Your local changes to the following files would be overwritten by checkout:\n\tfoo.txt\n" +
		"Please commit your changes or stash them before you switch branches.\nAborting"
	if got := blockKind(untracked); got != "untracked" {
		t.Errorf("untracked message: got %q want %q", got, "untracked")
	}
	if got := blockKind(tracked); got != "tracked" {
		t.Errorf("tracked message: got %q want %q", got, "tracked")
	}
	if got := blockKind("some unrelated git error"); got != "tracked" {
		t.Errorf("default: got %q want %q", got, "tracked")
	}
}

// TestValidatePathsMissingDetection guards the workspace missing-detection fix: a
// path is reported missing only when it confirmably does not exist (or is not a
// directory) — never on a transient/permission stat error, which previously hid
// still-valid workspaces from the picker.
func TestValidatePathsMissingDetection(t *testing.T) {
	s := &Server{}
	base := t.TempDir()
	notExist := filepath.Join(base, "nope")
	regularFile := filepath.Join(base, "file.txt")
	if err := os.WriteFile(regularFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	post := func(paths []string) []string {
		t.Helper()
		body, _ := json.Marshal(map[string][]string{"paths": paths})
		rec := httptest.NewRecorder()
		s.handleValidatePaths(rec, httptest.NewRequest(http.MethodPost, "/api/validate-paths", strings.NewReader(string(body))))
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
		}
		var resp struct {
			Missing []string `json:"missing"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Missing
	}

	got := post([]string{base, notExist, regularFile})
	want := map[string]bool{notExist: true, regularFile: true}
	if len(got) != len(want) {
		t.Fatalf("missing detection: got %v, want exactly {notExist, regularFile}", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("unexpected missing path %q (existing dir must not be missing); got %v", p, got)
		}
	}

	// The fix's core: a path under an unsearchable parent yields EACCES (not
	// not-exist) and must NOT be reported missing.
	if os.Geteuid() == 0 {
		t.Skip("permission check is a no-op as root")
	}
	denied := filepath.Join(base, "denied")
	if err := os.Mkdir(denied, 0o000); err != nil {
		t.Fatal(err)
	}
	// Restore perms so t.TempDir cleanup can remove it (runs before TempDir's own
	// cleanup, which was registered earlier — LIFO).
	t.Cleanup(func() { _ = os.Chmod(denied, 0o755) })
	if got := post([]string{filepath.Join(denied, "ws")}); len(got) != 0 {
		t.Fatalf("EACCES path must not be reported missing, got %v", got)
	}
}
