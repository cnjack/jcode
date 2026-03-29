package tools

import (
	"regexp"
	"strings"
)

// numbered step: "1. Do something" or "1) Do something"
var numberedStepRe = regexp.MustCompile(`^\d+[\.\)]\s+(.+)`)

// markdown checkbox: "- [ ] Do something" or "- [x] Do something"
var checkboxRe = regexp.MustCompile(`^-\s+\[[ xX]\]\s+(.+)`)

// ExtractTodosFromPlan parses the ## Plan or ## Steps section from plan markdown
// and returns TodoItems with status=pending.
func ExtractTodosFromPlan(planContent string) []TodoItem {
	lines := strings.Split(planContent, "\n")

	// Find the plan/steps section
	inSection := false
	var items []TodoItem
	id := 1

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect section headers
		if strings.HasPrefix(trimmed, "## ") {
			header := strings.ToLower(strings.TrimPrefix(trimmed, "## "))
			if header == "plan" || header == "steps" || strings.HasPrefix(header, "plan ") || strings.HasPrefix(header, "steps ") {
				inSection = true
				continue
			}
			if inSection {
				// Reached next section, stop
				break
			}
			continue
		}

		if !inSection {
			continue
		}

		// Try numbered step
		if m := numberedStepRe.FindStringSubmatch(trimmed); m != nil {
			title := cleanStepTitle(m[1])
			if title != "" {
				items = append(items, TodoItem{ID: id, Title: title, Status: TodoPending})
				id++
			}
			continue
		}

		// Try checkbox
		if m := checkboxRe.FindStringSubmatch(trimmed); m != nil {
			title := cleanStepTitle(m[1])
			if title != "" {
				items = append(items, TodoItem{ID: id, Title: title, Status: TodoPending})
				id++
			}
			continue
		}
	}

	// If no section found, try extracting from the whole content
	if len(items) == 0 {
		id = 1
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if m := numberedStepRe.FindStringSubmatch(trimmed); m != nil {
				title := cleanStepTitle(m[1])
				if title != "" {
					items = append(items, TodoItem{ID: id, Title: title, Status: TodoPending})
					id++
				}
			}
		}
	}

	return items
}

// cleanStepTitle strips markdown formatting and truncates long titles.
func cleanStepTitle(raw string) string {
	// Remove bold markers
	title := strings.ReplaceAll(raw, "**", "")
	// Remove backtick code spans
	title = strings.ReplaceAll(title, "`", "")
	title = strings.TrimSpace(title)

	// Truncate to reasonable length
	if len(title) > 120 {
		title = title[:117] + "..."
	}

	// Skip empty or very short noise
	if len(title) < 3 {
		return ""
	}

	return title
}
