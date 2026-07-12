package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// ─── Per-subagent progress slots (P3-11) ───
//
// Subagent lifecycle events carry the subagent's name, so parallel subagents
// (batched `subagent` tool calls or coordinator fan-out) each get their own
// slot instead of overwriting one shared progress box. Slots are ordered by
// start time; completed slots stay visible (as a one-line summary) while
// sibling subagents are still running, and the whole set is cleared once the
// last one finishes.

// subagentSlot tracks one running (or just-finished) subagent.
type subagentSlot struct {
	name      string
	agentType string
	steps     int      // tool calls so far
	progress  []string // recent tool-call lines (tail-capped)
	tokens    int64    // cumulative tokens for this subagent
	startedAt time.Time
	done      bool
	failed    bool
	duration  time.Duration // set when done
}

// subagentSlotMaxLines caps stored progress lines per slot; the live box only
// ever shows a tail, so older lines can be dropped as they scroll out.
const subagentSlotMaxLines = 32

// touchSubagents bumps the revision counter that keys the live-box cache.
func (m *Model) touchSubagents() {
	m.subagentRev++
}

// startSubagentSlot appends a new slot for a starting subagent.
func (m *Model) startSubagentSlot(name, agentType string) *subagentSlot {
	s := &subagentSlot{name: name, agentType: agentType, startedAt: time.Now()}
	m.subagentSlots = append(m.subagentSlots, s)
	m.touchSubagents()
	return s
}

// findActiveSubagent returns the most recently started slot with the given
// name that hasn't finished. When the name doesn't match any slot (defensive:
// event source and display disagree) it falls back to the sole active slot,
// preserving the old single-subagent behavior.
func (m *Model) findActiveSubagent(name string) *subagentSlot {
	var sole *subagentSlot
	active := 0
	for i := len(m.subagentSlots) - 1; i >= 0; i-- {
		s := m.subagentSlots[i]
		if s.done {
			continue
		}
		if s.name == name {
			return s
		}
		active++
		if sole == nil {
			sole = s
		}
	}
	if active == 1 {
		return sole
	}
	return nil
}

// activeSubagentCount returns how many slots are still running.
func (m *Model) activeSubagentCount() int {
	n := 0
	for _, s := range m.subagentSlots {
		if !s.done {
			n++
		}
	}
	return n
}

// clearSubagentSlots drops all slots (run finished or cancelled).
func (m *Model) clearSubagentSlots() {
	if len(m.subagentSlots) == 0 {
		return
	}
	m.subagentSlots = nil
	m.touchSubagents()
}

// recordSubagentProgress appends one tool-call line to the slot.
func (s *subagentSlot) recordSubagentProgress(line string) {
	s.steps++
	s.progress = append(s.progress, line)
	if len(s.progress) > subagentSlotMaxLines {
		s.progress = s.progress[len(s.progress)-subagentSlotMaxLines:]
	}
}

// finishSubagentSlot marks the slot done and freezes its duration.
func (s *subagentSlot) finishSubagentSlot(failed bool) {
	s.done = true
	s.failed = failed
	s.duration = time.Since(s.startedAt)
}

// subagentSummary renders "name · N steps · 4.2s" from whatever data the slot
// actually has — pieces without data are simply omitted, never invented.
func (s *subagentSlot) subagentSummary() string {
	parts := []string{s.name}
	if s.steps > 0 {
		parts = append(parts, fmt.Sprintf("%d steps", s.steps))
	}
	if s.duration > 0 {
		parts = append(parts, formatToolDuration(s.duration))
	}
	return strings.Join(parts, " · ")
}

// hasSubagentDisplay reports whether the live status area should show the
// subagent box: any slot with progress, or several slots (so section headers
// alone are worth showing).
func (m *Model) hasSubagentDisplay() bool {
	if len(m.subagentSlots) > 1 {
		return true
	}
	for _, s := range m.subagentSlots {
		if len(s.progress) > 0 {
			return true
		}
	}
	return false
}

// subagentTotals sums steps and tokens across all slots for the status line.
func (m *Model) subagentTotals() (steps int, tokens int64) {
	for _, s := range m.subagentSlots {
		steps += s.steps
		tokens += s.tokens
	}
	return steps, tokens
}

// renderSubagentBox returns a bordered box showing live subagent tool calls.
// A single subagent keeps the classic layout (a plain tail of progress
// lines); with parallel subagents each active one becomes a small section —
// name header plus its last couple of steps — and finished ones collapse to a
// one-line "✓ name · N steps · duration" summary. Cached by revision + width.
func (m *Model) renderSubagentBox() string {
	width := m.contentWidth()
	if m.subagentBoxCache != "" && m.subagentBoxCacheRev == m.subagentRev && m.subagentBoxCacheWidth == width {
		return m.subagentBoxCache
	}

	var content strings.Builder
	if len(m.subagentSlots) == 1 && !m.subagentSlots[0].done {
		s := m.subagentSlots[0]
		content.WriteString(renderSubagentTail(s.progress, 8, s.steps))
	} else {
		mutedItalic := lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
		first := true
		for _, s := range m.subagentSlots {
			if !first {
				content.WriteString("\n")
			}
			first = false
			if s.done {
				icon := toolSuccessStyle.Render("✓")
				if s.failed {
					icon = toolErrorStyle.Render("✗")
				}
				fmt.Fprintf(&content, "%s %s", icon, mutedItalic.Render(s.subagentSummary()))
				continue
			}
			header := fmt.Sprintf("%s %s",
				subagentLabelStyle.Render(s.name),
				toolArgsStyle.Render(fmt.Sprintf("(%s) [%d steps]", s.agentType, s.steps)))
			content.WriteString(header)
			// Current step: the last 1-2 progress lines, opencode-style "↳".
			tail := s.progress
			if len(tail) > 2 {
				tail = tail[len(tail)-2:]
			}
			for _, line := range tail {
				content.WriteString("\n")
				content.WriteString("  " + mutedItalic.Render("↳") + " " + line)
			}
		}
	}

	boxWidth := width - 8
	if boxWidth < 30 {
		boxWidth = 30
	}

	box := subagentBoxStyle.Width(boxWidth).Render(content.String())
	m.subagentBoxCache = box
	m.subagentBoxCacheRev = m.subagentRev
	m.subagentBoxCacheWidth = width
	return box
}

// renderSubagentTail renders the last maxVisible progress lines with an
// "… (N earlier steps)" marker — the classic single-subagent box body.
// totalSteps counts all steps ever recorded (stored lines are tail-capped),
// so the marker stays truthful after old lines have been dropped.
func renderSubagentTail(lines []string, maxVisible, totalSteps int) string {
	hidden := 0
	if len(lines) > maxVisible {
		lines = lines[len(lines)-maxVisible:]
	}
	if totalSteps > len(lines) {
		hidden = totalSteps - len(lines)
	}
	var content strings.Builder
	if hidden > 0 {
		content.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
			Render(fmt.Sprintf("... (%d earlier steps)", hidden)))
		content.WriteString("\n")
	}
	for i, line := range lines {
		content.WriteString(line)
		if i < len(lines)-1 {
			content.WriteString("\n")
		}
	}
	return content.String()
}
