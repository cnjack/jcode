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

	utils "github.com/cnjack/jcode/internal/util"
)

// runGit runs git in dir with an isolated config so host/global settings (default
// branch, signing, hooks) can't perturb the test. GIT_DIR/GIT_WORK_TREE are
// pinned to dir: when this suite runs inside a git hook (pre-push runs
// `go test ./...`), git exports an absolute GIT_DIR for the pushing repo, and
// an unpinned `git init` here would re-initialize THAT repo as bare instead of
// touching the temp dir. Fails the test on git error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(utils.ScrubbedGitEnv(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_DIR="+filepath.Join(dir, ".git"),
		"GIT_WORK_TREE="+dir,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func currentGitBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	cmd.Env = append(utils.ScrubbedGitEnv(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_DIR="+filepath.Join(dir, ".git"),
		"GIT_WORK_TREE="+dir,
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func initGitRepo(t *testing.T, filename string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "t@example.com")
	runGit(t, repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, filename), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", filename)
	runGit(t, repo, "commit", "-q", "-m", "init")
	return repo, currentGitBranch(t, repo)
}

func TestGitEndpointsResolveExplicitTask(t *testing.T) {
	repoA, branchA := initGitRepo(t, "a.txt")
	repoB, branchB := initGitRepo(t, "b.txt")
	runGit(t, repoB, "checkout", "-q", "-b", "task-b-target")
	runGit(t, repoB, "checkout", "-q", branchB)

	engA := &Engine{taskID: "task-a", pwd: repoA}
	engB := &Engine{taskID: "task-b", pwd: repoB}
	s := &Server{
		Engine: engA,
		tasks:  map[string]*Engine{"task-a": engA, "task-b": engB},
	}

	branchesRec := httptest.NewRecorder()
	s.handleGitBranches(branchesRec, httptest.NewRequest(
		http.MethodGet, "/api/git/branches?task_id=task-b", nil,
	))
	if branchesRec.Code != http.StatusOK || !strings.Contains(branchesRec.Body.String(), "task-b-target") {
		t.Fatalf("task-b branches code=%d body=%s", branchesRec.Code, branchesRec.Body.String())
	}

	checkoutRec := httptest.NewRecorder()
	s.handleGitCheckout(checkoutRec, httptest.NewRequest(
		http.MethodPost,
		"/api/git/checkout",
		strings.NewReader(`{"branch":"task-b-target","task_id":"task-b"}`),
	))
	if checkoutRec.Code != http.StatusOK {
		t.Fatalf("task-b checkout code=%d body=%s", checkoutRec.Code, checkoutRec.Body.String())
	}
	if got := currentGitBranch(t, repoB); got != "task-b-target" {
		t.Fatalf("task-b branch=%q, want task-b-target", got)
	}
	if got := currentGitBranch(t, repoA); got != branchA {
		t.Fatalf("active task-a branch changed=%q, want %q", got, branchA)
	}

	unknownBranches := httptest.NewRecorder()
	s.handleGitBranches(unknownBranches, httptest.NewRequest(
		http.MethodGet, "/api/git/branches?task_id=missing", nil,
	))
	if unknownBranches.Code != http.StatusNotFound {
		t.Fatalf("unknown branches code=%d body=%s", unknownBranches.Code, unknownBranches.Body.String())
	}
	unknownCheckout := httptest.NewRecorder()
	s.handleGitCheckout(unknownCheckout, httptest.NewRequest(
		http.MethodPost,
		"/api/git/checkout",
		strings.NewReader(`{"branch":"main","task_id":"missing"}`),
	))
	if unknownCheckout.Code != http.StatusNotFound {
		t.Fatalf("unknown checkout code=%d body=%s", unknownCheckout.Code, unknownCheckout.Body.String())
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
