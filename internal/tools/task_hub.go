package tools

import (
	"github.com/cnjack/jcode/internal/tasks"
)

// TaskHub bundles the durable task registry (internal/tasks) with the
// in-process subagent task manager and the caller's session identity. It is
// the single dependency every task_* tool takes, so every transport (TUI,
// ACP, Web) exposes the exact same task reference model.
//
// A nil Store means "no persistent registry in this context" (tests, legacy
// paths): list/get/stop then fall back to the in-process manager and
// create/read/message return an explicit error instead of silently writing
// somewhere unexpected.
type TaskHub struct {
	Store   *tasks.Store
	Manager *SubagentTaskManager
	// SessionIDFn identifies the calling session stamped on records and
	// messages (cross-session attribution). May be nil.
	SessionIDFn func() string
}

// NewTaskHub builds a hub. store and manager may be nil.
func NewTaskHub(store *tasks.Store, manager *SubagentTaskManager, sessionIDFn func() string) *TaskHub {
	return &TaskHub{Store: store, Manager: manager, SessionIDFn: sessionIDFn}
}

// SessionID returns the calling session's UUID ("" when unknown).
func (h *TaskHub) SessionID() string {
	if h == nil || h.SessionIDFn == nil {
		return ""
	}
	return h.SessionIDFn()
}

// HasStore reports whether the persistent registry is available.
func (h *TaskHub) HasStore() bool { return h != nil && h.Store != nil }

func localToStatus(s SubagentTaskStatus) tasks.Status {
	switch s {
	case TaskStatusCompleted:
		return tasks.StatusCompleted
	case TaskStatusFailed:
		return tasks.StatusFailed
	case TaskStatusStopped:
		return tasks.StatusStopped
	case TaskStatusRunning:
		return tasks.StatusRunning
	case TaskStatusPending:
		return tasks.StatusPending
	default:
		return tasks.StatusCreated
	}
}
