package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testDraftProvider struct {
	remote  string
	repo    draftRepository
	created int
	url     string
	fail    bool
}

func (p *testDraftProvider) repository(context.Context, string, string) (draftRepository, error) {
	return p.repo, nil
}
func (p *testDraftProvider) find(context.Context, string, string, string) (string, error) {
	return p.url, nil
}
func (p *testDraftProvider) create(context.Context, string, string, string, string, string, string) (string, error) {
	p.created++
	if p.fail {
		return "", errors.New("provider failed")
	}
	p.url = p.repo.URL + "/pull/42"
	return p.url, nil
}
func TestDraftPRSelectedFilesPreservesWorkspaceAndRetries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	dir := initGitWorkspace(t, "main")
	for _, name := range []string{"chosen.txt", "unrelated.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	changeFixtureGit(t, dir, "add", ".")
	changeFixtureGit(t, dir, "commit", "-m", "base")
	head := strings.TrimSpace(changeFixtureGit(t, dir, "rev-parse", "HEAD"))
	remote := filepath.Join(t.TempDir(), "repo.git")
	changeFixtureGit(t, dir, "init", "--bare", remote)
	changeFixtureGit(t, dir, "remote", "add", "origin", "https://github.com/owner/repo.git")

	for _, name := range []string{"chosen.txt", "unrelated.txt", "new.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("after\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	changeFixtureGit(t, dir, "add", "unrelated.txt")
	statusBefore := changeFixtureGit(t, dir, "status", "--porcelain=v1")
	indexBefore := changeFixtureGit(t, dir, "diff", "--cached")
	changes, err := inspectSessionChanges(t.Context(), "session", dir)
	if err != nil {
		t.Fatal(err)
	}
	provider := &testDraftProvider{remote: remote, repo: draftRepository{NameWithOwner: "owner/repo", URL: "https://github.com/owner/repo"}, fail: true}
	provider.repo.DefaultBranchRef.Name = "main"
	provider.repo.DefaultBranchRef.Target.OID = head
	request := draftPRRequest{SessionID: "session", RepositoryURL: provider.repo.URL, BaseSHA: head, Revision: changes.Revision, Paths: []string{"chosen.txt", "new.txt"}, Title: "Reviewed change"}
	_, err = publishSessionDraft(t.Context(), "session", dir, request, provider)
	if err == nil || !strings.Contains(err.Error(), "was pushed") {
		t.Fatalf("want actionable partial-push error: %v", err)
	}
	provider.fail = false
	result, err := publishSessionDraft(t.Context(), "session", dir, request, provider)
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != "https://github.com/owner/repo/pull/42" {
		t.Fatalf("result %+v", result)
	}
	if body := changeFixtureGit(t, dir, "show", result.Branch+":unrelated.txt"); body != "before\n" {
		t.Fatal("unrelated changes included")
	}
	if body := changeFixtureGit(t, dir, "show", result.Branch+":chosen.txt"); body != "after\n" {
		t.Fatal("selected changes missing")
	}
	if body := changeFixtureGit(t, dir, "show", result.Branch+":new.txt"); body != "after\n" {
		t.Fatal("untracked selection missing")
	}
	if statusBefore != changeFixtureGit(t, dir, "status", "--porcelain=v1") || indexBefore != changeFixtureGit(t, dir, "diff", "--cached") {
		t.Fatal("workspace or original index changed")
	}
	if strings.TrimSpace(changeFixtureGit(t, dir, "rev-parse", "HEAD")) != head {
		t.Fatal("checked-out HEAD changed")
	}
	again, err := publishSessionDraft(t.Context(), "session", dir, request, provider)
	if err != nil || again.URL != result.URL || provider.created != 2 {
		t.Fatalf("retry did not reuse PR: %+v %v count=%d", again, err, provider.created)
	}
	request.Revision = "stale"
	_, err = publishSessionDraft(t.Context(), "session", dir, request, provider)
	if err == nil || !strings.Contains(err.Error(), "since review") {
		t.Fatal("stale review accepted")
	}
}

func (p *testDraftProvider) push(ctx context.Context, pwd, repo, ref string) error {
	_, err := draftGit(ctx, pwd, "", "", "push", p.remote, ref+":"+ref)
	return err
}

func TestDraftPreviewIncludesCommittedChangesWithoutPublishingUnselectedFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	dir := initGitWorkspace(t, "feature")
	for _, name := range []string{"selected.txt", "other.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("base\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	changeFixtureGit(t, dir, "add", ".")
	changeFixtureGit(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(changeFixtureGit(t, dir, "rev-parse", "HEAD"))
	for _, name := range []string{"selected.txt", "other.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("feature\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	changeFixtureGit(t, dir, "add", ".")
	changeFixtureGit(t, dir, "commit", "-m", "existing feature commit")
	head := changeFixtureGit(t, dir, "rev-parse", "HEAD")
	remote := filepath.Join(t.TempDir(), "bare.git")
	changeFixtureGit(t, dir, "init", "--bare", remote)
	changeFixtureGit(t, dir, "remote", "add", "origin", "https://github.com/owner/repo.git")
	provider := &testDraftProvider{remote: remote, repo: draftRepository{URL: "https://github.com/owner/repo", NameWithOwner: "owner/repo"}}
	provider.repo.DefaultBranchRef.Name = "main"
	provider.repo.DefaultBranchRef.Target.OID = base
	preview, _, err := previewSessionDraft(t.Context(), "session", dir, provider)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Files) != 2 || preview.BaseSHA != base {
		t.Fatalf("committed changes missing: %+v", preview)
	}
	result, err := publishSessionDraft(t.Context(), "session", dir, draftPRRequest{SessionID: "session", RepositoryURL: provider.repo.URL, BaseSHA: base, Revision: preview.Revision, Paths: []string{"selected.txt"}, Title: "Selected committed change"}, provider)
	if err != nil {
		t.Fatal(err)
	}
	if content := changeFixtureGit(t, dir, "show", result.Branch+":other.txt"); content != "base\n" {
		t.Fatal("unselected committed file was included")
	}
	if content := changeFixtureGit(t, dir, "show", result.Branch+":selected.txt"); content != "feature\n" {
		t.Fatal("selected committed change missing")
	}
	if changeFixtureGit(t, dir, "rev-parse", "HEAD") != head {
		t.Fatal("current branch changed")
	}
}
