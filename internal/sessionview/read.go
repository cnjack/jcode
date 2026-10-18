package sessionview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cnjack/jcode/internal/session"
)

const (
	defaultReadLimit = 10
	maxReadLimit     = 200

	// Per-type rendering caps from design §5.6.
	toolCallArgsBytes   = 4000
	toolResultHeadRunes = 200
	metaRunes           = 300
)

// ReadRequest addresses entries of one authorized session.
type ReadRequest struct {
	// SessionID "" means the calling session. Otherwise a 1-based entry number
	// window is selected.
	SessionID string
	// Start is the 1-based entry number to start at; 0 means the tail window.
	Start int
	// Limit is the maximum number of entries; 0 means the default (10), values
	// above 200 are clamped.
	Limit int
	// EntryOffset is a rune offset inside the entry addressed by Start. It is
	// only valid together with Start > 0 and only meaningful for content
	// entries (user/assistant/compact).
	EntryOffset int
}

// EntryView is the projected, budgeted form of one entry. It is safe to render
// verbatim: Images, OpaqueResponseItems, operation capability keys, credential
// fingerprints, and provider hashes are never projected into it.
type EntryView struct {
	Seq  int
	Type string
	Role string
	Time string
	Text string
	Meta string
	// NextEntryOffset is set when a content entry (user/assistant/compact) was
	// cut; passing it back with start=Seq continues inside that entry.
	NextEntryOffset int
	// TruncatedBytes is the number of bytes of this entry that are not shown.
	// It is set for every cut entry, content or not.
	TruncatedBytes int
}

// ReadResult is the outcome of Read.
type ReadResult struct {
	Ref       SessionRef
	Entries   []EntryView
	Total     int // parsed entries (session_start included, corrupt lines excluded)
	FirstSeq  int // seq of the first returned entry (0 when none)
	Corrupt   int
	Torn      bool
	Truncated bool
	Bytes     int
}

// Read implements session_read: entry-number addressed reads with the design's
// two-tier output budget.
func (s *Service) Read(ctx context.Context, c Caller, r ReadRequest) (ReadResult, error) {
	var res ReadResult
	if c.SessionID == "" {
		return res, &viewError{sentinel: ErrNoCaller, reason: "no_caller"}
	}
	if r.Start < 0 {
		return res, invalidRange("start must be 0 or greater (0 means the most recent entries)")
	}
	if r.Limit < 0 {
		return res, invalidRange("limit must be 0 or greater (0 means the default)")
	}
	if r.EntryOffset < 0 {
		return res, invalidRange("entry_offset must be 0 or greater")
	}
	if r.EntryOffset > 0 && r.Start == 0 {
		// A tail-window first entry can shift under concurrent appends, so
		// continuing inside it is not allowed (binding correction).
		return res, invalidRange("entry_offset requires start to address the entry being continued")
	}
	limit := r.Limit
	if limit == 0 {
		limit = defaultReadLimit
	}
	if limit > maxReadLimit {
		limit = maxReadLimit
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}

	target, err := s.ResolveTarget(ctx, c, r.SessionID)
	if err != nil {
		return res, err
	}
	defer func() { _ = target.File.Close() }()
	res.Ref = target.Ref
	self := r.SessionID == "" || r.SessionID == c.SessionID

	window := newReadWindow(r.Start, limit, self)
	stats, err := s.scanFile(ctx, target.File, scanOptions{}, func(seq int, e session.Entry) bool {
		if ctx.Err() != nil {
			return false
		}
		// Never retain the multi-megabyte fields we are forbidden to render.
		e.Images = nil
		e.OpaqueResponseItems = nil
		window.observe(seq, e)
		return true
	})
	if err := ctx.Err(); err != nil {
		return res, err
	}
	if err != nil {
		s.logf("[session_view] error op=read target=%s err=%v", target.Ref.ID, err)
		return res, &viewError{sentinel: ErrIO}
	}
	res.Total = stats.Total
	res.Corrupt = stats.Corrupt
	res.Torn = stats.Torn

	selected := window.selected()
	remaining := s.cfg.MaxOutputBytes
	firstReturned := true
	for _, se := range selected {
		if se.e.Type == session.EntrySessionStart {
			// The header already carries the session metadata.
			continue
		}
		if remaining <= 0 {
			res.Truncated = true
			break
		}
		view, consumed, cut, omitted := s.projectEntry(se, firstReturned, r.Start, r.EntryOffset, remaining)
		if omitted {
			// No budget left for this entry: stop without emitting a stub.
			res.Truncated = true
			break
		}
		if cut {
			res.Truncated = true
		}
		res.Entries = append(res.Entries, view)
		res.Bytes += consumed
		remaining -= consumed
		if remaining < 0 {
			remaining = 0
		}
		firstReturned = false
	}
	if len(res.Entries) > 0 {
		res.FirstSeq = res.Entries[0].Seq
	}
	s.logf("[session_view] read caller=%s target=%s range=%d-%d offset=%d total=%d corrupt=%d torn=%t bytes=%d truncated=%t",
		c.SessionID, res.Ref.ID, r.Start, r.Start+limit-1, r.EntryOffset, res.Total,
		res.Corrupt, res.Torn, res.Bytes, res.Truncated)
	return res, nil
}

// projectEntry renders and budgets one entry. selectedFirst reports whether
// this is the first entry actually returned (which may consume the whole call
// budget when it is a content entry). remaining is the bytes left in the call.
//
// It returns the view, the bytes consumed by the view (Text+Meta), whether the
// entry itself was cut, and whether budget exhaustion dropped content.
func (s *Service) projectEntry(se seqEntry, selectedFirst bool, start, entryOffset, remaining int) (EntryView, int, bool, bool) {
	view := EntryView{
		Seq:  se.seq,
		Type: string(se.e.Type),
		Role: roleOf(se.e.Type),
		Time: se.e.Timestamp,
	}
	content := isContentType(se.e.Type)
	var text, meta string
	cutBytes := 0
	if content {
		text = se.e.Content
		if se.e.Type == session.EntryCompact {
			text = se.e.Summary
		}
	} else {
		text, meta, cutBytes = s.projectStructured(se.e)
	}

	offset := 0
	if content && selectedFirst && entryOffset > 0 && se.seq == start {
		offset = entryOffset
	}
	if offset > 0 {
		runes := []rune(text)
		if offset >= len(runes) {
			text = ""
		} else {
			text = string(runes[offset:])
		}
	}

	capBytes := remaining
	if content && !selectedFirst {
		capBytes = min(s.cfg.MaxEntryBytes, remaining)
	}
	if capBytes <= 0 {
		return view, 0, cutBytes > 0, true
	}
	if len(text) > capBytes {
		kept, keptRunes := truncateRunesBytes(text, capBytes)
		cutBytes += len(text) - len(kept)
		text = kept
		if content {
			view.NextEntryOffset = offset + keptRunes
		}
	}
	if cutBytes > 0 {
		view.TruncatedBytes = cutBytes
	}
	view.Text = text
	view.Meta = meta
	return view, len(text) + len(meta), cutBytes > 0, false
}

// projectStructured renders the one-line summary types. It returns the text
// body, the meta fragment, and the number of omitted bytes when the per-type
// cap cut the body.
func (s *Service) projectStructured(e session.Entry) (text, meta string, truncated int) {
	switch e.Type {
	case session.EntryToolCall:
		args := compactJSON(e.Args)
		// truncateRunesBytes reports the rune count kept, not omitted bytes.
		body, _ := truncateRunesBytes(args, toolCallArgsBytes)
		return body, capMeta(e.Name), len(args) - len(body)
	case session.EntryToolResult:
		head := firstLine(e.Output)
		if e.Error != "" {
			head = "error: " + firstLine(e.Error)
		}
		body, omitted := capRunes(head, toolResultHeadRunes)
		parts := []string{e.Name}
		if e.DurationMs > 0 {
			parts = append(parts, fmt.Sprintf("%dms", e.DurationMs))
		}
		if e.Denied {
			parts = append(parts, "DENIED")
		}
		return body, capMeta(joinMeta(parts...)), omitted
	case session.EntryPlanUpdate:
		return "", capMeta(joinMeta("status="+e.PlanStatus, "title="+e.PlanTitle)), 0
	case session.EntryGoalUpdate:
		return "", capMeta(joinMeta("status="+e.GoalStatus, "objective="+e.GoalObjective)), 0
	case session.EntryTodoSnapshot:
		return "", capMeta(fmt.Sprintf("items=%d", len(e.Todos))), 0
	case session.EntryModeChange:
		return "", capMeta(joinMeta("mode=" + e.Mode)), 0
	case session.EntryAgentChange:
		return "", capMeta(joinMeta("agent=" + e.Agent)), 0
	case session.EntrySubagentStart:
		return "", capMeta(joinMeta("name="+e.SubagentName, "type="+e.SubagentType)), 0
	case session.EntrySubagentResult:
		status := "ok"
		if e.Error != "" {
			status = "error"
		}
		return "", capMeta(joinMeta("name="+e.SubagentName, status)), 0
	case session.EntrySubagentAsync:
		return "", capMeta(joinMeta("name="+e.SubagentName, "type="+e.SubagentType, "task="+e.Output)), 0
	case session.EntryBudgetWarning:
		return "", "", 0
	case session.EntryToolObservation:
		parts := []string{}
		if e.ToolObservation != nil {
			parts = append(parts, "kind="+e.ToolObservation.Kind)
			if e.ToolObservation.ToolName != "" {
				parts = append(parts, "tool="+e.ToolObservation.ToolName)
			}
		}
		return "", capMeta(joinMeta(parts...)), 0
	case session.EntryArtifact:
		parts := []string{"id=" + e.ArtifactID}
		if e.ArtifactTitle != "" {
			parts = append(parts, "title="+e.ArtifactTitle)
		}
		if e.ArtifactKind != "" {
			parts = append(parts, "kind="+e.ArtifactKind)
		}
		return "", capMeta(joinMeta(parts...)), 0
	case session.EntryGenerationOperation:
		// N3/N4: state and the run id only — never the capability key,
		// credential fingerprint, or any request-id hash.
		return "", capMeta(joinMeta("state="+e.OperationState, "run="+e.OperationID)), 0
	case session.EntryProviderToolOperation:
		return "", capMeta(joinMeta("state="+e.ProviderToolState,
			"profile="+e.ProviderToolProviderProfileID, "tool="+e.ProviderToolName)), 0
	case session.EntrySessionToolOverride:
		return "", capMeta(joinMeta("tool="+e.SessionToolOverrideTool,
			fmt.Sprintf("persisted=%t", e.SessionToolOverridePersisted),
			fmt.Sprintf("revision=%d", e.SessionToolOverrideRevision))), 0
	case session.EntrySystemPrompt:
		return "", "", 0
	case session.EntrySessionStart:
		return "", "", 0
	default:
		// N10: an unknown entry type is never rendered from Content.
		return "", "(unrecognized)", 0
	}
}

// IsKnownEntryType reports whether t is one of the entry types the render
// table understands. Unknown types are projected with an empty Text and the
// meta marker "(unrecognized)".
func IsKnownEntryType(t string) bool {
	switch session.EntryType(t) {
	case session.EntrySessionStart, session.EntryUser, session.EntryAssistant,
		session.EntryToolCall, session.EntryToolResult, session.EntryPlanUpdate,
		session.EntryTodoSnapshot, session.EntrySubagentStart, session.EntrySubagentResult,
		session.EntrySubagentAsync, session.EntryModeChange, session.EntryAgentChange,
		session.EntryCompact, session.EntryBudgetWarning, session.EntrySystemPrompt,
		session.EntryGoalUpdate, session.EntryToolObservation, session.EntryArtifact,
		session.EntryGenerationOperation, session.EntryProviderToolOperation,
		session.EntrySessionToolOverride:
		return true
	default:
		return false
	}
}

// isContentType reports whether an entry carries model-visible content that may
// consume the full per-call output budget and support entry_offset
// continuation.
func isContentType(t session.EntryType) bool {
	switch t {
	case session.EntryUser, session.EntryAssistant, session.EntryCompact:
		return true
	default:
		return false
	}
}

func roleOf(t session.EntryType) string {
	switch t {
	case session.EntryUser:
		return "user"
	case session.EntryAssistant:
		return "assistant"
	case session.EntryCompact:
		return "compact"
	default:
		return ""
	}
}

// truncateRunesBytes cuts s to at most maxBytes without splitting a UTF-8
// sequence and reports the number of runes kept.
func truncateRunesBytes(s string, maxBytes int) (string, int) {
	if len(s) <= maxBytes {
		return s, utf8.RuneCountInString(s)
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], utf8.RuneCountInString(s[:cut])
}

// capRunes cuts s to at most maxRunes runes and reports the omitted bytes.
func capRunes(s string, maxRunes int) (string, int) {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s, 0
	}
	idx := runeIndexToByte(s, maxRunes)
	return s[:idx], len(s) - idx
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, "\r")
}

// compactJSON removes insignificant whitespace from tool arguments. Legacy or
// malformed args are returned unchanged.
func compactJSON(args string) string {
	if args == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(args)); err != nil {
		return args
	}
	return buf.String()
}

func capMeta(s string) string {
	out, _ := capRunes(s, metaRunes)
	return out
}

func joinMeta(parts ...string) string {
	kept := parts[:0]
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}

// seqEntry is one parsed entry with its 1-based ordinal.
type seqEntry struct {
	seq int
	e   session.Entry
}

// readWindow selects the entries a Read returns. Start > 0 selects the fixed
// [start, start+limit) window; Start == 0 keeps a ring buffer of the newest
// limit entries. In self-read mode everything from the last user entry onward
// is excluded (the in-flight turn, including this very tool call).
type readWindow struct {
	start int
	limit int
	self  bool

	buf  []seqEntry
	head int // ring position of the oldest entry once the ring is full

	saved   []seqEntry
	sawUser bool
}

func newReadWindow(start, limit int, self bool) *readWindow {
	return &readWindow{start: start, limit: limit, self: self}
}

func (w *readWindow) observe(seq int, e session.Entry) {
	if w.self && e.Type == session.EntryUser {
		// Remember the window as it stands BEFORE this user entry. The last
		// user entry is the in-flight turn (this very tool call is written
		// after it), so everything from it onward is excluded. The buffer keeps
		// filling normally so earlier user turns stay readable.
		w.saved = append(w.saved[:0], w.ordered()...)
		w.sawUser = true
	}
	if w.start > 0 {
		if seq < w.start || len(w.buf) >= w.limit {
			return
		}
		w.buf = append(w.buf, seqEntry{seq: seq, e: e})
		return
	}
	if len(w.buf) < w.limit {
		w.buf = append(w.buf, seqEntry{seq: seq, e: e})
		return
	}
	w.buf[w.head] = seqEntry{seq: seq, e: e}
	w.head = (w.head + 1) % w.limit
}

// ordered returns the buffered entries oldest first.
func (w *readWindow) ordered() []seqEntry {
	if w.start > 0 || len(w.buf) < w.limit {
		return w.buf
	}
	out := make([]seqEntry, 0, len(w.buf))
	out = append(out, w.buf[w.head:]...)
	out = append(out, w.buf[:w.head]...)
	return out
}

// selected returns the entries to render.
func (w *readWindow) selected() []seqEntry {
	if w.sawUser {
		return w.saved
	}
	return w.ordered()
}
