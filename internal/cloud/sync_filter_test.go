// sync_filter_test.go covers the M19 per-session sync gate in the connector:
// session-upsert filtering, event-pump filtering (durable + ephemeral, no seq
// allocation for dropped events), and the enable/disable switch points.
package cloud

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/cnjack/jcode/internal/session"
)

func TestSessionLastActivityAtPrefersUpdatedAtAndFallsBackToStartTime(t *testing.T) {
	if got := sessionLastActivityAt(session.SessionMeta{
		StartTime: "2026-07-23T16:00:00Z",
		UpdatedAt: "2026-07-24T01:30:00+08:00",
	}); got != "2026-07-23T17:30:00Z" {
		t.Fatalf("updated_at activity = %q, want UTC normalized updated_at", got)
	}
	if got := sessionLastActivityAt(session.SessionMeta{
		StartTime: "2026-07-24T01:30:00+08:00",
	}); got != "2026-07-23T17:30:00Z" {
		t.Fatalf("start_time activity = %q, want UTC normalized fallback", got)
	}
}

// collectSessions upserts only sessions with an explicit opt-in.
func TestCollectSessionsFiltersUnsynced(t *testing.T) {
	conn := newTestConnector(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{
			"/proj": {
				{UUID: "s1", Status: "idle"}, // enabled via newTestConnector store
				{UUID: "s3", Status: "idle"}, // unset → dropped
			},
		}, nil
	}
	// s2 has an entry in the default test store; make it explicit-disabled.
	if err := conn.syncStore.Set("s2", false); err != nil {
		t.Fatal(err)
	}
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{
			"/proj": {
				{UUID: "s1", Status: "idle"},
				{UUID: "s2", Status: "idle"}, // explicit disabled → dropped
				{UUID: "s3", Status: "idle"}, // unset → dropped
			},
		}, nil
	}

	upserts, err := conn.collectSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(upserts) != 1 || upserts[0].SessionID != "s1" {
		t.Fatalf("upserts = %+v, want only s1", upserts)
	}
}

func TestSyncSessionsSendsReplacementSnapshot(t *testing.T) {
	mock := newMockCloud()
	cloudSrv := httptest.NewServer(mock.handler())
	t.Cleanup(cloudSrv.Close)
	conn := newTestConnector(t, cloudSrv.URL, "http://127.0.0.1:1")
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{"/proj": {{UUID: "s1", Status: "idle"}}}, nil
	}
	if err := conn.syncSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.replaceReqs) != 1 || !mock.replaceReqs[0] {
		t.Fatalf("replace flags = %v, want [true]", mock.replaceReqs)
	}
}

// A nil/failed store fails closed: nothing is upserted.
func TestCollectSessionsFailsClosedWithoutStore(t *testing.T) {
	conn := NewConnector(ConnectorConfig{
		CloudURL:    "http://127.0.0.1:1",
		Credentials: &Credentials{DeviceID: "dev-1", DeviceToken: "tok"},
		LocalBase:   "http://127.0.0.1:1",
		SyncStore:   nil, // lazy load would hit the real HOME; force the failed state
	})
	conn.syncStoreTried = true // simulate "load attempted and failed"
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{"/proj": {{UUID: "s1"}}}, nil
	}
	upserts, err := conn.collectSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(upserts) != 0 {
		t.Fatalf("upserts = %+v, want empty (fail closed)", upserts)
	}
}

// The event pump drops durable AND ephemeral events of unsynced sessions and
// does NOT allocate seq numbers for them.
func TestEventPumpFiltersUnsynced(t *testing.T) {
	mock := newMockCloud()
	cloudSrv := httptest.NewServer(mock.handler())
	t.Cleanup(cloudSrv.Close)

	conn := newTestConnector(t, cloudSrv.URL, "http://127.0.0.1:1") // s1 enabled
	batcher := newEventBatcher(conn)
	ctx := context.Background()

	// s3 (unset): durable and ephemeral events both dropped.
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "tool_call", "s3", map[string]string{"name": "read"}))
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_text", "s3", map[string]string{"text": "x"}))
	// s1 (enabled): one durable event uploads.
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "tool_call", "s1", map[string]string{"name": "read"}))
	batcher.flushAll(ctx)

	if got := mock.allEvents(); len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("uploaded events = %+v, want exactly s1/seq=1", got)
	}
	mock.mu.Lock()
	eph := len(mock.ephemeral)
	mock.mu.Unlock()
	if eph != 0 {
		t.Errorf("ephemeral uploads = %d, want 0 (s3 dropped)", eph)
	}
	// No seq was allocated for the dropped session.
	if got := conn.seq.Next("s3"); got != 1 {
		t.Errorf("dropped session seq advanced to %d, want untouched (Next=1)", got)
	}
}

// Switch points: enabled→disabled stops uploads immediately (session snapshot
// reconciliation owns the separate cloud deletion); disabled→enabled resumes
// from that point on with a gapless seq stream (the disabled window leaves no
// holes) and no historical backfill.
func TestEventPumpSyncToggleMidStream(t *testing.T) {
	mock := newMockCloud()
	cloudSrv := httptest.NewServer(mock.handler())
	t.Cleanup(cloudSrv.Close)

	conn := newTestConnector(t, cloudSrv.URL, "http://127.0.0.1:1") // s1 enabled
	batcher := newEventBatcher(conn)
	ctx := context.Background()

	send := func() {
		conn.handleWSEvent(ctx, batcher, wsMsg(t, "tool_call", "s1", map[string]string{"name": "read"}))
		batcher.flushAll(ctx)
	}

	send() // enabled: uploads seq 1
	if err := conn.syncStore.Set("s1", false); err != nil {
		t.Fatal(err)
	}
	send() // disabled: dropped, no seq allocated
	if err := conn.syncStore.Set("s1", true); err != nil {
		t.Fatal(err)
	}
	send() // re-enabled: uploads seq 2 (no backfill, no gap)

	got := mock.allEvents()
	if len(got) != 2 {
		t.Fatalf("uploaded %d events, want 2 (the disabled window dropped one)", len(got))
	}
	if got[0].Seq != 1 || got[1].Seq != 2 {
		t.Errorf("seqs = [%d %d], want [1 2] (gapless across the toggle)", got[0].Seq, got[1].Seq)
	}
}

// Disabling mid-run also drops the accumulated agent_text buffer, so a
// re-enabled run's synthesized agent_message only covers text streamed after
// the re-enable point.
func TestEventPumpDisableDropsTextBuffer(t *testing.T) {
	conn := newTestConnector(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	batcher := newEventBatcher(conn)
	ctx := context.Background()

	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_text", "s1", map[string]string{"text": "before-disable"}))
	if err := conn.syncStore.Set("s1", false); err != nil {
		t.Fatal(err)
	}
	// Any event while disabled clears the buffer.
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_text", "s1", map[string]string{"text": "while-disabled"}))
	if err := conn.syncStore.Set("s1", true); err != nil {
		t.Fatal(err)
	}
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_text", "s1", map[string]string{"text": "after-enable"}))
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_done", "s1", map[string]any{}))

	var synth *EventUpload
	batcher.mu.Lock()
	for _, ev := range batcher.pending["s1"] {
		if ev.Kind == "agent_message" {
			copy := ev
			synth = &copy
		}
	}
	batcher.mu.Unlock()
	if synth == nil {
		t.Fatal("agent_done must synthesize an agent_message from post-enable text")
	}
	var msg struct {
		Data struct {
			Text string `json:"text"`
		} `json:"data"`
	}
	if err := json.Unmarshal(synth.Payload, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Data.Text != "after-enable" {
		t.Errorf("synthesized text = %q, want %q (pre-disable buffer dropped)", msg.Data.Text, "after-enable")
	}
}
