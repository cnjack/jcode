package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ---------------------------------------------------------------------------
// task_list
// ---------------------------------------------------------------------------

type taskListInput struct {
	StatusFilter string `json:"status_filter"`
}

type taskListEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AgentType string `json:"agent_type"`
	Model     string `json:"model,omitempty"`
	Status    string `json:"status"`
	Started   string `json:"started"`
	Ended     string `json:"ended,omitempty"`
	Error     string `json:"error,omitempty"`
}

// NewTaskListTool creates the task_list tool.
func NewTaskListTool(mgr *SubagentTaskManager) tool.InvokableTool {
	return &taskListTool{mgr: mgr}
}

type taskListTool struct {
	mgr *SubagentTaskManager
}

func (t *taskListTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "task_list",
		Desc: "List subagent tasks. Optionally filter by status: pending, running, completed, failed, stopped.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"status_filter": {
				Type: schema.String,
				Desc: "Filter by status (optional). One of: pending, running, completed, failed, stopped.",
			},
		}),
	}, nil
}

func (t *taskListTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input taskListInput
	if argumentsInJSON != "" {
		_ = json.Unmarshal([]byte(argumentsInJSON), &input)
	}
	tasks := t.mgr.List(SubagentTaskStatus(input.StatusFilter))
	entries := make([]taskListEntry, 0, len(tasks))
	for _, tk := range tasks {
		e := taskListEntry{
			ID:        tk.ID,
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
	ID        string `json:"id"`
	Name      string `json:"name"`
	AgentType string `json:"agent_type"`
	Model     string `json:"model,omitempty"`
	Status    string `json:"status"`
	Depth     int    `json:"depth"`
	ParentID  string `json:"parent_id,omitempty"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	Started   string `json:"started"`
	Ended     string `json:"ended,omitempty"`
}

// NewTaskGetTool creates the task_get tool.
func NewTaskGetTool(mgr *SubagentTaskManager) tool.InvokableTool {
	return &taskGetTool{mgr: mgr}
}

type taskGetTool struct {
	mgr *SubagentTaskManager
}

func (t *taskGetTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "task_get",
		Desc: "Get detailed information about a specific subagent task by its ID.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"task_id": {
				Type: schema.String, Desc: "The task ID (e.g. bg_subagent_1)", Required: true,
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
	tk, err := t.mgr.Get(input.TaskID)
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	out := taskGetOutput{
		ID:        tk.ID,
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

// ---------------------------------------------------------------------------
// task_stop
// ---------------------------------------------------------------------------

type taskStopInput struct {
	TaskID string `json:"task_id"`
}

// NewTaskStopTool creates the task_stop tool.
func NewTaskStopTool(mgr *SubagentTaskManager) tool.InvokableTool {
	return &taskStopTool{mgr: mgr}
}

type taskStopTool struct {
	mgr *SubagentTaskManager
}

func (t *taskStopTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "task_stop",
		Desc: "Stop a running background subagent task by its ID.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"task_id": {
				Type: schema.String, Desc: "The task ID to stop", Required: true,
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
	if err := t.mgr.Stop(input.TaskID); err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}
	return fmt.Sprintf("task %s stopped", input.TaskID), nil
}
