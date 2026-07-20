// events.go is the connector's uplink event path: it subscribes to the local
// web server's WS event stream (/api/ws), classifies each event as durable or
// ephemeral, assigns per-session seq numbers, and uploads — durable events in
// batches (persisted by the orchestrator for offline replay), ephemeral events
// fire-and-forget (SSE fanout only).
package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// eventDurability is the explicit durable/ephemeral classification of every WS
// event type emitted by internal/web (ws.go broadcast sites) and
// internal/handler (WebHandler emit sites). Kinds are the WS event `type`
// strings, uploaded as-is.
//
// Durable = complete message-level / state-change events: the orchestrator
// persists them and remote clients replay them after a reconnect. Ephemeral =
// token-level increments that only make sense live; they ride the SSE fanout
// and are never stored.
//
// Any type NOT listed here defaults to DURABLE — 宁可多存不可丢.
var eventDurability = map[string]bool{
	// --- durable: complete message-level events ---
	"user_message":     true, // user message (carries the channel source marker)
	"tool_call":        true, // tool invocation (name + args)
	"tool_result":      true, // tool completion (output / error / denied)
	"approval_request": true, // approval prompt awaiting a decision
	"ask_user_request": true, // ask_user question awaiting an answer
	// --- durable: session state changes ---
	"agent_start":   true, // run started (session busy)
	"agent_done":    true, // run finished / stopped / failed (also the "error" carrier)
	"task_status":   true, // idle/running flip (global envelope; task_id lives in data)
	"todo_update":   true, // todo list changed
	"goal_update":   true, // session goal changed
	"session_reset": true, // session history reset
	"mode_changed":  true, // session mode switch (build/plan/full_access)
	"model_changed": true, // session model switch
	// --- durable: subagent lifecycle (done carries the final result) ---
	"subagent_event": true,
	// --- ephemeral: token-level increments (lossy by design) ---
	"agent_text":        false, // streaming assistant text chunk; the full message is replayable from the session file and the terminal state rides agent_done
	"token_update":      false, // cumulative token counters, fire after every LLM call
	"subagent_progress": false, // intermediate subagent tool progress lines
}

// isDurableEvent reports whether a WS event type is uploaded as a durable
// event. Unknown types default to durable.
func isDurableEvent(eventType string) bool {
	durable, known := eventDurability[eventType]
	return !known || durable
}

// wsEvent mirrors web.WSEvent on the wire (the connector is a pure client and
// must not import internal/web).
type wsEvent struct {
	Type   string          `json:"type"`
	TaskID string          `json:"task_id,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// seqAllocator hands out per-session monotonically increasing seq numbers
// (1-based). It is seeded from the server's per-session last_seq at startup /
// on every session upsert, and resynced to the server's max_seq whenever an
// upload reports a conflict.
type seqAllocator struct {
	mu   sync.Mutex
	next map[string]int64
}

func newSeqAllocator() *seqAllocator {
	return &seqAllocator{next: make(map[string]int64)}
}

// Next assigns the next seq for sid.
func (a *seqAllocator) Next(sid string) int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := a.next[sid]
	if n < 1 {
		n = 1
	}
	a.next[sid] = n + 1
	return n
}

// Seed resumes numbering after the server's last_seq, never moving backwards.
func (a *seqAllocator) Seed(sid string, lastSeq int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if lastSeq+1 > a.next[sid] {
		a.next[sid] = lastSeq + 1
	}
}

// Resync restarts numbering after the server's max_seq following an upload
// conflict (some of our seqs were already taken by an earlier incarnation).
func (a *seqAllocator) Resync(sid string, maxSeq int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.next[sid] = maxSeq + 1
}

// eventBatcher accumulates durable events per session and flushes them on a
// time window or when a session's buffer reaches the size cap.
type eventBatcher struct {
	c      *Connector
	window time.Duration
	max    int

	mu      sync.Mutex
	pending map[string][]EventUpload
}

// requeueCap bounds how many undelivered events one session may buffer (e.g.
// during a cloud outage); beyond it the oldest are dropped with a warning.
const requeueCap = 500

func newEventBatcher(c *Connector) *eventBatcher {
	return &eventBatcher{
		c:       c,
		window:  c.batchWindow(),
		max:     c.batchMax(),
		pending: make(map[string][]EventUpload),
	}
}

// add buffers one event, flushing the session's batch inline when it hits the
// size cap.
func (b *eventBatcher) add(ctx context.Context, sid string, ev EventUpload) {
	b.mu.Lock()
	b.pending[sid] = append(b.pending[sid], ev)
	full := len(b.pending[sid]) >= b.max
	var batch []EventUpload
	if full {
		batch = b.pending[sid]
		delete(b.pending, sid)
	}
	b.mu.Unlock()
	if full {
		b.upload(ctx, sid, batch)
	}
}

// run flushes all non-empty batches every window until ctx is done.
func (b *eventBatcher) run(ctx context.Context) {
	ticker := time.NewTicker(b.window)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.flushAll(ctx)
		}
	}
}

func (b *eventBatcher) flushAll(ctx context.Context) {
	b.mu.Lock()
	batches := b.pending
	b.pending = make(map[string][]EventUpload)
	b.mu.Unlock()
	for sid, batch := range batches {
		if len(batch) > 0 {
			b.upload(ctx, sid, batch)
		}
	}
}

// upload POSTs one batch. Conflicted seqs are skipped server-side and the
// allocator resyncs to the server's max_seq. On transport/HTTP failure the
// batch is requeued (front, capped) so the next tick retries it.
func (b *eventBatcher) upload(ctx context.Context, sid string, batch []EventUpload) {
	resp, err := b.c.client.UploadEvents(ctx, b.c.token, sid, batch)
	if err != nil {
		if ctx.Err() == nil {
			b.c.logf("event upload for session %s failed (%d events, will retry): %v", sid, len(batch), err)
		}
		b.mu.Lock()
		kept := b.pending[sid]
		b.pending[sid] = append(batch, kept...)
		if len(b.pending[sid]) > requeueCap {
			dropped := len(b.pending[sid]) - requeueCap
			b.pending[sid] = b.pending[sid][dropped:]
			b.c.logf("event buffer for session %s overflowed, dropped %d oldest events", sid, dropped)
		}
		b.mu.Unlock()
		return
	}
	if len(resp.Conflicted) > 0 {
		b.c.logf("event upload for session %s: %d accepted, %d conflicted (max_seq=%d), resyncing",
			sid, len(resp.Accepted), len(resp.Conflicted), resp.MaxSeq)
		b.c.seq.Resync(sid, resp.MaxSeq)
	}
}

// eventPumpLoop maintains the local WS subscription, reconnecting with
// backoff whenever the stream drops.
func (c *Connector) eventPumpLoop(ctx context.Context) {
	bo := c.backoff()
	batcher := newEventBatcher(c)
	go batcher.run(ctx)
	for {
		err := c.pumpEvents(ctx, batcher)
		if ctx.Err() != nil {
			return
		}
		c.logf("local event stream disconnected: %v", err)
		bo.Reset() // a live connection resets the backoff before the next wait
		if werr := bo.Wait(ctx); werr != nil {
			return
		}
	}
}

// pumpEvents runs one WS connection until it breaks.
func (c *Connector) pumpEvents(ctx context.Context, batcher *eventBatcher) error {
	wsURL := "ws" + strings.TrimPrefix(c.cfg.LocalBase, "http") + "/api/ws"
	header := http.Header{}
	if c.cfg.LocalToken != "" {
		// Same bearer-via-subprotocol handshake the browser uses (see
		// internal/web/auth.go): ["jcode-auth", "<token>"].
		header.Set("Sec-WebSocket-Protocol", "jcode-auth, "+c.cfg.LocalToken)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	// Unblock ReadMessage on shutdown.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	c.logf("subscribed to local event stream %s", wsURL)
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		c.handleWSEvent(ctx, batcher, msg)
	}
}

// handleWSEvent classifies and routes one local WS event uplink.
func (c *Connector) handleWSEvent(ctx context.Context, batcher *eventBatcher, msg []byte) {
	var ev wsEvent
	if err := json.Unmarshal(msg, &ev); err != nil || ev.Type == "" {
		return
	}
	// Resolve the owning session. Task-tagged events carry it on the envelope;
	// task_status is a global envelope with the id inside data. Everything else
	// global (mcp_changed, pong, model_changed without task, …) has no session
	// to key seq/upload on and is skipped.
	sid := ev.TaskID
	if sid == "" && ev.Type == "task_status" {
		var d struct {
			TaskID string `json:"task_id"`
		}
		if json.Unmarshal(ev.Data, &d) == nil {
			sid = d.TaskID
		}
	}
	if sid == "" {
		return
	}

	if !isDurableEvent(ev.Type) {
		// Ephemeral: forward immediately, drop on failure — never retried.
		if err := c.client.SendEphemeral(ctx, c.token, sid, ev.Type, msg); err != nil && ctx.Err() == nil {
			c.logf("ephemeral event %q for session %s dropped: %v", ev.Type, sid, err)
		}
		return
	}
	// Durable: assign the per-session seq and batch. The payload is the full
	// original WS message JSON (type + task_id + data), as-is (明文阶段).
	batcher.add(ctx, sid, EventUpload{Seq: c.seq.Next(sid), Kind: ev.Type, Payload: json.RawMessage(msg)})
}
