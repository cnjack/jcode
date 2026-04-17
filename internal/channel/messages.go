package channel

import "time"

// WelcomeMessage returns a time-aware welcome message.
func WelcomeMessage(t time.Time) string {
	greeting := timeGreeting(t)
	if isWeekend(t) {
		return greeting + " jcode is online.\n" +
			"————————————————\n" +
			"Weekend mode — I'll notify you\n" +
			"when approval is needed or tasks complete.\n" +
			"Enjoy your time off!"
	}
	return greeting + " jcode is online.\n" +
		"————————————————\n" +
		"I'll notify you when approval is\n" +
		"needed or tasks complete."
}

// GoodbyeMessage returns a time-aware goodbye message.
func GoodbyeMessage(t time.Time) string {
	farewell := timeFarewell(t)
	if isWeekend(t) {
		return "jcode session ended.\n" +
			"————————————————\n" +
			farewell + " Enjoy your weekend!"
	}
	return "jcode session ended.\n" +
		"————————————————\n" +
		farewell
}

// ApprovalMessage returns a formatted approval notification.
func ApprovalMessage(toolName, toolArgs, hint string) string {
	argsSummary := toolArgs
	if len(argsSummary) > 150 {
		argsSummary = argsSummary[:150] + "..."
	}
	return "⏳ Approval Needed\n" +
		"————————————————\n" +
		"Tool: " + toolName + "\n" +
		"Args: " + argsSummary + "\n" +
		"————————————————\n" +
		hint
}

// DoneMessage returns a formatted task completion notification.
func DoneMessage(summary string, err error) string {
	if err != nil {
		return "❌ Task Failed\n" +
			"————————————————\n" +
			err.Error()
	}
	s := summary
	if len(s) > 500 {
		s = s[:500] + "..."
	}
	if s == "" {
		s = "(no summary)"
	}
	return "✅ Task Completed\n" +
		"————————————————\n" +
		s
}

// BusyMessage returns a message for when a task is already in progress.
func BusyMessage() string {
	return "⏳ Task In Progress\n" +
		"————————————————\n" +
		"A task is currently running.\n" +
		"Your message has been queued and\n" +
		"will be processed after the current\n" +
		"task completes."
}

// LoginReminderMessage returns a reminder that the user must send a message
// to activate the 24-hour session window on WeChat iLink Bot.
func LoginReminderMessage() string {
	return "⚠️ Important\n" +
		"————————————————\n" +
		"Please send any message to this bot\n" +
		"on WeChat now to activate the session.\n" +
		"Once activated, you can receive\n" +
		"notifications for 24 hours."
}

func isWeekend(t time.Time) bool {
	d := t.Weekday()
	return d == time.Saturday || d == time.Sunday
}

func timeGreeting(t time.Time) string {
	h := t.Hour()
	switch {
	case h < 6:
		return "🌙 Burning the midnight oil?"
	case h < 9:
		return "🌅 Good morning!"
	case h < 12:
		return "☀️ Good morning!"
	case h < 14:
		return "🍽 Good afternoon!"
	case h < 18:
		return "☀️ Good afternoon!"
	case h < 22:
		return "🌆 Good evening!"
	default:
		return "🌙 Working late?"
	}
}

func timeFarewell(t time.Time) string {
	h := t.Hour()
	switch {
	case h < 6:
		return "Get some rest! 😴"
	case h < 12:
		return "Have a great day ahead!"
	case h < 18:
		return "See you later!"
	case h < 22:
		return "Have a good evening!"
	default:
		return "Good night! 🌙"
	}
}
