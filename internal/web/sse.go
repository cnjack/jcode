package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
)

// SSEEvent is a single Server-Sent Event.
type SSEEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// SSEBroker manages multiple SSE client connections and broadcasts events.
type SSEBroker struct {
	mu      sync.RWMutex
	clients map[uint64]chan SSEEvent
	nextID  atomic.Uint64
	closed  atomic.Bool
}

// NewSSEBroker creates a new SSE broker.
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[uint64]chan SSEEvent),
	}
}

// Subscribe registers a new client and returns a channel + unsubscribe func.
func (b *SSEBroker) Subscribe() (<-chan SSEEvent, func()) {
	id := b.nextID.Add(1)
	ch := make(chan SSEEvent, 64)

	b.mu.Lock()
	b.clients[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		delete(b.clients, id)
		b.mu.Unlock()
	}
}

// Broadcast sends an event to all connected clients.
func (b *SSEBroker) Broadcast(event SSEEvent) {
	if b.closed.Load() {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.clients {
		select {
		case ch <- event:
		default:
			// Drop if client is slow.
		}
	}
}

// Close shuts down the broker.
func (b *SSEBroker) Close() {
	b.closed.Store(true)
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, ch := range b.clients {
		close(ch)
		delete(b.clients, id)
	}
}

// ClientCount returns the number of connected clients.
func (b *SSEBroker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// ServeSSE writes SSE events from a channel to an HTTP response.
func ServeSSE(w http.ResponseWriter, r *http.Request, events <-chan SSEEvent) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			data, err := json.Marshal(ev.Data)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, data)
			flusher.Flush()
		}
	}
}
