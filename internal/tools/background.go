package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/coding/internal/config"
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
	ID      string
	Command string
	Status  BgTaskStatus
	Output  string
	Started time.Time
	Ended   time.Time
}

// BgNotification is a completion notification queued for injection.
type BgNotification struct {
	TaskID  string
	Command string
	Status  BgTaskStatus
	Output  string
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
}

// NewBackgroundManager creates a new background task manager.
func NewBackgroundManager(env *Env) *BackgroundManager {
	return &BackgroundManager{
		tasks: make(map[string]*BgTask),
		env:   env,
	}
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

	go bm.execute(ctx, task)

	return taskID
}

func (bm *BackgroundManager) execute(ctx context.Context, task *BgTask) {
	timeout := 5 * time.Minute
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
	// Truncate output to keep notifications lean.
	result := output.String()
	if len(result) > 2000 {
		result = result[:2000] + "\n... (truncated)"
	}

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

	bm.notifications = append(bm.notifications, BgNotification{
		TaskID:  task.ID,
		Command: task.Command,
		Status:  task.Status,
		Output:  result,
	})

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

// GetTask returns the current state of a task.
func (bm *BackgroundManager) GetTask(id string) *BgTask {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.tasks[id]
}

// ListTasks returns all tasks.
func (bm *BackgroundManager) ListTasks() []*BgTask {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	result := make([]*BgTask, 0, len(bm.tasks))
	for _, t := range bm.tasks {
		result = append(result, t)
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

// --- background_run tool ---

type bgRunInput struct {
	Command string `json:"command"`
}

func (e *Env) NewBackgroundRunTool(bm *BackgroundManager) tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "background_run",
		Desc: "Run a shell command in the background. Returns immediately with a task ID. " +
			"Use for long-running commands (npm install, go test, docker build, etc.) so you can keep working. " +
			"Check results later with check_background.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"command": {
				Type: schema.String, Desc: "The shell command to run in the background.", Required: true,
			},
		}),
	}
	return &bgRunTool{env: e, bm: bm, info: info}
}

type bgRunTool struct {
	env  *Env
	bm   *BackgroundManager
	info *schema.ToolInfo
}

func (t *bgRunTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *bgRunTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input bgRunInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}
	if input.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	taskID := t.bm.Run(ctx, input.Command)
	return fmt.Sprintf("Background task %s started: %s", taskID, input.Command), nil
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
	sb.WriteString(fmt.Sprintf("Task %s: %s\n", t.ID, t.Status))
	sb.WriteString(fmt.Sprintf("Command: %s\n", t.Command))
	sb.WriteString(fmt.Sprintf("Started: %s\n", t.Started.Format("15:04:05")))
	if !t.Ended.IsZero() {
		sb.WriteString(fmt.Sprintf("Ended: %s (took %s)\n", t.Ended.Format("15:04:05"), t.Ended.Sub(t.Started).Round(time.Millisecond)))
	}
	if t.Output != "" {
		sb.WriteString(fmt.Sprintf("Output:\n%s\n", t.Output))
	}
	return sb.String()
}
