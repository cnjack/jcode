package handler

import (
	"context"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/channel"
)

// NotifyingHandler wraps another AgentEventHandler and adds delayed notification
// capabilities for approval requests and agent completion events.
// It also supports a list of channel.Notifier instances for lightweight
// one-way status pushes (e.g. BLE IoT devices).
type NotifyingHandler struct {
	inner AgentEventHandler

	mu                sync.Mutex
	onApprovalDelay   func(toolName, toolArgs string)     // called after delay if approval not resolved
	onAgentDone       func(summaryText string, err error) // called on agent completion
	approvalTimers    map[string]*time.Timer
	resolvedApprovals map[string]bool // approvalIDs that were already answered
	approvalDelay     time.Duration
	lastText          string // capture last text for summary

	notifiers   []channel.Notifier
	sentWorking bool // avoid repeated "working" pushes within one run
}

// NewNotifyingHandler creates a handler that wraps inner and can fire external
// notifications for approvals (after a delay) and agent completion.
func NewNotifyingHandler(inner AgentEventHandler, delay time.Duration) *NotifyingHandler {
	return &NotifyingHandler{
		inner:             inner,
		approvalTimers:    make(map[string]*time.Timer),
		resolvedApprovals: make(map[string]bool),
		approvalDelay:     delay,
	}
}

// SetApprovalNotifier sets the callback fired when an approval is not resolved within the delay.
func (h *NotifyingHandler) SetApprovalNotifier(fn func(toolName, toolArgs string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onApprovalDelay = fn
}

// SetDoneNotifier sets the callback fired when the agent finishes.
func (h *NotifyingHandler) SetDoneNotifier(fn func(summary string, err error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onAgentDone = fn
}

// AddNotifier registers a lightweight notifier (e.g. BLE device).
// Notifiers receive automatic status pushes for agent lifecycle events.
func (h *NotifyingHandler) AddNotifier(n channel.Notifier) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.notifiers = append(h.notifiers, n)
}

// notifyAll sends a lifecycle event to all registered notifiers (best-effort).
func (h *NotifyingHandler) notifyAll(event channel.NotifyEvent) {
	h.mu.Lock()
	ns := make([]channel.Notifier, len(h.notifiers))
	copy(ns, h.notifiers)
	h.mu.Unlock()
	for _, n := range ns {
		if n.Available() {
			n.Notify(event)
		}
	}
}

// CloseNotifiers closes all registered notifiers.
func (h *NotifyingHandler) CloseNotifiers() {
	h.mu.Lock()
	ns := h.notifiers
	h.notifiers = nil
	h.mu.Unlock()
	for _, n := range ns {
		n.Close()
	}
}

func (h *NotifyingHandler) OnAgentText(text string) {
	h.mu.Lock()
	h.lastText += text
	// Keep only last 600 chars for summary
	if len(h.lastText) > 600 {
		h.lastText = h.lastText[len(h.lastText)-600:]
	}
	firstText := !h.sentWorking
	if firstText {
		h.sentWorking = true
	}
	h.mu.Unlock()

	// Push "working" status on first text chunk of a run.
	if firstText {
		h.notifyAll(channel.NotifyEvent{Type: channel.EventWorking})
	}
	h.inner.OnAgentText(text)
}

func (h *NotifyingHandler) OnToolCall(name, args, toolCallID string) {
	h.inner.OnToolCall(name, args, toolCallID)
}

func (h *NotifyingHandler) OnToolResult(name, output, toolCallID string, err error) {
	h.inner.OnToolResult(name, output, toolCallID, err)
}

func (h *NotifyingHandler) OnTodoUpdate() {
	h.inner.OnTodoUpdate()
}

func (h *NotifyingHandler) OnAgentDone(err error) {
	h.inner.OnAgentDone(err)

	h.mu.Lock()
	fn := h.onAgentDone
	summary := h.lastText
	h.lastText = ""
	h.sentWorking = false
	h.mu.Unlock()

	// Push completion status to notifiers, then return to idle after 5s.
	h.notifyAll(channel.NotifyEvent{Type: channel.EventDone, Err: err})
	time.AfterFunc(5*time.Second, func() {
		h.notifyAll(channel.NotifyEvent{Type: channel.EventIdle})
	})

	if fn != nil {
		fn(summary, err)
	}
}

func (h *NotifyingHandler) OnTokenUpdate(info TokenUsage) {
	h.inner.OnTokenUpdate(info)
}

func (h *NotifyingHandler) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	// Push attention status to notifiers immediately.
	h.notifyAll(channel.NotifyEvent{Type: channel.EventApproval, Tool: req.ToolName})

	// Use toolCallID if available, otherwise fall back to tool name + args
	approvalID := req.ToolCallID
	if approvalID == "" {
		approvalID = req.ToolName + ":" + req.ToolArgs
	}

	// Schedule delayed notification only if not already resolved
	h.mu.Lock()
	fn := h.onApprovalDelay
	if fn != nil && h.approvalDelay > 0 {
		timer := time.AfterFunc(h.approvalDelay, func() {
			h.mu.Lock()
			delete(h.approvalTimers, approvalID)
			already := h.resolvedApprovals[approvalID]
			notifyFn := h.onApprovalDelay
			h.mu.Unlock()
			// Only notify if approval hasn't been answered yet
			if !already && notifyFn != nil {
				notifyFn(req.ToolName, req.ToolArgs)
			}
		})
		h.approvalTimers[approvalID] = timer
	}
	h.mu.Unlock()

	// Delegate to inner handler (blocks until user responds)
	resp, err := h.inner.RequestApproval(ctx, req)

	// Resume working status after approval resolved.
	if resp.Approved {
		h.notifyAll(channel.NotifyEvent{Type: channel.EventWorking})
	}

	// Mark as resolved and cancel the timer if it hasn't fired yet
	h.mu.Lock()
	h.resolvedApprovals[approvalID] = true
	if timer, ok := h.approvalTimers[approvalID]; ok {
		timer.Stop()
		delete(h.approvalTimers, approvalID)
	}
	h.mu.Unlock()

	return resp, err
}
