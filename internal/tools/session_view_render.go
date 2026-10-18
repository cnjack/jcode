package tools

import (
	"fmt"
	"strings"

	appconfig "github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/sessionview"
)

// Renders the three cross-session view tool results (design §5.6).
//
// The service projects every entry into a safe EntryView: Text never contains
// Images, OpaqueResponseItems, OperationCapabilityKey,
// OperationCredentialFingerprint, or provider hashes, and it is already cut to
// the byte budget (TruncatedBytes / NextEntryOffset describe the cut). This
// file therefore does NOT marshal entries, re-read files, or truncate entry
// text again — it only lays the projection out, elides bookkeeping entries to
// one line, and enforces the per-call output budget.

// sessionTitle renders a stored session title, using the design's placeholder
// for missing titles.
func sessionTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "(untitled)"
	}
	return title
}

// sessionModelLabel renders "provider/model", dropping empty halves.
func sessionModelLabel(provider, model string) string {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	switch {
	case provider == "" && model == "":
		return ""
	case provider == "":
		return model
	case model == "":
		return provider
	default:
		return provider + "/" + model
	}
}

// sessionMatchLabel renders the "N matches in this entry" clause.
func sessionMatchLabel(matches int) string {
	if matches <= 1 {
		return "1 match in this entry"
	}
	return fmt.Sprintf("%d matches in this entry", matches)
}

// renderSessionList renders a session_list result (§5.6).
func renderSessionList(project string, result sessionview.ListResult) string {
	if len(result.Sessions) == 0 {
		return "No prior sessions in this workspace.\n"
	}
	shown := result.Shown
	if shown <= 0 || shown > len(result.Sessions) {
		shown = len(result.Sessions)
	}
	eligible := result.Eligible
	if eligible < shown {
		eligible = shown
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Workspace: %s\n", project)
	fmt.Fprintf(&b, "Sessions (%d of %d shown, newest first):\n", shown, eligible)
	for i, ref := range result.Sessions {
		fmt.Fprintf(&b, "%d. %s", i+1, ref.ID)
		if ref.IsCaller {
			// N1: the calling session is listed too, so the model can read
			// earlier turns that compaction dropped from its context.
			b.WriteString("  (this session)")
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "   Title: %s\n", sessionTitle(ref.Title))
		line := "   Updated: " + ref.UpdatedAt
		if model := sessionModelLabel(ref.Provider, ref.Model); model != "" {
			line += " | Model: " + model
		}
		if ref.Archived {
			line += " | archived"
		}
		if ref.Automation {
			line += " | automation"
		}
		b.WriteString(line + "\n")
		if ref.SubagentTranscripts > 0 {
			fmt.Fprintf(&b, "   Subagent transcripts: %d\n", ref.SubagentTranscripts)
		}
	}
	if eligible > shown {
		fmt.Fprintf(&b, "More sessions exist (%d not shown). Raise limit or use session_search.\n", eligible-shown)
	}
	return b.String()
}

// renderSessionSearch renders a session_search result (§5.5/§5.6).
func renderSessionSearch(query, project string, result sessionview.SearchResult) string {
	var b strings.Builder
	if len(result.Hits) == 0 {
		// Zero hits is not the same as "this workspace has no matching text":
		// the search may have stopped at the time budget with most of the
		// corpus unscanned. The empty-state sentence stays verbatim (§5.6);
		// the partial-search footer below tells the model it was partial.
		fmt.Fprintf(&b, "No prior session matches found for %q.\n", query)
	} else {
		fmt.Fprintf(&b, "Session search results (%d) for %q in %s:\n", len(result.Hits), query, project)
	}
	for i, hit := range result.Hits {
		fmt.Fprintf(&b, "%d. %s — %s\n", i+1, hit.Session.ID, sessionTitle(hit.Session.Title))
		line := "   Updated: " + hit.Session.UpdatedAt
		if model := sessionModelLabel(hit.Session.Provider, hit.Session.Model); model != "" {
			line += " | Model: " + model
		}
		if hit.Session.Archived {
			line += " | archived"
		}
		if hit.Session.Automation {
			line += " | automation"
		}
		b.WriteString(line + "\n")

		role := strings.TrimSpace(hit.Role)
		if role == "" {
			role = "entry"
		}
		matchLine := fmt.Sprintf("   Best match: entry %d (%s) at %s · %s",
			hit.Seq, role, hit.Timestamp, sessionMatchLabel(hit.Matches))
		if hit.MoreHits > 0 {
			matchLine += fmt.Sprintf(", %d more entries", hit.MoreHits)
		}
		b.WriteString(matchLine + "\n")
		fmt.Fprintf(&b, "   Snippet: %s\n", hit.Snippet)
	}
	// The only three truncation markers in this design are the per-entry
	// continuation footer, the call-budget footer, and this cap line.
	if result.Capped && len(result.Hits) > 0 {
		fmt.Fprintf(&b, "Result cap reached (%d sessions). Narrow the query to find more.\n", len(result.Hits))
	}
	if result.TimedOut {
		fmt.Fprintf(&b,
			"Stopped after %.1fs (scanned %d of %d sessions). Narrow the query to search the rest.\n",
			result.Elapsed.Seconds(), result.Scanned, result.Eligible,
		)
	}
	return b.String()
}

// renderSessionRead renders a session_read result (§5.6).
func renderSessionRead(
	result sessionview.ReadResult,
	request sessionview.ReadRequest,
	settings appconfig.SessionViewResolved,
) string {
	ref := result.Ref
	var b strings.Builder
	fmt.Fprintf(&b, "Session %s — %s\n", ref.ID, sessionTitle(ref.Title))
	model := sessionModelLabel(ref.Provider, ref.Model)
	if model == "" {
		model = "unknown"
	}
	updated := ref.UpdatedAt
	if updated == "" {
		updated = "unknown"
	}
	fmt.Fprintf(&b, "Workspace: %s | Model: %s | Updated: %s\n", ref.Project, model, updated)
	b.WriteString("Parent: none")
	if ref.SubagentTranscripts > 0 {
		// v1 renders the count only: subagent transcripts are not addressable
		// through this tool (§5.7).
		fmt.Fprintf(&b, " | Children: %d subagent transcripts (not readable by this tool)",
			ref.SubagentTranscripts)
	}
	b.WriteString("\n")

	switch {
	case result.Total == 0:
		fmt.Fprintf(&b, "Session %s has no recorded entries.\n", ref.ID)
		return b.String()
	case len(result.Entries) == 0 && request.Start <= 0:
		// A tail-window read only comes back empty when the caller's own
		// in-flight turn is the only thing left: the service excludes
		// everything from the last user entry onward (§5.5 self-exclusion).
		if ref.IsCaller {
			b.WriteString("No earlier entries are readable in this session (the in-flight turn is excluded).\n")
		} else {
			fmt.Fprintf(&b, "Session %s has no recorded entries.\n", ref.ID)
		}
		return b.String()
	case len(result.Entries) == 0:
		// A start past the end is not an error (§5.5): tell the model the
		// valid range and the start of the most recent window.
		limit := request.Limit
		if limit <= 0 {
			limit = sessionReadDefaultLimit
		}
		if limit > sessionReadMaxLimit {
			limit = sessionReadMaxLimit
		}
		suggested := result.Total - limit + 1
		if suggested < 1 {
			suggested = 1
		}
		fmt.Fprintf(&b, "Entry %d is past the last entry (%d); use start=%d for the most recent window.\n",
			request.Start, result.Total, suggested)
		return b.String()
	}

	first := result.Entries[0].Seq
	last := result.Entries[len(result.Entries)-1].Seq
	fmt.Fprintf(&b, "Entries %d-%d of %d", first, last, result.Total)
	var notes []string
	if result.Corrupt > 0 {
		notes = append(notes, fmt.Sprintf("corrupt lines skipped: %d", result.Corrupt))
	}
	if result.Torn {
		notes = append(notes, "last line incomplete")
	}
	if len(notes) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(notes, ", "))
	}
	b.WriteString("\n\n")

	// The service already applied the per-entry byte budgets. What remains is
	// the per-call output budget: stop emitting entries instead of exceeding it
	// by much, and say which start= continues.
	remaining := settings.MaxOutputBytes - b.Len()
	if remaining < 512 {
		remaining = 512
	}
	shown := 0
	lastShown := first
	nextSeq := 0
	for _, entry := range result.Entries {
		block := renderSessionViewEntry(entry)
		if block == "" {
			continue
		}
		// The first returned entry may use the whole per-call budget; later
		// entries must fit in what is left.
		if shown > 0 && len(block) > remaining {
			nextSeq = entry.Seq
			break
		}
		b.WriteString(block)
		remaining -= len(block)
		lastShown = entry.Seq
		shown++
	}
	if shown > 0 && nextSeq > 0 {
		fmt.Fprintf(&b,
			"\n… [output truncated at %d bytes: entries %d-%d of %d shown; use start=%d to continue]\n",
			settings.MaxOutputBytes, first, lastShown, result.Total, nextSeq,
		)
	}
	return b.String()
}

// renderSessionViewEntry renders one projected entry. Every rendered entry
// starts with its [seq], unknown types are labelled instead of dumped, and
// bookkeeping entries are elided to a single structured line (§5.6 / N10).
func renderSessionViewEntry(entry sessionview.EntryView) string {
	typ := strings.TrimSpace(entry.Type)
	if typ == "" {
		return ""
	}
	switch typ {
	case string(session.EntrySessionStart):
		// The read header already carries the session identity.
		return ""
	case string(session.EntrySystemPrompt):
		return fmt.Sprintf("[%d] %s (elided)\n", entry.Seq, typ)
	}
	if !sessionview.IsKnownEntryType(typ) {
		// N10: never fall back to Content for an unknown type.
		return fmt.Sprintf("[%d] %s (unrecognized)\n", entry.Seq, typ)
	}

	var b strings.Builder
	label := strings.TrimSpace(entry.Role)
	if label == "" {
		label = typ
	}
	fmt.Fprintf(&b, "[%d] %s", entry.Seq, label)
	if stamp := strings.TrimSpace(entry.Time); stamp != "" {
		fmt.Fprintf(&b, " · %s", stamp)
	}
	if meta := strings.TrimSpace(entry.Meta); meta != "" {
		fmt.Fprintf(&b, " · %s", meta)
	}
	b.WriteString("\n")
	if body := sessionEntryBody(typ, entry.Text); body != "" {
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	writeSessionEntryTruncation(&b, entry)
	return b.String()
}

// sessionEntryBody returns the body a type is allowed to show. Bookkeeping
// types never paste their text: their Meta fragment is the whole summary.
func sessionEntryBody(typ, text string) string {
	if text == "" {
		return ""
	}
	switch typ {
	case string(session.EntryUser), string(session.EntryAssistant), string(session.EntryCompact):
		return text
	case string(session.EntryToolCall):
		trimmed := strings.TrimSpace(text)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			return "args: " + text
		}
		return text
	case string(session.EntryToolResult):
		return text
	default:
		return ""
	}
}

// writeSessionEntryTruncation emits the per-entry truncation marker. Only
// content-bearing entries (user/assistant/compact) ever carry a continuation
// offset; tool and elided entries get the bare byte count so the model is not
// invited to loop on a first-line summary.
func writeSessionEntryTruncation(b *strings.Builder, entry sessionview.EntryView) {
	if entry.TruncatedBytes <= 0 {
		return
	}
	if entry.NextEntryOffset > 0 {
		fmt.Fprintf(b, "… [entry truncated: %d more bytes; continue with start=%d entry_offset=%d]\n",
			entry.TruncatedBytes, entry.Seq, entry.NextEntryOffset)
		return
	}
	fmt.Fprintf(b, "… [entry truncated: %d more bytes]\n", entry.TruncatedBytes)
}
