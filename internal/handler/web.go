package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// WebEvent is an event sent from the agent to web clients.
type WebEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// WebTextData carries a streaming text chunk.
type WebTextData struct {
	Text string `json:"text"`
}

// WebToolCallData carries tool invocation info.
type WebToolCallData struct {
	Name       string `json:"name"`
	Args       string `json:"args"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// WebToolResultData carries tool completion info.
type WebToolResultData struct {
	Name       string `json:"name"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// WebTokenData carries token usage.
type WebTokenData struct {
	PromptTokens      int64 `json:"prompt_tokens"`
	CompletionTokens  int64 `json:"completion_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
	ModelContextLimit int   `json:"model_context_limit"`
}

// WebSubagentData carries subagent lifecycle events.
type WebSubagentData struct {
	Name      string `json:"name"`
	AgentType string `json:"agent_type"`
	Done      bool   `json:"done"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

// WebSubagentProgressData carries intermediate subagent tool call/result events.
type WebSubagentProgressData struct {
	AgentName string `json:"agent_name"`
	Event     string `json:"event"` // "tool_call" or "tool_result"
	ToolName  string `json:"tool_name"`
	Detail    string `json:"detail"`
}

// WebDoneData signals agent completion.
type WebDoneData struct {
	Error string `json:"error,omitempty"`
}

// WebApprovalRequestData carries an approval request.
type WebApprovalRequestData struct {
	ID         string `json:"id"`
	ToolName   string `json:"tool_name"`
	ToolArgs   string `json:"tool_args"`
	IsExternal bool   `json:"is_external"`
}

// WebHandler implements AgentEventHandler by sending events to web clients
// through a channel-based event broker.
type WebHandler struct {
	eventCh chan WebEvent

	mu              sync.Mutex
	approvalCounter int
	pendingApproval map[string]chan ApprovalResponse
}

// NewWebHandler creates a handler that sends events to the given channel.
func NewWebHandler() *WebHandler {
	return &WebHandler{
		eventCh:         make(chan WebEvent, 256),
		pendingApproval: make(map[string]chan ApprovalResponse),
	}
}

// Events returns the read-only event channel.
func (h *WebHandler) Events() <-chan WebEvent {
	return h.eventCh
}

func (h *WebHandler) emit(event string, data any) {
	select {
	case h.eventCh <- WebEvent{Event: event, Data: data}:
	default:
		// Drop if channel is full.
	}
}

// Emit sends a custom event to all connected web clients.
func (h *WebHandler) Emit(event string, data any) {
	h.emit(event, data)
}

// --- Output events ---

func (h *WebHandler) OnAgentText(text string) {
	h.emit("agent_text", WebTextData{Text: text})
}

func (h *WebHandler) OnToolCall(name, args, toolCallID string) {
	h.emit("tool_call", WebToolCallData{Name: name, Args: args, ToolCallID: toolCallID})
}

func (h *WebHandler) OnToolResult(name, output, toolCallID string, err error) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	h.emit("tool_result", WebToolResultData{Name: name, Output: output, ToolCallID: toolCallID, Error: errMsg})
}

func (h *WebHandler) OnTodoUpdate() {
	h.emit("todo_update", nil)
}

// --- Subagent events ---

func (h *WebHandler) OnSubagentEvent(name, agentType string, done bool, result string, err error) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	h.emit("subagent_event", WebSubagentData{
		Name: name, AgentType: agentType, Done: done, Result: result, Error: errMsg,
	})
}

func (h *WebHandler) OnSubagentProgress(agentName, event, toolName, detail string) {
	h.emit("subagent_progress", WebSubagentProgressData{
		AgentName: agentName, Event: event, ToolName: toolName, Detail: detail,
	})
}

// --- Lifecycle ---

func (h *WebHandler) OnAgentDone(err error) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	h.emit("agent_done", WebDoneData{Error: errMsg})
}

func (h *WebHandler) OnTokenUpdate(info TokenUsage) {
	h.emit("token_update", WebTokenData(info))
}

// --- Approval flow ---

func (h *WebHandler) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	h.mu.Lock()
	h.approvalCounter++
	id := fmt.Sprintf("approval_%d", h.approvalCounter)
	respCh := make(chan ApprovalResponse, 1)
	h.pendingApproval[id] = respCh
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pendingApproval, id)
		h.mu.Unlock()
	}()

	h.emit("approval_request", WebApprovalRequestData{
		ID:         id,
		ToolName:   req.ToolName,
		ToolArgs:   req.ToolArgs,
		IsExternal: req.IsExternal,
	})

	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		return ApprovalResponse{}, ctx.Err()
	}
}

// ResolveApproval resolves a pending approval request. Called by API handler.
func (h *WebHandler) ResolveApproval(id string, approved bool) error {
	h.mu.Lock()
	ch, ok := h.pendingApproval[id]
	h.mu.Unlock()

	if !ok {
		return fmt.Errorf("no pending approval with id %q", id)
	}

	mode := ModeManual
	if approved {
		mode = ModeAuto
	}

	select {
	case ch <- ApprovalResponse{Approved: approved, Mode: mode}:
		return nil
	default:
		return fmt.Errorf("approval %q already resolved", id)
	}
}

// MarshalEvent marshals a WebEvent to JSON bytes.
func MarshalEvent(ev WebEvent) ([]byte, error) {
	return json.Marshal(ev)
}
