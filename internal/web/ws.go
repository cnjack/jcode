package web

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/cnjack/jcode/internal/config"
	"github.com/gorilla/websocket"
)

// WSClient represents a connected WebSocket client.
type WSClient struct {
	conn    *websocket.Conn
	sendCh  chan []byte
	mu      sync.Mutex
	closeCh sync.Once

	// subMu guards the task subscription set. subAll is true until the client
	// sends its first `subscribe` (so legacy clients that never subscribe keep
	// receiving every task's events). Once subscribed, only the listed task ids
	// (plus global events, TaskID=="") are delivered, preventing a busy task from
	// flooding a client that is only viewing a quiet one.
	subMu  sync.Mutex
	subs   map[string]bool
	subAll bool
}

func newWSClient(conn *websocket.Conn) *WSClient {
	return &WSClient{
		conn:   conn,
		sendCh: make(chan []byte, 256),
		subs:   make(map[string]bool),
		subAll: true,
	}
}

// subscribe replaces the client's subscription set with the given task ids
// (additive across calls). After the first call the client stops receiving
// every task and only gets its subscribed ids + global events.
func (c *WSClient) subscribe(taskIDs []string) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	c.subAll = false
	for _, id := range taskIDs {
		if id != "" {
			c.subs[id] = true
		}
	}
}

// unsubscribe drops task ids from the client's subscription set.
func (c *WSClient) unsubscribe(taskIDs []string) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	for _, id := range taskIDs {
		delete(c.subs, id)
	}
}

// wants reports whether this client should receive an event for taskID. Global
// events (empty taskID) always pass.
func (c *WSClient) wants(taskID string) bool {
	if taskID == "" {
		return true
	}
	c.subMu.Lock()
	defer c.subMu.Unlock()
	return c.subAll || c.subs[taskID]
}

func (c *WSClient) writePump() {
	defer func() { _ = c.conn.Close() }()
	for msg := range c.sendCh {
		c.mu.Lock()
		err := c.conn.WriteMessage(websocket.TextMessage, msg)
		c.mu.Unlock()
		if err != nil {
			return
		}
	}
}

func (c *WSClient) send(data []byte) {
	select {
	case c.sendCh <- data:
	default:
		// drop slow client
	}
}

func (c *WSClient) close() {
	c.closeCh.Do(func() { close(c.sendCh) })
}

// WSBroker manages WebSocket client connections and broadcasts events.
type WSBroker struct {
	mu      sync.RWMutex
	clients map[uint64]*WSClient
	nextID  atomic.Uint64
	closed  atomic.Bool
}

// NewWSBroker creates a new WebSocket broker.
func NewWSBroker() *WSBroker {
	return &WSBroker{
		clients: make(map[uint64]*WSClient),
	}
}

// Register adds a new WebSocket client and returns an ID + unsubscribe func.
func (b *WSBroker) Register(conn *websocket.Conn) (uint64, *WSClient, func()) {
	id := b.nextID.Add(1)
	client := newWSClient(conn)

	b.mu.Lock()
	b.clients[id] = client
	b.mu.Unlock()

	return id, client, func() {
		b.mu.Lock()
		delete(b.clients, id)
		b.mu.Unlock()
		client.close()
	}
}

// Broadcast sends an event to all connected WebSocket clients.
func (b *WSBroker) Broadcast(event WSEvent) {
	if b.closed.Load() {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		config.Logger().Printf("[ws] marshal error: %v", err)
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, client := range b.clients {
		if !client.wants(event.TaskID) {
			continue
		}
		client.send(data)
	}
}

// Close shuts down the broker.
func (b *WSBroker) Close() {
	b.closed.Store(true)
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, client := range b.clients {
		client.close()
		delete(b.clients, id)
	}
}

// ClientCount returns the number of connected clients.
func (b *WSBroker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// WSEvent is a WebSocket message envelope.
type WSEvent struct {
	Type string `json:"type"`
	// TaskID tags the event with the task (engine) it came from so the client can
	// route it to the right task view, and so the broker can deliver it only to
	// clients subscribed to that task. Empty for global/server-wide events
	// (mcp_changed, model_changed, pong, …), which every client receives.
	TaskID string `json:"task_id,omitempty"`
	Data   any    `json:"data,omitempty"`
}

// WSIncoming represents a message from the client over WebSocket.
type WSIncoming struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}
