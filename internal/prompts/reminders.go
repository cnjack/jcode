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
}

// reminder is a single conditional reminder rule.
type reminder struct {
	name      string
	condition func(*ReminderContext) bool
	message   func(*ReminderContext) string
}

var builtinReminders = []reminder{
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
