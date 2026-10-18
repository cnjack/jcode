package sessionview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cnjack/jcode/internal/session"
)

func seedSearchSession(t *testing.T, id, updated string, entries ...session.Entry) {
	t.Helper()
	all := append([]session.Entry{mkStart(id, projectA)}, entries...)
	writeSessionEntries(t, id, all)
	addIndexMeta(t, projectA, mkMeta(id, projectA, updated))
}

func TestSearchLiteralSemantics(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		query         string
		caseSensitive bool
		wantHits      int
	}{
		{name: "ascii case-insensitive default", content: "The FLaky Test fails", query: "flaky", wantHits: 1},
		{name: "case sensitive mismatch", content: "The FLaky Test fails", query: "flaky", caseSensitive: true, wantHits: 0},
		{name: "case sensitive match", content: "The flaky Test fails", query: "flaky", caseSensitive: true, wantHits: 1},
		{name: "cjk literal", content: "会话历史检索很重要", query: "历史", wantHits: 1},
		{name: "double quotes", content: `he said "hi" twice`, query: `"hi"`, wantHits: 1},
		{name: "backslash", content: `path C:\temp\file`, query: `\`, wantHits: 1},
		{name: "html escaped tag", content: `use <tag> here`, query: `<tag>`, wantHits: 1},
		{name: "html escaped ampersand", content: `a & b`, query: `&`, wantHits: 1},
		{name: "newline inside query", content: "alpha\nbeta gamma", query: "alpha\nbeta", wantHits: 1},
		{name: "no match", content: "hello world", query: "absent", wantHits: 0},
		{name: "non-ascii is exact", content: "ÜBER", query: "über", wantHits: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupHome(t)
			seedSearchSession(t, "peer-1", "2026-10-18T09:00:00+08:00", mkUser(tc.content))
			svc := New(Config{})
			res, err := svc.Search(context.Background(), Caller{SessionID: "caller-1", Project: projectA},
				Query{Text: tc.query, CaseSensitive: tc.caseSensitive}, SearchOptions{})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(res.Hits) != tc.wantHits {
				t.Fatalf("hits=%d want %d: %+v", len(res.Hits), tc.wantHits, res.Hits)
			}
		})
	}
}

func TestSearchQueryValidation(t *testing.T) {
	setupHome(t)
	svc := New(Config{})
	caller := Caller{SessionID: "caller-1", Project: projectA}
	tests := []struct {
		name   string
		query  string
		detail string
	}{
		{name: "empty", query: "", detail: "query must contain at least one non-whitespace character"},
		{name: "whitespace", query: " \t\n ", detail: "query must contain at least one non-whitespace character"},
		{name: "nul", query: "a\x00b", detail: "query must not contain NUL"},
		{name: "too long", query: strings.Repeat("a", 513), detail: "query must be at most 512 characters"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Search(context.Background(), caller, Query{Text: tc.query}, SearchOptions{})
			if !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("want ErrInvalidQuery, got %v", err)
			}
			if err.Error() != tc.detail {
				t.Fatalf("detail=%q want %q", err.Error(), tc.detail)
			}
		})
	}
	if _, err := svc.Search(context.Background(), caller, Query{Text: strings.Repeat("a", 512)}, SearchOptions{}); err != nil {
		t.Fatalf("512 runes must be valid: %v", err)
	}
}

func TestSearchSnippetUTF8(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		query       string
		wantMarkers bool
	}{
		{
			name:        "cjk window",
			content:     strings.Repeat("前", 120) + "关" + strings.Repeat("后", 120),
			query:       "关",
			wantMarkers: true,
		},
		{
			name:        "emoji boundary",
			content:     strings.Repeat("😀", 100) + "needle" + strings.Repeat("🌍", 100),
			query:       "needle",
			wantMarkers: true,
		},
		{
			name:    "control characters collapsed",
			content: "line1\n\nline2\tneedle\r\nline3",
			query:   "needle",
		},
		{
			name:    "short content has no markers",
			content: "the needle here",
			query:   "needle",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupHome(t)
			seedSearchSession(t, "peer-1", "2026-10-18T09:00:00+08:00", mkUser(tc.content))
			svc := New(Config{})
			res, err := svc.Search(context.Background(), Caller{SessionID: "caller-1", Project: projectA},
				Query{Text: tc.query}, SearchOptions{})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(res.Hits) != 1 {
				t.Fatalf("hits=%+v", res.Hits)
			}
			snippet := res.Hits[0].Snippet
			if !utf8.ValidString(snippet) {
				t.Fatalf("snippet is not valid UTF-8: %q", snippet)
			}
			if n := utf8.RuneCountInString(snippet); n > 161 {
				t.Fatalf("snippet has %d runes: %q", n, snippet)
			}
			if !strings.Contains(snippet, tc.query) {
				t.Fatalf("snippet %q lost the match %q", snippet, tc.query)
			}
			if strings.ContainsAny(snippet, "\n\t\r") {
				t.Fatalf("snippet contains control characters: %q", snippet)
			}
			if strings.Contains(snippet, "  ") {
				t.Fatalf("snippet has uncollapsed whitespace: %q", snippet)
			}
			if !tc.wantMarkers {
				if strings.Contains(snippet, "…") {
					t.Fatalf("snippet=%q must not have markers", snippet)
				}
				if tc.name == "short content has no markers" && snippet != "the needle here" {
					t.Fatalf("snippet=%q", snippet)
				}
				return
			}
			if !strings.HasPrefix(snippet, "…") || !strings.HasSuffix(snippet, "…") {
				t.Fatalf("snippet=%q want ellipsis on both ends", snippet)
			}
		})
	}
}

func TestSearchRankingOrder(t *testing.T) {
	setupHome(t)
	// 3 matches, 2026-10-18T10:00, two matching entries.
	seedSearchSession(t, "aaa", "2026-10-18T10:00:00+08:00",
		mkUser("needle needle needle"),
		mkAssistant("needle"),
	)
	// 1 match, newest.
	seedSearchSession(t, "bbb", "2026-10-18T11:00:00+08:00", mkUser("needle"))
	// 3 matches, oldest.
	seedSearchSession(t, "ccc", "2026-10-18T09:00:00+08:00", mkUser("needle needle needle"))
	// Equal matches + equal UpdatedAt => UUID ascending.
	seedSearchSession(t, "ddd", "2026-10-18T08:00:00+08:00", mkUser("needle"))
	seedSearchSession(t, "eee", "2026-10-18T08:00:00+08:00", mkUser("needle"))
	// UpdatedAt missing: StartTime must be the fallback and sort first.
	writeSessionEntries(t, "fff", []session.Entry{mkStart("fff", projectA), mkUser("needle")})
	meta := mkMeta("fff", projectA, "")
	meta.StartTime = "2026-10-19T00:00:00+08:00"
	addIndexMeta(t, projectA, meta)

	svc := New(Config{})
	res, err := svc.Search(context.Background(), Caller{SessionID: "caller-1", Project: projectA}, Query{Text: "needle"}, SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := make([]string, 0, len(res.Hits))
	for _, hit := range res.Hits {
		got = append(got, hit.Session.ID)
	}
	// Matches desc, then UpdatedAt desc (StartTime fallback), then UUID asc.
	want := []string{"aaa", "ccc", "fff", "bbb", "ddd", "eee"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order=%v want %v", got, want)
	}
	if res.Hits[0].Matches != 3 || res.Hits[0].MoreHits != 1 {
		t.Fatalf("aaa hit=%+v", res.Hits[0])
	}
	if res.Hits[0].Seq != 2 {
		t.Fatalf("aaa seq=%d want 2", res.Hits[0].Seq)
	}
	if res.Hits[2].Matches != 1 || res.Hits[2].MoreHits != 0 {
		t.Fatalf("fff hit=%+v", res.Hits[2])
	}
}

func TestSearchSelfExclusion(t *testing.T) {
	setupHome(t)
	seedSearchSession(t, "caller-1", "2026-10-18T10:00:00+08:00", mkUser("zebra in my own history"))
	seedSearchSession(t, "peer-1", "2026-10-18T09:00:00+08:00", mkUser("zebra in a peer"))
	svc := New(Config{})
	res, err := svc.Search(context.Background(), Caller{SessionID: "caller-1", Project: projectA}, Query{Text: "zebra"}, SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Eligible != 1 || res.Scanned != 1 {
		t.Fatalf("eligible=%d scanned=%d", res.Eligible, res.Scanned)
	}
	if len(res.Hits) != 1 || res.Hits[0].Session.ID != "peer-1" {
		t.Fatalf("hits=%+v", res.Hits)
	}
}

func TestSearchArchivedAutomation(t *testing.T) {
	setupHome(t)
	archived := mkMeta("archived-1", projectA, "2026-10-18T10:00:00+08:00")
	archived.Archived = true
	auto := mkMeta("auto-1", projectA, "2026-10-18T09:00:00+08:00")
	auto.AutomationID = "automation-7"
	writeSessionEntries(t, "archived-1", []session.Entry{mkStart("archived-1", projectA), mkUser("needle")})
	writeSessionEntries(t, "auto-1", []session.Entry{mkStart("auto-1", projectA), mkUser("needle")})
	addIndexMeta(t, projectA, archived, auto)

	svc := New(Config{})
	caller := Caller{SessionID: "caller-1", Project: projectA}
	res, err := svc.Search(context.Background(), caller, Query{Text: "needle"}, SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Session.ID != "auto-1" {
		t.Fatalf("archived must be excluded: %+v", res.Hits)
	}
	if !res.Hits[0].Session.Automation {
		t.Fatalf("automation must be marked: %+v", res.Hits[0].Session)
	}
	res, err = svc.Search(context.Background(), caller, Query{Text: "needle"}, SearchOptions{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Search archived: %v", err)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("archived must be searchable with the flag: %+v", res.Hits)
	}
}

func TestSearchSkipsInvalidIndexUUID(t *testing.T) {
	setupHome(t)
	const token = "CANARY_TOKEN_SHOULD_NEVER_APPEAR"
	// A file just above the sessions directory that an unvalidated
	// "../../canary" id would open.
	canary := filepath.Join(os.Getenv("HOME"), "canary.json")
	canaryData := `{"type":"session_start","uuid":"../../canary","project":"` + projectA + `"}` + "\n" +
		`{"type":"user","content":"` + token + `"}` + "\n"
	if err := os.WriteFile(canary, []byte(canaryData), 0o600); err != nil {
		t.Fatal(err)
	}
	addIndexMeta(t, projectA,
		mkMeta("../../canary", projectA, ""),
		mkMeta("a/b", projectA, ""),
		mkMeta("", projectA, ""),
		mkMeta("..", projectA, ""),
	)
	svc := New(Config{})
	res, err := svc.Search(context.Background(), Caller{SessionID: "caller-1", Project: projectA}, Query{Text: token}, SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Skipped != 4 {
		t.Fatalf("Skipped=%d want 4", res.Skipped)
	}
	if res.Eligible != 0 || res.Scanned != 0 {
		t.Fatalf("eligible=%d scanned=%d", res.Eligible, res.Scanned)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("hits=%+v", res.Hits)
	}
	for _, hit := range res.Hits {
		if strings.Contains(hit.Snippet, token) {
			t.Fatalf("canary content leaked: %+v", hit)
		}
	}
}

func TestSearchScannedEligibleNumbers(t *testing.T) {
	setupHome(t)
	// Indexed but deleted: eligible, opening fails.
	addIndexMeta(t, projectA, mkMeta("gone-1", projectA, ""))
	// Invalid id: skipped.
	addIndexMeta(t, projectA, mkMeta("../bad", projectA, ""))
	// Valid but no match.
	seedSearchSession(t, "peer-1", "2026-10-18T09:00:00+08:00", mkUser("nothing here"))

	svc := New(Config{})
	res, err := svc.Search(context.Background(), Caller{SessionID: "caller-1", Project: projectA}, Query{Text: "needle"}, SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Eligible != 2 {
		t.Fatalf("Eligible=%d want 2", res.Eligible)
	}
	if res.Skipped != 1 {
		t.Fatalf("Skipped=%d want 1", res.Skipped)
	}
	if res.Scanned != 1 {
		t.Fatalf("Scanned=%d want 1", res.Scanned)
	}
	if res.TimedOut || res.Capped {
		t.Fatalf("unexpected flags: %+v", res)
	}
}

func TestSearchCancelVsBudget(t *testing.T) {
	setupHome(t)
	seedSearchSession(t, "peer-1", "2026-10-18T09:00:00+08:00", mkUser("needle"))
	caller := Caller{SessionID: "caller-1", Project: projectA}

	t.Run("budget returns partial results", func(t *testing.T) {
		svc := New(Config{TimeBudget: time.Nanosecond})
		res, err := svc.Search(context.Background(), caller, Query{Text: "needle"}, SearchOptions{})
		if err != nil {
			t.Fatalf("budget exhaustion must not be an error: %v", err)
		}
		if !res.TimedOut {
			t.Fatalf("TimedOut must be set: %+v", res)
		}
		if res.Eligible != 1 {
			t.Fatalf("Eligible=%d want 1", res.Eligible)
		}
		if res.Scanned != 0 {
			t.Fatalf("Scanned=%d want 0", res.Scanned)
		}
		if len(res.Hits) != 0 {
			t.Fatalf("no hit can be produced before the first scan: %+v", res.Hits)
		}
	})

	t.Run("parent cancellation is returned", func(t *testing.T) {
		svc := New(Config{})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := svc.Search(ctx, caller, Query{Text: "needle"}, SearchOptions{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	})

	t.Run("normal search is not timed out", func(t *testing.T) {
		svc := New(Config{})
		res, err := svc.Search(context.Background(), caller, Query{Text: "needle"}, SearchOptions{})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if res.TimedOut || res.Capped || len(res.Hits) != 1 {
			t.Fatalf("res=%+v", res)
		}
	})
}

func TestSearchCap(t *testing.T) {
	setupHome(t)
	writeSessionEntries(t, "peer-1", []session.Entry{mkStart("peer-1", projectA), mkUser("needle")})
	writeSessionEntries(t, "peer-2", []session.Entry{mkStart("peer-2", projectA), mkUser("needle")})
	// 203 fileless index entries push the eligible set to 205, past the 200
	// sessions one search may open. Eligible must still report 205 (N7).
	const filler = 203
	metas := []session.SessionMeta{
		mkMeta("peer-1", projectA, "2026-10-18T11:00:00+08:00"),
		mkMeta("peer-2", projectA, "2026-10-18T10:00:00+08:00"),
	}
	for i := 0; i < filler; i++ {
		metas = append(metas, mkMeta(fmt.Sprintf("filler-%03d", i), projectA, "2026-10-17T09:00:00+08:00"))
	}
	writeIndexMap(t, map[string][]session.SessionMeta{projectA: metas})

	svc := New(Config{MaxResults: 1})
	res, err := svc.Search(context.Background(), Caller{SessionID: "caller-1", Project: projectA}, Query{Text: "needle"}, SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 1 || !res.Capped {
		t.Fatalf("hits=%d capped=%t", len(res.Hits), res.Capped)
	}
	if res.Eligible != filler+2 {
		t.Fatalf("Eligible=%d want %d (full eligible set, not the 200 scan cap)", res.Eligible, filler+2)
	}
	if res.Scanned != 1 {
		t.Fatalf("Scanned=%d want 1", res.Scanned)
	}
}

func TestSearchRequiresCallerAndWorkspace(t *testing.T) {
	setupHome(t)
	svc := New(Config{})
	if _, err := svc.Search(context.Background(), Caller{}, Query{Text: "x"}, SearchOptions{}); !errors.Is(err, ErrNoCaller) {
		t.Fatalf("want ErrNoCaller, got %v", err)
	}
	if _, err := svc.Search(context.Background(), Caller{SessionID: "x"}, Query{Text: "x"}, SearchOptions{}); !errors.Is(err, ErrNoWorkspace) {
		t.Fatalf("want ErrNoWorkspace, got %v", err)
	}
}

func TestSearchOnlyMatchesMessageText(t *testing.T) {
	setupHome(t)
	seedSearchSession(t, "peer-1", "2026-10-18T09:00:00+08:00",
		session.Entry{Type: session.EntryToolCall, Name: "grep", Args: `{"pattern":"needle"}`, Timestamp: "2026-10-18T09:00:01+08:00"},
		session.Entry{Type: session.EntryToolResult, Name: "grep", Output: "needle in output", Timestamp: "2026-10-18T09:00:02+08:00"},
		session.Entry{Type: session.EntrySystemPrompt, Content: "needle in prompt", Timestamp: "2026-10-18T09:00:03+08:00"},
		session.Entry{Type: session.EntryCompact, Summary: "compaction mentions needle", Timestamp: "2026-10-18T09:00:04+08:00"},
	)
	svc := New(Config{})
	res, err := svc.Search(context.Background(), Caller{SessionID: "caller-1", Project: projectA}, Query{Text: "needle"}, SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("hits=%+v", res.Hits)
	}
	if res.Hits[0].Role != "compact" {
		t.Fatalf("only the compact summary may match, got role %q", res.Hits[0].Role)
	}
}

func TestSearchStaleIndexProjectMismatchSkipped(t *testing.T) {
	setupHome(t)
	// Index claims projectA but the file's own session_start says projectB.
	writeSessionEntries(t, "moved", []session.Entry{mkStart("moved", projectB), mkUser("needle")})
	addIndexMeta(t, projectA, mkMeta("moved", projectA, ""))
	svc := New(Config{})
	res, err := svc.Search(context.Background(), Caller{SessionID: "caller-1", Project: projectA}, Query{Text: "needle"}, SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("project-mismatched file must be skipped: %+v", res.Hits)
	}
	if res.Scanned != 1 {
		t.Fatalf("Scanned=%d want 1 (opened then skipped)", res.Scanned)
	}
}
