package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/automation"
)

type automationCreateInput struct {
	Name        string `json:"name"`
	Prompt      string `json:"prompt"`
	Cadence     string `json:"cadence"`      // hourly|daily|weekly|cron|once|manual
	Hour        int    `json:"hour"`         // 0-23 (daily/weekly)
	Minute      int    `json:"minute"`       // 0-59
	Weekday     int    `json:"weekday"`      // 0=Sun..6=Sat (weekly)
	CronExpr    string `json:"cron_expr"`    // 5-field cron expression (cadence=cron)
	At          string `json:"at"`           // pinned time (cadence=once): RFC3339 or "YYYY-MM-DD HH:MM" local
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
		Desc: `Propose a new automation (a scheduled or one-shot agent task) for the user.

The automation is created DISABLED and will NOT run until the user reviews it and enables it on the Automations page — you cannot arm it yourself. Use this when the user asks to run something on a recurring schedule (e.g. "every morning summarize new issues"), at a pinned future time ("tomorrow 9am, run the smoke test"), or to save a reusable manual task.

cadence must be one of:
- "hourly" (uses minute)
- "daily" (uses hour+minute)
- "weekly" (uses weekday+hour+minute)
- "cron" (uses cron_expr — a 5-field expression "M H DoM Mon DoW" in local time, e.g. "*/15 * * * *" every 15 minutes, "0 9 * * 1-5" weekdays at 9am; use this for weekday sets or sub-hour intervals the named cadences can't express)
- "once" (uses at — fires exactly one time, then auto-disables; best for "remind me at X" / "at time T do Y")
- "manual" (no schedule)

When the user's requested time is approximate, avoid the :00 and :30 minute marks (e.g. pick 9:57 or 10:03 for "around 9am") — exact times land every user on the API at the same instant. It runs in the current project by default.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"name":         {Type: schema.String, Desc: "Short human-readable name for the automation.", Required: true},
			"prompt":       {Type: schema.String, Desc: "The instruction the agent will run each time the automation fires.", Required: true},
			"cadence":      {Type: schema.String, Desc: `One of "hourly", "daily", "weekly", "cron", "once", "manual".`, Required: true},
			"hour":         {Type: schema.Integer, Desc: "Hour of day 0-23 (daily/weekly)."},
			"minute":       {Type: schema.Integer, Desc: "Minute of hour 0-59."},
			"weekday":      {Type: schema.Integer, Desc: "Day of week 0=Sunday..6=Saturday (weekly)."},
			"cron_expr":    {Type: schema.String, Desc: `5-field cron expression, local time (cadence="cron"). E.g. "*/15 * * * *", "0 9 * * 1-5".`},
			"at":           {Type: schema.String, Desc: `One-shot fire time (cadence="once"): RFC3339, or "YYYY-MM-DD HH:MM" in local time. Must be in the future.`},
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
	store := t.env.automationStore()
	if store == nil {
		return "", fmt.Errorf("automation store unavailable")
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
	case "cron": // automation.CadenceCron
		normalized := strings.Join(strings.Fields(in.CronExpr), " ")
		if _, err := automation.ParseCronExpr(normalized); err != nil {
			return automation.Trigger{}, fmt.Errorf("invalid cron_expr: %w", err)
		}
		return automation.Trigger{Type: automation.TriggerSchedule, Cadence: automation.CadenceCron, Expr: normalized}, nil
	case "once": // the tool-level cadence name for TriggerOnce
		at, err := parseFlexibleTime(in.At)
		if err != nil {
			return automation.Trigger{}, fmt.Errorf("invalid at for once trigger: %w", err)
		}
		return automation.Trigger{Type: automation.TriggerOnce, At: at.Format(time.RFC3339)}, nil
	default:
		return automation.Trigger{}, fmt.Errorf("invalid cadence %q (want hourly|daily|weekly|cron|once|manual)", in.Cadence)
	}
}

// parseFlexibleTime accepts RFC3339 (with or without nanoseconds) or a local
// "YYYY-MM-DD HH:MM[:SS]" (with space or "T" separator) so the model doesn't
// need to know the host's UTC offset to say "tomorrow 9am", and datetime-local
// style values with seconds still parse.
func parseFlexibleTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{
		"2006-01-02 15:04", "2006-01-02T15:04",
		"2006-01-02 15:04:05", "2006-01-02T15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not RFC3339 or \"YYYY-MM-DD HH:MM\"", s)
}

// automationStore returns the live shared store when one is wired into the Env
// (web server), falling back to a fresh store in contexts with no live server
// (CLI/ACP).
func (e *Env) automationStore() *automation.Store {
	if e.AutomationStore != nil {
		return e.AutomationStore
	}
	store, err := automation.NewStore()
	if err != nil {
		return nil
	}
	return store
}

// NewAutomationListTool creates the read-only automation_list tool. It takes
// no arguments; InvokableRun ignores the raw JSON input.
func (e *Env) NewAutomationListTool() tool.InvokableTool {
	info := &schema.ToolInfo{
		Name:        "automation_list",
		Desc:        `List the user's automations (scheduled or one-shot agent tasks) with their ids, schedules, enabled state, and last run outcome. Read-only. Use it before proposing a similar automation, or when the user asks what automations exist.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}
	return &automationListTool{env: e, info: info}
}

type automationListTool struct {
	env  *Env
	info *schema.ToolInfo
}

func (t *automationListTool) Info(_ context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *automationListTool) InvokableRun(_ context.Context, _ string, _ ...tool.Option) (string, error) {
	store := t.env.automationStore()
	if store == nil {
		return "", fmt.Errorf("automation store unavailable")
	}
	list := store.List()
	if len(list) == 0 {
		return "automations: 0\nNo automations defined.", nil
	}
	records := make([]string, 0, len(list))
	for _, a := range list {
		st := store.State(a.ID)
		records = append(records, strings.Join([]string{
			"id: " + a.ID,
			"name: " + a.Name,
			"schedule: " + automation.HumanSchedule(a.Trigger),
			"enabled: " + boolText(a.Enabled),
			"mode: " + a.Mode,
			"project: " + a.ProjectPath,
			"next_run_at: " + st.NextRunAt,
			"last_status: " + st.LastStatus,
			"prompt: " + truncateText(a.Prompt, 160),
		}, "\n"))
	}
	return fmt.Sprintf("automations: %d\n%s", len(list), strings.Join(records, "\n---\n")), nil
}

type automationDeleteInput struct {
	ID string `json:"id"`
}

// NewAutomationDeleteTool creates the automation_delete tool. Deleting is a
// mutation: it follows the normal approval flow, and the web transport drops
// it from unattended automation runs (interactiveToolNames) so a headless run
// can never remove the user's automations.
func (e *Env) NewAutomationDeleteTool() tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "automation_delete",
		Desc: `Permanently delete one of the user's automations by id (see automation_list for ids). This cannot be undone. Confirm with the user before deleting.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"id": {Type: schema.String, Desc: "The automation id, as returned by automation_list or automation_create.", Required: true},
		}),
	}
	return &automationDeleteTool{env: e, info: info}
}

type automationDeleteTool struct {
	env  *Env
	info *schema.ToolInfo
}

func (t *automationDeleteTool) Info(_ context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *automationDeleteTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var in automationDeleteInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("failed to parse automation_delete input: %w", err)
	}
	if strings.TrimSpace(in.ID) == "" {
		return "", fmt.Errorf("id is required (see automation_list)")
	}
	store := t.env.automationStore()
	if store == nil {
		return "", fmt.Errorf("automation store unavailable")
	}
	a := store.Get(strings.TrimSpace(in.ID))
	if a == nil {
		return "", fmt.Errorf("no automation with id %q — call automation_list to see current ids", in.ID)
	}
	if err := store.Delete(a.ID); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted automation %q (%s, id: %s).", a.Name, automation.HumanSchedule(a.Trigger), a.ID), nil
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func truncateText(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
