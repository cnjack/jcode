package sessionview

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cnjack/jcode/internal/session"
)

func seedReadSession(t *testing.T, id string, entries ...session.Entry) {
	t.Helper()
	all := append([]session.Entry{mkStart(id, projectA)}, entries...)
	writeSessionEntries(t, id, all)
	addIndexMeta(t, projectA, mkMeta(id, projectA, "2026-10-18T09:00:00+08:00"))
}

func readCaller() Caller {
	return Caller{SessionID: "caller-1", Project: projectA}
}

func TestReadSelfCutsCurrentTurn(t *testing.T) {
	setupHome(t)
	rec, err := session.NewRecorder(projectA, "test-provider", "test-model")
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	rec.RecordUser("first")
	rec.RecordAssistant("reply")
	rec.RecordUser("current turn")
	rec.RecordToolCall("session_read", `{"session_id":""}`, "call-1", "", 0, 0)
	id := rec.UUID()
	rec.Close()

	svc := New(Config{})
	caller := Caller{SessionID: id, Project: projectA}

	res, err := svc.Read(context.Background(), caller, ReadRequest{SessionID: "", Start: 0, Limit: 10})
	if err != nil {
		t.Fatalf("Read self: %v", err)
	}
	// parsed: 1 session_start, 2 user(first), 3 assistant(reply),
	// 4 user(current turn), 5 tool_call(session_read).
	if res.Total != 5 {
		t.Fatalf("Total=%d want 5", res.Total)
	}
	got := entrySeqs(res.Entries)
	if strings.TrimSpace(got) != "2 3" {
		t.Fatalf("self tail seqs=%q want \"2 3\" (entries=%+v)", got, res.Entries)
	}
	if res.FirstSeq != 2 {
		t.Fatalf("FirstSeq=%d want 2", res.FirstSeq)
	}
	for _, view := range res.Entries {
		if strings.Contains(view.Text, "current turn") || strings.Contains(view.Text, "session_id") {
			t.Fatalf("in-flight turn leaked into self read: %+v", view)
		}
	}

	// Explicit start windows obey the same cutoff.
	res, err = svc.Read(context.Background(), caller, ReadRequest{SessionID: "", Start: 3, Limit: 10})
	if err != nil {
		t.Fatalf("Read self start=3: %v", err)
	}
	if got := entrySeqs(res.Entries); strings.TrimSpace(got) != "3" {
		t.Fatalf("start=3 seqs=%q want \"3\"", got)
	}

	// Tail window is ring-bounded: only the newest two entries before the last
	// user survive (but earlier turns are never dropped wholesale).
	res, err = svc.Read(context.Background(), caller, ReadRequest{SessionID: "", Start: 0, Limit: 2})
	if err != nil {
		t.Fatalf("Read self limit=2: %v", err)
	}
	if got := entrySeqs(res.Entries); strings.TrimSpace(got) != "2 3" {
		t.Fatalf("limit=2 seqs=%q want \"2 3\"", got)
	}
}

func TestReadLongEntryContinuation(t *testing.T) {
	setupHome(t)
	const size = 24 * 1024
	big := strings.Repeat("a", size)
	seedReadSession(t, "long-1", mkAssistant(big), mkUser("tail"))
	svc := New(Config{})

	res, err := svc.Read(context.Background(), readCaller(), ReadRequest{SessionID: "long-1", Start: 2, Limit: 1})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("entries=%+v", res.Entries)
	}
	first := res.Entries[0]
	if first.Seq != 2 || first.Type != "assistant" {
		t.Fatalf("entry=%+v", first)
	}
	if len(first.Text) != 16384 {
		t.Fatalf("first chunk=%d want 16384", len(first.Text))
	}
	if first.NextEntryOffset != 16384 {
		t.Fatalf("NextEntryOffset=%d want 16384", first.NextEntryOffset)
	}
	if first.TruncatedBytes != size-16384 {
		t.Fatalf("TruncatedBytes=%d want %d", first.TruncatedBytes, size-16384)
	}
	if !res.Truncated || res.Bytes != 16384 {
		t.Fatalf("truncated=%t bytes=%d", res.Truncated, res.Bytes)
	}

	res2, err := svc.Read(context.Background(), readCaller(), ReadRequest{
		SessionID: "long-1", Start: 2, Limit: 1, EntryOffset: first.NextEntryOffset,
	})
	if err != nil {
		t.Fatalf("Read continuation: %v", err)
	}
	if len(res2.Entries) != 1 {
		t.Fatalf("continuation entries=%+v", res2.Entries)
	}
	second := res2.Entries[0]
	if len(second.Text) != size-16384 {
		t.Fatalf("second chunk=%d want %d", len(second.Text), size-16384)
	}
	if second.NextEntryOffset != 0 || second.TruncatedBytes != 0 {
		t.Fatalf("continuation must complete the entry: %+v", second)
	}
	if first.Text+second.Text != big {
		t.Fatalf("continuation did not reconstruct the entry")
	}
}

// TestReadMultibyteEntryContinuation crosses the 16 KiB first-entry boundary
// inside CJK/emoji text and checks the rune-safe cut plus entry_offset resume.
func TestReadMultibyteEntryContinuation(t *testing.T) {
	setupHome(t)
	original := strings.Repeat("中文🚀", 2000) // 20,000 bytes, 6,000 runes
	seedReadSession(t, "utf8-long", mkUser(original))
	svc := New(Config{})
	caller := readCaller()

	res, err := svc.Read(context.Background(), caller, ReadRequest{SessionID: "utf8-long", Start: 2, Limit: 1})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("entries=%+v", res.Entries)
	}
	first := res.Entries[0]
	if !utf8.ValidString(first.Text) {
		t.Fatalf("first chunk is not valid UTF-8")
	}
	if !strings.HasPrefix(original, first.Text) {
		t.Fatalf("first chunk is not a prefix of the original")
	}
	if first.NextEntryOffset <= 0 {
		t.Fatalf("expected a continuation offset: %+v", first)
	}
	if got := utf8.RuneCountInString(first.Text); got != first.NextEntryOffset {
		t.Fatalf("NextEntryOffset=%d want %d runes in the first chunk", first.NextEntryOffset, got)
	}
	// The cut lands on a rune boundary of the original text.
	if !utf8.RuneStart(original[len(first.Text)]) {
		t.Fatalf("first chunk cut inside a rune")
	}

	res2, err := svc.Read(context.Background(), caller, ReadRequest{
		SessionID: "utf8-long", Start: 2, Limit: 1, EntryOffset: first.NextEntryOffset,
	})
	if err != nil {
		t.Fatalf("Read continuation: %v", err)
	}
	second := res2.Entries[0]
	if !utf8.ValidString(second.Text) {
		t.Fatalf("second chunk is not valid UTF-8")
	}
	if first.Text+second.Text != original {
		t.Fatalf("continuation does not reconstruct the original entry")
	}
	runes := []rune(original)
	if want, got := string(runes[first.NextEntryOffset]), string([]rune(second.Text)[0]); want != got {
		t.Fatalf("second chunk starts with %q want %q", got, want)
	}
	if second.TruncatedBytes != 0 || second.NextEntryOffset != 0 {
		t.Fatalf("continuation must complete the entry: %+v", second)
	}
}

func TestReadBudgetAfterFirstEntry(t *testing.T) {
	setupHome(t)
	big1 := strings.Repeat("b", 10000)
	big2 := strings.Repeat("c", 10000)
	seedReadSession(t, "budget-1", mkAssistant(big1), mkAssistant(big2))
	svc := New(Config{})
	res, err := svc.Read(context.Background(), readCaller(), ReadRequest{SessionID: "budget-1", Start: 0, Limit: 2})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("entries=%d", len(res.Entries))
	}
	if len(res.Entries[0].Text) != 10000 || res.Entries[0].TruncatedBytes != 0 {
		t.Fatalf("first entry must use the whole-call budget: %+v", res.Entries[0])
	}
	second := res.Entries[1]
	if len(second.Text) != 4096 {
		t.Fatalf("second entry text=%d want 4096", len(second.Text))
	}
	if second.NextEntryOffset != 4096 || second.TruncatedBytes != 10000-4096 {
		t.Fatalf("second entry=%+v", second)
	}
	if res.Bytes > 16384 {
		t.Fatalf("Bytes=%d over budget", res.Bytes)
	}
}

func TestReadNoContinuationForStructuredTypes(t *testing.T) {
	setupHome(t)
	args := `{"data":"` + strings.Repeat("x", 10000) + `"}`
	longOutput := strings.Repeat("y", 500) + "\nsecond line"
	seedReadSession(t, "structured-1",
		session.Entry{Type: session.EntryToolCall, Name: "write", Args: args, Timestamp: "2026-10-18T09:00:01+08:00"},
		session.Entry{Type: session.EntryToolResult, Name: "write", Output: longOutput, DurationMs: 12, Timestamp: "2026-10-18T09:00:02+08:00"},
		session.Entry{Type: session.EntrySystemPrompt, Content: "secret prompt", Timestamp: "2026-10-18T09:00:03+08:00"},
	)
	svc := New(Config{})
	res, err := svc.Read(context.Background(), readCaller(), ReadRequest{SessionID: "structured-1", Start: 0, Limit: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	bySeq := map[int]EntryView{}
	for _, view := range res.Entries {
		bySeq[view.Seq] = view
	}
	call := bySeq[2]
	if len(call.Text) > toolCallArgsBytes {
		t.Fatalf("tool_call args=%d over cap", len(call.Text))
	}
	if call.TruncatedBytes == 0 {
		t.Fatalf("tool_call must report truncation: %+v", call)
	}
	if call.NextEntryOffset != 0 {
		t.Fatalf("tool_call must never expose a continuation offset: %+v", call)
	}
	result := bySeq[3]
	if result.NextEntryOffset != 0 {
		t.Fatalf("tool_result must never expose a continuation offset: %+v", result)
	}
	if utf8Len(result.Text) != toolResultHeadRunes {
		t.Fatalf("tool_result head=%d runes want %d", utf8Len(result.Text), toolResultHeadRunes)
	}
	if result.TruncatedBytes == 0 {
		t.Fatalf("tool_result must report the cut bytes: %+v", result)
	}
	if !strings.Contains(result.Meta, "12ms") {
		t.Fatalf("tool_result meta=%q", result.Meta)
	}
	prompt := bySeq[4]
	if prompt.Text != "" || prompt.Meta != "" {
		t.Fatalf("system_prompt must be elided: %+v", prompt)
	}
	// Entry offset is ignored for non-content entries.
	res, err = svc.Read(context.Background(), readCaller(), ReadRequest{
		SessionID: "structured-1", Start: 2, Limit: 1, EntryOffset: 10,
	})
	if err != nil {
		t.Fatalf("Read offset on tool_call: %v", err)
	}
	if len(res.Entries) != 1 || !strings.HasPrefix(res.Entries[0].Text, `{"data":"`) {
		t.Fatalf("offset must be ignored for tool_call: %+v", res.Entries)
	}
}

func TestReadShortToolCallIsNotMarkedTruncated(t *testing.T) {
	setupHome(t)
	args := `{"file_path":"a.go","content":"x"}`
	seedReadSession(t, "short-call",
		session.Entry{Type: session.EntryToolCall, Name: "write", Args: args, Timestamp: "2026-10-18T09:00:01+08:00"},
	)
	svc := New(Config{})
	res, err := svc.Read(context.Background(), readCaller(), ReadRequest{SessionID: "short-call"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("entries=%+v", res.Entries)
	}
	view := res.Entries[0]
	if view.Text != args {
		t.Fatalf("text=%q want %q", view.Text, args)
	}
	if view.TruncatedBytes != 0 || view.NextEntryOffset != 0 || res.Truncated {
		t.Fatalf("complete tool_call must not be marked truncated: %+v", view)
	}
}

func TestReadRangesAndBudgets(t *testing.T) {
	setupHome(t)
	var entries []session.Entry
	for i := 1; i <= 12; i++ {
		entries = append(entries, mkUser("msg-"+string(rune('a'+i))))
	}
	seedReadSession(t, "ranges-1", entries...)
	svc := New(Config{})
	caller := readCaller()

	res, err := svc.Read(context.Background(), caller, ReadRequest{SessionID: "ranges-1"})
	if err != nil {
		t.Fatalf("Read tail: %v", err)
	}
	if res.Total != 13 {
		t.Fatalf("Total=%d want 13", res.Total)
	}
	if len(res.Entries) != 10 {
		t.Fatalf("default limit returned %d entries", len(res.Entries))
	}
	if res.FirstSeq != 4 {
		t.Fatalf("FirstSeq=%d want 4", res.FirstSeq)
	}

	res, err = svc.Read(context.Background(), caller, ReadRequest{SessionID: "ranges-1", Start: 500, Limit: 5})
	if err != nil {
		t.Fatalf("Read past end must not error: %v", err)
	}
	if len(res.Entries) != 0 || res.Total != 13 || res.FirstSeq != 0 {
		t.Fatalf("past-end result=%+v", res)
	}

	res, err = svc.Read(context.Background(), caller, ReadRequest{SessionID: "ranges-1", Start: 1, Limit: 999})
	if err != nil {
		t.Fatalf("Read clamped limit: %v", err)
	}
	if len(res.Entries) != 12 {
		t.Fatalf("entries=%d want 12 (session_start is never rendered)", len(res.Entries))
	}
	if res.FirstSeq != 2 {
		t.Fatalf("FirstSeq=%d want 2", res.FirstSeq)
	}
	if res.Bytes > 16384 {
		t.Fatalf("Bytes=%d over budget", res.Bytes)
	}
}

func TestReadCorruptAndTorn(t *testing.T) {
	setupHome(t)
	data := []byte(`{"type":"session_start","uuid":"ragged","project":"` + projectA + `","timestamp":"2026-10-18T09:00:00+08:00"}` + "\n")
	data = append(data, []byte(`{"type":"user","content":`+"\n")...)
	data = append(data, []byte(`{"type":"user","content":"kept","timestamp":"2026-10-18T09:00:01+08:00"}`+"\n")...)
	data = append(data, []byte(`{"type":"assistant","content":"torn`)...)
	writeRawSession(t, "ragged", data)
	addIndexMeta(t, projectA, mkMeta("ragged", projectA, ""))

	svc := New(Config{})
	res, err := svc.Read(context.Background(), readCaller(), ReadRequest{SessionID: "ragged"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if res.Corrupt != 1 || !res.Torn {
		t.Fatalf("corrupt=%d torn=%t", res.Corrupt, res.Torn)
	}
	if res.Total != 2 {
		t.Fatalf("Total=%d want 2", res.Total)
	}
	if len(res.Entries) != 1 || res.Entries[0].Text != "kept" {
		t.Fatalf("entries=%+v", res.Entries)
	}
}

func TestReadInvalidRange(t *testing.T) {
	setupHome(t)
	seedReadSession(t, "range-1", mkUser("hello"))
	svc := New(Config{})
	tests := []struct {
		name   string
		req    ReadRequest
		detail string
	}{
		{name: "negative start", req: ReadRequest{SessionID: "range-1", Start: -1}, detail: "start must be 0 or greater (0 means the most recent entries)"},
		{name: "negative limit", req: ReadRequest{SessionID: "range-1", Limit: -1}, detail: "limit must be 0 or greater (0 means the default)"},
		{name: "negative offset", req: ReadRequest{SessionID: "range-1", Start: 2, EntryOffset: -1}, detail: "entry_offset must be 0 or greater"},
		{name: "offset without start", req: ReadRequest{SessionID: "range-1", EntryOffset: 5}, detail: "entry_offset requires start to address the entry being continued"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Read(context.Background(), readCaller(), tc.req)
			if !errors.Is(err, ErrInvalidRange) {
				t.Fatalf("want ErrInvalidRange, got %v", err)
			}
			if err.Error() != tc.detail {
				t.Fatalf("detail=%q want %q", err.Error(), tc.detail)
			}
		})
	}
}

func TestReadForbiddenFieldsNeverRendered(t *testing.T) {
	setupHome(t)
	user := session.Entry{
		Type:    session.EntryUser,
		Content: "visible content",
		Images:  []session.EntryImage{{MimeType: "image/png", Data: "IMAGEDATA_CANARY"}},
		OpaqueResponseItems: []json.RawMessage{
			json.RawMessage(`"OPAQUE_CANARY"`),
		},
		Timestamp: "2026-10-18T09:00:01+08:00",
	}
	gen := session.Entry{
		Type:                           session.EntryGenerationOperation,
		OperationID:                    "gen_ab12",
		OperationState:                 "succeeded",
		OperationCapabilityKey:         &session.OperationCapabilityKey{ProviderProfileID: "CAPKEY_CANARY", CredentialKind: "CREDKIND_CANARY", EndpointProfile: "ENDPOINT_CANARY", ModelID: "MODEL_CANARY"},
		OperationCredentialFingerprint: "CRED_CANARY",
		OperationProviderRequestIDHash: "HASH_CANARY",
		Timestamp:                      "2026-10-18T09:00:02+08:00",
	}
	provider := session.Entry{
		Type:                          session.EntryProviderToolOperation,
		ProviderToolState:             "succeeded",
		ProviderToolProviderProfileID: "tavily",
		ProviderToolName:              "search",
		ProviderToolIntentHash:        "TOOLHASH_CANARY",
		ProviderToolIdempotencyKey:    "TOOLKEY_CANARY",
		Timestamp:                     "2026-10-18T09:00:03+08:00",
	}
	future := session.Entry{
		Type:      session.EntryType("future_kind"),
		Content:   "UNKNOWN_CONTENT_CANARY",
		Timestamp: "2026-10-18T09:00:04+08:00",
	}
	seedReadSession(t, "fields-1", user, gen, provider, future)

	svc := New(Config{})
	res, err := svc.Read(context.Background(), readCaller(), ReadRequest{SessionID: "fields-1", Start: 1, Limit: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var all strings.Builder
	for _, view := range res.Entries {
		all.WriteString(view.Text)
		all.WriteString(view.Meta)
	}
	out := all.String()
	for _, canary := range []string{"IMAGEDATA_CANARY", "OPAQUE_CANARY", "CAPKEY_CANARY", "CREDKIND_CANARY",
		"ENDPOINT_CANARY", "MODEL_CANARY", "CRED_CANARY", "HASH_CANARY", "TOOLHASH_CANARY", "TOOLKEY_CANARY"} {
		if strings.Contains(out, canary) {
			t.Fatalf("forbidden field %q leaked into read output: %q", canary, out)
		}
	}
	if !strings.Contains(out, "visible content") {
		t.Fatalf("user content must be rendered: %q", out)
	}
	var unknown *EntryView
	for i := range res.Entries {
		if res.Entries[i].Type == "future_kind" {
			unknown = &res.Entries[i]
		}
	}
	if unknown == nil {
		t.Fatalf("unknown entry type missing: %+v", res.Entries)
	}
	if unknown.Text != "" || !strings.Contains(unknown.Meta, "(unrecognized)") {
		t.Fatalf("unknown type must fail closed: %+v", *unknown)
	}
	if strings.Contains(unknown.Text+unknown.Meta, "UNKNOWN_CONTENT_CANARY") {
		t.Fatalf("unknown type must never fall back to Content")
	}
	if IsKnownEntryType("future_kind") {
		t.Fatalf("future_kind must not be a known type")
	}
	if !IsKnownEntryType("subagent_async") {
		t.Fatalf("subagent_async must be a known type")
	}
}

func TestReadRequiresCaller(t *testing.T) {
	setupHome(t)
	svc := New(Config{})
	if _, err := svc.Read(context.Background(), Caller{}, ReadRequest{}); !errors.Is(err, ErrNoCaller) {
		t.Fatalf("want ErrNoCaller, got %v", err)
	}
}

func entrySeqs(views []EntryView) string {
	parts := make([]string, 0, len(views))
	for _, view := range views {
		parts = append(parts, strconv.Itoa(view.Seq))
	}
	return strings.Join(parts, " ")
}

func utf8Len(s string) int {
	return len([]rune(s))
}
