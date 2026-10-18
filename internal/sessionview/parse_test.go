package sessionview

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

func scanAll(t *testing.T, svc *Service, id string) ([]seqEntry, parseStats) {
	t.Helper()
	f := openSessionFile(t, id)
	var got []seqEntry
	stats, err := svc.scanFile(context.Background(), f, scanOptions{}, func(seq int, e session.Entry) bool {
		got = append(got, seqEntry{seq: seq, e: e})
		return true
	})
	if err != nil {
		t.Fatalf("scanFile: %v", err)
	}
	return got, stats
}

func rawJSON(t *testing.T, e session.Entry) []byte {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestParseSessionTolerant(t *testing.T) {
	start := mkStart("tolerant", projectA)
	user := mkUser("hello")
	assistant := mkAssistant("world")
	corrupt := []byte(`{"type":"user","content":`)

	tests := []struct {
		name        string
		data        []byte
		wantTotal   int
		wantCorrupt int
		wantTorn    bool
		wantStart   bool
	}{
		{name: "empty file"},
		{
			name:      "only session_start",
			data:      append(rawJSON(t, start), '\n'),
			wantTotal: 1, wantStart: true,
		},
		{
			name: "corrupt middle line",
			data: append(bytes.Join([][]byte{
				rawJSON(t, start), corrupt, rawJSON(t, user), rawJSON(t, assistant),
			}, []byte("\n")), '\n'),
			wantTotal: 3, wantCorrupt: 1, wantStart: true,
		},
		{
			name: "torn tail line is dropped",
			data: append(bytes.Join([][]byte{
				rawJSON(t, start), rawJSON(t, user),
			}, []byte("\n")), []byte("\n{\"type\":\"assistant\",\"content\":\"torn")...),
			wantTotal: 2, wantTorn: true, wantStart: true,
		},
		{
			name:      "blank lines ignored",
			data:      []byte("\n\n" + string(rawJSON(t, start)) + "\n\n\n" + string(rawJSON(t, user)) + "\n\n"),
			wantTotal: 2, wantStart: true,
		},
		{
			name: "oversized line skipped",
			data: append(bytes.Join([][]byte{
				rawJSON(t, start),
				bytes.Repeat([]byte("x"), maxLineBytes+1024),
				rawJSON(t, assistant),
			}, []byte("\n")), '\n'),
			wantTotal: 2, wantCorrupt: 1, wantStart: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupHome(t)
			writeRawSession(t, "tolerant", tc.data)
			svc := New(Config{})
			_, stats := scanAll(t, svc, "tolerant")
			if stats.Total != tc.wantTotal {
				t.Fatalf("Total=%d want %d", stats.Total, tc.wantTotal)
			}
			if stats.Corrupt != tc.wantCorrupt {
				t.Fatalf("Corrupt=%d want %d", stats.Corrupt, tc.wantCorrupt)
			}
			if stats.Torn != tc.wantTorn {
				t.Fatalf("Torn=%t want %t", stats.Torn, tc.wantTorn)
			}
			if stats.HasStart != tc.wantStart {
				t.Fatalf("HasStart=%t want %t", stats.HasStart, tc.wantStart)
			}
			if tc.wantStart && stats.Start.UUID != "tolerant" {
				t.Fatalf("Start.UUID=%q", stats.Start.UUID)
			}
		})
	}
}

func TestParseMatchesLoadSession(t *testing.T) {
	setupHome(t)
	start := mkStart("fixture", projectA)
	user := mkUser("hello")
	assistant := mkAssistant("world")
	data := bytes.Join([][]byte{
		rawJSON(t, start),
		{},
		rawJSON(t, user),
		[]byte(`{"type":"user","content":`),
		rawJSON(t, assistant),
	}, []byte("\r\n"))
	data = append(data, []byte("\r\n")...)
	writeRawSession(t, "fixture", data)

	entries, err := session.LoadSession("fixture")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	svc := New(Config{})
	got, stats := scanAll(t, svc, "fixture")
	if stats.Total != len(entries) {
		t.Fatalf("parse Total=%d LoadSession=%d", stats.Total, len(entries))
	}
	if stats.Corrupt != 1 {
		t.Fatalf("Corrupt=%d want 1", stats.Corrupt)
	}
	if len(got) != len(entries) {
		t.Fatalf("visit count=%d want %d", len(got), len(entries))
	}
	for i, se := range got {
		if entries[i].Type != se.e.Type || entries[i].Content != se.e.Content || entries[i].Summary != se.e.Summary {
			t.Fatalf("entry %d mismatch: parse=%+v load=%+v", i, se.e, entries[i])
		}
	}
}

func TestParseSeqOrdinalsAreContiguous(t *testing.T) {
	setupHome(t)
	data := bytes.Join([][]byte{
		rawJSON(t, mkStart("seqs", projectA)),
		[]byte(`not-json`),
		rawJSON(t, mkUser("one")),
		rawJSON(t, mkAssistant("two")),
	}, []byte("\n"))
	writeRawSession(t, "seqs", append(data, '\n'))
	svc := New(Config{})
	got, stats := scanAll(t, svc, "seqs")
	if stats.Corrupt != 1 {
		t.Fatalf("Corrupt=%d", stats.Corrupt)
	}
	for i, se := range got {
		if se.seq != i+1 {
			t.Fatalf("entry %d has seq %d", i, se.seq)
		}
	}
	if len(got) != 3 {
		t.Fatalf("entries=%d want 3", len(got))
	}
}

func TestParsePrefilterKeepsOrdinals(t *testing.T) {
	setupHome(t)
	target := mkUser("needle value")
	data := bytes.Join([][]byte{
		rawJSON(t, mkStart("prefilter", projectA)),
		rawJSON(t, mkUser("haystack without the word")),
		rawJSON(t, target),
		[]byte(`broken`),
		rawJSON(t, mkAssistant("another entry")),
	}, []byte("\n"))
	writeRawSession(t, "prefilter", append(data, '\n'))
	svc := New(Config{})
	f := openSessionFile(t, "prefilter")
	needle, folded := searchNeedles("needle")
	var seqs []int
	stats, err := svc.scanFile(context.Background(), f, scanOptions{prefilter: func(line []byte) bool {
		return bytes.Contains(line, needle) || bytes.Contains(asciiLowerBytes(line), folded)
	}}, func(seq int, e session.Entry) bool {
		seqs = append(seqs, seq)
		return true
	})
	if err != nil {
		t.Fatalf("scanFile: %v", err)
	}
	// The matching entry must keep its full-decode ordinal (3), and the broken
	// line must still be counted as corrupt.
	if len(seqs) != 1 || seqs[0] != 3 {
		t.Fatalf("seqs=%v want [3]", seqs)
	}
	if stats.Corrupt != 1 {
		t.Fatalf("Corrupt=%d want 1", stats.Corrupt)
	}
	if stats.Total != 4 {
		t.Fatalf("Total=%d want 4", stats.Total)
	}
}

// TestParsePrefilterMatchesDecodeOrdinals pins N2: the prefilter must count a
// skipped line with the same judgment as the decode path (json.Unmarshal into
// Entry), so search seq numbers address the same entries read reports.
func TestParsePrefilterMatchesDecodeOrdinals(t *testing.T) {
	setupHome(t)
	user := mkUser("the needle lives here")
	data := bytes.Join([][]byte{
		rawJSON(t, mkStart("mixed", projectA)),
		[]byte(`123`),        // JSON-valid scalar: not an Entry
		[]byte(`[1,2,3]`),    // JSON-valid array: not an Entry
		rawJSON(t, user),     // seq 2 in both paths
		[]byte(`"a scalar"`), // JSON-valid string: not an Entry
	}, []byte("\n"))
	writeRawSession(t, "mixed", append(data, '\n'))
	addIndexMeta(t, projectA, mkMeta("mixed", projectA, "2026-10-18T09:00:00+08:00"))

	svc := New(Config{})
	ctx := context.Background()

	// Decode path (what session_read counts).
	var decoded []seqEntry
	stats, err := svc.scanFile(ctx, openSessionFile(t, "mixed"), scanOptions{}, func(seq int, e session.Entry) bool {
		decoded = append(decoded, seqEntry{seq: seq, e: e})
		return true
	})
	if err != nil {
		t.Fatalf("decode scan: %v", err)
	}
	if stats.Total != 2 || stats.Corrupt != 3 {
		t.Fatalf("decode path total=%d corrupt=%d want 2/3", stats.Total, stats.Corrupt)
	}
	if len(decoded) != 2 || decoded[1].seq != 2 {
		t.Fatalf("decoded=%+v", decoded)
	}

	// Prefilter path (what session_search counts).
	needle, folded := searchNeedles("needle")
	var visited []int
	prefiltered, err := svc.scanFile(ctx, openSessionFile(t, "mixed"), scanOptions{prefilter: func(line []byte) bool {
		return bytes.Contains(line, needle) || bytes.Contains(asciiLowerBytes(line), folded)
	}}, func(seq int, e session.Entry) bool {
		visited = append(visited, seq)
		return true
	})
	if err != nil {
		t.Fatalf("prefilter scan: %v", err)
	}
	if len(visited) != 1 || visited[0] != 2 {
		t.Fatalf("prefilter visited=%v want [2]", visited)
	}
	if prefiltered.Total != stats.Total || prefiltered.Corrupt != stats.Corrupt {
		t.Fatalf("prefilter total/corrupt=%d/%d decode=%d/%d",
			prefiltered.Total, prefiltered.Corrupt, stats.Total, stats.Corrupt)
	}

	// End to end: the search hit's seq must address the same entry in Read.
	search, err := svc.Search(ctx, Caller{SessionID: "caller-1", Project: projectA}, Query{Text: "needle"}, SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(search.Hits) != 1 || search.Hits[0].Seq != 2 {
		t.Fatalf("hits=%+v want one hit at seq 2", search.Hits)
	}
	read, err := svc.Read(ctx, readCaller(), ReadRequest{SessionID: "mixed", Start: search.Hits[0].Seq, Limit: 1})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(read.Entries) != 1 || read.Entries[0].Seq != search.Hits[0].Seq || read.Entries[0].Text != user.Content {
		t.Fatalf("read=%+v does not match hit seq %d", read.Entries, search.Hits[0].Seq)
	}
}

func TestParseTornTailWithValidJSONFragmentIsDropped(t *testing.T) {
	setupHome(t)
	fragment := rawJSON(t, mkUser("complete but unterminated"))
	data := append(rawJSON(t, mkStart("torn-valid", projectA)), '\n')
	data = append(data, fragment...)
	writeRawSession(t, "torn-valid", data)
	svc := New(Config{})
	got, stats := scanAll(t, svc, "torn-valid")
	if !stats.Torn {
		t.Fatalf("Torn must be reported")
	}
	if stats.Total != 1 || len(got) != 1 {
		t.Fatalf("torn fragment must not count: total=%d entries=%d", stats.Total, len(got))
	}
}

func TestReadFirstEntrySkipsBlankLines(t *testing.T) {
	setupHome(t)
	data := []byte("\n\n" + string(rawJSON(t, mkStart("first", projectA))) + "\n" + string(rawJSON(t, mkUser("later"))) + "\n")
	writeRawSession(t, "first", data)
	f, err := os.Open(sessionsDir(t) + "/first.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	entry, ok, err := readFirstEntry(f)
	if err != nil || !ok {
		t.Fatalf("readFirstEntry: ok=%t err=%v", ok, err)
	}
	if entry.Type != session.EntrySessionStart || entry.UUID != "first" {
		t.Fatalf("entry=%+v", entry)
	}
	if !strings.HasPrefix(string(data), "\n\n") {
		t.Fatal("fixture sanity")
	}
}
