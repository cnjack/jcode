package web

import (
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strings"

	utils "github.com/cnjack/jcode/internal/util"
)

// handleGitBranches lists local branch names (most-recently-committed first)
// plus the current branch, for the composer's branch picker. A non-git
// directory or any git error yields an empty list rather than an error,
// mirroring handleWorkspace so the UI degrades gracefully.
func (s *Server) handleGitBranches(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	eng := s.resolveEngine(taskID)
	if taskID != "" && eng == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if eng == nil {
		writeJSON(w, http.StatusOK, map[string]any{"current": "", "branches": []string{}})
		return
	}
	listCmd := exec.CommandContext(r.Context(), "git", "for-each-ref",
		"--format=%(refname:short)", "--sort=-committerdate", "refs/heads")
	listCmd.Dir = eng.pwd
	listCmd.Env = utils.ScrubbedGitEnv()
	out, err := listCmd.Output()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"current": "", "branches": []string{}})
		return
	}

	// `branch --show-current` reports the unborn branch of a fresh repo (e.g.
	// "main"), where `rev-parse --abbrev-ref HEAD` would just say "HEAD".
	curCmd := exec.CommandContext(r.Context(), "git", "branch", "--show-current")
	curCmd.Dir = eng.pwd
	curCmd.Env = utils.ScrubbedGitEnv()
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
// switch rewrites the working tree under a live task).
//
// When a plain switch would clobber uncommitted work, git aborts
// non-destructively. Rather than surfacing that raw error we report it as a
// recoverable result (HTTP 200, blocked:true, plus the at-risk files) so the UI
// can ask the user how to proceed instead of dead-ending on an error. The
// client then retries with an explicit strategy:
//   - "stash": `git stash push -u` the changes first, then switch (recoverable
//     via `git stash pop`).
//   - "force": `git checkout -f`, discarding the local changes.
//
// Any failure after a strategy was chosen is genuine and returned as an error.
func (s *Server) handleGitCheckout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Branch   string `json:"branch"`
		Create   bool   `json:"create"`
		Strategy string `json:"strategy"` // "" (plain) | "stash" | "force"
		TaskID   string `json:"task_id,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Resolve the target engine once so the running guard and every git command
	// operate on the same task even if another tab changes the server foreground.
	eng := s.resolveEngine(req.TaskID)
	if eng == nil {
		status := http.StatusServiceUnavailable
		if req.TaskID != "" {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "task not found"})
		return
	}
	if eng.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "agent is running — stop it before switching branch",
		})
		return
	}
	dir := eng.pwd

	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "branch is required"})
		return
	}
	// A leading dash would be parsed by git as a flag rather than a ref — e.g.
	// branch "-f" turns `git checkout <branch>` into `git checkout -f`, silently
	// force-switching and discarding all uncommitted work (and returning 200).
	// Reject it outright; valid git refs never begin with "-" (git
	// check-ref-format forbids it), so this rejects nothing legitimate. Note a
	// "--" separator is NOT a fix here: `git checkout -- <ref>` treats <ref> as a
	// pathspec and breaks branch switching.
	if strings.HasPrefix(branch, "-") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid branch name"})
		return
	}

	// Force stable English git output so the block detection below doesn't depend
	// on the host locale. We present our own UI copy, so this C-locale text is
	// only used internally, never shown to the user.
	env := append(utils.ScrubbedGitEnv(), "LC_ALL=C", "LANG=C")

	// "stash" strategy: tuck the working changes (including untracked files) away
	// before switching so nothing is lost. A genuine stash failure is fatal.
	stashed := false
	if req.Strategy == "stash" {
		stashCmd := exec.CommandContext(r.Context(), "git", "stash", "push", "-u",
			"-m", "jcode: auto-stash before switching to "+branch)
		stashCmd.Dir = dir
		stashCmd.Env = env
		stashOut, stashErr := stashCmd.CombinedOutput()
		if stashErr != nil {
			msg := strings.TrimSpace(string(stashOut))
			if msg == "" {
				msg = stashErr.Error()
			}
			writeJSON(w, http.StatusConflict, map[string]string{"error": msg})
			return
		}
		stashed = !strings.Contains(string(stashOut), "No local changes to save")
	}

	args := []string{"checkout"}
	if req.Strategy == "force" {
		args = append(args, "-f")
	}
	if req.Create {
		args = append(args, "-b")
	}
	args = append(args, branch)

	cmd := exec.CommandContext(r.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		// A plain switch aborted by uncommitted work is recoverable: report it as
		// such (the working tree is untouched) so the UI can offer stash/discard.
		// `kind` tells the UI whether the at-risk files are tracked modifications
		// (force = discard edits) or untracked files (force = irrecoverable
		// deletion), so it can pick safe recovery options and accurate copy.
		if req.Strategy == "" && checkoutBlockedByLocalChanges(msg) {
			writeJSON(w, http.StatusOK, map[string]any{
				"branch":  "",
				"blocked": true,
				"kind":    blockKind(msg),
				"message": msg,
				"files":   parseOverwriteFiles(msg),
			})
			return
		}
		// The checkout failed after we stashed the user's work (stash strategy).
		// Restore the pre-switch tree so nothing is silently orphaned in the stash.
		// After `stash push -u` the tree is clean, so pop normally applies cleanly;
		// if it doesn't, name the stash so the user can recover it by hand.
		if stashed {
			popCmd := exec.CommandContext(r.Context(), "git", "stash", "pop")
			popCmd.Dir = dir
			popCmd.Env = env
			if popOut, popErr := popCmd.CombinedOutput(); popErr != nil {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error": msg + "\n\nYour changes were stashed but could not be restored " +
						"automatically; recover them with `git stash pop` (see `git stash list`): " +
						strings.TrimSpace(string(popOut)),
				})
				return
			}
		}
		writeJSON(w, http.StatusConflict, map[string]string{"error": msg})
		return
	}

	curCmd := exec.CommandContext(r.Context(), "git", "branch", "--show-current")
	curCmd.Dir = dir
	curCmd.Env = utils.ScrubbedGitEnv()
	curOut, _ := curCmd.Output()
	writeJSON(w, http.StatusOK, map[string]any{
		"branch":  strings.TrimSpace(string(curOut)),
		"stashed": stashed,
	})
}

// checkoutBlockedByLocalChanges reports whether a failed `git checkout` aborted
// because uncommitted work (modified or untracked files) would be overwritten —
// the recoverable case the UI resolves by stashing or discarding. Matched
// against C-locale git output.
func checkoutBlockedByLocalChanges(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "would be overwritten by checkout") ||
		strings.Contains(m, "would be overwritten by merge") ||
		strings.Contains(m, "please commit your changes or stash them")
}

// blockKind classifies a blocked checkout so the UI can offer safe recovery
// options and accurate copy. "untracked" means new files would be clobbered
// (force = irrecoverable deletion); "tracked" means committed-file
// modifications (force = discard edits). Both remain recoverable via
// `git stash push -u`. Matched against C-locale git output. The untracked
// check is first and specific so a mixed message is classified as the more
// dangerous case.
func blockKind(msg string) string {
	if strings.Contains(strings.ToLower(msg), "untracked working tree files would be overwritten") {
		return "untracked"
	}
	return "tracked"
}

// parseOverwriteFiles pulls the tab-indented paths git lists between the
// "would be overwritten" header and the trailing "Please commit…/Aborting"
// lines, so the UI can show exactly which files are at risk.
func parseOverwriteFiles(msg string) []string {
	files := make([]string, 0, 8)
	for _, line := range strings.Split(msg, "\n") {
		if strings.HasPrefix(line, "\t") {
			if f := strings.TrimSpace(line); f != "" {
				files = append(files, f)
			}
		}
	}
	return files
}
