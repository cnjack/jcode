package automation

// Template is a built-in starting point shown on the Templates page. Selecting
// one pre-fills the editor; the user picks a project and confirms.
type Template struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Badge       string  `json:"badge"` // Daily|Weekly|Manual (display)
	Prompt      string  `json:"prompt"`
	Trigger     Trigger `json:"trigger"`
	SuggestMode string  `json:"suggest_mode"`
}

// BuiltinTemplates returns the curated templates (aligned with the reference UI).
func BuiltinTemplates() []Template {
	return []Template{
		{
			ID:          "issue-triage",
			Name:        "Issue triage",
			Description: "Review the latest GitHub issues and propose priorities and owners.",
			Badge:       "Daily",
			Prompt:      "Review the newest open issues in this repository. For each, propose a priority (P0–P3) and a likely owner, and summarize the triage as a short report.",
			Trigger:     Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 9, Minute: 0},
			SuggestMode: "full_access",
		},
		{
			ID:          "changelog-draft",
			Name:        "Changelog draft",
			Description: "Summarize key merged PRs this week into a release-note draft.",
			Badge:       "Weekly",
			Prompt:      "Summarize the PRs merged into the main branch over the last 7 days into a concise, user-facing release-note draft grouped by Features / Fixes / Chores.",
			Trigger:     Trigger{Type: TriggerSchedule, Cadence: CadenceWeekly, Weekday: 5, Hour: 17, Minute: 0},
			SuggestMode: "full_access",
		},
		{
			ID:          "repo-audit",
			Name:        "Repo audit",
			Description: "Audit open PRs and identify blockers or risky changes.",
			Badge:       "Manual",
			Prompt:      "Audit the currently open pull requests. Identify any that are blocked, stale, or contain risky changes, and produce a prioritized action list.",
			Trigger:     Trigger{Type: TriggerManual},
			SuggestMode: "full_access",
		},
		{
			ID:          "perf-improvements",
			Name:        "Performance improvements",
			Description: "Identify high-impact performance improvements and summarize them.",
			Badge:       "Weekly",
			Prompt:      "Identify up to 10 concrete performance improvements in this codebase, ranked by impact and effort. Summarize each with the file and the rationale.",
			Trigger:     Trigger{Type: TriggerSchedule, Cadence: CadenceWeekly, Weekday: 1, Hour: 9, Minute: 0},
			SuggestMode: "full_access",
		},
		{
			ID:          "a11y-audit",
			Name:        "Accessibility audit",
			Description: "Review recent changes and summarize any accessibility issues.",
			Badge:       "Daily",
			Prompt:      "Review the changes merged in the last day for accessibility issues (labels, contrast, keyboard nav, ARIA) and summarize findings with suggested fixes.",
			Trigger:     Trigger{Type: TriggerSchedule, Cadence: CadenceDaily, Hour: 10, Minute: 0},
			SuggestMode: "full_access",
		},
		{
			ID:          "cost-tips",
			Name:        "Cost tips",
			Description: "Get personalized tips to reduce token usage and cost.",
			Badge:       "Weekly",
			Prompt:      "Analyze recent agent usage in this project and suggest concrete ways to reduce token usage and cost without losing capability.",
			Trigger:     Trigger{Type: TriggerSchedule, Cadence: CadenceWeekly, Weekday: 1, Hour: 8, Minute: 0},
			SuggestMode: "full_access",
		},
	}
}
