package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/automation"
)

type automationCreateInput struct {
	Name        string `json:"name"`
	Prompt      string `json:"prompt"`
	Cadence     string `json:"cadence"`      // hourly|daily|weekly|manual
	Hour        int    `json:"hour"`         // 0-23 (daily/weekly)
	Minute      int    `json:"minute"`       // 0-59
	Weekday     int    `json:"weekday"`      // 0=Sun..6=Sat (weekly)
	ProjectPath string `json:"project_path"` // defaults to the current working directory
}

// NewAutomationCreateTool creates the automation_create tool. The agent can
// PROPOSE an automation from natural language, but the automation is always
// created DISABLED with source="agent": only the user can arm it (enable it) on
// the Automations page. This human-in-the-loop gate means a prompt-injected
// agent can never silently stand up a recurring, unattended, auto-approving run.
func (e *Env) NewAutomationCreateTool() tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "automation_create",
		Desc: `Propose a new automation (a scheduled or manual agent task) for the user.

The automation is created DISABLED and will NOT run until the user reviews it and enables it on the Automations page — you cannot arm it yourself. Use this when the user asks to run something on a recurring schedule (e.g. "every morning summarize new issues") or to save a reusable manual task.

cadence must be one of: "hourly" (uses minute), "daily" (uses hour+minute), "weekly" (uses weekday+hour+minute), or "manual" (no schedule). It runs in the current project by default.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"name":         {Type: schema.String, Desc: "Short human-readable name for the automation.", Required: true},
			"prompt":       {Type: schema.String, Desc: "The instruction the agent will run each time the automation fires.", Required: true},
			"cadence":      {Type: schema.String, Desc: `One of "hourly", "daily", "weekly", "manual".`, Required: true},
			"hour":         {Type: schema.Integer, Desc: "Hour of day 0-23 (daily/weekly)."},
			"minute":       {Type: schema.Integer, Desc: "Minute of hour 0-59."},
			"weekday":      {Type: schema.Integer, Desc: "Day of week 0=Sunday..6=Saturday (weekly)."},
			"project_path": {Type: schema.String, Desc: "Absolute local project path; defaults to the current working directory."},
		}),
	}
	return &automationCreateTool{env: e, info: info}
}

type automationCreateTool struct {
	env  *Env
	info *schema.ToolInfo
}

func (t *automationCreateTool) Info(_ context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *automationCreateTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var in automationCreateInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("failed to parse automation_create input: %w", err)
	}

	trigger, err := triggerFromCadence(in)
	if err != nil {
		return "", err
	}
	project := in.ProjectPath
	if project == "" {
		project = t.env.Pwd()
	}

	// Write through the server's live store so the new automation is immediately
	// visible to the REST API and scheduler (a throwaway store would only touch
	// disk, leaving the server's in-memory cache stale). Fall back to a fresh
	// store in contexts with no live server (CLI/ACP).
	store := t.env.AutomationStore
	if store == nil {
		var err error
		if store, err = automation.NewStore(); err != nil {
			return "", fmt.Errorf("automation store unavailable: %w", err)
		}
	}
	created, err := store.Create(automation.Automation{
		Name:        in.Name,
		Prompt:      in.Prompt,
		Trigger:     trigger,
		ProjectPath: project,
		Mode:        "full_access",
		Source:      automation.SourceAgent,
		Enabled:     false, // human-in-the-loop: the user must enable it
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"Proposed automation %q (%s) — created DISABLED. Ask the user to review and enable it on the Automations page; it will not run until they do. (id: %s)",
		created.Name, automation.HumanSchedule(created.Trigger), created.ID), nil
}

func triggerFromCadence(in automationCreateInput) (automation.Trigger, error) {
	switch in.Cadence {
	case "manual", "":
		return automation.Trigger{Type: automation.TriggerManual}, nil
	case "hourly":
		return automation.Trigger{Type: automation.TriggerSchedule, Cadence: automation.CadenceHourly, Minute: in.Minute}, nil
	case "daily":
		return automation.Trigger{Type: automation.TriggerSchedule, Cadence: automation.CadenceDaily, Hour: in.Hour, Minute: in.Minute}, nil
	case "weekly":
		return automation.Trigger{Type: automation.TriggerSchedule, Cadence: automation.CadenceWeekly, Weekday: in.Weekday, Hour: in.Hour, Minute: in.Minute}, nil
	default:
		return automation.Trigger{}, fmt.Errorf("invalid cadence %q (want hourly|daily|weekly|manual)", in.Cadence)
	}
}
