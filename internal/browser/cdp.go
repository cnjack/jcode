// Package browser implements the Browser Use capability: a CDP-driven browser
// the agent can see (text a11y snapshots + screenshots) and operate (click,
// fill, navigate) behind tiered approvals. Two backends share one TabConn
// abstraction: a managed Chrome launched by jcode, and the user's own Chrome
// reached through the jcode extension bridge. See internal-doc/browser-use-design.md.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// cdpMessage is the wire format of a Chrome DevTools Protocol frame.
type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string { return fmt.Sprintf("cdp error %d: %s", e.Code, e.Message) }

// EventHandler receives CDP events for one tab.
type EventHandler func(method string, params json.RawMessage)

// TabConn is a single controllable tab, regardless of backend.
type TabConn interface {
	// ID is the backend-scoped tab identifier (targetId or extension tab id).
	ID() string
	// Send issues a CDP command against this tab and returns its raw result.
	Send(ctx context.Context, method string, params any) (json.RawMessage, error)
	// SetEventHandler registers the sink for CDP events from this tab.
	// Only one handler is active at a time; nil clears it.
	SetEventHandler(h EventHandler)
	// Close closes the underlying page/tab.
	Close(ctx context.Context) error
	// Detach releases control of the tab without closing it (extension backend
	// leaves the page to the user; managed backend is equivalent to a no-op
	// because nobody else is driving that Chrome).
	Detach(ctx context.Context) error
}

// TabInfo describes a tab visible to a backend.
type TabInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	UserTab  bool   `json:"user_tab"` // pre-existing user tab (extension backend)
	Attached bool   `json:"attached"` // currently under jcode control
}

// Backend abstracts a browser jcode can drive.
type Backend interface {
	Kind() string // "managed" | "extension"
	NewTab(ctx context.Context, url string) (TabConn, error)
	ListTabs(ctx context.Context) ([]TabInfo, error)
	// AttachTab takes control of an existing tab (claim). Managed backend
	// attaches to its own targets; extension backend claims a user tab.
	AttachTab(ctx context.Context, id string) (TabConn, error)
	Close() error
}

// ---------------------------------------------------------------------------
// wsCDP — a minimal CDP client over one websocket (the managed backend's
// browser-level connection). Zero external deps beyond gorilla/websocket.
// ---------------------------------------------------------------------------

type wsCDP struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	nextID  atomic.Int64

	mu       sync.Mutex
	pending  map[int64]chan cdpMessage
	handlers map[string]EventHandler // sessionID → handler ("" = browser-level)
	closed   chan struct{}
	closeErr error
}

func newWSCDP(conn *websocket.Conn) *wsCDP {
	c := &wsCDP{
		conn:     conn,
		pending:  make(map[int64]chan cdpMessage),
		handlers: make(map[string]EventHandler),
		closed:   make(chan struct{}),
	}
	// Screenshots arrive base64-encoded in a single frame; be generous.
	conn.SetReadLimit(256 << 20)
	go c.readLoop()
	return c
}

func (c *wsCDP) readLoop() {
	for {
		var msg cdpMessage
		if err := c.conn.ReadJSON(&msg); err != nil {
			c.mu.Lock()
			c.closeErr = err
			for id, ch := range c.pending {
				close(ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
			close(c.closed)
			return
		}
		if msg.ID != 0 {
			c.mu.Lock()
			ch := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
			continue
		}
		if msg.Method != "" {
			c.mu.Lock()
			h := c.handlers[msg.SessionID]
			c.mu.Unlock()
			if h != nil {
				h(msg.Method, msg.Params)
			}
		}
	}
}

// send issues a command, optionally scoped to a session (tab).
func (c *wsCDP) send(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	frame := map[string]any{"id": id, "method": method}
	if params != nil {
		frame["params"] = params
	}
	if sessionID != "" {
		frame["sessionId"] = sessionID
	}

	ch := make(chan cdpMessage, 1)
	c.mu.Lock()
	if c.closeErr != nil {
		err := c.closeErr
		c.mu.Unlock()
		return nil, fmt.Errorf("cdp connection closed: %w", err)
	}
	c.pending[id] = ch
	c.mu.Unlock()

	c.writeMu.Lock()
	err := c.conn.WriteJSON(frame)
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("cdp write %s: %w", method, err)
	}

	select {
	case msg, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("cdp connection closed during %s", method)
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("%s: %w", method, msg.Error)
		}
		return msg.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.closed:
		// The read loop signals c.closed only after delivering (or closing) every
		// pending channel, so a response for THIS request may already be waiting in
		// ch — e.g. a server that writes a result frame and immediately drops the
		// socket. Prefer the real response over reporting the close; select alone
		// would pick between the two ready cases at random (a source of flakes).
		select {
		case msg, ok := <-ch:
			if ok {
				if msg.Error != nil {
					return nil, fmt.Errorf("%s: %w", method, msg.Error)
				}
				return msg.Result, nil
			}
		default:
		}
		return nil, fmt.Errorf("cdp connection closed during %s", method)
	}
}

func (c *wsCDP) setHandler(sessionID string, h EventHandler) {
	c.mu.Lock()
	if h == nil {
		delete(c.handlers, sessionID)
	} else {
		c.handlers[sessionID] = h
	}
	c.mu.Unlock()
}

func (c *wsCDP) close() error {
	return c.conn.Close()
}

// isClosed reports whether the read loop has exited (connection dropped: Chrome
// quit, crashed, or the socket died).
func (c *wsCDP) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// managedBackend — Chrome launched by jcode, driven over its browser-level
// websocket. Tabs are CDP targets attached in flatten mode.
// ---------------------------------------------------------------------------

type managedBackend struct {
	cdp  *wsCDP
	stop func() // terminates the Chrome process (nil when attached externally)
}

// Kind implements Backend.
func (b *managedBackend) Kind() string { return "managed" }

// alive reports whether the underlying Chrome connection is still usable. The
// Manager uses this to drop and relaunch a managed backend whose Chrome has
// died instead of handing out a dead one.
func (b *managedBackend) alive() bool { return !b.cdp.isClosed() }

func (b *managedBackend) NewTab(ctx context.Context, url string) (TabConn, error) {
	if url == "" {
		url = "about:blank"
	}
	res, err := b.cdp.send(ctx, "", "Target.createTarget", map[string]any{"url": url})
	if err != nil {
		return nil, err
	}
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(res, &created); err != nil {
		return nil, fmt.Errorf("parse createTarget: %w", err)
	}
	return b.AttachTab(ctx, created.TargetID)
}

func (b *managedBackend) AttachTab(ctx context.Context, targetID string) (TabConn, error) {
	res, err := b.cdp.send(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	})
	if err != nil {
		return nil, err
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(res, &attached); err != nil {
		return nil, fmt.Errorf("parse attachToTarget: %w", err)
	}
	return &managedTab{backend: b, targetID: targetID, sessionID: attached.SessionID}, nil
}

func (b *managedBackend) ListTabs(ctx context.Context) ([]TabInfo, error) {
	res, err := b.cdp.send(ctx, "", "Target.getTargets", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			Title    string `json:"title"`
			URL      string `json:"url"`
			Attached bool   `json:"attached"`
		} `json:"targetInfos"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("parse getTargets: %w", err)
	}
	var tabs []TabInfo
	for _, t := range out.TargetInfos {
		if t.Type != "page" {
			continue
		}
		tabs = append(tabs, TabInfo{ID: t.TargetID, Title: t.Title, URL: t.URL, Attached: t.Attached})
	}
	return tabs, nil
}

func (b *managedBackend) Close() error {
	err := b.cdp.close()
	if b.stop != nil {
		b.stop()
	}
	return err
}

type managedTab struct {
	backend   *managedBackend
	targetID  string
	sessionID string
}

func (t *managedTab) ID() string { return t.targetID }

func (t *managedTab) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return t.backend.cdp.send(ctx, t.sessionID, method, params)
}

func (t *managedTab) SetEventHandler(h EventHandler) {
	t.backend.cdp.setHandler(t.sessionID, h)
}

func (t *managedTab) Close(ctx context.Context) error {
	t.SetEventHandler(nil)
	_, err := t.backend.cdp.send(ctx, "", "Target.closeTarget", map[string]any{"targetId": t.targetID})
	return err
}

func (t *managedTab) Detach(ctx context.Context) error {
	t.SetEventHandler(nil)
	_, err := t.backend.cdp.send(ctx, "", "Target.detachFromTarget", map[string]any{"sessionId": t.sessionID})
	return err
}

// connectManaged dials a browser-level CDP websocket endpoint.
func connectManaged(ctx context.Context, wsURL string, stop func()) (*managedBackend, error) {
	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial cdp %s: %w", wsURL, err)
	}
	return &managedBackend{cdp: newWSCDP(conn), stop: stop}, nil
}
