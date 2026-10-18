package sessionview

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cnjack/jcode/internal/session"
)

const (
	maxQueryRunes = 512
	// maxMatchesPerEntry bounds the per-entry match counter so a pathological
	// entry cannot dominate the ranking denominator or the scan budget.
	maxMatchesPerEntry = 10000
	// snippetHalfWindow is the number of runes kept on each side of the first
	// hit inside a snippet.
	snippetHalfWindow = 80
	// maxSnippetRunes is the hard cap for a snippet, ellipsis markers included.
	maxSnippetRunes = 161
)

// Query is a literal substring search request.
type Query struct {
	Text string
	// CaseSensitive disables the default ASCII case-insensitive matching.
	CaseSensitive bool
}

// SearchOptions controls Search.
type SearchOptions struct {
	IncludeArchived bool
}

// SearchHit is the strongest hit of one session.
type SearchHit struct {
	Session   SessionRef
	Seq       int
	Role      string
	Timestamp string
	Snippet   string
	Matches   int // matches inside the reported entry
	MoreHits  int // other matching entries in the same session
}

// SearchResult is the outcome of Search.
type SearchResult struct {
	Hits     []SearchHit
	Scanned  int // files opened by the scan
	Eligible int // validated candidates excluding the caller's session
	Skipped  int // index entries dropped because their id was invalid
	Capped   bool
	TimedOut bool
	Elapsed  time.Duration
}

// Search implements session_search: literal substring matching over user and
// assistant message text plus compaction summaries, one strongest hit per
// session, never the caller's own session.
func (s *Service) Search(ctx context.Context, c Caller, q Query, o SearchOptions) (SearchResult, error) {
	started := s.now()
	var res SearchResult
	if err := validateQuery(q); err != nil {
		return res, err
	}
	cands, skipped, eligible, err := s.candidates(ctx, c, o.IncludeArchived, true, maxSearchCandidates)
	if err != nil {
		return res, err
	}
	res.Skipped = skipped
	res.Eligible = eligible

	// Distinguish the caller's cancellation from our own time budget (N7).
	parentCtx := ctx
	scanCtx, cancel := context.WithTimeout(parentCtx, s.cfg.TimeBudget)
	defer cancel()

	needle, needleFold := searchNeedles(q.Text)
	prefilter := func(line []byte) bool {
		if q.CaseSensitive {
			return bytes.Contains(line, needle)
		}
		if bytes.Contains(line, needle) {
			return true
		}
		return bytes.Contains(asciiLowerBytes(line), needleFold)
	}

	hits := make([]SearchHit, 0, s.cfg.MaxResults)
	timedOut := false
	for _, cand := range cands {
		if stopped(scanCtx) {
			timedOut = true
			break
		}
		// Defense in depth: the candidate list already excluded self.
		if cand.meta.UUID == c.SessionID {
			continue
		}
		outcome := s.scanCandidate(scanCtx, c, cand, q, prefilter)
		if outcome.opened {
			res.Scanned++
		}
		if outcome.stopped {
			timedOut = true
		}
		if outcome.found {
			hits = append(hits, outcome.hit)
			if len(hits) >= s.cfg.MaxResults {
				res.Capped = true
				break
			}
		}
		if timedOut {
			break
		}
	}
	sortHits(hits)
	res.Hits = hits
	res.Elapsed = s.now().Sub(started)

	canceled := parentCtx.Err() != nil
	if canceled {
		s.logf("[session_view] search caller=%s project=%s qlen=%d eligible=%d scanned=%d hits=%d skipped=%d capped=%t timed_out=%t canceled=%t elapsed_ms=%d",
			c.SessionID, c.Project, utf8.RuneCountInString(q.Text), res.Eligible, res.Scanned,
			len(res.Hits), res.Skipped, res.Capped, false, true, res.Elapsed.Milliseconds())
		return res, parentCtx.Err()
	}
	res.TimedOut = timedOut
	s.logf("[session_view] search caller=%s project=%s qlen=%d eligible=%d scanned=%d hits=%d skipped=%d capped=%t timed_out=%t canceled=%t elapsed_ms=%d",
		c.SessionID, c.Project, utf8.RuneCountInString(q.Text), res.Eligible, res.Scanned,
		len(res.Hits), res.Skipped, res.Capped, res.TimedOut, false, res.Elapsed.Milliseconds())
	return res, nil
}

// validateQuery returns the exact self-correcting detail strings from §5.5.
func validateQuery(q Query) error {
	if strings.TrimSpace(q.Text) == "" {
		return invalidQuery("query must contain at least one non-whitespace character")
	}
	if strings.ContainsRune(q.Text, '\x00') {
		return invalidQuery("query must not contain NUL")
	}
	if utf8.RuneCountInString(q.Text) > maxQueryRunes {
		return invalidQuery("query must be at most 512 characters")
	}
	return nil
}

// scanOutcome is the result of scanning one candidate session.
type scanOutcome struct {
	hit     SearchHit
	found   bool
	opened  bool
	stopped bool
}

// scanCandidate authorizes and scans one indexed session file. Every failure
// except the caller's cancellation is a skip: one unreadable file must never
// fail the whole search.
func (s *Service) scanCandidate(ctx context.Context, c Caller, cand candidate, q Query, prefilter func([]byte) bool) scanOutcome {
	var out scanOutcome
	f, opened, ok := s.openCandidate(cand, c.Project)
	out.opened = opened
	if !ok {
		return out
	}
	defer func() { _ = f.Close() }()

	if info, statErr := f.Stat(); statErr == nil && info.Size() > maxScanFileBytes {
		s.logf("[session_view] skip target=%s reason=file_too_large size=%d", cand.meta.UUID, info.Size())
		return out
	}

	var acc hitAccumulator
	_, scanErr := s.scanFile(ctx, f, scanOptions{prefilter: prefilter}, func(seq int, e session.Entry) bool {
		if stopped(ctx) {
			out.stopped = true
			return false
		}
		role, text, ok := matchableText(e)
		if !ok || text == "" {
			return true
		}
		firstByte, count := findMatches(text, q.Text, q.CaseSensitive)
		if count == 0 {
			return true
		}
		acc.observe(seq, role, e.Timestamp, text, firstByte, count)
		return true
	})
	if scanErr != nil {
		s.logf("[session_view] skip target=%s reason=read_failed err=%v", cand.meta.UUID, scanErr)
		return out
	}
	if acc.best == nil {
		return out
	}
	out.found = true
	out.hit = SearchHit{
		Session:   s.refFor(cand.meta, c.SessionID, session.Entry{}),
		Seq:       acc.best.seq,
		Role:      acc.best.role,
		Timestamp: acc.best.ts,
		Snippet:   buildSnippet(acc.best.text, acc.best.firstByte, len(q.Text)),
		Matches:   acc.best.count,
		MoreHits:  acc.matched - 1,
	}
	return out
}

// matchInfo is one entry that contained the query.
type matchInfo struct {
	seq       int
	role      string
	ts        string
	text      string
	firstByte int
	count     int
}

// hitAccumulator keeps the strongest match of one session. "Strongest" is the
// design's ranking: most matches in the entry, then shorter content, then the
// smaller seq.
type hitAccumulator struct {
	matched int
	best    *matchInfo
}

func (a *hitAccumulator) observe(seq int, role, ts, text string, firstByte, count int) {
	a.matched++
	if count > maxMatchesPerEntry {
		count = maxMatchesPerEntry
	}
	cand := matchInfo{seq: seq, role: role, ts: ts, text: text, firstByte: firstByte, count: count}
	if a.best == nil || betterMatch(cand, *a.best) {
		a.best = &cand
	}
}

func betterMatch(a, b matchInfo) bool {
	if a.count != b.count {
		return a.count > b.count
	}
	if len(a.text) != len(b.text) {
		return len(a.text) < len(b.text)
	}
	return a.seq < b.seq
}

// matchableText returns the searchable text of an entry. Only user and
// assistant message text plus compaction summaries are searchable; tool call
// arguments, command output, and file contents are never matched (D6).
func matchableText(e session.Entry) (role, text string, ok bool) {
	switch e.Type {
	case session.EntryUser:
		return "user", e.Content, true
	case session.EntryAssistant:
		return "assistant", e.Content, true
	case session.EntryCompact:
		return "compact", e.Summary, true
	default:
		return "", "", false
	}
}

// findMatches returns the byte offset of the first match and the number of
// non-overlapping matches. Matching is ASCII case-insensitive by default;
// non-ASCII bytes are compared exactly.
func findMatches(text, query string, caseSensitive bool) (int, int) {
	if query == "" || len(text) < len(query) {
		return -1, 0
	}
	if caseSensitive {
		first := strings.Index(text, query)
		if first < 0 {
			return -1, 0
		}
		return first, strings.Count(text, query)
	}
	lowerText, lowerQuery := asciiLower(text), asciiLower(query)
	first := strings.Index(lowerText, lowerQuery)
	if first < 0 {
		return -1, 0
	}
	return first, strings.Count(lowerText, lowerQuery)
}

// searchNeedles builds the JSON-escape-aware prefilter needles. The writer
// stores entries via json.Marshal, so the escaping of the query (quotes,
// backslashes, '<', '>', '&', U+2028/2029, control characters) is deterministic
// and can be matched against the raw JSONL bytes.
func searchNeedles(query string) (exact, folded []byte) {
	raw, err := json.Marshal(query)
	if err != nil || len(raw) < 2 {
		return []byte(query), asciiLowerBytes([]byte(query))
	}
	exact = raw[1 : len(raw)-1]
	return exact, asciiLowerBytes(exact)
}

// buildSnippet renders a rune-safe window of at most snippetHalfWindow runes
// around the first match, marking trimmed ends with an ellipsis.
func buildSnippet(text string, firstByte, matchBytes int) string {
	if firstByte < 0 || firstByte > len(text) {
		firstByte = 0
	}
	end := firstByte + matchBytes
	if end > len(text) {
		end = len(text)
	}
	prefix := text[:firstByte]
	left := prefix
	leftTrimmed := false
	if n := utf8.RuneCountInString(prefix); n > snippetHalfWindow {
		left = prefix[runeIndexToByte(prefix, n-snippetHalfWindow):]
		leftTrimmed = true
	}
	rest := text[end:]
	right := rest
	rightTrimmed := false
	if n := utf8.RuneCountInString(rest); n > snippetHalfWindow {
		right = rest[:runeIndexToByte(rest, snippetHalfWindow)]
		rightTrimmed = true
	}
	body := sanitizeSnippet(left + text[firstByte:end] + right)
	markers := 0
	if leftTrimmed {
		markers++
	}
	if rightTrimmed {
		markers++
	}
	// The design's 80+80 rune window plus a 1-rune match fits in 161 runes; for
	// longer queries trim the tail instead of overrunning the cap.
	body = truncateRunes(body, maxSnippetRunes-markers)
	var b strings.Builder
	b.Grow(len(body) + 6)
	if leftTrimmed {
		b.WriteString("…")
	}
	b.WriteString(body)
	if rightTrimmed {
		b.WriteString("…")
	}
	return b.String()
}

// sanitizeSnippet replaces control characters with spaces and collapses runs
// of whitespace to one space.
func sanitizeSnippet(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// truncateRunes cuts s to at most max runes without splitting a sequence.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return s[:runeIndexToByte(s, max)]
}

// runeIndexToByte returns the byte offset of the n-th rune in s (n <= rune
// count). It never splits a UTF-8 sequence.
func runeIndexToByte(s string, n int) int {
	if n <= 0 {
		return 0
	}
	count := 0
	for i := range s {
		if count == n {
			return i
		}
		count++
	}
	return len(s)
}

// asciiLower lowercases ASCII letters only, preserving byte length and all
// non-ASCII bytes. This is the design's case-folding contract: ASCII
// case-insensitive, everything else exact.
func asciiLower(s string) string {
	needs := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	b := []byte(s)
	lowerASCIIBytes(b)
	return string(b)
}

func asciiLowerBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = lowerASCII(b[i])
	}
	return out
}

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// lowerASCIIBytes lowercases ASCII letters in place.
func lowerASCIIBytes(b []byte) {
	for i := range b {
		b[i] = lowerASCII(b[i])
	}
}

// stopped reports whether the context is done. It also checks the deadline
// directly so very small budgets are observed deterministically, without
// waiting for the context timer goroutine to fire.
func stopped(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	if deadline, ok := ctx.Deadline(); ok {
		return !time.Now().Before(deadline)
	}
	return false
}

// sortHits orders hits by matches desc, then UpdatedAt desc, then UUID asc
// (design N12). UpdatedAt is compared as an instant, never as a string.
func sortHits(hits []SearchHit) {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Matches != hits[j].Matches {
			return hits[i].Matches > hits[j].Matches
		}
		ti, tj := refTime(hits[i].Session), refTime(hits[j].Session)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return hits[i].Session.ID < hits[j].Session.ID
	})
}

func refTime(ref SessionRef) time.Time {
	ts, err := time.Parse(time.RFC3339, ref.UpdatedAt)
	if err != nil {
		return time.Time{}
	}
	return ts
}
