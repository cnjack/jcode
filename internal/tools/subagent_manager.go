package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

// SubagentTaskStatus represents the lifecycle state of a subagent task.
type SubagentTaskStatus string

const (
	TaskStatusPending   SubagentTaskStatus = "pending"
	TaskStatusRunning   SubagentTaskStatus = "running"
	TaskStatusCompleted SubagentTaskStatus = "completed"
	TaskStatusFailed    SubagentTaskStatus = "failed"
	TaskStatusStopped   SubagentTaskStatus = "stopped"
)

// SubagentTask holds the state of a single subagent task.
type SubagentTask struct {
	ID        string
	Name      string
	AgentType string
	Model     string
	Status    SubagentTaskStatus
	Depth     int
	ParentID  string
	Output    string
	Error     string
	Started   time.Time
	Ended     time.Time
	Cancel    context.CancelFunc
}

// SubagentNotification is produced when a background task changes state.
type SubagentNotification struct {
	TaskID    string
	Name      string
	AgentType string
	Status    SubagentTaskStatus
	Summary   string
}

// SubagentTaskManager tracks synchronous and asynchronous subagent tasks.
type SubagentTaskManager struct {
	mu            sync.RWMutex
	tasks         map[string]*SubagentTask
	notifications []SubagentNotification
	nextID        int
	maxParallel   int
	maxCompleted  int
}

// NewSubagentTaskManager creates a task manager.
// maxParallel limits concurrently running background tasks (0 = unlimited).
// maxCompleted is the number of completed tasks retained before eviction.
func NewSubagentTaskManager(maxParallel, maxCompleted int) *SubagentTaskManager {
	return &SubagentTaskManager{
		tasks:        make(map[string]*SubagentTask),
		maxParallel:  maxParallel,
		maxCompleted: maxCompleted,
	}
}

// Submit runs or enqueues a task. When background is false the call blocks and
// returns the final result. When background is true the task is launched in a
// goroutine and the task ID is returned immediately.
func (m *SubagentTaskManager) Submit(
	ctx context.Context,
	task *SubagentTask,
	runFn func(ctx context.Context) (string, error),
	background bool,
) (taskID, result string, err error) {
	m.mu.Lock()
	m.nextID++
	task.ID = fmt.Sprintf("bg_subagent_%d", m.nextID)
	task.Status = TaskStatusPending
	task.Started = time.Now()
	m.tasks[task.ID] = task

	if background {
		running := m.runningCountLocked()
		if m.maxParallel > 0 && running >= m.maxParallel {
			task.Status = TaskStatusFailed
			task.Error = fmt.Sprintf("max parallel limit (%d) reached", m.maxParallel)
			task.Ended = time.Now()
			m.mu.Unlock()
			return task.ID, "", fmt.Errorf("%s", task.Error)
		}
	}
	m.mu.Unlock()

	if !background {
		return m.runSync(ctx, task, runFn)
	}
	return m.runAsync(ctx, task, runFn)
}

func (m *SubagentTaskManager) runSync(ctx context.Context, task *SubagentTask, runFn func(ctx context.Context) (string, error)) (string, string, error) {
	m.setStatus(task.ID, TaskStatusRunning)
	result, err := runFn(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	task.Ended = time.Now()
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err.Error()
	} else {
		task.Status = TaskStatusCompleted
		task.Output = result
	}
	m.evictOldest()
	return task.ID, result, err
}

func (m *SubagentTaskManager) runAsync(ctx context.Context, task *SubagentTask, runFn func(ctx context.Context) (string, error)) (string, string, error) {
	childCtx, cancel := context.WithCancel(ctx)
	task.Cancel = cancel
	m.setStatus(task.ID, TaskStatusRunning)
	taskID := task.ID

	go func() {
		defer cancel()
		// A panic escaping runFn would crash the whole process (background
		// goroutines have no upstream recover). Mark the task failed and
		// notify, mirroring the error path below.
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			task.Ended = time.Now()
			task.Status = TaskStatusFailed
			task.Error = fmt.Sprintf("panic: %v", r)
			m.notifications = append(m.notifications, SubagentNotification{
				TaskID:    task.ID,
				Name:      task.Name,
				AgentType: task.AgentType,
				Status:    task.Status,
				Summary:   "error: " + task.Error,
			})
			m.evictOldest()
			config.Logger().Printf("[task-manager] async task %s panicked: %v", task.ID, r)
		}()
		result, err := runFn(childCtx)
		m.mu.Lock()
		defer m.mu.Unlock()
		task.Ended = time.Now()
		if err != nil {
			if task.Status == TaskStatusStopped {
				// Already marked stopped via Stop().
				task.Error = "stopped by user"
			} else {
				task.Status = TaskStatusFailed
				task.Error = err.Error()
			}
		} else {
			if task.Status != TaskStatusStopped {
				task.Status = TaskStatusCompleted
				task.Output = result
			}
		}
		summary := task.Output
		if task.Error != "" {
			summary = "error: " + task.Error
		}
		m.notifications = append(m.notifications, SubagentNotification{
			TaskID:    task.ID,
			Name:      task.Name,
			AgentType: task.AgentType,
			Status:    task.Status,
			Summary:   summary,
		})
		m.evictOldest()
		config.Logger().Printf("[task-manager] async task %s finished status=%s", task.ID, task.Status)
	}()

	return taskID, "", nil
}

// Get returns a copy-safe snapshot of a task by ID.
func (m *SubagentTaskManager) Get(taskID string) (*SubagentTask, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task %q not found", taskID)
	}
	// Return a shallow copy so callers cannot mutate internal state.
	cp := *t
	cp.Cancel = nil
	return &cp, nil
}

// List returns tasks matching the given status filter.
// An empty statusFilter returns all tasks.
func (m *SubagentTaskManager) List(statusFilter SubagentTaskStatus) []*SubagentTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*SubagentTask
	for _, t := range m.tasks {
		if statusFilter != "" && t.Status != statusFilter {
			continue
		}
		cp := *t
		cp.Cancel = nil
		out = append(out, &cp)
	}
	return out
}

// Stop cancels a running background task.
func (m *SubagentTaskManager) Stop(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}
	if t.Status != TaskStatusRunning && t.Status != TaskStatusPending {
		return fmt.Errorf("task %q is not running (status=%s)", taskID, t.Status)
	}
	t.Status = TaskStatusStopped
	t.Ended = time.Now()
	if t.Cancel != nil {
		t.Cancel()
	}
	m.notifications = append(m.notifications, SubagentNotification{
		TaskID:    t.ID,
		Name:      t.Name,
		AgentType: t.AgentType,
		Status:    TaskStatusStopped,
		Summary:   "stopped by user",
	})
	config.Logger().Printf("[task-manager] stopped task %s", taskID)
	return nil
}

// DrainNotifications returns and clears all pending notifications.
func (m *SubagentTaskManager) DrainNotifications() []SubagentNotification {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.notifications
	m.notifications = nil
	return out
}

// RunningCount returns the number of currently running tasks.
func (m *SubagentTaskManager) RunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.runningCountLocked()
}

func (m *SubagentTaskManager) runningCountLocked() int {
	n := 0
	for _, t := range m.tasks {
		if t.Status == TaskStatusRunning {
			n++
		}
	}
	return n
}

func (m *SubagentTaskManager) setStatus(taskID string, status SubagentTaskStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskID]; ok {
		t.Status = status
	}
}

// evictOldest removes the oldest completed/failed/stopped tasks when maxCompleted
// is exceeded. Must be called with m.mu held.
func (m *SubagentTaskManager) evictOldest() {
	if m.maxCompleted <= 0 {
		return
	}
	var finished []*SubagentTask
	for _, t := range m.tasks {
		if t.Status == TaskStatusCompleted || t.Status == TaskStatusFailed || t.Status == TaskStatusStopped {
			finished = append(finished, t)
		}
	}
	if len(finished) <= m.maxCompleted {
		return
	}
	// Sort by ended time, evict oldest.
	for len(finished) > m.maxCompleted {
		oldest := finished[0]
		for _, t := range finished[1:] {
			if t.Ended.Before(oldest.Ended) {
				oldest = t
			}
		}
		delete(m.tasks, oldest.ID)
		// Remove from finished slice.
		for i, t := range finished {
			if t.ID == oldest.ID {
				finished = append(finished[:i], finished[i+1:]...)
				break
			}
		}
	}
}
