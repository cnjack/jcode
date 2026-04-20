package tui

import "github.com/cnjack/jcode/internal/session"

// ConvertSessionEntries converts recorded session entries to display-ready
// SessionEntry values for the TUI session-replay view.
func ConvertSessionEntries(entries []session.Entry) []SessionEntry {
	result := make([]SessionEntry, 0, len(entries))
	for _, e := range entries {
		if e.Type == session.EntrySessionStart {
			continue
		}

		// Convert todos
		var todos []TodoSnapshotItem
		if len(e.Todos) > 0 {
			todos = make([]TodoSnapshotItem, len(e.Todos))
			for i, t := range e.Todos {
				todos[i] = TodoSnapshotItem{
					ID:     t.ID,
					Title:  t.Title,
					Status: t.Status,
				}
			}
		}

		result = append(result, SessionEntry{
			Type:         string(e.Type),
			Content:      e.Content,
			Name:         e.Name,
			Args:         e.Args,
			Output:       e.Output,
			Error:        e.Error,
			ToolCallID:   e.ToolCallID,
			SubagentName: e.SubagentName,
			SubagentType: e.SubagentType,
			// Plan fields
			PlanStatus:  e.PlanStatus,
			PlanTitle:   e.PlanTitle,
			PlanContent: e.PlanContent,
			Feedback:    e.Feedback,
			// Todo fields
			Todos: todos,
			// Mode change
			Mode: e.Mode,
			// Compact fields
			Summary:    e.Summary,
			CompactedN: e.CompactedN,
		})
	}
	return result
}
