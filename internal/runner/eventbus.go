package runner

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/cnjack/jcode/internal/config"
)

// EventType categorises events flowing through the EventBus.
type EventType int

const (
	EventAssistantText EventType = iota
	EventAssistantDone
	EventToolCall
	EventToolResult
	EventError
	EventBudgetWarning
	EventCompaction
	EventWorkerStatus
)

// ToolCallEvent describes a tool invocation.
type ToolCallEvent struct {
	Name string
	Args string
}

// ToolResultEvent describes a tool's result.
type ToolResultEvent struct {
	Name   string
	Output string
	Err    error
}

// Event is the unit of data emitted by the agent loop.
type Event struct {
	Type       EventType
	Text       string
	ToolCall   *ToolCallEvent
	ToolResult *ToolResultEvent
	Error      error
	Meta       map[string]any
}

// EventBus is a channel-based event bus for streaming agent loop events
// to the TUI and other consumers.
type EventBus struct {
	ch        chan Event
	done      chan struct{}
	cancel    context.CancelFunc
	closeOnce sync.Once
	closed    atomic.Bool
}

// NewEventBus creates an event bus with the given channel buffer size.
func NewEventBus(bufferSize int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = 64
	}
	_, cancel := context.WithCancel(context.Background())
	return &EventBus{
		ch:     make(chan Event, bufferSize),
		done:   make(chan struct{}),
		cancel: cancel,
	}
}

// Emit sends an event to the bus. Non-blocking: drops the event if the
// channel is full or the bus is closed, and logs a warning.
func (eb *EventBus) Emit(event Event) {
	if eb.closed.Load() {
		return
	}
	select {
	case eb.ch <- event:
	default:
		config.Logger().Printf("[eventbus] dropped event type=%d (buffer full)", event.Type)
	}
}

// Subscribe returns the read-only event channel.
func (eb *EventBus) Subscribe() <-chan Event {
	return eb.ch
}

// Close shuts down the event bus. Safe to call multiple times.
func (eb *EventBus) Close() {
	eb.closeOnce.Do(func() {
		eb.closed.Store(true)
		eb.cancel()
		close(eb.done)
		close(eb.ch)
	})
}

// Done returns a channel that is closed when the event bus is shut down.
func (eb *EventBus) Done() <-chan struct{} {
	return eb.done
}
