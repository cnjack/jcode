package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/cnjack/jcode/internal/tasks"
	"github.com/cnjack/jcode/internal/tools"
)

// agentTasksStore lazily opens the server-level agent-task registry for the
// bootstrap project. Per-engine registries (remote tasks) stay keyed to their
// own project path; this one serves the server's primary project.
func (s *Server) agentTasksStore() *tasks.Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentTasks != nil {
		return s.agentTasks
	}
	store, err := tasks.OpenDefault(s.Engine.pwd)
	if err != nil {
		// Cached as nil on failure: handlers report "unavailable" instead of
		// retrying the filesystem on every request.
		return nil
	}
	s.agentTasks = store
	return store
}

func (s *Server) requireAgentTasks(w http.ResponseWriter) *tasks.Store {
	store := s.agentTasksStore()
	if store == nil {
		http.Error(w, "agent task registry unavailable", http.StatusServiceUnavailable)
		return nil
	}
	return store
}

// handleListAgentTasks GET /api/agent-tasks?status=running
func (s *Server) handleListAgentTasks(w http.ResponseWriter, r *http.Request) {
	store := s.requireAgentTasks(w)
	if store == nil {
		return
	}
	recs, err := store.List(tasks.Status(r.URL.Query().Get("status")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, recs)
}

type agentTaskCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleCreateAgentTask POST /api/agent-tasks
func (s *Server) handleCreateAgentTask(w http.ResponseWriter, r *http.Request) {
	store := s.requireAgentTasks(w)
	if store == nil {
		return
	}
	var req agentTaskCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	sessionID := ""
	if s.Engine != nil && s.Engine.recorder != nil {
		sessionID = s.Engine.recorder.UUID()
	}
	rec, err := store.Create(tasks.CreateInput{
		Name:        req.Name,
		Description: req.Description,
		Kind:        tasks.KindWorkItem,
		SessionID:   sessionID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

// handleGetAgentTask GET /api/agent-tasks/{ref}
func (s *Server) handleGetAgentTask(w http.ResponseWriter, r *http.Request) {
	store := s.requireAgentTasks(w)
	if store == nil {
		return
	}
	ref := r.PathValue("ref")
	rec, err := store.Resolve(ref)
	if err != nil {
		writeAgentTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

type agentTaskMessageRequest struct {
	Message        string `json:"message"`
	IdempotencyKey string `json:"idempotency_key"`
}

// handleMessageAgentTask POST /api/agent-tasks/{ref}/messages
func (s *Server) handleMessageAgentTask(w http.ResponseWriter, r *http.Request) {
	store := s.requireAgentTasks(w)
	if store == nil {
		return
	}
	ref := r.PathValue("ref")
	var req agentTaskMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}
	sessionID := "web"
	if s.Engine != nil && s.Engine.recorder != nil {
		sessionID = s.Engine.recorder.UUID()
	}
	rec, err := store.Resolve(ref)
	if err != nil {
		writeAgentTaskError(w, err)
		return
	}
	updated, err := store.Message(rec.Ref, sessionID, "user", req.Message, req.IdempotencyKey)
	if err != nil {
		writeAgentTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleStopAgentTask POST /api/agent-tasks/{ref}/stop
func (s *Server) handleStopAgentTask(w http.ResponseWriter, r *http.Request) {
	store := s.requireAgentTasks(w)
	if store == nil {
		return
	}
	ref := r.PathValue("ref")
	rec, err := store.Resolve(ref)
	if err != nil {
		writeAgentTaskError(w, err)
		return
	}

	// A live task can only be cancelled by the engine whose manager runs it.
	if mgr := s.findManagerForRef(rec.Ref); mgr != nil {
		if err := mgr.Stop(rec.Ref); err == nil {
			writeJSON(w, http.StatusOK, map[string]string{"ref": rec.Ref, "status": "stopped"})
			return
		}
	}

	switch rec.Status {
	case tasks.StatusRunning, tasks.StatusPending:
		if rec.Zombie {
			writeAgentTaskError(w, fmt.Errorf("task %s is no longer running (owning process exited)", rec.Ref))
			return
		}
		// Ownership conflict: the caller may see the task but cannot stop it.
		http.Error(w, fmt.Sprintf("task %s is %s in another session/process (owner pid %d on %s); stop it from that session",
			rec.Ref, rec.Status, rec.OwnerPID, rec.Hostname), http.StatusConflict)
	case tasks.StatusArchived:
		writeAgentTaskError(w, fmt.Errorf("%w: %s", tasks.ErrArchived, rec.Ref))
	default:
		writeAgentTaskError(w, fmt.Errorf("task %s is not running (status=%s)", rec.Ref, rec.Status))
	}
}

// handleArchiveAgentTask POST /api/agent-tasks/{ref}/archive
func (s *Server) handleArchiveAgentTask(w http.ResponseWriter, r *http.Request) {
	store := s.requireAgentTasks(w)
	if store == nil {
		return
	}
	ref := r.PathValue("ref")
	rec, err := store.Resolve(ref)
	if err != nil {
		writeAgentTaskError(w, err)
		return
	}
	archived, err := store.Archive(rec.Ref)
	if err != nil {
		writeAgentTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, archived)
}

// findManagerForRef scans live engines for the in-process manager that owns
// the task ref. Only that manager can cancel the goroutine.
func (s *Server) findManagerForRef(ref string) *tools.SubagentTaskManager {
	s.tasksMu.RLock()
	engines := make([]*Engine, 0, len(s.tasks)+1)
	for _, e := range s.tasks {
		engines = append(engines, e)
	}
	s.tasksMu.RUnlock()
	if s.Engine != nil {
		engines = append(engines, s.Engine)
	}
	for _, e := range engines {
		if e == nil || e.taskHub == nil || e.taskHub.Manager == nil {
			continue
		}
		if _, err := e.taskHub.Manager.FindByRef(ref); err == nil {
			return e.taskHub.Manager
		}
	}
	return nil
}

func writeAgentTaskError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tasks.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, tasks.ErrCrossProject):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, tasks.ErrArchived), errors.Is(err, tasks.ErrTerminal):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, tasks.ErrAmbiguous):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
