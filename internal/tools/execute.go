package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/config"
)

type ExecuteInput struct {
	Command    string `json:"command"`
	Timeout    int    `json:"timeout,omitempty"`    // milliseconds
	Background bool   `json:"background,omitempty"` // run in background
}

func (e *Env) NewExecuteTool(bm *BackgroundManager) tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "execute",
		Desc: "Executes a bash command. By default runs synchronously and returns output. " +
			"Set background=true to run in the background (returns immediately with a task ID). " +
			"Use background mode for long-running commands (npm install, go test, docker build, etc.) " +
			"so you can keep working. Check results later with check_background.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"command": {
				Type:     schema.String,
				Desc:     "The command to execute.",
				Required: true,
			},
			"timeout": {
				Type:     schema.Integer,
				Desc:     "Optional timeout in milliseconds (max 600000ms / 10 minutes). Default is 120000ms (2 minutes). Ignored when background=true.",
				Required: false,
			},
			"background": {
				Type:     schema.Boolean,
				Desc:     "If true, run the command in the background and return immediately with a task ID. Default is false.",
				Required: false,
			},
		}),
	}

	return &executeTool{env: e, bm: bm, info: info}
}

type executeTool struct {
	env  *Env
	bm   *BackgroundManager
	info *schema.ToolInfo
}

func (et *executeTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return et.info, nil
}

func (et *executeTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var input ExecuteInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	if input.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	// Background mode: delegate to BackgroundManager and return immediately.
	if input.Background && et.bm != nil {
		taskID := et.bm.Run(ctx, input.Command)
		return fmt.Sprintf("Background task %s started: %s\nUse check_background to check status.", taskID, input.Command), nil
	}

	timeout := 120000 // 2 min default
	if input.Timeout > 0 {
		timeout = input.Timeout
		if timeout > 600000 {
			timeout = 600000
		}
	}

	config.Logger().Printf("[execute] running (timeout=%dms): %s", timeout, input.Command)
	start := time.Now()
	stdout, stderr, err := et.env.Exec.Exec(ctx, input.Command, et.env.pwd, time.Duration(timeout)*time.Millisecond)
	config.Logger().Printf("[execute] finished in %v, err=%v", time.Since(start), err)

	var result strings.Builder
	if stdout != "" {
		result.WriteString("STDOUT:\n")
		result.WriteString(stdout)
	}
	if stderr != "" {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString("STDERR:\n")
		result.WriteString(stderr)
	}

	if err != nil {
		return result.String(), fmt.Errorf("command failed: %w", err)
	}
	if result.Len() == 0 {
		return "Command executed successfully (no output)", nil
	}

	return result.String(), nil
}
