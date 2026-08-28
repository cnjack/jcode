package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cnjack/jcode/internal/tasks"
	"github.com/cnjack/jcode/internal/tools"
)

// taskSuggestion is one candidate in the @mention completion list.
type taskSuggestion struct {
	ref    string
	name   string
	status tasks.Status
	kind   string
}

// WithTaskHub wires the persistent agent-task registry into the TUI so /task
// commands and @task mentions work.
func WithTaskHub(hub *tools.TaskHub) ModelOption {
	return func(m *Model) {
		m.taskHub = hub
	}
}

// mentionToken returns the @token currently being typed (without @).
func mentionToken(val string) string {
	return tasks.TrailingMention(val)
}

// updateTaskSuggestions refreshes the @mention completion list from the
// caller's visible (project-scoped) tasks. Only tasks this session may see
// are ever listed.
func (m *Model) updateTaskSuggestions() {
	m.taskSuggestions = nil
	m.taskSuggestionActive = false
	m.taskSuggestionIndex = 0
	if m.taskHub == nil || !m.taskHub.HasStore() {
		return
	}
	tok := mentionToken(m.textarea.Value())
	if tok == "" {
		return
	}
	recs, err := m.taskHub.Store.List("")
	if err != nil {
		return
	}
	needle := strings.ToLower(tok)
	for _, rec := range recs {
		if rec.Status == tasks.StatusArchived {
			continue
		}
		name := strings.ToLower(rec.Name)
		ref := strings.ToLower(rec.Ref)
		short := strings.TrimPrefix(ref, "task_")
		if strings.HasPrefix(name, needle) || strings.HasPrefix(ref, needle) || strings.HasPrefix(short, needle) {
			m.taskSuggestions = append(m.taskSuggestions, taskSuggestion{
				ref:    rec.Ref,
				name:   rec.Name,
				status: rec.Status,
				kind:   rec.Kind,
			})
			if len(m.taskSuggestions) >= 8 {
				break
			}
		}
	}
	if len(m.taskSuggestions) > 0 {
		m.taskSuggestionActive = true
	}
}

// acceptTaskSuggestion replaces the trailing @token with the selected ref.
func (m *Model) acceptTaskSuggestion(sel taskSuggestion) {
	val := m.textarea.Value()
	tok := mentionToken(val)
	if tok == "" {
		return
	}
	idx := strings.LastIndex(val, "@"+tok)
	if idx < 0 {
		return
	}
	m.textarea.SetValue(val[:idx] + "@" + sel.ref + " ")
	m.textarea.CursorEnd()
	m.taskSuggestionActive = false
	m.taskSuggestions = nil
	m.taskSuggestionIndex = 0
	m.textareaLines = m.recalcTextareaLines()
	m.textarea.SetHeight(m.textareaLines)
}

// renderTaskSuggestions renders the @mention completion popup.
func (m Model) renderTaskSuggestions() string {
	if len(m.taskSuggestions) == 0 {
		return ""
	}
	maxVisible := 5
	total := len(m.taskSuggestions)
	start := 0
	if m.taskSuggestionIndex >= maxVisible {
		start = m.taskSuggestionIndex - maxVisible + 1
	}
	if start+maxVisible > total {
		start = total - maxVisible
	}
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > total {
		end = total
	}

	var lines []string
	for i := start; i < end; i++ {
		s := m.taskSuggestions[i]
		label := fmt.Sprintf("@%s", s.name)
		desc := fmt.Sprintf(" %s · %s · @%s", s.status, s.kind, s.ref)
		if i == m.taskSuggestionIndex {
			labelStyled := lipgloss.NewStyle().Bold(true).Foreground(colorOnPrimary).Background(colorPrimary).Render(label)
			descStyled := lipgloss.NewStyle().Foreground(colorOnPrimary).Background(colorPrimary).Render(desc)
			indicator := lipgloss.NewStyle().Foreground(colorPrimary).Render("❯")
			lines = append(lines, fmt.Sprintf("  %s %s%s", indicator, labelStyled, descStyled))
		} else {
			labelStyled := lipgloss.NewStyle().Foreground(colorText).Render(label)
			descStyled := lipgloss.NewStyle().Foreground(colorMuted).Render(desc)
			lines = append(lines, fmt.Sprintf("    %s%s", labelStyled, descStyled))
		}
	}
	if total > end {
		remaining := total - end
		lines = append(lines, lipgloss.NewStyle().PaddingLeft(3).Foreground(colorMuted).Italic(true).
			Render(fmt.Sprintf("  ... and %d more (↓ to scroll)", remaining)))
	}
	return strings.Join(lines, "\n")
}

// expandMentions resolves every @mention in the prompt against the visible
// task registry and appends an injection-safe context block. It returns the
// expanded prompt and a list of resolution errors (unknown / ambiguous /
// cross-project mentions) — when non-empty the caller should refuse to send.
func (m *Model) expandMentions(prompt string) (string, []string) {
	if m.taskHub == nil || !m.taskHub.HasStore() {
		return prompt, nil
	}
	tokens := tasks.ParseMentions(prompt)
	if len(tokens) == 0 {
		return prompt, nil
	}
	var resolved []*tasks.Record
	var errs []string
	seen := map[string]bool{}
	for _, tok := range tokens {
		if seen[tok] {
			continue
		}
		seen[tok] = true
		rec, err := m.taskHub.Store.Resolve(tok)
		if err != nil {
			errs = append(errs, fmt.Sprintf("@%s: %v", tok, err))
			continue
		}
		if rec.Status == tasks.StatusArchived {
			errs = append(errs, fmt.Sprintf("@%s: task is archived", tok))
			continue
		}
		resolved = append(resolved, rec)
	}
	if len(errs) > 0 {
		return prompt, errs
	}
	block := tasks.RenderMentionContext(resolved)
	if block == "" {
		return prompt, nil
	}
	return prompt + "\n\n" + block, nil
}

// handleTaskInput implements the /task command family against the task hub
// (local operations — no LLM round-trip).
func (m *Model) handleTaskInput(prompt string, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	prompt = strings.TrimSpace(prompt)
	arg := strings.TrimSpace(strings.TrimPrefix(prompt, "/task"))
	var sub, rest string
	if i := strings.IndexAny(arg, " \t"); i >= 0 {
		sub, rest = arg[:i], strings.TrimSpace(arg[i+1:])
	} else {
		sub = arg
	}

	out := func(format string, a ...any) {
		m.lines = append(m.lines, textLine(toolLabelStyle.Render("  "+fmt.Sprintf(format, a...))))
	}
	if m.taskHub == nil || !m.taskHub.HasStore() {
		out("📋 Task registry is not available in this session.")
		m.refreshViewport()
		return m, tea.Batch(cmds...)
	}
	hub := m.taskHub
	session := hub.SessionID()

	switch sub {
	case "", "list":
		recs, err := hub.Store.List("")
		if err != nil {
			out("📋 task list failed: %v", err)
			break
		}
		if len(recs) == 0 {
			out("📋 No tasks in this project yet. Create one with /task create <name>.")
			break
		}
		out("📋 Tasks in this project (%d):", len(recs))
		for _, rec := range recs {
			ended := ""
			if !rec.EndedAt.IsZero() {
				ended = " · ended " + rec.EndedAt.Format("15:04:05")
			}
			out("  @%s  [%s] %s%s", rec.Ref, rec.Status, rec.Name, ended)
		}
		out("   /task read|message|stop|archive <ref> · mention with @name")

	case "create":
		name, desc, _ := strings.Cut(rest, " ")
		if name == "" {
			out("📋 Usage: /task create <name> [description]")
			break
		}
		rec, err := hub.Store.Create(tasks.CreateInput{
			Name:        name,
			Description: desc,
			Kind:        tasks.KindWorkItem,
			SessionID:   session,
		})
		if err != nil {
			out("📋 create failed: %v", err)
			break
		}
		out("📋 Task created: %s (@%s) — reference it from any session in this project.", rec.Ref, rec.Name)

	case "read":
		if rest == "" {
			out("📋 Usage: /task read <ref|name>")
			break
		}
		rec, err := hub.Store.Resolve(rest)
		if err != nil {
			out("📋 %v", err)
			break
		}
		out("📋 %s — %q [%s] kind=%s session=%s", rec.Ref, rec.Name, rec.Status, rec.Kind, rec.SessionID)
		if rec.Description != "" {
			out("   %s", rec.Description)
		}
		if rec.Output != "" {
			out("   output: %s", truncateForLine(rec.Output))
		}
		if rec.Error != "" {
			out("   error: %s", truncateForLine(rec.Error))
		}
		for _, ev := range rec.Timeline {
			out("   [%s] %s", ev.FromRole, truncateForLine(ev.Body))
		}

	case "message":
		ref, body, ok := strings.Cut(rest, " ")
		if !ok || ref == "" || body == "" {
			out("📋 Usage: /task message <ref|name> <text>")
			break
		}
		rec, err := hub.Store.Resolve(ref)
		if err != nil {
			out("📋 %v", err)
			break
		}
		updated, err := hub.Store.Message(rec.Ref, session, "user", body, "")
		if err != nil {
			out("📋 message failed: %v", err)
			break
		}
		out("💬 Sent to %s (%s) — timeline has %d message(s).", rec.Ref, updated.Status, len(updated.Timeline))

	case "stop":
		if rest == "" {
			out("📋 Usage: /task stop <ref|name>")
			break
		}
		rec, err := hub.Store.Resolve(rest)
		if err != nil {
			out("📋 %v", err)
			break
		}
		stopped := false
		if hub.Manager != nil {
			stopped = hub.Manager.Stop(rec.Ref) == nil
		}
		if stopped {
			out("🛑 Stopped %s.", rec.Ref)
		} else {
			switch rec.Status {
			case tasks.StatusRunning, tasks.StatusPending:
				out("🛑 %s runs in another session/process (owner pid %d on %s); stop it there.", rec.Ref, rec.OwnerPID, rec.Hostname)
			default:
				out("🛑 %s is not running (status=%s).", rec.Ref, rec.Status)
			}
		}

	case "archive":
		if rest == "" {
			out("📋 Usage: /task archive <ref|name>")
			break
		}
		rec, err := hub.Store.Resolve(rest)
		if err != nil {
			out("📋 %v", err)
			break
		}
		if _, err := hub.Store.Archive(rec.Ref); err != nil {
			out("📋 archive failed: %v", err)
			break
		}
		out("📦 Archived %s — reads now report it as archived.", rec.Ref)

	default:
		out("📋 Unknown subcommand %q. Usage: /task [list|create|read|message|stop|archive]", sub)
	}

	m.refreshViewport()
	return m, tea.Batch(cmds...)
}

func truncateForLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}
