package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/tasks"
)

// Task reference help shared by the tool descriptions.
const taskRefHelp = "A task reference is task_<16 hex> (from task_list/task_create/subagent background output); a unique task name or a bare <16 hex> suffix also resolves."

// ---------------------------------------------------------------------------
// task_list
// ---------------------------------------------------------------------------

type taskListInput struct {
	StatusFilter string `json:"status_filter"`
}

type taskListEntry struct {
	ID        string `json:"id"`
	Ref       string `json:"ref,omitempty"`
	Name      string `json:"name"`
	Kind      string `json:"kind,omitempty"`
	AgentType string `json:"agent_type"`
	Model     string `json:"model,omitempty"`
	Status    string `json:"status"`
	Session   string `json:"session,omitempty"`
	Origin    string `json:"origin,omitempty"`
	Started   string `json:"started"`
	Ended     string `json:"ended,omitempty"`
	Error     string `json:"error,omitempty"`
}

// NewTaskListTool creates the task_list tool. With a persistent registry the
// list spans sessions and processes (project-scoped); without one it lists
// the in-process subagent tasks (legacy behavior).
func NewTaskListTool(hub *TaskHub) tool.InvokableTool {
	return &taskListTool{hub: hub}
}

type taskListTool struct {
	hub *TaskHub
}

func (t *taskListTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "task_list",
		Desc: "List agent tasks. With the persistent registry this spans sessions and restarts (scoped to this project); optionally filter by status: created, pending, running, completed, failed, stopped.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"status_filter": {
				Type: schema.String,
				Desc: "Filter by status (optional). One of: created, pending, running, completed, failed, stopped.",
			},
		}),
	}, nil
}

func (t *taskListTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input taskListInput
	if argumentsInJSON != "" {
		_ = json.Unmarshal([]byte(argumentsInJSON), &input)
	}
	if t.hub.HasStore() {
		recs, err := t.hub.Store.List(tasks.Status(input.StatusFilter))
		if err != nil {
			return fmt.Sprintf("error: %v", err), nil
		}
		entries := make([]taskListEntry, 0, len(recs))
		for _, rec := range recs {
			e := taskListEntry{
				Ref:       rec.Ref,
				Name:      rec.Name,
				Kind:      rec.Kind,
				AgentType: rec.AgentType,
				Model:     rec.Model,
				Status:    string(rec.Status),
				Session:   rec.SessionID,
				Origin:    rec.Origin,
				Started:   rec.CreatedAt.Format(time.RFC3339),
				Error:     rec.Error,
			}
			if !rec.EndedAt.IsZero() {
				e.Ended = rec.EndedAt.Format(time.RFC3339)
			}
			entries = append(entries, e)
		}
		data, _ := json.MarshalIndent(entries, "", "  ")
		return string(data), nil
	}
	if t.hub.Manager == nil {
		return "[]", nil
	}
	// Legacy in-process listing (no persistent registry configured).
	list := t.hub.Manager.List(SubagentTaskStatus(input.StatusFilter))
	entries := make([]taskListEntry, 0, len(list))
	for _, tk := range list {
		e := taskListEntry{
			ID:        tk.ID,
			Ref:       tk.Ref,
			Name:      tk.Name,
			AgentType: tk.AgentType,
			Model:     tk.Model,
			Status:    string(tk.Status),
			Started:   tk.Started.Format(time.RFC3339),
			Error:     tk.Error,
		}
		if !tk.Ended.IsZero() {
			e.Ended = tk.Ended.Format(time.RFC3339)
		}
		entries = append(entries, e)
	}
	data, _ := json.MarshalIndent(entries, "", "  ")
	return string(data), nil
}

// ---------------------------------------------------------------------------
// task_get
// ---------------------------------------------------------------------------

type taskGetInput struct {
	TaskID string `json:"task_id"`
}

type taskGetOutput struct {
	ID        string        `json:"id"`
	Ref       string        `json:"ref,omitempty"`
	Name      string        `json:"name"`
	Kind      string        `json:"kind,omitempty"`
	AgentType string        `json:"agent_type"`
	Model     string        `json:"model,omitempty"`
	Status    string        `json:"status"`
	Depth     int           `json:"depth"`
	ParentID  string        `json:"parent_id,omitempty"`
	Session   string        `json:"session,omitempty"`
	Output    string        `json:"output,omitempty"`
	Error     string        `json:"error,omitempty"`
	Started   string        `json:"started"`
	Ended     string        `json:"ended,omitempty"`
	Messages  []tasks.Event `json:"messages,omitempty"`
}

// NewTaskGetTool creates the task_get tool.
func NewTaskGetTool(hub *TaskHub) tool.InvokableTool {
	return &taskGetTool{hub: hub}
}

type taskGetTool struct {
	hub *TaskHub
}

func (t *taskGetTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "task_get",
		Desc: "Get detailed information about a task: status, output, error and message timeline. " + taskRefHelp,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"task_id": {
				Type: schema.String, Desc: "The task reference or task ID", Required: true,
			},
		}),
	}, nil
}

func (t *taskGetTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input taskGetInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return fmt.Sprintf("failed to parse input: %v", err), nil
	}
	if input.TaskID == "" {
		return "task_id is required", nil
	}

	// Persistent registry first: cross-session reads.
	if t.hub.HasStore() {
		if rec, err := t.hub.Store.Resolve(input.TaskID); err == nil {
			out := taskGetOutput{
				Ref:       rec.Ref,
				Name:      rec.Name,
				Kind:      rec.Kind,
				AgentType: rec.AgentType,
				Model:     rec.Model,
				Status:    string(rec.Status),
				Session:   rec.SessionID,
				Output:    rec.Output,
				Error:     rec.Error,
				Started:   rec.CreatedAt.Format(time.RFC3339),
				Messages:  rec.Timeline,
			}
			if !rec.EndedAt.IsZero() {
				out.Ended = rec.EndedAt.Format(time.RFC3339)
			}
			data, _ := json.MarshalIndent(out, "", "  ")
			return string(data), nil
		}
	}
	// Fall back to (or prefer, for bg_subagent_N ids) the live manager.
	if t.hub.Manager != nil {
		if tk, err := t.hub.Manager.Resolve(input.TaskID); err == nil {
			out := taskGetOutput{
				ID:        tk.ID,
				Ref:       tk.Ref,
				Name:      tk.Name,
				AgentType: tk.AgentType,
				Model:     tk.Model,
				Status:    string(tk.Status),
				Depth:     tk.Depth,
				ParentID:  tk.ParentID,
				Output:    tk.Output,
				Error:     tk.Error,
				Started:   tk.Started.Format(time.RFC3339),
			}
			if !tk.Ended.IsZero() {
				out.Ended = tk.Ended.Format(time.RFC3339)
			}
			data, _ := json.MarshalIndent(out, "", "  ")
			return string(data), nil
		}
	}
	return fmt.Sprintf("error: task %q not found in this project's registry or this session's live tasks", input.TaskID), nil
}

// ---------------------------------------------------------------------------
// task_read
// ---------------------------------------------------------------------------

type taskReadInput struct {
	TaskRef string `json:"task_ref"`
	// OmitTimeline suppresses the message timeline (default: include).
	OmitTimeline bool `json:"omit_timeline"`
}

// NewTaskReadTool creates the task_read tool: read any task in this project's
// persistent registry, including tasks created by previous sessions.
func NewTaskReadTool(hub *TaskHub) tool.InvokableTool {
	return &taskReadTool{hub: hub}
}

type taskReadTool struct {
	hub *TaskHub
}

func (t *taskReadTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "task_read",
		Desc: "Read a task from the persistent registry — including tasks created by earlier sessions — and return its status, output, error and message timeline. " + taskRefHelp,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"task_ref": {
				Type: schema.String, Desc: "The task reference (or unique task name)", Required: true,
			},
			"omit_timeline": {
				Type: schema.Boolean, Desc: "Set true to omit the message timeline (default: included)", Required: false,
			},
		}),
	}, nil
}

func (t *taskReadTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input taskReadInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return fmt.Sprintf("failed to parse input: %v", err), nil
	}
	if input.TaskRef == "" {
		return "task_ref is required", nil
	}
	if !t.hub.HasStore() {
		return "error: persistent task registry is not available in this context; use task_get for live in-session tasks", nil
	}
	rec, err := t.hub.Store.Resolve(input.TaskRef)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "task %s (%q)\n", rec.Ref, rec.Name)
	fmt.Fprintf(&b, "kind: %s  status: %s  origin: %s  session: %s\n", rec.Kind, rec.Status, rec.Origin, rec.SessionID)
	if rec.AgentType != "" {
		fmt.Fprintf(&b, "agent_type: %s  model: %s\n", rec.AgentType, rec.Model)
	}
	fmt.Fprintf(&b, "created: %s", rec.CreatedAt.Format(time.RFC3339))
	if !rec.EndedAt.IsZero() {
		fmt.Fprintf(&b, "  ended: %s", rec.EndedAt.Format(time.RFC3339))
	}
	b.WriteString("\n")
	if rec.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", rec.Description)
	}
	if rec.Error != "" {
		fmt.Fprintf(&b, "error: %s\n", rec.Error)
	}
	if rec.Output != "" {
		fmt.Fprintf(&b, "output:\n%s\n", rec.Output)
	}
	if input.OmitTimeline {
		return b.String(), nil
	}
	if len(rec.Timeline) == 0 {
		b.WriteString("timeline: (no messages)\n")
		return b.String(), nil
	}
	b.WriteString("timeline:\n")
	for _, ev := range rec.Timeline {
		role := ev.FromRole
		if role == "" {
			role = "unknown"
		}
		fmt.Fprintf(&b, "  [%s] %s: %s\n", ev.Time.Format(time.RFC3339), role, ev.Body)
	}
	return b.String(), nil
}

// ---------------------------------------------------------------------------
// task_create
// ---------------------------------------------------------------------------

type taskCreateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// NewTaskCreateTool creates the task_create tool: mint a durable,
// cross-session work item that any later session can read, message, or
// reference via @mentions.
func NewTaskCreateTool(hub *TaskHub) tool.InvokableTool {
	return &taskCreateTool{hub: hub}
}

type taskCreateTool struct {
	hub *TaskHub
}

func (t *taskCreateTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "task_create",
		Desc: "Create a durable task (work item) that persists across sessions in this project and can be referenced later by its task_<hex> ref or @name mention. For a task that runs immediately, use the subagent tool with run_in_background=true instead.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"name": {
				Type: schema.String, Desc: "Short unique-ish name (1-3 words); used for @name mentions", Required: true,
			},
			"description": {
				Type: schema.String, Desc: "What the task should accomplish", Required: false,
			},
		}),
	}, nil
}

func (t *taskCreateTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input taskCreateInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return fmt.Sprintf("failed to parse input: %v", err), nil
	}
	if !t.hub.HasStore() {
		return "error: persistent task registry is not available in this context", nil
	}
	rec, err := t.hub.Store.Create(tasks.CreateInput{
		Name:        input.Name,
		Description: input.Description,
		Kind:        tasks.KindWorkItem,
		SessionID:   t.hub.SessionID(),
	})
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	return fmt.Sprintf("Task created: %s\nReference: %s (mention as @%s or @%s)\nUse task_message to send follow-ups and task_read to review it later.",
		rec.Name, rec.Ref, rec.Ref, rec.Name), nil
}

// ---------------------------------------------------------------------------
// task_message
// ---------------------------------------------------------------------------

type taskMessageInput struct {
	TaskRef        string `json:"task_ref"`
	Message        string `json:"message"`
	IdempotencyKey string `json:"idempotency_key"`
}

// NewTaskMessageTool creates the task_message tool: send a follow-up message
// to a task's timeline. Exactly-once per idempotency key.
func NewTaskMessageTool(hub *TaskHub) tool.InvokableTool {
	return &taskMessageTool{hub: hub}
}

type taskMessageTool struct {
	hub *TaskHub
}

func (t *taskMessageTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "task_message",
		Desc: "Send a message to a task's timeline (readable by the task's owner session via task_read/task_get). Retries with the same idempotency_key deliver exactly once. Errors clearly when the task is archived, already finished, or belongs to another project. " + taskRefHelp,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"task_ref": {
				Type: schema.String, Desc: "The task reference or unique task name", Required: true,
			},
			"message": {
				Type: schema.String, Desc: "Message body to append to the task timeline", Required: true,
			},
			"idempotency_key": {
				Type: schema.String, Desc: "Optional key making retried deliveries exactly-once", Required: false,
			},
		}),
	}, nil
}

func (t *taskMessageTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input taskMessageInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return fmt.Sprintf("failed to parse input: %v", err), nil
	}
	if input.TaskRef == "" || strings.TrimSpace(input.Message) == "" {
		return "task_ref and message are required", nil
	}
	if !t.hub.HasStore() {
		return "error: persistent task registry is not available in this context", nil
	}
	rec, err := t.hub.Store.Resolve(input.TaskRef)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	updated, err := t.hub.Store.Message(rec.Ref, t.hub.SessionID(), "agent", input.Message, input.IdempotencyKey)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	delivery := "queued in the task timeline"
	if t.hub.Manager != nil {
		if live, liveErr := t.hub.Manager.FindByRef(rec.Ref); liveErr == nil &&
			(live.Status == TaskStatusRunning || live.Status == TaskStatusPending) {
			delivery = "queued in the timeline of the live task running in this session"
		}
	}
	return fmt.Sprintf("Message delivered to task %s (%s) — %s. Timeline now has %d message(s).",
		rec.Ref, updated.Status, delivery, len(updated.Timeline)), nil
}

// ---------------------------------------------------------------------------
// task_stop
// ---------------------------------------------------------------------------

type taskStopInput struct {
	TaskID string `json:"task_id"`
}

// NewTaskStopTool creates the task_stop tool.
func NewTaskStopTool(hub *TaskHub) tool.InvokableTool {
	return &taskStopTool{hub: hub}
}

type taskStopTool struct {
	hub *TaskHub
}

func (t *taskStopTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "task_stop",
		Desc: "Stop a running background task. Accepts the local task ID (bg_subagent_N) or a durable task reference. Tasks running in another session or process are refused with an explicit error. " + taskRefHelp,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"task_id": {
				Type: schema.String, Desc: "The task ID or task reference to stop", Required: true,
			},
		}),
	}, nil
}

func (t *taskStopTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input taskStopInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return fmt.Sprintf("failed to parse input: %v", err), nil
	}
	if input.TaskID == "" {
		return "task_id is required", nil
	}

	// Live in-process task (by ID or ref) wins: only this process can cancel it.
	if t.hub.Manager != nil {
		if err := t.hub.Manager.Stop(input.TaskID); err == nil {
			ref := input.TaskID
			if tasks.ValidateRef(ref) {
				// already a ref
			} else if live, lerr := t.hub.Manager.Resolve(input.TaskID); lerr == nil && live.Ref != "" {
				ref = live.Ref
			}
			return fmt.Sprintf("task %s stopped", ref), nil
		}
	}
	// Not stoppable here; explain why using the registry when available.
	if t.hub.HasStore() {
		if rec, err := t.hub.Store.Resolve(input.TaskID); err == nil {
			switch rec.Status {
			case tasks.StatusRunning, tasks.StatusPending:
				if rec.Zombie {
					return fmt.Sprintf("error: task %s is no longer running (owning process exited)", rec.Ref), nil
				}
				return fmt.Sprintf("error: task %s is %s in another session/process (owner pid %d on %s); stop it from that session",
					rec.Ref, rec.Status, rec.OwnerPID, rec.Hostname), nil
			case tasks.StatusArchived:
				return fmt.Sprintf("error: task %s is archived", rec.Ref), nil
			default:
				return fmt.Sprintf("error: task %s is not running (status=%s)", rec.Ref, rec.Status), nil
			}
		}
	}
	return fmt.Sprintf("error: task %q not found in this session or project registry", input.TaskID), nil
}
