package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	utils "github.com/cnjack/jcode/internal/util"
)

type draftPRRequest struct {
	SessionID     string   `json:"session_id"`
	RepositoryURL string   `json:"repository_url"`
	BaseSHA       string   `json:"base_sha"`
	Revision      string   `json:"revision"`
	Paths         []string `json:"paths"`
	Title         string   `json:"title"`
	Body          string   `json:"body"`
}
type draftPRResult struct {
	URL    string `json:"url"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	Base   string `json:"base"`
}
type draftRepository struct {
	NameWithOwner    string `json:"nameWithOwner"`
	URL              string `json:"url"`
	DefaultBranchRef struct {
		Name   string `json:"name"`
		Target struct {
			OID string `json:"oid"`
		} `json:"target"`
	} `json:"defaultBranchRef"`
}

type draftProvider interface {
	push(context.Context, string, string, string) error
	repository(context.Context, string, string) (draftRepository, error)
	find(context.Context, string, string, string) (string, error)
	create(context.Context, string, string, string, string, string, string) (string, error)
}
type githubDraftProvider struct{}

func draftCommand(ctx context.Context, pwd, program string, env []string, input string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = pwd
	cmd.Env = env
	cmd.Stdin = strings.NewReader(input)
	out := &boundedChangeOutput{limit: 64 * 1024}
	cmd.Stdout = out
	// Do not include subprocess stderr: Git helpers and providers may echo credentials.
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}
func draftGit(ctx context.Context, pwd, index, input string, args ...string) (string, error) {
	env := append(utils.ScrubbedGitEnv(), "GIT_TERMINAL_PROMPT=0")
	if index != "" {
		env = append(env, "GIT_INDEX_FILE="+index)
	}
	return draftCommand(ctx, pwd, "git", env, input, append([]string{"-c", "commit.gpgsign=false", "-c", "core.fsmonitor=false"}, args...)...)
}
func ghDraftCommand(ctx context.Context, pwd string, args ...string) (string, error) {
	env := append(os.Environ(), "GH_PROMPT_DISABLED=1", "GIT_TERMINAL_PROMPT=0")
	return draftCommand(ctx, pwd, "gh", env, "", args...)
}
func (githubDraftProvider) repository(ctx context.Context, pwd, remote string) (draftRepository, error) {
	var repo draftRepository
	out, err := ghDraftCommand(ctx, pwd, "repo", "view", remote, "--json", "nameWithOwner,url,defaultBranchRef")
	if err != nil {
		return repo, errors.New("install GitHub CLI on the device and run gh auth login for this repository's host, then retry. This action uses the device's existing GitHub identity")
	}
	if err = json.Unmarshal([]byte(out), &repo); err != nil || repo.NameWithOwner == "" || repo.DefaultBranchRef.Name == "" {
		return repo, errors.New("could not identify the GitHub repository and default branch. Check origin and GitHub CLI access on the device")
	}
	// gh repo view's GraphQL shape only exposes the default branch name. Resolve
	// its current remote commit explicitly before allowing publication.
	parsed, err := url.Parse(repo.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return repo, errors.New("the GitHub repository URL is invalid")
	}
	branch, err := ghDraftCommand(ctx, pwd, "api", "--hostname", parsed.Host, "repos/"+repo.NameWithOwner+"/branches/"+url.PathEscape(repo.DefaultBranchRef.Name), "--jq", ".commit.sha")
	if err != nil || len(branch) != 40 {
		return repo, errors.New("could not verify the remote base branch. Check GitHub CLI access and retry")
	}
	repo.DefaultBranchRef.Target.OID = branch
	return repo, nil
}
func (githubDraftProvider) find(ctx context.Context, pwd, repo, branch string) (string, error) {
	out, err := ghDraftCommand(ctx, pwd, "pr", "list", "--repo", repo, "--head", branch, "--state", "all", "--json", "url", "--jq", ".[0].url // empty")
	if err != nil {
		return "", errors.New("could not check whether a pull request already exists. Check GitHub CLI access and retry")
	}
	return out, nil
}
func (githubDraftProvider) create(ctx context.Context, pwd, repo, base, branch, title, body string) (string, error) {
	return ghDraftCommand(ctx, pwd, "pr", "create", "--repo", repo, "--draft", "--base", base, "--head", branch, "--title", title, "--body", body)
}

func (s *Server) handleSessionDraftPR(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	var req draftPRRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || sid == "" || sid == "new" || req.SessionID != sid {
		writeJSON(w, 400, map[string]string{"error": "An existing session and a valid reviewed draft PR request are required."})
		return
	}
	if eng := s.resolveEngine(sid); eng != nil && eng.env != nil && eng.env.IsRemote() {
		writeJSON(w, 409, map[string]string{"error": "Open this SSH or Docker workspace on its host to create a pull request."})
		return
	}
	pwd, err := s.workspacePwdForTask(sid)
	if err != nil || pwd == "" || !filepath.IsAbs(pwd) {
		writeJSON(w, 404, map[string]string{"error": "Session workspace not found. Reopen it on the device."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	result, err := publishSessionDraft(ctx, sid, pwd, req, githubDraftProvider{})
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func publishSessionDraft(ctx context.Context, sid, pwd string, req draftPRRequest, provider draftProvider) (*draftPRResult, error) {
	req.Title = strings.TrimSpace(req.Title)
	if req.Revision == "" || len(req.Paths) == 0 || len(req.Paths) > changeFileLimit || req.Title == "" || len(req.Title) > 200 || len(req.Body) > 20000 {
		return nil, errors.New("select files from a complete changes preview and enter a pull request title (up to 200 characters)")
	}
	changes, repo, err := previewSessionDraft(ctx, sid, pwd, provider)
	if err != nil {
		return nil, err
	}
	if req.SessionID != sid || req.RepositoryURL != repo.URL || changes.Truncated || changes.Revision != req.Revision || changes.BaseSHA != req.BaseSHA {
		return nil, errors.New("workspace or base branch changed since review. Refresh the draft preview, review the files again, then retry")
	}
	patches := make(map[string]string, len(changes.Files))
	for _, file := range changes.Files {
		patches[file.Path] = file.Patch
	}
	sort.Strings(req.Paths)
	var patch strings.Builder
	for i, path := range req.Paths {
		p, ok := patches[path]
		if !ok || p == "" || (i > 0 && req.Paths[i-1] == path) {
			return nil, errors.New("the selected files no longer match the changes preview. Refresh and select them again")
		}
		patch.WriteString(p)
	}
	raw, _ := json.Marshal(struct {
		Session, Head, Repository string
		Request                   draftPRRequest
	}{sid, changes.Head, repo.URL, req})
	digest := sha256.Sum256(raw)
	branch := "jcode/cloud-" + hex.EncodeToString(digest[:16])
	existing, err := provider.find(ctx, pwd, repo.URL, branch)
	if err != nil {
		return nil, err
	}
	if existing != "" {
		if !validDraftURL(repo.URL, existing) {
			return nil, errors.New("the provider returned a pull request URL outside this repository")
		}
		return &draftPRResult{URL: existing, Branch: branch, Base: repo.DefaultBranchRef.Name}, nil
	}
	temp, err := os.MkdirTemp("", "jcode-draft-index-")
	if err != nil {
		return nil, errors.New("could not prepare a temporary Git index. Check device disk space and retry")
	}
	defer os.RemoveAll(temp) //nolint:errcheck // temporary index only
	index := filepath.Join(temp, "index")
	if _, err = draftGit(ctx, pwd, index, "", "read-tree", changes.BaseSHA); err != nil {
		return nil, errors.New("could not prepare the reviewed base commit. Check Git on the device and retry")
	}
	if _, err = draftGit(ctx, pwd, index, patch.String(), "apply", "--cached", "--binary", "--whitespace=nowarn", "-"); err != nil {
		return nil, errors.New("the selected changes could not be applied to the reviewed base. Refresh and inspect the files on the device")
	}
	tree, err := draftGit(ctx, pwd, index, "", "write-tree")
	if err != nil {
		return nil, errors.New("could not write the reviewed Git tree. Check device disk space and retry")
	}
	ref := "refs/heads/" + branch
	commit, _ := draftGit(ctx, pwd, "", "", "rev-parse", "--verify", ref)
	if commit != "" {
		gotTree, _ := draftGit(ctx, pwd, "", "", "rev-parse", commit+"^{tree}")
		parent, _ := draftGit(ctx, pwd, "", "", "rev-parse", commit+"^")
		if gotTree != tree || parent != changes.BaseSHA {
			return nil, fmt.Errorf("delivery branch %s already exists with different contents. Inspect it on the device before retrying", branch)
		}
	} else {
		commit, err = draftGit(ctx, pwd, index, req.Title+"\n\n"+req.Body, "commit-tree", tree, "-p", changes.BaseSHA)
		if err != nil {
			return nil, errors.New("could not create the reviewed commit. Configure Git user.name and user.email on the device and retry")
		}
		if _, err = draftGit(ctx, pwd, "", "", "update-ref", ref, commit, ""); err != nil {
			return nil, fmt.Errorf("could not reserve delivery branch %s. Check that branch on the device and retry", branch)
		}
	}
	if err = provider.push(ctx, pwd, repo.URL, ref); err != nil {
		return nil, fmt.Errorf("the commit is preserved on local branch %s, but push did not complete. Check origin credentials, push URL and branch permissions on the device, then retry. No force push was attempted", branch)
	}
	prURL, err := provider.create(ctx, pwd, repo.URL, repo.DefaultBranchRef.Name, branch, req.Title, req.Body)
	if err != nil {
		prURL, _ = provider.find(ctx, pwd, repo.URL, branch)
	}
	if prURL == "" || !validDraftURL(repo.URL, prURL) {
		return nil, fmt.Errorf("branch %s was pushed, but a draft pull request could not be confirmed. Check GitHub CLI access and retry; the existing branch will be reused", branch)
	}
	return &draftPRResult{URL: prURL, Branch: branch, Commit: commit, Base: repo.DefaultBranchRef.Name}, nil
}
func validDraftURL(repo, pr string) bool {
	prefix := strings.TrimSuffix(repo, "/") + "/pull/"
	if !strings.HasPrefix(pr, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(pr, prefix)
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func draftRemoteURL(remote string) (string, error) {
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "ssh") {
			parsed.Scheme = "https"
			parsed.User = nil
			parsed.RawQuery = ""
			parsed.Fragment = ""
			return strings.TrimSuffix(parsed.String(), ".git"), nil
		}
	} else if colon := strings.Index(remote, ":"); colon > 0 {
		host := remote[:colon]
		if at := strings.LastIndex(host, "@"); at >= 0 {
			host = host[at+1:]
		}
		if !strings.ContainsAny(host, "/\\ ") {
			return "https://" + host + "/" + strings.TrimSuffix(strings.TrimPrefix(remote[colon+1:], "/"), ".git"), nil
		}
	}
	return "", errors.New("draft pull requests require a GitHub or GitHub Enterprise origin over HTTPS or SSH. Configure the repository origin on the device and retry")
}

func (githubDraftProvider) push(ctx context.Context, pwd, repo, ref string) error {
	pushURL, err := draftGit(ctx, pwd, "", "", "remote", "get-url", "--push", "origin")
	if err != nil {
		return errors.New("origin push URL is unavailable")
	}
	target, err := draftRemoteURL(pushURL)
	if err != nil || strings.TrimSuffix(target, "/") != strings.TrimSuffix(repo, "/") {
		return errors.New("origin push URL differs from the reviewed repository")
	}
	_, err = draftGit(ctx, pwd, "", "", "push", "origin", ref+":"+ref)
	return err
}

func previewSessionDraft(ctx context.Context, sid, pwd string, provider draftProvider) (*sessionChanges, draftRepository, error) {
	var empty draftRepository
	remote, err := draftGit(ctx, pwd, "", "", "remote", "get-url", "origin")
	if err != nil {
		return nil, empty, errors.New("configure the origin remote on the device before creating a pull request")
	}
	safeRemote, err := draftRemoteURL(remote)
	if err != nil {
		return nil, empty, err
	}
	repo, err := provider.repository(ctx, pwd, safeRemote)
	if err != nil {
		return nil, repo, err
	}
	base := repo.DefaultBranchRef.Target.OID
	if len(base) != 40 {
		return nil, repo, errors.New("could not verify the repository's base commit. Refresh and retry")
	}
	if _, err = draftGit(ctx, pwd, "", "", "cat-file", "-e", base+"^{commit}"); err != nil {
		if _, err = draftGit(ctx, pwd, "", "", "fetch", "--no-tags", "origin", "refs/heads/"+repo.DefaultBranchRef.Name); err != nil {
			return nil, repo, errors.New("could not fetch the pull request base branch. Check origin credentials on the device and retry")
		}
	}
	changes, err := inspectSessionChangesFromBase(ctx, sid, pwd, base)
	if err != nil {
		return nil, repo, err
	}
	changes.BaseBranch = repo.DefaultBranchRef.Name
	changes.RemoteURL = repo.URL
	return changes, repo, nil
}
func (s *Server) handleSessionDraftPreview(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	if sid == "" || sid == "new" {
		writeJSON(w, 400, map[string]string{"error": "An existing session is required."})
		return
	}
	if eng := s.resolveEngine(sid); eng != nil && eng.env != nil && eng.env.IsRemote() {
		writeJSON(w, 409, map[string]string{"error": "Open this SSH or Docker workspace on its host to create a pull request."})
		return
	}
	pwd, err := s.workspacePwdForTask(sid)
	if err != nil || pwd == "" || !filepath.IsAbs(pwd) {
		writeJSON(w, 404, map[string]string{"error": "Session workspace not found. Reopen it on the device."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	changes, _, err := previewSessionDraft(ctx, sid, pwd, githubDraftProvider{})
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, changes)
}
