package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
)

// In-memory cap for a background task's output (head+tail, tail-biased so a
// long build/test log keeps its trailing errors); the untruncated output is in
// the task's log file on disk.
const (
	bgOutputHeadBytes = 1500
	bgOutputTailBytes = 2500
)

// --- BackgroundManager ---

// BgTaskStatus represents the state of a background task.
type BgTaskStatus string

const (
	BgStatusRunning BgTaskStatus = "running"
	BgStatusDone    BgTaskStatus = "done"
	BgStatusFailed  BgTaskStatus = "failed"
	BgStatusTimeout BgTaskStatus = "timeout"
)

// BgTask is a single background task.
type BgTask struct {
	ID          string
	Command     string
	Description string
	Status      BgTaskStatus
	Output      string
	LogPath     string // full-output log on the local disk ("" if unavailable)
	Started     time.Time
	Ended       time.Time
	Timeout     time.Duration // 0 means use default (5 minutes)
}

// BgNotification is a completion notification queued for injection.
type BgNotification struct {
	TaskID  string
	Command string
	Status  BgTaskStatus
	Output  string
	LogPath string
}

// BgNotifier is called on background task lifecycle events.
type BgNotifier func(taskID, command, status string)

// BackgroundManager manages background task execution and notifications.
type BackgroundManager struct {
	mu            sync.Mutex
	tasks         map[string]*BgTask
	notifications []BgNotification
	nextID        int
	env           *Env
	notifier      BgNotifier
	storage       *StorageManager
}

// NewBackgroundManager creates a new background task manager.
func NewBackgroundManager(env *Env) *BackgroundManager {
	return &BackgroundManager{
		tasks: make(map[string]*BgTask),
		env:   env,
	}
}

// SetStorage sets the optional StorageManager for TaskLog integration.
func (bm *BackgroundManager) SetStorage(s *StorageManager) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.storage = s
}

// SetNotifier sets the callback for TUI notifications.
func (bm *BackgroundManager) SetNotifier(n BgNotifier) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.notifier = n
}

// Run starts a command in the background and returns immediately.
func (bm *BackgroundManager) Run(ctx context.Context, command string) string {
	bm.mu.Lock()
	bm.nextID++
	taskID := fmt.Sprintf("bg_%d", bm.nextID)
	task := &BgTask{
		ID:      taskID,
		Command: command,
		Status:  BgStatusRunning,
		Started: time.Now(),
	}
	bm.tasks[taskID] = task
	bm.mu.Unlock()

	config.Logger().Printf("[background] started task %s: %s", taskID, command)

	// Notify TUI of task start.
	bm.mu.Lock()
	notify := bm.notifier
	bm.mu.Unlock()
	if notify != nil {
		notify(taskID, command, string(BgStatusRunning))
	}

	// Detach from parent context so background tasks survive after the
	// caller's request context is cancelled.
	go bm.execute(context.WithoutCancel(ctx), task)

	return taskID
}

func (bm *BackgroundManager) execute(ctx context.Context, task *BgTask) {
	// Spill the full output to a log file on disk. The tasks dir comes from
	// the StorageManager when one was wired via SetStorage, otherwise
	// ~/.jcode/tasks — ConfigDir always resolves (it falls back to the OS temp
	// dir), so the log is available unconditionally.
	bm.mu.Lock()
	storage := bm.storage
	bm.mu.Unlock()

	logDir := filepath.Join(config.ConfigDir(), "tasks")
	if storage != nil {
		logDir = storage.TasksDir()
	}
	taskLog, err := NewTaskLog(logDir, task.ID)
	if err != nil {
		config.Logger().Printf("[background] failed to create task log for %s: %v", task.ID, err)
		taskLog = nil
	}
	if taskLog != nil {
		defer func() { _ = taskLog.Close() }()
		// The log lives on the local disk. Only advertise the path to the
		// model when the executor is local too — over SSH/Docker the command
		// runs remotely and read/grep on a local path would fail.
		if !bm.env.IsRemote() {
			bm.mu.Lock()
			task.LogPath = taskLog.Path()
			bm.mu.Unlock()
		} else {
			config.Logger().Printf("[background] task %s full log (local file): %s", task.ID, taskLog.Path())
		}
	}

	timeout := task.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	stdout, stderr, err := bm.env.Exec.Exec(ctx, task.Command, bm.env.pwd, timeout)

	var output strings.Builder
	if stdout != "" {
		output.WriteString(stdout)
	}
	if stderr != "" {
		if output.Len() > 0 {
			output.WriteString("\n")
		}
		output.WriteString(stderr)
	}

	// Write full output to TaskLog on disk.
	if taskLog != nil {
		taskLog.Write([]byte(output.String())) //nolint:errcheck
	}

	// Truncate the in-memory output (head+tail) to keep notifications lean;
	// the trailing errors of a long build/test log survive, and the full
	// output remains in the task log.
	result, _, _ := truncateHeadTail(output.String(), bgOutputHeadBytes, bgOutputTailBytes)

	bm.mu.Lock()

	task.Ended = time.Now()
	task.Output = result

	if err != nil {
		if strings.Contains(err.Error(), "timed out") {
			task.Status = BgStatusTimeout
		} else {
			task.Status = BgStatusFailed
		}
	} else {
		task.Status = BgStatusDone
	}

	// Cap notifications to prevent unbounded memory growth.
	const maxNotifications = 100
	if len(bm.notifications) < maxNotifications {
		bm.notifications = append(bm.notifications, BgNotification{
			TaskID:  task.ID,
			Command: task.Command,
			Status:  task.Status,
			Output:  result,
			LogPath: task.LogPath,
		})
	}

	notify := bm.notifier
	bm.mu.Unlock()

	// Notify TUI of task completion (outside lock).
	if notify != nil {
		notify(task.ID, task.Command, string(task.Status))
	}

	config.Logger().Printf("[background] task %s finished: %s", task.ID, task.Status)
}

// DrainNotifications returns and clears all pending completion notifications.
func (bm *BackgroundManager) DrainNotifications() []BgNotification {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	if len(bm.notifications) == 0 {
		return nil
	}
	notifs := bm.notifications
	bm.notifications = nil
	return notifs
}

// GetTask returns a snapshot of the task's current state.
func (bm *BackgroundManager) GetTask(id string) *BgTask {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	t := bm.tasks[id]
	if t == nil {
		return nil
	}
	copy := *t
	return &copy
}

// ListTasks returns snapshots of all tasks.
func (bm *BackgroundManager) ListTasks() []*BgTask {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	result := make([]*BgTask, 0, len(bm.tasks))
	for _, t := range bm.tasks {
		copy := *t
		result = append(result, &copy)
	}
	return result
}

// RunningCount returns the number of currently running tasks.
func (bm *BackgroundManager) RunningCount() int {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	count := 0
	for _, t := range bm.tasks {
		if t.Status == BgStatusRunning {
			count++
		}
	}
	return count
}

// --- check_background tool ---

type bgCheckInput struct {
	TaskID string `json:"task_id"`
}

func (e *Env) NewCheckBackgroundTool(bm *BackgroundManager) tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "check_background",
		Desc: "Check the status of background tasks. If task_id is provided, shows that task. " +
			"Otherwise lists all tasks with their status.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"task_id": {
				Type: schema.String, Desc: "Optional task ID to check. Omit to list all.", Required: false,
			},
		}),
	}
	return &bgCheckTool{bm: bm, info: info}
}

type bgCheckTool struct {
	bm   *BackgroundManager
	info *schema.ToolInfo
}

func (t *bgCheckTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *bgCheckTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input bgCheckInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	if input.TaskID != "" {
		task := t.bm.GetTask(input.TaskID)
		if task == nil {
			return fmt.Sprintf("No task found with ID %q", input.TaskID), nil
		}
		return formatTask(task), nil
	}

	// List all tasks
	tasks := t.bm.ListTasks()
	if len(tasks) == 0 {
		return "No background tasks.", nil
	}
	var sb strings.Builder
	for _, task := range tasks {
		sb.WriteString(formatTask(task))
		sb.WriteString("\n---\n")
	}
	return sb.String(), nil
}

func formatTask(t *BgTask) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Task %s: %s\n", t.ID, t.Status)
	fmt.Fprintf(&sb, "Command: %s\n", t.Command)
	fmt.Fprintf(&sb, "Started: %s\n", t.Started.Format("15:04:05"))
	if !t.Ended.IsZero() {
		fmt.Fprintf(&sb, "Ended: %s (took %s)\n", t.Ended.Format("15:04:05"), t.Ended.Sub(t.Started).Round(time.Millisecond))
	}
	if t.Output != "" {
		fmt.Fprintf(&sb, "Output:\n%s\n", t.Output)
	}
	// Point at the full log whether or not the in-memory output was truncated,
	// so the model can read/grep the complete output on demand.
	if t.LogPath != "" {
		fmt.Fprintf(&sb, "Full log: %s\n", t.LogPath)
	}
	return sb.String()
}
