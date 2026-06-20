package web

import (
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strings"
)

// handleGitBranches lists local branch names (most-recently-committed first)
// plus the current branch, for the composer's branch picker. A non-git
// directory or any git error yields an empty list rather than an error,
// mirroring handleWorkspace so the UI degrades gracefully.
func (s *Server) handleGitBranches(w http.ResponseWriter, r *http.Request) {
	listCmd := exec.CommandContext(r.Context(), "git", "for-each-ref",
		"--format=%(refname:short)", "--sort=-committerdate", "refs/heads")
	listCmd.Dir = s.pwd
	out, err := listCmd.Output()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"current": "", "branches": []string{}})
		return
	}

	// `branch --show-current` reports the unborn branch of a fresh repo (e.g.
	// "main"), where `rev-parse --abbrev-ref HEAD` would just say "HEAD".
	curCmd := exec.CommandContext(r.Context(), "git", "branch", "--show-current")
	curCmd.Dir = s.pwd
	curOut, _ := curCmd.Output()
	current := strings.TrimSpace(string(curOut))

	branches := make([]string, 0, 16)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if b := strings.TrimSpace(line); b != "" {
			branches = append(branches, b)
		}
	}
	// A freshly initialised repo's current branch has no ref yet, so
	// for-each-ref omits it — surface it anyway so the picker shows the branch
	// you're actually on (with its checkmark).
	if current != "" {
		found := false
		for _, b := range branches {
			if b == current {
				found = true
				break
			}
		}
		if !found {
			branches = append([]string{current}, branches...)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current":  current,
		"branches": branches,
	})
}

// handleGitCheckout switches to an existing branch, or creates and checks out a
// new branch when create=true. It refuses while the agent is running (a branch
// switch rewrites the working tree under a live task) and surfaces git's own
// error verbatim (e.g. "Your local changes would be overwritten") rather than
// forcing a destructive checkout — the user decides how to resolve a dirty tree.
func (s *Server) handleGitCheckout(w http.ResponseWriter, r *http.Request) {
	if s.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "agent is running — stop it before switching branch",
		})
		return
	}

	var req struct {
		Branch string `json:"branch"`
		Create bool   `json:"create"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "branch is required"})
		return
	}

	args := []string{"checkout"}
	if req.Create {
		args = append(args, "-b")
	}
	args = append(args, branch)

	cmd := exec.CommandContext(r.Context(), "git", args...)
	cmd.Dir = s.pwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		writeJSON(w, http.StatusConflict, map[string]string{"error": msg})
		return
	}

	curCmd := exec.CommandContext(r.Context(), "git", "branch", "--show-current")
	curCmd.Dir = s.pwd
	curOut, _ := curCmd.Output()
	writeJSON(w, http.StatusOK, map[string]any{"branch": strings.TrimSpace(string(curOut))})
}
