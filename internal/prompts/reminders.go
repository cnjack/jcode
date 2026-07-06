package prompts

import (
	"fmt"
	"strings"
)

// ReminderContext carries the runtime state needed to evaluate reminder conditions.
type ReminderContext struct {
	Iteration         int
	TokensUsed        int64
	ContextLimit      int
	HasIncompleteTodo bool
	IncompleteTodoN   int
	ConsecutiveErrors int
	EnvLabel          string
	IsRemote          bool
	PlanContent       string // non-empty when executing an approved plan

	// Goal context, set when an active session goal exists.
	GoalActive    bool
	GoalObjective string

	// Files the FileTracker sweep found modified (or deleted) outside the
	// session since the agent last read them.
	ExternalChangedFiles []string
	ExternalGoneFiles    []string

	// EnvDiff is the non-empty BuildEnvDiff output when the periodic env
	// refresh detected drift since the last snapshot (includes date changes).
	EnvDiff string

	// AGENTS.md reload: the new merged content when it changed on disk, or a
	// removal flag when the file disappeared. Mutually exclusive.
	AgentsMdUpdate  string
	AgentsMdRemoved bool
}

// reminder is a single conditional reminder rule.
type reminder struct {
	name      string
	condition func(*ReminderContext) bool
	message   func(*ReminderContext) string
}

var builtinReminders = []reminder{
	{
		name: "goal_active",
		condition: func(rc *ReminderContext) bool {
			return rc.GoalActive && rc.GoalObjective != ""
		},
		message: func(rc *ReminderContext) string {
			obj := rc.GoalObjective
			if len(obj) > 2000 {
				obj = obj[:2000] + "\n... (objective truncated)"
			}
			return fmt.Sprintf("[Active Goal]\nYou are pursuing a persistent session goal. Keep working toward it until it is verifiably complete, then call goal_update with status=\"complete\".\n\nObjective: %s", obj)
		},
	},
	{
		name: "plan_execution",
		condition: func(rc *ReminderContext) bool {
			return rc.PlanContent != ""
		},
		message: func(rc *ReminderContext) string {
			plan := rc.PlanContent
			// Truncate very long plans to keep context manageable
			if len(plan) > 2000 {
				plan = plan[:2000] + "\n... (plan truncated)"
			}
			return fmt.Sprintf("[Executing Approved Plan]\nYou are executing a user-approved plan. Follow it closely.\n\n%s\n\nTrack progress using the todo list. Mark each step complete as you finish it. If you need to deviate significantly, explain why.", plan)
		},
	},
	{
		name: "todo_check",
		condition: func(rc *ReminderContext) bool {
			return rc.HasIncompleteTodo && rc.Iteration > 5
		},
		message: func(rc *ReminderContext) string {
			return fmt.Sprintf("You have %d incomplete todo(s). Check your task list before continuing.", rc.IncompleteTodoN)
		},
	},
	{
		name: "token_warning",
		condition: func(rc *ReminderContext) bool {
			if rc.ContextLimit <= 0 {
				return false
			}
			pct := float64(rc.TokensUsed) / float64(rc.ContextLimit)
			return pct > 0.60 && pct <= 0.85
		},
		message: func(rc *ReminderContext) string {
			pct := int(100 * float64(rc.TokensUsed) / float64(rc.ContextLimit))
			return fmt.Sprintf("Context is %d%% full. Keep responses concise.", pct)
		},
	},
	{
		name: "token_critical",
		condition: func(rc *ReminderContext) bool {
			if rc.ContextLimit <= 0 {
				return false
			}
			return float64(rc.TokensUsed)/float64(rc.ContextLimit) > 0.85
		},
		message: func(rc *ReminderContext) string {
			pct := int(100 * float64(rc.TokensUsed) / float64(rc.ContextLimit))
			return fmt.Sprintf("Context is %d%% full. Wrap up the current task promptly.", pct)
		},
	},
	{
		name: "tool_error_streak",
		condition: func(rc *ReminderContext) bool {
			return rc.ConsecutiveErrors >= 2
		},
		message: func(_ *ReminderContext) string {
			return "Two or more tool calls have failed in a row. Try a different approach."
		},
	},
	{
		name: "external_file_changed",
		condition: func(rc *ReminderContext) bool {
			return len(rc.ExternalChangedFiles) > 0 || len(rc.ExternalGoneFiles) > 0
		},
		message: func(rc *ReminderContext) string {
			var sb strings.Builder
			sb.WriteString("[External file changes]\nThe following files were modified outside this session since you last read them; re-read before relying on or editing them:")
			for _, p := range rc.ExternalChangedFiles {
				sb.WriteString("\n- " + p)
			}
			for _, p := range rc.ExternalGoneFiles {
				sb.WriteString("\n- " + p + " (deleted)")
			}
			return sb.String()
		},
	},
	{
		name: "env_drift",
		condition: func(rc *ReminderContext) bool {
			return rc.EnvDiff != ""
		},
		message: func(rc *ReminderContext) string {
			// BuildEnvDiff output already carries its own header line.
			return rc.EnvDiff
		},
	},
	{
		name: "agents_md_changed",
		condition: func(rc *ReminderContext) bool {
			return rc.AgentsMdUpdate != "" || rc.AgentsMdRemoved
		},
		message: func(rc *ReminderContext) string {
			if rc.AgentsMdUpdate == "" {
				return "AGENTS.md was removed; the custom agent instructions in your system prompt may no longer apply."
			}
			content := rc.AgentsMdUpdate
			if len(content) > 10000 {
				content = content[:10000] + "\n... (content truncated)"
			}
			return "[AGENTS.md updated]\nThe project agent instructions changed on disk. The version below supersedes the one in your system prompt:\n\n" + content
		},
	},
}

// CollectReminders evaluates all built-in reminders and returns the messages
// for those whose condition is met. Returns nil if no reminders fire.
func CollectReminders(rc *ReminderContext) []string {
	var msgs []string
	for _, r := range builtinReminders {
		if r.condition(rc) {
			msgs = append(msgs, r.message(rc))
		}
	}
	return msgs
}

// FormatReminders formats collected reminder strings into a single system
// message block, or returns empty string if none.
func FormatReminders(msgs []string) string {
	if len(msgs) == 0 {
		return ""
	}
	return "[System Reminder]\n" + strings.Join(msgs, "\n")
}
