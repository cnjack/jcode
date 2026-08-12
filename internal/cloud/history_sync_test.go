package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

func TestProjectSessionHistoryUsesCloudDurableVocabulary(t *testing.T) {
	entries := []session.Entry{
		{Type: session.EntrySessionStart},
		{Type: session.EntryUser, Content: "hello"},
		{Type: session.EntryAssistant, Content: "hi"},
		{Type: session.EntryToolCall, Name: "read", Args: `{"path":"a"}`, ToolCallID: "call-1", BatchID: "b", BatchIndex: 0, BatchSize: 1},
		{Type: session.EntryToolResult, Name: "read", Output: "body", ToolCallID: "call-1", DurationMs: 12},
		{Type: session.EntryModeChange, Mode: "plan"},
		{Type: session.EntryGoalUpdate, GoalObjective: "ship", GoalStatus: "active", GoalTokensUsed: 9},
		{Type: session.EntrySubagentStart, SubagentName: "research", SubagentType: "explore"},
		{Type: session.EntrySubagentResult, SubagentName: "research", Output: "done"},
		{Type: session.EntryArtifact, ArtifactID: "not-rendered"},
	}
	events, err := projectSessionHistory("s1", entries)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []string{"user_message", "agent_message", "tool_call", "tool_result", "mode_changed", "goal_update", "subagent_event", "subagent_event"}
	if len(events) != len(wantKinds) {
		t.Fatalf("projected events = %d, want %d", len(events), len(wantKinds))
	}
	for i, want := range wantKinds {
		if events[i].Kind != want {
			t.Fatalf("events[%d].Kind = %q, want %q", i, events[i].Kind, want)
		}
		var envelope struct {
			Type   string         `json:"type"`
			TaskID string         `json:"task_id"`
			Data   map[string]any `json:"data"`
		}
		if err := json.Unmarshal(events[i].Payload, &envelope); err != nil {
			t.Fatalf("events[%d] payload: %v", i, err)
		}
		if envelope.Type != want || envelope.TaskID != "s1" {
			t.Fatalf("events[%d] envelope = %+v", i, envelope)
		}
	}
}

func TestLoadSessionHistoryStrictRejectsCorruptLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".jcode", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte("{\"type\":\"user\",\"content\":\"ok\"}\nnot-json\n")
	if err := os.WriteFile(filepath.Join(dir, "s1.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSessionHistoryStrict("s1"); err == nil {
		t.Fatal("corrupt JSONL was accepted for cloud backfill")
	}
}

func TestSyncSessionsBackfillsEmptyCloudHistoryBeforeLiveSeq(t *testing.T) {
	mock := newMockCloud()
	server := httptest.NewServer(mock.handler())
	t.Cleanup(server.Close)

	conn := newTestConnector(t, server.URL, "http://127.0.0.1:1")
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{
			"ssh://host/work": {{UUID: "s1", Project: "ssh://host/work", Status: "idle"}},
		}, nil
	}
	conn.cfg.LoadSessionFn = func(id string) ([]session.Entry, error) {
		return []session.Entry{
			{Type: session.EntryUser, Content: "old question"},
			{Type: session.EntryAssistant, Content: "old answer"},
			{Type: session.EntryToolCall, Name: "read", Args: `{}`, ToolCallID: "c1"},
			{Type: session.EntryToolResult, Name: "read", Output: "ok", ToolCallID: "c1"},
		}, nil
	}

	if err := conn.syncSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := mock.allEvents()
	if len(events) != 4 {
		t.Fatalf("uploaded history = %d events, want 4", len(events))
	}
	for i, event := range events {
		if event.Seq != int64(i+1) {
			t.Fatalf("events[%d].Seq = %d, want %d", i, event.Seq, i+1)
		}
	}
	if got := conn.seq.Next("s1"); got != 5 {
		t.Fatalf("first live seq = %d, want 5", got)
	}
	ledger, err := loadHistorySyncLedger(conn.cfg.HistorySyncPath)
	if err != nil {
		t.Fatal(err)
	}
	if record := ledger.Sessions["s1"]; !record.Complete || record.EventCount != 4 || record.NextSeq != 5 {
		t.Fatalf("history ledger = %+v", record)
	}
}

func TestSyncSessionsPreservesUnrelatedPartialCloudHistory(t *testing.T) {
	mock := newMockCloud()
	mock.lastSeq["s1"] = 3
	server := httptest.NewServer(mock.handler())
	t.Cleanup(server.Close)

	conn := newTestConnector(t, server.URL, "http://127.0.0.1:1")
	conn.cfg.HistorySyncPath = filepath.Join(t.TempDir(), historySyncFile)
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{"/p": {{UUID: "s1", Status: "idle"}}}, nil
	}
	conn.cfg.LoadSessionFn = func(string) ([]session.Entry, error) {
		return []session.Entry{{Type: session.EntryUser, Content: "older"}}, nil
	}

	if err := conn.syncSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(mock.allEvents()); got != 0 {
		t.Fatalf("legacy partial history was rewritten with %d events", got)
	}
	if got := conn.seq.Next("s1"); got != 4 {
		t.Fatalf("first live seq = %d, want 4", got)
	}
}

func TestBackfillResumesMatchingCrashLedger(t *testing.T) {
	mock := newMockCloud()
	mock.lastSeq["s1"] = 2
	server := httptest.NewServer(mock.handler())
	t.Cleanup(server.Close)

	conn := newTestConnector(t, server.URL, "http://127.0.0.1:1")
	entries := []session.Entry{
		{Type: session.EntryUser, Content: "one"},
		{Type: session.EntryAssistant, Content: "two"},
		{Type: session.EntryUser, Content: "three"},
	}
	conn.cfg.LoadSessionFn = func(string) ([]session.Entry, error) { return entries, nil }
	events, err := projectSessionHistory("s1", entries)
	if err != nil {
		t.Fatal(err)
	}
	ledger := &historySyncLedger{Sessions: map[string]historySyncRecord{
		"s1": {
			ProjectionVersion: historyProjectionVersion,
			ProjectionHash:    historyProjectionHash(events),
			EventCount:        3,
			NextSeq:           3,
		},
	}}
	if err := saveHistorySyncLedger(conn.cfg.HistorySyncPath, ledger); err != nil {
		t.Fatal(err)
	}

	last, err := conn.backfillSessionHistory(context.Background(), "s1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if last != 3 {
		t.Fatalf("last seq = %d, want 3", last)
	}
	uploaded := mock.allEvents()
	if len(uploaded) != 1 || uploaded[0].Seq != 3 {
		t.Fatalf("resumed uploads = %+v, want only seq 3", uploaded)
	}
}

func TestBackfillStrictlySealsHistoryWhenE2EEIsActive(t *testing.T) {
	mock := newMockCloud()
	server := httptest.NewServer(mock.handler())
	t.Cleanup(server.Close)

	conn := newTestConnector(t, server.URL, "http://127.0.0.1:1")
	cipher, err := NewEnvelopeCipher(bytes.Repeat([]byte{0x5a}, cekSize), 7)
	if err != nil {
		t.Fatal(err)
	}
	conn.setCipher(cipher)
	conn.cfg.LoadSessionFn = func(string) ([]session.Entry, error) {
		return []session.Entry{{Type: session.EntryUser, Content: "secret history"}}, nil
	}

	last, err := conn.backfillSessionHistory(context.Background(), "s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if last != 1 {
		t.Fatalf("last seq = %d, want 1", last)
	}
	uploaded := mock.allEvents()
	if len(uploaded) != 1 || !IsEnvelope(uploaded[0].Payload) {
		t.Fatalf("history payload was not E2EE sealed: %+v", uploaded)
	}
	plain, err := cipher.Open(uploaded[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	if err := json.Unmarshal(plain, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Content != "secret history" {
		t.Fatalf("decrypted content = %q", envelope.Data.Content)
	}
}
