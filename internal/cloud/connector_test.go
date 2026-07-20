package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/session"
	"github.com/gorilla/websocket"
)

// --- mock orchestrator ---

type mockCloud struct {
	mu           sync.Mutex
	acks         map[string]CommandAck
	eventBatches map[string][]EventUpload
	ephemeral    []ephemeralRecord
	sessionReqs  [][]SessionUpsert
	lastSeq      map[string]int64

	pollScripts [][]DeviceCommand // consumed in order; afterwards 204
	pollCount   atomic.Int64
	pollFail    atomic.Bool

	conflictNextEvents atomic.Bool // next events upload answers all-conflicted
	conflictMaxSeq     int64
}

type ephemeralRecord struct {
	sid     string
	kind    string
	payload json.RawMessage
}

func newMockCloud() *mockCloud {
	return &mockCloud{
		acks:         make(map[string]CommandAck),
		eventBatches: make(map[string][]EventUpload),
		lastSeq:      make(map[string]int64),
	}
}

func (m *mockCloud) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/device/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /internal/v1/device/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /internal/v1/device/poll", func(w http.ResponseWriter, r *http.Request) {
		m.pollCount.Add(1)
		if m.pollFail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		m.mu.Lock()
		var cmds []DeviceCommand
		if len(m.pollScripts) > 0 {
			cmds = m.pollScripts[0]
			m.pollScripts = m.pollScripts[1:]
		}
		m.mu.Unlock()
		if cmds == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"commands": cmds})
	})
	mux.HandleFunc("POST /internal/v1/device/commands/{id}/ack", func(w http.ResponseWriter, r *http.Request) {
		var ack CommandAck
		_ = json.NewDecoder(r.Body).Decode(&ack)
		m.mu.Lock()
		m.acks[r.PathValue("id")] = ack
		m.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /internal/v1/device/sessions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Sessions []SessionUpsert `json:"sessions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.mu.Lock()
		m.sessionReqs = append(m.sessionReqs, req.Sessions)
		resp := SessionsUpsertResponse{}
		for _, s := range req.Sessions {
			resp.Sessions = append(resp.Sessions, SessionSeqInfo{SessionID: s.SessionID, LastSeq: m.lastSeq[s.SessionID]})
		}
		m.mu.Unlock()
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("POST /internal/v1/device/sessions/{sid}/events", func(w http.ResponseWriter, r *http.Request) {
		sid := r.PathValue("sid")
		var req struct {
			Events []EventUpload `json:"events"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if m.conflictNextEvents.Swap(false) {
			seqs := make([]int64, 0, len(req.Events))
			for _, e := range req.Events {
				seqs = append(seqs, e.Seq)
			}
			_ = json.NewEncoder(w).Encode(EventsUploadResponse{
				Accepted:   []int64{},
				Conflicted: seqs,
				MaxSeq:     m.conflictMaxSeq,
			})
			return
		}
		m.mu.Lock()
		m.eventBatches[sid] = append(m.eventBatches[sid], req.Events...)
		m.mu.Unlock()
		seqs := make([]int64, 0, len(req.Events))
		var max int64
		for _, e := range req.Events {
			seqs = append(seqs, e.Seq)
			if e.Seq > max {
				max = e.Seq
			}
		}
		_ = json.NewEncoder(w).Encode(EventsUploadResponse{Accepted: seqs, MaxSeq: max})
	})
	mux.HandleFunc("POST /internal/v1/device/sessions/{sid}/ephemeral", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Kind    string          `json:"kind"`
			Payload json.RawMessage `json:"payload"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.mu.Lock()
		m.ephemeral = append(m.ephemeral, ephemeralRecord{sid: r.PathValue("sid"), kind: req.Kind, payload: req.Payload})
		m.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// allEvents returns every durable event uploaded for the test session ("s1").
func (m *mockCloud) allEvents() []EventUpload {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]EventUpload(nil), m.eventBatches["s1"]...)
}

func (m *mockCloud) ephemeralCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ephemeral)
}

func (m *mockCloud) ackCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.acks)
}

// --- fake local web control plane ---

type fakeLocal struct {
	mu             sync.Mutex
	chatBodies     []map[string]any
	stopBodies     []map[string]any
	approvalBodies []map[string]any
	chatSessionID  string
}

func newFakeLocal(t *testing.T) (*fakeLocal, *httptest.Server) {
	t.Helper()
	f := &fakeLocal{chatSessionID: "sess-new-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/chat", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.chatBodies = append(f.chatBodies, body)
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "processing", "session_id": f.chatSessionID})
	})
	mux.HandleFunc("POST /api/stop", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.stopBodies = append(f.stopBodies, body)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
	})
	mux.HandleFunc("POST /api/approval", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.approvalBodies = append(f.approvalBodies, body)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeLocal) chatBody(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chatBodies[i]
}

// --- helpers ---

func newTestConnector(cloudURL, localBase string) *Connector {
	return NewConnector(ConnectorConfig{
		CloudURL:          cloudURL,
		Credentials:       &Credentials{DeviceID: "dev-1", DeviceToken: "tok", DeviceName: "test"},
		LocalBase:         localBase,
		Version:           "test",
		HeartbeatInterval: time.Hour, // effectively disabled in tests
		PollWait:          20 * time.Millisecond,
		IndexPollInterval: time.Hour,
		BatchWindow:       10 * time.Millisecond,
		BatchMax:          20,
		Backoff:           NewBackoff(5*time.Millisecond, 40*time.Millisecond),
	})
}

// waitFor polls cond until it holds, failing the test after a fixed deadline.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}

func mustPayload(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// --- command execution tests ---

func TestChatSendNewSession(t *testing.T) {
	local, localSrv := newFakeLocal(t)
	conn := newTestConnector("http://127.0.0.1:1", localSrv.URL)

	cmd := DeviceCommand{
		ID:   "cmd-1",
		Kind: "chat.send",
		// no session_id → new session: the local request must NOT carry one
		Payload: mustPayload(t, map[string]any{"text": "hello", "channel": "console"}),
	}
	status, result := conn.executeCommand(context.Background(), cmd)
	if status != "ok" {
		t.Fatalf("status = %q, result = %v", status, result)
	}
	res, ok := result.(map[string]string)
	if !ok || res["session_id"] != local.chatSessionID {
		t.Fatalf("ack result = %v, want session_id %q", result, local.chatSessionID)
	}
	body := local.chatBody(0)
	if body["message"] != "hello" {
		t.Errorf("message = %v, want hello", body["message"])
	}
	if _, has := body["session_id"]; has {
		t.Errorf("new-session chat.send must omit session_id, got %v", body["session_id"])
	}
	if body["source"] != "console" {
		t.Errorf("source = %v, want console (channel passthrough)", body["source"])
	}
}

func TestChatSendExistingSession(t *testing.T) {
	local, localSrv := newFakeLocal(t)
	conn := newTestConnector("http://127.0.0.1:1", localSrv.URL)

	cmd := DeviceCommand{
		ID:        "cmd-2",
		Kind:      "chat.send",
		SessionID: "sess-42",
		Payload: mustPayload(t, map[string]any{
			"text": "go on", "channel": "mobile", "mode": "plan",
			"images": []map[string]string{{"data": "aGk=", "media_type": "image/png"}},
		}),
	}
	status, result := conn.executeCommand(context.Background(), cmd)
	if status != "ok" {
		t.Fatalf("status = %q, result = %v", status, result)
	}
	body := local.chatBody(0)
	if body["session_id"] != "sess-42" {
		t.Errorf("session_id = %v, want sess-42", body["session_id"])
	}
	if body["mode"] != "plan" {
		t.Errorf("mode = %v, want plan", body["mode"])
	}
	if body["source"] != "mobile" {
		t.Errorf("source = %v, want mobile", body["source"])
	}
	imgs, ok := body["images"].([]any)
	if !ok || len(imgs) != 1 {
		t.Errorf("images = %v, want one image", body["images"])
	}
}

func TestChatSendEmptyTextRejected(t *testing.T) {
	_, localSrv := newFakeLocal(t)
	conn := newTestConnector("http://127.0.0.1:1", localSrv.URL)
	cmd := DeviceCommand{ID: "cmd-3", Kind: "chat.send", Payload: mustPayload(t, map[string]any{"text": "  "})}
	if status, _ := conn.executeCommand(context.Background(), cmd); status != "error" {
		t.Fatalf("status = %q, want error for empty text", status)
	}
}

func TestChatStop(t *testing.T) {
	local, localSrv := newFakeLocal(t)
	conn := newTestConnector("http://127.0.0.1:1", localSrv.URL)

	cmd := DeviceCommand{ID: "cmd-4", Kind: "chat.stop", SessionID: "sess-7"}
	status, result := conn.executeCommand(context.Background(), cmd)
	if status != "ok" {
		t.Fatalf("status = %q, result = %v", status, result)
	}
	local.mu.Lock()
	defer local.mu.Unlock()
	if len(local.stopBodies) != 1 || local.stopBodies[0]["task_id"] != "sess-7" {
		t.Fatalf("stop bodies = %v, want one call with task_id sess-7", local.stopBodies)
	}
}

func TestApprovalRespond(t *testing.T) {
	local, localSrv := newFakeLocal(t)
	conn := newTestConnector("http://127.0.0.1:1", localSrv.URL)

	cases := []struct {
		decision              string
		wantApproved, wantAll bool
	}{
		{"approve", true, false},
		{"approve_all", true, true},
		{"deny", false, false},
	}
	for i, tc := range cases {
		cmd := DeviceCommand{
			ID:        fmt.Sprintf("cmd-a%d", i),
			Kind:      "approval.respond",
			SessionID: "sess-9",
			Payload:   mustPayload(t, map[string]any{"approval_id": "approval_3", "decision": tc.decision}),
		}
		status, result := conn.executeCommand(context.Background(), cmd)
		if status != "ok" {
			t.Fatalf("decision %q: status = %q, result = %v", tc.decision, status, result)
		}
		local.mu.Lock()
		ab := local.approvalBodies[len(local.approvalBodies)-1]
		local.mu.Unlock()
		if ab["id"] != "approval_3" || ab["task_id"] != "sess-9" {
			t.Errorf("decision %q: body = %v", tc.decision, ab)
		}
		if ab["approved"] != tc.wantApproved || ab["approve_all"] != tc.wantAll {
			t.Errorf("decision %q: approved=%v approve_all=%v, want %v/%v",
				tc.decision, ab["approved"], ab["approve_all"], tc.wantApproved, tc.wantAll)
		}
	}

	// Unknown decision must fail without touching the local API.
	cmd := DeviceCommand{ID: "cmd-bad", Kind: "approval.respond", Payload: mustPayload(t, map[string]any{"approval_id": "x", "decision": "shrug"})}
	if status, _ := conn.executeCommand(context.Background(), cmd); status != "error" {
		t.Fatalf("unknown decision: status = %q, want error", status)
	}
}

func TestUnknownCommandKind(t *testing.T) {
	conn := newTestConnector("http://127.0.0.1:1", "http://127.0.0.1:1")
	status, result := conn.executeCommand(context.Background(), DeviceCommand{ID: "c", Kind: "nope"})
	if status != "error" {
		t.Fatalf("status = %q, want error", status)
	}
	if !strings.Contains(fmt.Sprint(result), "unknown command kind") {
		t.Fatalf("result = %v, want unknown-kind error", result)
	}
}

// --- poll loop tests ---

func TestPollLoopExecutesAndAcks(t *testing.T) {
	cloud := newMockCloud()
	cloudSrv := httptest.NewServer(cloud.handler())
	t.Cleanup(cloudSrv.Close)
	local, localSrv := newFakeLocal(t)

	cloud.pollScripts = [][]DeviceCommand{{
		{ID: "cmd-1", Kind: "chat.send", Payload: mustPayload(t, map[string]any{"text": "hi", "channel": "console"})},
	}}

	conn := newTestConnector(cloudSrv.URL, localSrv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go conn.pollLoop(ctx)

	waitFor(t, func() bool { return cloud.ackCount() == 1 }, "one ack")
	cloud.mu.Lock()
	ack := cloud.acks["cmd-1"]
	cloud.mu.Unlock()
	if ack.Status != "ok" {
		t.Fatalf("ack status = %q, want ok", ack.Status)
	}
	res, ok := ack.Result.(map[string]any)
	if !ok || res["session_id"] != local.chatSessionID {
		t.Fatalf("ack result = %v, want session_id %q", ack.Result, local.chatSessionID)
	}
}

func TestPollLoopBackoffOnError(t *testing.T) {
	cloud := newMockCloud()
	cloud.pollFail.Store(true)
	cloudSrv := httptest.NewServer(cloud.handler())
	t.Cleanup(cloudSrv.Close)

	conn := newTestConnector(cloudSrv.URL, "http://127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go conn.pollLoop(ctx)

	// With a 5ms→40ms exponential backoff, ~250ms must allow several retries.
	time.Sleep(250 * time.Millisecond)
	if n := cloud.pollCount.Load(); n < 3 {
		t.Fatalf("poll attempts = %d, want >= 3 (backoff retry)", n)
	}
	cancel()
}

// --- event pump tests ---

func wsMsg(t *testing.T, typ, taskID string, data any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": typ, "task_id": taskID, "data": data})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestEventPumpDurableAndEphemeral(t *testing.T) {
	cloud := newMockCloud()
	cloudSrv := httptest.NewServer(cloud.handler())
	t.Cleanup(cloudSrv.Close)

	conn := newTestConnector(cloudSrv.URL, "http://127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	batcher := newEventBatcher(conn)
	go batcher.run(ctx)

	// Durable: user_message, tool_call, task_status (global envelope, id in data).
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "user_message", "s1", map[string]string{"content": "hi", "source": "console"}))
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "tool_call", "s1", map[string]string{"name": "read"}))
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "task_status", "", map[string]any{"task_id": "s1", "status": "running", "running": true}))
	// Ephemeral: token-level deltas.
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_text", "s1", map[string]string{"text": "chunk"}))
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "token_update", "s1", map[string]int64{"total_tokens": 42}))
	// Global non-session event: skipped entirely.
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "mcp_changed", "", map[string]string{"name": "x"}))

	waitFor(t, func() bool { return len(cloud.allEvents()) == 3 }, "3 durable events uploaded")
	waitFor(t, func() bool { return cloud.ephemeralCount() == 2 }, "2 ephemeral events forwarded")

	events := cloud.allEvents()
	for i, ev := range events {
		if ev.Seq != int64(i+1) {
			t.Fatalf("events[%d].Seq = %d, want %d (per-session monotonic from 1)", i, ev.Seq, i+1)
		}
	}
	wantKinds := []string{"user_message", "tool_call", "task_status"}
	for i, ev := range events {
		if ev.Kind != wantKinds[i] {
			t.Errorf("events[%d].Kind = %q, want %q", i, ev.Kind, wantKinds[i])
		}
		var payload map[string]any
		if err := json.Unmarshal(ev.Payload, &payload); err != nil || payload["type"] != wantKinds[i] {
			t.Errorf("events[%d] payload = %s, want original WS message with type %q", i, ev.Payload, wantKinds[i])
		}
	}
}

func TestEventPumpLastSeqResumeAndConflictResync(t *testing.T) {
	cloud := newMockCloud()
	cloudSrv := httptest.NewServer(cloud.handler())
	t.Cleanup(cloudSrv.Close)

	conn := newTestConnector(cloudSrv.URL, "http://127.0.0.1:1")
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{
			"/proj": {{UUID: "s1", Project: "/proj", Status: "idle"}},
		}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Startup upsert: server already holds events up to seq 41 for s1.
	cloud.lastSeq["s1"] = 41
	if err := conn.syncSessions(ctx); err != nil {
		t.Fatalf("syncSessions: %v", err)
	}
	if got := conn.seq.Next("s1"); got != 42 {
		t.Fatalf("first seq after upsert = %d, want 42 (last_seq 41 续号)", got)
	}
	// Rewind the allocator via Seed to replay the scenario through handleWSEvent.
	conn.seq.Seed("s1", 0) // no-op: never moves backwards

	batcher := newEventBatcher(conn)

	// The next batch conflicts (another incarnation beat us): server says
	// max_seq=57, so numbering must resync to 58. flushAll is synchronous, so
	// the Resync has happened by the time it returns — no ticker involved.
	cloud.conflictMaxSeq = 57
	cloud.conflictNextEvents.Store(true)
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "tool_call", "s1", map[string]string{"name": "read"}))
	batcher.flushAll(ctx)
	if cloud.conflictNextEvents.Load() {
		t.Fatal("conflict response was not consumed by the first flush")
	}

	conn.handleWSEvent(ctx, batcher, wsMsg(t, "tool_result", "s1", map[string]string{"name": "read"}))
	batcher.flushAll(ctx)
	if got := len(cloud.allEvents()); got != 1 {
		t.Fatalf("uploaded events after conflict = %d, want 1 (conflicted batch skipped server-side)", got)
	}
	if got := cloud.allEvents()[0].Seq; got != 58 {
		t.Fatalf("seq after conflict = %d, want 58 (max_seq 57 + 1)", got)
	}
}

// TestEventPumpOverRealWS exercises the WS dial + message parse path against a
// real websocket server (the connector speaks the internal/web WS protocol:
// server→client WSEvent JSON, no subscribe needed).
func TestEventPumpOverRealWS(t *testing.T) {
	cloud := newMockCloud()
	cloudSrv := httptest.NewServer(cloud.handler())
	t.Cleanup(cloudSrv.Close)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ws" {
			http.NotFound(w, r)
			return
		}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_ = c.WriteMessage(websocket.TextMessage, wsMsg(t, "user_message", "s1", map[string]string{"content": "hi"}))
		_ = c.WriteMessage(websocket.TextMessage, wsMsg(t, "agent_text", "s1", map[string]string{"text": "delta"}))
		// Keep the socket open until the client goes away.
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(wsSrv.Close)

	conn := newTestConnector(cloudSrv.URL, wsSrv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	batcher := newEventBatcher(conn)
	go batcher.run(ctx)
	go func() { _ = conn.pumpEvents(ctx, batcher) }()

	waitFor(t, func() bool { return len(cloud.allEvents()) == 1 }, "durable event via real WS")
	waitFor(t, func() bool { return cloud.ephemeralCount() == 1 }, "ephemeral event via real WS")
	if got := cloud.allEvents()[0].Seq; got != 1 {
		t.Fatalf("seq = %d, want 1", got)
	}
}

// TestEventPumpSynthesizesAgentMessage pins the durable assistant-text path:
// ephemeral agent_text deltas accumulate per session and, when agent_done
// arrives, the connector synthesizes a durable agent_message event carrying
// the full text, batched right after agent_done. A done with no buffered
// text synthesizes nothing.
func TestEventPumpSynthesizesAgentMessage(t *testing.T) {
	cloud := newMockCloud()
	cloudSrv := httptest.NewServer(cloud.handler())
	t.Cleanup(cloudSrv.Close)

	conn := newTestConnector(cloudSrv.URL, "http://127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	batcher := newEventBatcher(conn)

	// Deltas are ephemeral (still forwarded live) AND buffered.
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_text", "s1", map[string]string{"text": "Hello, "}))
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_text", "s1", map[string]string{"text": "world"}))
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_done", "s1", map[string]any{}))
	batcher.flushAll(ctx)

	events := cloud.allEvents()
	if len(events) != 2 {
		t.Fatalf("uploaded events = %d, want 2 (agent_done + agent_message)", len(events))
	}
	if events[0].Kind != "agent_done" || events[1].Kind != "agent_message" {
		t.Fatalf("kinds = %q, %q; want agent_done followed by agent_message", events[0].Kind, events[1].Kind)
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("seqs = %d, %d; want 1, 2", events[0].Seq, events[1].Seq)
	}
	var payload struct {
		Type string `json:"type"`
		Data struct {
			Text string `json:"text"`
		} `json:"data"`
	}
	if err := json.Unmarshal(events[1].Payload, &payload); err != nil {
		t.Fatalf("agent_message payload: %v", err)
	}
	if payload.Type != "agent_message" || payload.Data.Text != "Hello, world" {
		t.Fatalf("agent_message payload = %s, want data.text %q", events[1].Payload, "Hello, world")
	}

	// The buffer was consumed: a second done without deltas adds no message.
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_done", "s1", map[string]any{}))
	batcher.flushAll(ctx)
	if got := len(cloud.allEvents()); got != 3 {
		t.Fatalf("uploaded events after empty done = %d, want 3 (bare agent_done)", got)
	}
}

// TestEventPumpSessionResetClearsAgentText pins that session_reset drops any
// buffered deltas, so a reset followed by done synthesizes no agent_message.
func TestEventPumpSessionResetClearsAgentText(t *testing.T) {
	cloud := newMockCloud()
	cloudSrv := httptest.NewServer(cloud.handler())
	t.Cleanup(cloudSrv.Close)

	conn := newTestConnector(cloudSrv.URL, "http://127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	batcher := newEventBatcher(conn)
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_text", "s1", map[string]string{"text": "partial"}))
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "session_reset", "s1", map[string]any{}))
	conn.handleWSEvent(ctx, batcher, wsMsg(t, "agent_done", "s1", map[string]any{}))
	batcher.flushAll(ctx)

	for _, ev := range cloud.allEvents() {
		if ev.Kind == "agent_message" {
			t.Fatalf("agent_message uploaded after session_reset cleared the buffer: %s", ev.Payload)
		}
	}
}

// TestAgentTextBufferCap pins the 256KB per-session cap: the buffer stops
// growing past the cap and take marks the text truncated.
func TestAgentTextBufferCap(t *testing.T) {
	bufs := newAgentTextBuffers()
	bufs.append("s1", strings.Repeat("a", agentTextBufCap+1024))
	text := bufs.take("s1")
	if !strings.HasSuffix(text, "[…truncated by the device connector at 256KB]") {
		t.Fatalf("capped text missing truncation marker (len=%d)", len(text))
	}
	if !strings.HasPrefix(text, strings.Repeat("a", agentTextBufCap)) {
		t.Fatal("capped text must keep the first 256KB of deltas")
	}
	// take cleared the buffer.
	if got := bufs.take("s1"); got != "" {
		t.Fatalf("second take = %q, want empty", got)
	}
}

// --- session sync tests ---

func TestSyncSessionsUpsert(t *testing.T) {
	cloud := newMockCloud()
	cloudSrv := httptest.NewServer(cloud.handler())
	t.Cleanup(cloudSrv.Close)

	conn := newTestConnector(cloudSrv.URL, "http://127.0.0.1:1")
	conn.cfg.ListSessionsFn = func() (map[string][]session.SessionMeta, error) {
		return map[string][]session.SessionMeta{
			"/proj": {
				{UUID: "s1", Project: "/proj", Model: "m1", Status: "running", Title: "t1"},
				{UUID: "s2", Project: "/proj", Model: "m1", Status: "idle"},
			},
		}, nil
	}
	cloud.lastSeq["s1"] = 7

	if err := conn.syncSessions(context.Background()); err != nil {
		t.Fatalf("syncSessions: %v", err)
	}

	cloud.mu.Lock()
	reqs := cloud.sessionReqs
	cloud.mu.Unlock()
	if len(reqs) != 1 || len(reqs[0]) != 2 {
		t.Fatalf("session upserts = %v, want one round with 2 sessions", reqs)
	}
	byID := map[string]SessionUpsert{}
	for _, s := range reqs[0] {
		byID[s.SessionID] = s
	}
	if byID["s1"].Status != "running" {
		t.Errorf("s1 status = %q, want running", byID["s1"].Status)
	}
	if byID["s2"].Status != "idle" {
		t.Errorf("s2 status = %q, want idle", byID["s2"].Status)
	}
	// Meta is the SessionMeta JSON, as-is.
	var meta session.SessionMeta
	if err := json.Unmarshal(byID["s1"].Meta, &meta); err != nil || meta.Title != "t1" || meta.Model != "m1" {
		t.Errorf("s1 meta = %s, want SessionMeta JSON with title t1", byID["s1"].Meta)
	}
	// last_seq seeds the allocator (续号).
	if got := conn.seq.Next("s1"); got != 8 {
		t.Errorf("seq after upsert = %d, want 8 (last_seq 7 + 1)", got)
	}
	if got := conn.seq.Next("s2"); got != 1 {
		t.Errorf("seq for unseen session = %d, want 1", got)
	}
}

// --- startup gate ---

func TestShouldConnect(t *testing.T) {
	creds := &Credentials{DeviceToken: "tok"}
	if !ShouldConnect(true, creds) {
		t.Error("logged in + auto_connect → should connect")
	}
	if ShouldConnect(false, creds) {
		t.Error("auto_connect=false → must not connect")
	}
	if ShouldConnect(true, nil) {
		t.Error("not logged in → must not connect")
	}
	if ShouldConnect(true, &Credentials{}) {
		t.Error("empty device token → must not connect")
	}
}
