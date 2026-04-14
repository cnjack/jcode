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
}

func newWSClient(conn *websocket.Conn) *WSClient {
	return &WSClient{
		conn:   conn,
		sendCh: make(chan []byte, 256),
	}
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
	Data any    `json:"data,omitempty"`
}

// WSIncoming represents a message from the client over WebSocket.
type WSIncoming struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}
