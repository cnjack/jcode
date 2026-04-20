package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	Name        string           `json:"name"`
	Args        string           `json:"args"`
	ToolCallID  string           `json:"tool_call_id,omitempty"`
	DisplayInfo *ToolDisplayInfo `json:"display_info,omitempty"`
}

// ToolDisplayInfo carries human-readable tool metadata for UI rendering.
type ToolDisplayInfo struct {
	Title    string `json:"title"`              // Human-readable tool name (e.g. "Read", "Edit", "Shell")
	Subtitle string `json:"subtitle,omitempty"` // Context info (file path, command description, pattern)
	Icon     string `json:"icon,omitempty"`     // Icon identifier
	Category string `json:"category,omitempty"` // "context" (read-only), "mutation", "execution"
}

// extractToolDisplayInfo extracts display metadata from tool name and args.
func extractToolDisplayInfo(name, argsJSON string) *ToolDisplayInfo {
	info := &ToolDisplayInfo{}

	// Parse args to extract contextual info
	var args map[string]interface{}
	_ = json.Unmarshal([]byte(argsJSON), &args)

	getString := func(key string) string {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	switch name {
	case "read":
		info.Title = "Read"
		info.Icon = "file"
		info.Category = "context"
		info.Subtitle = shortenPath(getString("file_path"))
	case "write":
		info.Title = "Write"
		info.Icon = "file-edit"
		info.Category = "mutation"
		info.Subtitle = shortenPath(getString("file_path"))
	case "edit":
		info.Title = "Edit"
		info.Icon = "file-edit"
		info.Category = "mutation"
		info.Subtitle = shortenPath(getString("file_path"))
	case "multi_edit":
		info.Title = "Multi Edit"
		info.Icon = "file-edit"
		info.Category = "mutation"
		info.Subtitle = shortenPath(getString("file_path"))
	case "glob":
		info.Title = "Glob"
		info.Icon = "search"
		info.Category = "context"
		info.Subtitle = getString("pattern")
	case "grep":
		info.Title = "Search"
		info.Icon = "search"
		info.Category = "context"
		info.Subtitle = getString("pattern")
	case "execute":
		info.Title = "Shell"
		info.Icon = "terminal"
		info.Category = "execution"
		info.Subtitle = getString("description")
		if info.Subtitle == "" {
			cmd := getString("command")
			if len(cmd) > 100 {
				cmd = cmd[:100] + "…"
			}
			info.Subtitle = cmd
		}
	case "background":
		info.Title = "Background"
		info.Icon = "terminal"
		info.Category = "execution"
		info.Subtitle = getString("description")
	case "todowrite":
		info.Title = "Update Todos"
		info.Icon = "checklist"
		info.Category = "mutation"
	case "todoread":
		info.Title = "Read Todos"
		info.Icon = "checklist"
		info.Category = "context"
	case "subagent":
		info.Title = "Subagent"
		info.Icon = "agent"
		info.Category = "execution"
		info.Subtitle = getString("description")
		if info.Subtitle == "" {
			info.Subtitle = getString("name")
		}
	case "ask_user":
		info.Title = "Ask User"
		info.Icon = "question"
		info.Category = "context"
		info.Subtitle = getString("question")
		if len(info.Subtitle) > 60 {
			info.Subtitle = info.Subtitle[:60] + "…"
		}
	default:
		// MCP or unknown tools
		info.Title = name
		info.Icon = "tool"
		info.Category = ""
	}

	return info
}

// shortenPath returns the last 2 path components for display.
func shortenPath(path string) string {
	if path == "" {
		return ""
	}
	// Handle both / and \ separators
	parts := strings.Split(strings.ReplaceAll(path, "\\", "/"), "/")
	if len(parts) <= 2 {
		return path
	}
	return "…/" + strings.Join(parts[len(parts)-2:], "/")
}

// cleanToolOutput strips AI-oriented metadata from tool output for UI display.
// For execute tools: removes STDOUT:/STDERR: prefixes, [Exit code], [Completed], [Hint] lines.
// Returns empty string if no cleaning is needed (frontend will use raw output).
func cleanToolOutput(name, output string) string {
	if name != "execute" {
		return ""
	}
	lines := strings.Split(output, "\n")
	var clean []string
	for _, line := range lines {
		// Skip STDOUT:/STDERR: header lines anywhere in output
		if line == "STDOUT:" || line == "STDERR:" {
			continue
		}
		// Skip metadata lines at the end
		if strings.HasPrefix(line, "[Exit code:") ||
			strings.HasPrefix(line, "[Completed in") ||
			strings.HasPrefix(line, "[Hint:") {
			continue
		}
		clean = append(clean, line)
	}
	// Trim trailing empty lines
	for len(clean) > 0 && strings.TrimSpace(clean[len(clean)-1]) == "" {
		clean = clean[:len(clean)-1]
	}
	result := strings.Join(clean, "\n")
	if result == output {
		return "" // no change, don't send duplicate
	}
	return result
}

// WebToolResultData carries tool completion info.
type WebToolResultData struct {
	Name          string `json:"name"`
	Output        string `json:"output"`
	DisplayOutput string `json:"display_output,omitempty"` // clean output for UI display
	Error         string `json:"error,omitempty"`
	ToolCallID    string `json:"tool_call_id,omitempty"`
}

// WebTokenData carries token usage.
type WebTokenData struct {
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
	h.emit("tool_call", WebToolCallData{
		Name:        name,
		Args:        args,
		ToolCallID:  toolCallID,
		DisplayInfo: extractToolDisplayInfo(name, args),
	})
}

func (h *WebHandler) OnToolResult(name, output, toolCallID string, err error) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	display := cleanToolOutput(name, output)
	h.emit("tool_result", WebToolResultData{
		Name:          name,
		Output:        output,
		DisplayOutput: display,
		ToolCallID:    toolCallID,
		Error:         errMsg,
	})
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

func (h *WebHandler) OnAgentStart() {
	h.emit("agent_start", nil)
}

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
