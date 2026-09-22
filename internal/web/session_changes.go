package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	utils "github.com/cnjack/jcode/internal/util"
)

const changesLimit = 512 * 1024
const changeFileLimit = 100

// sessionChanges describes current workspace state, not authorship by the agent.
// It includes staged, unstaged and untracked files relative to HEAD.
type sessionChanges struct {
	SessionID  string               `json:"session_id"`
	Workspace  string               `json:"workspace"`
	Repository string               `json:"repository"`
	Branch     string               `json:"branch"`
	Head       string               `json:"head"`
	RemoteURL  string               `json:"remote_url"`
	BaseSHA    string               `json:"base_sha"`
	BaseBranch string               `json:"base_branch"`
	Revision   string               `json:"revision"`
	Files      []sessionChangedFile `json:"files"`
	Truncated  bool                 `json:"truncated"`
}
type sessionChangedFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Patch     string `json:"patch"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
}

type boundedChangeOutput struct {
	bytes.Buffer
	limit int
}

func (b *boundedChangeOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		n, _ := b.Buffer.Write(p[:b.limit-b.Len()])
		return n, errors.New("workspace diff exceeds output limit")
	}
	return b.Buffer.Write(p)
}
func changesGit(ctx context.Context, pwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false"}, args...)...)
	cmd.Dir = pwd
	cmd.Env = utils.ScrubbedGitEnv()
	out := &boundedChangeOutput{limit: changesLimit}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return out.String(), err
}

func (s *Server) handleSessionChanges(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	if sid == "" || sid == "new" {
		writeJSON(w, 400, map[string]string{"error": "an existing session id is required"})
		return
	}
	if eng := s.resolveEngine(sid); eng != nil && eng.env != nil && eng.env.IsRemote() {
		writeJSON(w, 409, map[string]string{"error": "This session uses SSH or Docker. Inspect changes in that workspace from jcode on the device."})
		return
	}
	pwd, err := s.workspacePwdForTask(sid)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "Could not resolve the session workspace. Reopen this session on the device and retry."})
		return
	}
	if pwd == "" {
		writeJSON(w, 404, map[string]string{"error": "Session workspace not found. Reopen this session on the device."})
		return
	}
	if !filepath.IsAbs(pwd) {
		writeJSON(w, 409, map[string]string{"error": "The session workspace is not a local directory. Open the session on the device."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := inspectSessionChanges(ctx, sid, pwd)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func inspectSessionChanges(ctx context.Context, sid, pwd string) (*sessionChanges, error) {
	return inspectSessionChangesFromBase(ctx, sid, pwd, "")
}
func inspectSessionChangesFromBase(ctx context.Context, sid, pwd, base string) (*sessionChanges, error) {
	root, err := changesGit(ctx, pwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, errors.New("this session workspace is unavailable or is not a Git repository. Open its directory on the device and retry")
	}
	root = strings.TrimSpace(root)
	// Only expose the selected workspace. A nested session directory must not
	// silently expose sibling files from an enclosing Git worktree.
	resolvedPwd, resolveErr := filepath.EvalSymlinks(pwd)
	if resolveErr != nil {
		return nil, errors.New("the session workspace is unavailable. Reopen it on the device")
	}
	rel, err := filepath.Rel(root, resolvedPwd)
	if err != nil || rel != "." {
		return nil, errors.New("open a session at the Git repository root to inspect all workspace changes")
	}
	status, err := changesGit(ctx, pwd, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return nil, errors.New("could not read Git status. Check the workspace on the device and retry")
	}
	head, _ := changesGit(ctx, pwd, "rev-parse", "--verify", "HEAD")
	if base == "" {
		base = strings.TrimSpace(head)
	}
	if base != "" && base != strings.TrimSpace(head) {
		names, diffErr := changesGit(ctx, pwd, "diff", "--name-only", "-z", "--no-renames", base, "--")
		if diffErr != nil {
			return nil, errors.New("could not inspect changes against the reviewed base commit. Fetch the base branch on the device and retry")
		}
		known := map[string]bool{}
		for _, row := range strings.Split(status, "\x00") {
			if len(row) >= 4 {
				known[row[3:]] = true
			}
		}
		for _, name := range strings.Split(names, "\x00") {
			if name != "" && !known[name] {
				status += " M " + name + "\x00"
			}
		}
	}
	branch, _ := changesGit(ctx, pwd, "branch", "--show-current")
	remote, _ := changesGit(ctx, pwd, "remote", "get-url", "origin")
	result := &sessionChanges{SessionID: sid, Workspace: pwd, Repository: filepath.Base(root), Head: strings.TrimSpace(head), Branch: strings.TrimSpace(branch), RemoteURL: redactGitRemote(strings.TrimSpace(remote)), Files: []sessionChangedFile{}}
	result.BaseSHA = base
	hash := sha256.New()
	_, _ = io.WriteString(hash, head+"\x00"+base+"\x00"+status)
	remaining := changesLimit
	for _, row := range strings.Split(status, "\x00") {
		if len(row) < 4 {
			continue
		}
		if len(result.Files) == changeFileLimit || remaining <= 0 {
			result.Truncated = true
			break
		}
		file := sessionChangedFile{Path: row[3:], Status: strings.TrimSpace(row[:2])}
		if file.Status == "??" || result.Head == "" {
			file.Patch, file.Binary, file.Truncated, err = untrackedChange(ctx, pwd, file.Path, remaining)
		} else {
			file.Patch, err = changesGit(ctx, pwd, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", "--binary", base, "--", ":(literal)"+file.Path)
			file.Binary = strings.Contains(file.Patch, "Binary files ") || strings.Contains(file.Patch, "GIT binary patch")
		}
		if err != nil && len(file.Patch) == changesLimit {
			file.Truncated = true
			err = nil
		}
		if err != nil {
			return nil, fmt.Errorf("could not read changes for %s. Refresh after the workspace finishes changing", file.Path)
		}
		if file.Patch == "" {
			continue
		}
		if len(file.Patch) > remaining {
			file.Patch = file.Patch[:remaining]
			file.Truncated = true
		}
		remaining -= len(file.Patch)
		for _, line := range strings.Split(file.Patch, "\n") {
			if file.Binary {
				break
			}
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				file.Additions++
			}
			if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				file.Deletions++
			}
		}
		result.Truncated = result.Truncated || file.Truncated
		_, _ = io.WriteString(hash, file.Path+"\x00"+file.Patch+"\x00")
		result.Files = append(result.Files, file)
	}
	if result.Truncated {
		result.Revision = ""
	} else {
		result.Revision = hex.EncodeToString(hash.Sum(nil))
	}
	return result, nil
}

func untrackedChange(ctx context.Context, root, path string, limit int) (string, bool, bool, error) {
	if !withinWorkspace(root, filepath.Join(root, path)) {
		return "", false, false, errors.New("invalid workspace path")
	}
	patch, err := changesGit(ctx, root, "diff", "--no-index", "--binary", "--no-ext-diff", "--no-textconv", "--no-color", "--", os.DevNull, path)
	var exitErr *exec.ExitError
	if len(patch) == changesLimit {
		return patch, strings.Contains(patch, "GIT binary patch"), true, nil
	}
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		return "", false, false, err
	}
	truncated := len(patch) > limit
	if truncated {
		patch = patch[:limit]
	}
	return patch, strings.Contains(patch, "GIT binary patch"), truncated, nil
}

func redactGitRemote(remote string) string {
	if parsed, err := url.Parse(remote); err == nil && parsed.Host != "" {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	// SCP-style remotes contain a username, never expose embedded credentials.
	if at := strings.LastIndex(remote, "@"); at >= 0 {
		return remote[at+1:]
	}
	return remote
}
