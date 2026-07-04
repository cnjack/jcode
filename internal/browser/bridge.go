package browser

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/gorilla/websocket"
)

// Bridge is the server side of the jcode Chrome extension channel. The
// extension's service worker connects over a websocket, presents a long-lived
// token (obtained via native-messaging Auto-connect), and then relays CDP
// commands to the user's Chrome via chrome.debugger. See §5.3 of the design.
type Bridge struct {
	mu        sync.Mutex
	conn      *bridgeConn // the single connected extension (nil when offline)
	tokens    map[string]bool
	tokenPath string
	upgrader  websocket.Upgrader
}

// NewBridge creates a bridge. tokens are persisted to ~/.jcode/browser/ext-tokens.json.
func NewBridge() *Bridge {
	b := &Bridge{
		tokens:   make(map[string]bool),
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}
	b.loadTokens()
	return b
}

// Connected reports whether an extension is currently attached.
func (b *Bridge) Connected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conn != nil
}

func (b *Bridge) validToken(token string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tokens[token]
}

// IssueToken mints and persists a token without a pairing code. Used by the
// native-messaging path, where the running server hands the extension a token
// directly (the OS-level native host launch is the trust anchor).
func (b *Bridge) IssueToken() string {
	token := randomToken()
	b.mu.Lock()
	b.tokens[token] = true
	b.saveTokensLocked()
	b.mu.Unlock()
	return token
}

// HandleWS upgrades an extension connection and runs its read loop.
func (b *Bridge) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := b.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// First frame must be a hello with a valid token (issued via Auto-connect).
	var hello struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err := conn.ReadJSON(&hello); err != nil {
		_ = conn.Close()
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	if hello.Token == "" || !b.validToken(hello.Token) {
		_ = conn.WriteJSON(map[string]any{"type": "error", "message": "authentication required"})
		_ = conn.Close()
		return
	}
	token := hello.Token

	_ = conn.WriteJSON(map[string]any{"type": "welcome", "token": token})

	bc := newBridgeConn(conn)
	b.mu.Lock()
	if b.conn != nil {
		b.conn.close()
	}
	b.conn = bc
	b.mu.Unlock()

	config.Logger().Printf("[browser] extension connected")
	go bc.keepAlive()
	bc.readLoop()

	b.mu.Lock()
	if b.conn == bc {
		b.conn = nil
	}
	b.mu.Unlock()
	config.Logger().Printf("[browser] extension disconnected")
}

// Backend returns an extension-backed Backend, or an error when offline.
func (b *Bridge) Backend() (Backend, error) {
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()
	if conn == nil {
		return nil, fmt.Errorf("no jcode Chrome extension connected")
	}
	return &extensionBackend{conn: conn}, nil
}

// ---------------------------------------------------------------------------
// bridgeConn — request/response + event correlation over the extension ws.
// ---------------------------------------------------------------------------

type bridgeEnvelope struct {
	Type   string          `json:"type"`
	ID     int64           `json:"id,omitempty"`
	TabID  string          `json:"tabId,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Tabs   []TabInfo       `json:"tabs,omitempty"`
	URL    string          `json:"url,omitempty"`
}

// Keepalive timing. Chrome/Edge kill an MV3 service worker after ~30s idle, and
// an inbound websocket message resets that timer — so a steady server→extension
// ping is what actually keeps the extension worker (and thus the whole bridge)
// alive between browser commands. Without it the worker naps, the socket drops,
// and the popup flaps to "Reconnecting…". keepAliveWait is the read side: if no
// frame (pong, alarm ping, or command reply) arrives within two ping periods,
// treat the extension as dead and tear the socket down.
// vars, not consts, so tests can shrink them.
var (
	keepAlivePing = 15 * time.Second
	keepAliveWait = 40 * time.Second
)

type bridgeConn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
	nextID  atomic.Int64

	mu       sync.Mutex
	pending  map[int64]chan bridgeEnvelope
	handlers map[string]EventHandler // tabID → handler
	closed   chan struct{}
	closeErr error
}

// writeJSON serializes all writes to the socket (gorilla forbids concurrent
// writers) and bounds a stuck write so a wedged peer can't block a ping or a
// command forever.
func (c *bridgeConn) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteJSON(v)
}

// keepAlive pings the extension on an interval so its service worker stays awake
// and the socket stays up between commands. It exits when the read loop closes
// the conn.
func (c *bridgeConn) keepAlive() {
	t := time.NewTicker(keepAlivePing)
	defer t.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-t.C:
			if err := c.writeJSON(bridgeEnvelope{Type: "ping"}); err != nil {
				c.close() // wake the read loop so it tears the conn down
				return
			}
		}
	}
}

func newBridgeConn(ws *websocket.Conn) *bridgeConn {
	ws.SetReadLimit(256 << 20)
	return &bridgeConn{
		ws:       ws,
		pending:  make(map[int64]chan bridgeEnvelope),
		handlers: make(map[string]EventHandler),
		closed:   make(chan struct{}),
	}
}

func (c *bridgeConn) readLoop() {
	_ = c.ws.SetReadDeadline(time.Now().Add(keepAliveWait))
	for {
		var env bridgeEnvelope
		if err := c.ws.ReadJSON(&env); err != nil {
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
		// Any inbound frame proves the extension is alive; extend the window.
		_ = c.ws.SetReadDeadline(time.Now().Add(keepAliveWait))
		switch env.Type {
		case "ping", "pong":
			// Keepalive traffic (the extension's own alarm ping, or a pong to
			// ours) — nothing to route.
		case "cdp.result", "cdp.error", "tabs.result", "tab.result":
			c.mu.Lock()
			ch := c.pending[env.ID]
			delete(c.pending, env.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- env
			}
		case "cdp.event":
			c.mu.Lock()
			h := c.handlers[env.TabID]
			c.mu.Unlock()
			if h != nil {
				h(env.Method, env.Params)
			}
		}
	}
}

func (c *bridgeConn) request(ctx context.Context, env bridgeEnvelope) (bridgeEnvelope, error) {
	id := c.nextID.Add(1)
	env.ID = id
	ch := make(chan bridgeEnvelope, 1)

	c.mu.Lock()
	if c.closeErr != nil {
		c.mu.Unlock()
		return bridgeEnvelope{}, fmt.Errorf("extension disconnected")
	}
	c.pending[id] = ch
	c.mu.Unlock()

	err := c.writeJSON(env)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return bridgeEnvelope{}, err
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return bridgeEnvelope{}, fmt.Errorf("extension disconnected")
		}
		if resp.Error != "" {
			return resp, fmt.Errorf("%s", resp.Error)
		}
		return resp, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return bridgeEnvelope{}, ctx.Err()
	case <-c.closed:
		return bridgeEnvelope{}, fmt.Errorf("extension disconnected")
	}
}

func (c *bridgeConn) setHandler(tabID string, h EventHandler) {
	c.mu.Lock()
	if h == nil {
		delete(c.handlers, tabID)
	} else {
		c.handlers[tabID] = h
	}
	c.mu.Unlock()
}

func (c *bridgeConn) close() { _ = c.ws.Close() }

// ---------------------------------------------------------------------------
// extensionBackend / extensionTab — Backend over the bridge.
// ---------------------------------------------------------------------------

type extensionBackend struct {
	conn *bridgeConn
}

func (b *extensionBackend) Kind() string { return "extension" }

func (b *extensionBackend) NewTab(ctx context.Context, url string) (TabConn, error) {
	resp, err := b.conn.request(ctx, bridgeEnvelope{Type: "tab.new", URL: url})
	if err != nil {
		return nil, err
	}
	return &extensionTab{conn: b.conn, id: resp.TabID}, nil
}

func (b *extensionBackend) AttachTab(ctx context.Context, id string) (TabConn, error) {
	if _, err := b.conn.request(ctx, bridgeEnvelope{Type: "tab.attach", TabID: id}); err != nil {
		return nil, err
	}
	return &extensionTab{conn: b.conn, id: id}, nil
}

func (b *extensionBackend) ListTabs(ctx context.Context) ([]TabInfo, error) {
	resp, err := b.conn.request(ctx, bridgeEnvelope{Type: "tabs.list"})
	if err != nil {
		return nil, err
	}
	return resp.Tabs, nil
}

func (b *extensionBackend) Close() error { return nil } // shared conn; do not close

type extensionTab struct {
	conn *bridgeConn
	id   string
}

func (t *extensionTab) ID() string { return t.id }

func (t *extensionTab) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	resp, err := t.conn.request(ctx, bridgeEnvelope{Type: "cdp.send", TabID: t.id, Method: method, Params: raw})
	if err != nil {
		return nil, err
	}
	return resp.Result, nil
}

func (t *extensionTab) SetEventHandler(h EventHandler) { t.conn.setHandler(t.id, h) }

func (t *extensionTab) Close(ctx context.Context) error {
	t.SetEventHandler(nil)
	_, err := t.conn.request(ctx, bridgeEnvelope{Type: "tab.close", TabID: t.id})
	return err
}

func (t *extensionTab) Detach(ctx context.Context) error {
	t.SetEventHandler(nil)
	_, err := t.conn.request(ctx, bridgeEnvelope{Type: "tab.detach", TabID: t.id})
	return err
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func randomToken() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, 32)
	for i := range buf {
		v, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		buf[i] = alphabet[v.Int64()]
	}
	return string(buf)
}
