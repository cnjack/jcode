package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
)

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		dir = home
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type folderItem struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	var folders []folderItem
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip hidden folders
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		folders = append(folders, folderItem{
			Name: e.Name(),
			Path: filepath.Join(abs, e.Name()),
		})
	}
	if folders == nil {
		folders = []folderItem{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"current": abs,
		"folders": folders,
	})
}

// handleValidatePaths reports which of the given local paths no longer exist (or
// are not directories). The web UI keeps its workspace list in localStorage and
// can't stat the disk itself, so it calls this to prune dead workspaces from the
// picker instead of letting the user click one and hit "path does not exist".
// Callers send local paths only; ssh:// labels can't be stat'd here and would be
// wrongly reported missing, so they must be filtered out client-side.
func (s *Server) handleValidatePaths(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	missing := []string{}
	for _, p := range req.Paths {
		if p == "" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			// Only a confirmed not-exist means the workspace is gone. Transient
			// errors (permission, NFS hiccup) are inconclusive — keep the path
			// rather than silently dropping a still-valid workspace from the picker.
			if os.IsNotExist(err) {
				missing = append(missing, p)
			}
			continue
		}
		if !info.IsDir() {
			missing = append(missing, p)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"missing": missing})
}

func (s *Server) handleSwitchProject(w http.ResponseWriter, r *http.Request) {
	// No running gate: "switch project" builds a NEW independent engine and leaves
	// the previous task running in the background — switching to another task while
	// one is chatting is the whole point of concurrent tasks.
	if s.newEngine == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "project switching is not supported",
		})
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	// Validate path exists and is a directory.
	info, err := os.Stat(req.Path)
	if err != nil || !info.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path does not exist or is not a directory"})
		return
	}

	// Snapshot the outgoing task once, build the new engine BEFORE tearing down its
	// PTYs — a failed build must not kill the current task's terminals.
	prevTaskID, curMode := "", ""
	if cur := s.activeEngine(); cur != nil {
		prevTaskID, curMode = cur.taskID, cur.curMode()
	}

	// "Switch project" = build a fresh engine rooted at the new path and make it
	// active. This replaces in-place env mutation, so no other live task's
	// execution context is disturbed.
	eng, err := s.buildLocalEngine("", req.Path, curMode)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("failed to switch project: %v", err),
		})
		return
	}
	s.ptyMgr.closeForTask(prevTaskID) // outgoing task's PTYs only
	s.setActiveEngine(eng)

	// Reset todos for the (now empty) active task view.
	if eng.todoStore != nil {
		eng.todoStore.Update(nil)
	}

	// Broadcast project change to clients.
	s.wsBroker.Broadcast(WSEvent{
		Type: "project_switched",
		Data: map[string]string{
			"pwd": req.Path,
		},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"pwd":    req.Path,
	})
}

func (s *Server) handleGetApprovalMode(w http.ResponseWriter, r *http.Request) {
	autoApprove := false
	if eng := s.activeEngine(); eng != nil && eng.approvalState != nil {
		autoApprove = eng.approvalState.GetMode() == handler.ModeAuto
	}
	writeJSON(w, http.StatusOK, map[string]any{"auto_approve": autoApprove})
}

func (s *Server) handleSetApprovalMode(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()
	if eng == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no active task"})
		return
	}
	// No running gate: the rebuild is emu-safe and applies next turn, consistent
	// with the "Allow all" approval path which also flips full_access mid-run.
	var req struct {
		AutoApprove bool `json:"auto_approve"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	// Legacy endpoint: auto-approve now maps onto the unified mode (Full access vs
	// Approval). Both are non-plan, so rebuild to the full tool set for consistency.
	sm := mode.Approval
	if req.AutoApprove {
		sm = mode.FullAccess
	}
	// Rebuild first; abort the toggle if the rebuild fails (don't desync the
	// reported mode from the live agent).
	var newAg *adk.ChatModelAgent
	if eng.rebuildForMode != nil {
		eng.rebuildMu.Lock()
		ag, err := eng.rebuildForMode(false)
		if err != nil {
			eng.rebuildMu.Unlock()
			config.Logger().Printf("[web] approval mode agent rebuild error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to set approval mode"})
			return
		}
		newAg = ag
	}
	if eng.approvalState != nil {
		eng.approvalState.SetSessionMode(sm)
	}
	eng.applyModeSwitch(sm.String(), newAg)
	if eng.rebuildForMode != nil {
		eng.rebuildMu.Unlock()
	}
	// Persist as the default startup mode so the preference survives restarts —
	// resolveStartupMode reads cfg.DefaultMode. cfgMu serializes the config RMW.
	s.cfgMu.Lock()
	if s.cfg != nil {
		s.cfg.DefaultMode = sm.String()
		if err := config.SaveConfig(s.cfg); err != nil {
			config.Logger().Printf("[web] approval mode save config failed: %v", err)
		}
	}
	s.cfgMu.Unlock()

	s.wsBroker.Broadcast(WSEvent{
		Type:   "approval_mode_changed",
		TaskID: eng.taskID,
		Data:   map[string]any{"auto_approve": req.AutoApprove},
	})
	// Also emit the unified mode event so updated clients keep their selector synced.
	s.wsBroker.Broadcast(WSEvent{Type: "mode_changed", TaskID: eng.taskID, Data: map[string]string{"mode": sm.String()}})
	writeJSON(w, http.StatusOK, map[string]any{"auto_approve": req.AutoApprove})
}
