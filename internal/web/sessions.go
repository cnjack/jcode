package web

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
)

// taskItem is the sidebar's view of a session: its persisted metadata plus a
// live-running flag, with created_at normalized from the on-disk start_time.
// Both the task list and the metadata-update endpoint return this exact shape so
// the web client can splice an updated task straight into its list without the
// field drifting (start_time vs created_at) that would blank created_at and
// scramble the recency sort.
type taskItem struct {
	UUID      string `json:"uuid"`
	Project   string `json:"project"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Title     string `json:"title,omitempty"`
	Pinned    bool   `json:"pinned"`
	Archived  bool   `json:"archived"`
	Unread    bool   `json:"unread"`
	Status    string `json:"status,omitempty"`
	Running   bool   `json:"running"`
}

func newTaskItem(m *session.SessionMeta, project string, running bool) taskItem {
	return taskItem{
		UUID:      m.UUID,
		Project:   project,
		CreatedAt: m.StartTime,
		UpdatedAt: m.UpdatedAt,
		Provider:  m.Provider,
		Model:     m.Model,
		Title:     m.Title,
		Pinned:    m.Pinned,
		Archived:  m.Archived,
		Unread:    m.Unread,
		Status:    m.Status,
		Running:   running,
	}
}

// isTaskRunning reports whether a live engine for this task id is mid-run.
func (s *Server) isTaskRunning(id string) bool {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()
	e := s.tasks[id]
	return e != nil && e.running.Load()
}

// handleListAllTasks returns every session across all projects (flat list,
// each tagged with its project path) so the web sidebar can render a
// Workspace > Project > Task tree without switching the active project.
func (s *Server) handleListAllTasks(w http.ResponseWriter, r *http.Request) {
	all, err := session.ListAllSessions()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Snapshot which task ids are currently running (live engines) so the sidebar
	// can show a running indicator even on a fresh page load.
	running := make(map[string]bool)
	s.tasksMu.RLock()
	for id, e := range s.tasks {
		if e != nil && e.running.Load() {
			running[id] = true
		}
	}
	s.tasksMu.RUnlock()

	items := make([]taskItem, 0)
	for project, metas := range all {
		for i := range metas {
			m := &metas[i]
			// Automation runs are surfaced on the Automations page ("Recent
			// runs"), not the main task list — exclude them here so a nightly
			// automation doesn't bury the sidebar.
			if m.AutomationID != "" {
				continue
			}
			items = append(items, newTaskItem(m, project, running[m.UUID]))
		}
	}
	writeJSON(w, http.StatusOK, items)
}

// projectItem is the sidebar's view of a project: its path plus the persisted
// last-activity timestamp. The timestamp lives at the project level (bumped on
// session create / turn start / turn end) and is deliberately NOT recomputed
// from the surviving sessions, so deleting a conversation never reorders the
// project list.
type projectItem struct {
	Path      string `json:"path"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// handleListProjects returns every project that has persisted metadata (last
// activity timestamp) so the sidebar can order project groups by a stable,
// delete-resistant timestamp instead of re-deriving it from child sessions.
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	meta, err := session.ListProjectMeta()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items := make([]projectItem, 0, len(meta))
	for path, pm := range meta {
		items = append(items, projectItem{Path: path, UpdatedAt: pm.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, items)
}

// handleUpdateTask applies a partial metadata update (pin/archive/unread/title)
// to a task by uuid across all projects.
func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Pinned   *bool   `json:"pinned"`
		Archived *bool   `json:"archived"`
		Unread   *bool   `json:"unread"`
		Title    *string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	meta, err := session.UpdateSessionMeta(id, func(m *session.SessionMeta) {
		if req.Pinned != nil {
			m.Pinned = *req.Pinned
		}
		if req.Archived != nil {
			m.Archived = *req.Archived
		}
		if req.Unread != nil {
			m.Unread = *req.Unread
		}
		if req.Title != nil {
			m.Title = *req.Title
		}
		// Deliberately do NOT bump UpdatedAt here. UpdatedAt is the "last activity"
		// key the sidebar sorts by, and activity means a real turn (a user prompt →
		// setTaskStatus on run start/end), not a metadata edit. Bumping it on
		// pin/archive/mark-read/rename made a task jump to the top of the recency
		// sort the instant it was opened (open marks it read), which is exactly the
		// reordering we don't want. Resuming/opening a session must not reorder it.
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if meta == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	// Echo the same normalized task shape the list endpoint returns (created_at,
	// running, …) — not the raw SessionMeta — so the client can splice the result
	// straight back into its task list without corrupting the recency sort.
	writeJSON(w, http.StatusOK, newTaskItem(meta, meta.Project, s.isTaskRunning(id)))
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	metas, err := session.ListSessions(s.activePwd())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type sessionItem struct {
		UUID      string `json:"uuid"`
		CreatedAt string `json:"created_at"`
		Provider  string `json:"provider"`
		Model     string `json:"model"`
		Title     string `json:"title,omitempty"`
	}

	items := make([]sessionItem, 0, len(metas))
	for _, m := range metas {
		items = append(items, sessionItem{
			UUID:      m.UUID,
			CreatedAt: m.StartTime,
			Provider:  m.Provider,
			Model:     m.Model,
			Title:     m.Title,
		})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entries, err := session.LoadSession(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session id is required"})
		return
	}
	// Refuse while the agent is mid-run. Cancelling + deleting a live task races
	// the recorder (file/index resurrection) and is a bad UX for an intentional
	// stop — the user should stop the run first, then delete.
	if eng := s.resolveEngine(id); eng != nil && eng.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent is currently running"})
		return
	}
	// Tear down the live engine for this task (if any) so leftover cancel state
	// is cleared and resources reclaimed. The active foreground engine is left
	// in place — but its recorder is reset so post-delete writes don't land in
	// the now-unlinked file (silent data loss).
	if eng := s.resolveEngine(id); eng != nil {
		eng.emu.Lock()
		cancel := eng.runCancel
		eng.emu.Unlock()
		if cancel != nil {
			cancel()
		}
		if eng != s.activeEngine() {
			s.deleteEngine(id)
		} else {
			// Not running (guarded above): safe to close the recorder now.
			eng.emu.Lock()
			if eng.recorder != nil && eng.recorder.UUID() == id {
				eng.recorder.Close()
				eng.recorder = nil
				eng.history = nil
			}
			eng.emu.Unlock()
		}
	}

	// Resolve the owning project across all projects: a task deleted from the
	// sidebar tree may not belong to the active project.
	if _, err := session.DeleteSessionByUUID(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleTruncateHistory(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()
	if eng == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no active task"})
		return
	}
	if eng.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent is currently running"})
		return
	}

	var req struct {
		// BeforeUserMessage: keep all history entries that come before the
		// Nth user message (0-indexed). Everything from that user message
		// onward is discarded. Pass 0 to clear everything.
		BeforeUserMessage int `json:"before_user_message"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	// Capture the recorder under eng.emu (same lock submitMessage uses) but do
	// file I/O outside the lock.
	eng.emu.Lock()
	rec := eng.recorder
	eng.emu.Unlock()
	sessionID := ""
	if rec != nil {
		sessionID = rec.UUID()
	}

	// Persist first — if the file rewrite fails we abort without touching
	// the in-memory history so state never diverges.
	if rec != nil {
		if err := rec.TruncateAtUserMessage(req.BeforeUserMessage); err != nil {
			config.Logger().Printf("[truncate] rewrite session file failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to truncate session file"})
			return
		}
	}

	// Now truncate in-memory history under eng.emu.
	eng.emu.Lock()
	truncAt := 0
	if req.BeforeUserMessage > 0 {
		userCount := 0
		truncAt = len(eng.history) // default: keep all
		for i, msg := range eng.history {
			if msg.Role == schema.User {
				if userCount == req.BeforeUserMessage {
					truncAt = i
					break
				}
				userCount++
			}
		}
	}
	if truncAt == 0 {
		eng.history = nil
	} else {
		eng.history = eng.history[:truncAt]
	}
	eng.emu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"session_id": sessionID,
	})
}

func (s *Server) handleNewSession(w http.ResponseWriter, r *http.Request) {
	// Parse optional resume session ID + project. Creating a task no longer
	// blocks on "is the agent running" — tasks run concurrently.
	var req struct {
		SessionID string `json:"session_id,omitempty"`
		Pwd       string `json:"pwd,omitempty"`
	}
	// The body is optional (empty = brand-new task → EOF), but a non-empty
	// malformed body should be rejected rather than creating a zero-value task.
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Already-live task: just focus it (do not disturb its run).
	if req.SessionID != "" {
		if eng := s.resolveEngine(req.SessionID); eng != nil {
			s.setActiveEngine(eng)
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "session_id": eng.taskID})
			return
		}
	}

	if s.newEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task creation is not supported"})
		return
	}

	// Each new/resumed task gets its OWN engine (env, agent, recorder, handler),
	// so it runs independently of every other task.
	pwd := req.Pwd
	if pwd == "" {
		if a := s.activeEngine(); a != nil {
			pwd = a.pwd
		}
	}
	eng, err := s.buildLocalEngine(req.SessionID, pwd, s.activeMode())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Resume: hydrate the fresh engine with the persisted conversation/todos/goal.
	if req.SessionID != "" {
		entries, lerr := session.LoadSession(req.SessionID)
		if lerr != nil {
			// Stale/nonexistent session id: don't silently register a phantom empty
			// engine under it — tear the just-built engine down and report not-found.
			s.deleteEngine(eng.taskID)
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		st := session.ReconstructState(entries)
		eng.emu.Lock()
		eng.history = st.History
		eng.emu.Unlock()
		if eng.todoStore != nil {
			items := make([]tools.TodoItem, len(st.Todos))
			for i, t := range st.Todos {
				items[i] = tools.TodoItem{ID: t.ID, Title: t.Title, Status: tools.TodoStatus(t.Status)}
			}
			eng.todoStore.Update(items)
		}
		if eng.env != nil && eng.env.GoalStore != nil {
			eng.env.GoalStore.RestoreFromSnapshot(st.Goal)
			if eng.handler != nil {
				eng.handler.Emit("goal_update", eng.env.GoalStore.Get())
			}
		}
	}

	s.setActiveEngine(eng)

	// Brand-new task: tell its view to start clean.
	if req.SessionID == "" {
		s.wsBroker.Broadcast(WSEvent{TaskID: eng.taskID, Type: "session_reset", Data: map[string]string{}})
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "session_id": eng.taskID})
}
