package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/tools"
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

// WebToolCallData carries tool invocation info. The batch fields group tool
// calls issued by the same assistant message (batch_size > 1 → concurrent
// batch); started_at is unix milliseconds.
type WebToolCallData struct {
	Name        string           `json:"name"`
	Args        string           `json:"args"`
	ToolCallID  string           `json:"tool_call_id,omitempty"`
	DisplayInfo *ToolDisplayInfo `json:"display_info,omitempty"`
	BatchID     string           `json:"batch_id,omitempty"`
	BatchIndex  int              `json:"batch_index,omitempty"`
	BatchSize   int              `json:"batch_size,omitempty"`
	StartedAt   int64            `json:"started_at,omitempty"`
}

// ToolDisplayInfo carries human-readable tool metadata for UI rendering.
type ToolDisplayInfo struct {
	Title       string `json:"title"`              // Human-readable tool name (e.g. "Read", "Edit", "Shell")
	Subtitle    string `json:"subtitle,omitempty"` // Context info (file path, command description, pattern)
	Icon        string `json:"icon,omitempty"`     // Icon identifier
	Category    string `json:"category,omitempty"` // "context" (read-only), "mutation", "execution"
	Kind        string `json:"kind,omitempty"`     // presentation kind: read|search|list|shell|edit|agent|other
	Collapsible bool   `json:"collapsible,omitempty"`
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
		info.Subtitle = todoWriteSubtitle(args)
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
	case "show_artifact":
		info.Title = "Artifact"
		info.Icon = "file"
		info.Category = "context"
		info.Kind = "read"
		info.Subtitle = getString("title")
		if info.Subtitle == "" {
			info.Subtitle = shortenPath(getString("path"))
		}
	case "load_skill":
		info.Title = "Load Skill"
		info.Icon = "skill"
		info.Category = "context"
		info.Subtitle = getString("name")
	case "team_list":
		info.Title = "Team"
		info.Icon = "agent"
		info.Category = "context"
	case "team_send_message":
		info.Title = "Message"
		info.Icon = "agent"
		info.Category = "execution"
		to := getString("to")
		if to == "*" {
			info.Subtitle = "→ all"
		} else if to != "" {
			info.Subtitle = "→ @" + to
		}
	case "team_create":
		info.Title = "Create Team"
		info.Icon = "agent"
		info.Category = "execution"
		info.Subtitle = getString("team_name")
	case "team_spawn":
		info.Title = "Spawn"
		info.Icon = "agent"
		info.Category = "execution"
		info.Subtitle = getString("name")
	case "team_delete":
		info.Title = "Delete Team"
		info.Icon = "agent"
		info.Category = "mutation"
	case "browser_open":
		info.Title = "Browser Open"
		info.Icon = "browser"
		info.Category = "execution"
		info.Subtitle = getString("url")
	case "browser_snapshot":
		info.Title = "Page Snapshot"
		info.Icon = "browser"
		info.Category = "context"
	case "browser_screenshot":
		info.Title = "Screenshot"
		info.Icon = "browser"
		info.Category = "context"
	case "browser_act":
		info.Title = "Browser Action"
		info.Icon = "browser"
		info.Category = "execution"
		info.Subtitle = strings.TrimSpace(getString("action") + " " + getString("uid"))
	case "browser_read":
		info.Title = "Read Page"
		info.Icon = "browser"
		info.Category = "context"
	case "browser_tabs":
		info.Title = "Browser Tabs"
		info.Icon = "browser"
		info.Category = "context"
		info.Subtitle = getString("op")
	case "browser_eval":
		info.Title = "Browser Eval"
		info.Icon = "browser"
		info.Category = "execution"
	case "computer_open":
		info.Title = "Open App"
		info.Icon = "computer"
		info.Category = "execution"
		info.Subtitle = getString("app")
	case "computer_snapshot":
		info.Title = "App Snapshot"
		info.Icon = "computer"
		info.Category = "context"
		info.Subtitle = getString("app")
	case "computer_screenshot":
		info.Title = "App Screenshot"
		info.Icon = "computer"
		info.Category = "context"
		info.Subtitle = getString("app")
	case "computer_act":
		info.Title = "Computer Action"
		info.Icon = "computer"
		info.Category = "execution"
		// A batch says how many actions it carries; a single action names itself.
		// "12 actions" is the thing a reader needs at a glance — the individual
		// steps are in the renderer.
		if steps, ok := args["steps"].([]interface{}); ok && len(steps) > 0 {
			info.Subtitle = fmt.Sprintf("%d actions", len(steps))
		} else {
			info.Subtitle = strings.TrimSpace(getString("action") + " " + getString("uid"))
		}
	case "computer_apps":
		info.Title = "List Apps"
		info.Icon = "computer"
		info.Category = "context"
	default:
		if displayName, ok := tools.MCPDisplayNameForTool(name); ok {
			// MCP tool, codex-style: "server.tool" title + compact-JSON args
			// subtitle ("Calling server.tool(args)").
			info.Title = displayName
			info.Icon = "mcp"
			info.Category = ""
			info.Subtitle = compactToolArgs(argsJSON, 80)
		} else {
			// Unknown tools
			info.Title = name
			info.Icon = "tool"
			info.Category = ""
		}
	}

	// Presentation kind / collapsible for exploring-group UI (additive).
	kind, collapsible := tools.PresentationKindForTool(name, argsJSON)
	info.Kind = kind
	info.Collapsible = collapsible

	return info
}

// todoWriteSubtitle summarizes a todowrite call from its parsed args:
// "3/8 · <in-progress title>" for list payloads (legacy `todos` or enhanced
// `items`), the action name for single-item actions, "" when unparseable.
// This keeps the timeline call line to a compact change summary instead of a
// raw dump of the whole todos array.
func todoWriteSubtitle(args map[string]interface{}) string {
	list, ok := args["todos"].([]interface{})
	if !ok {
		list, ok = args["items"].([]interface{})
	}
	if !ok || len(list) == 0 {
		if action, _ := args["action"].(string); action != "" && action != "update" {
			return action
		}
		return ""
	}
	completed := 0
	current := ""
	for _, raw := range list {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		status, _ := item["status"].(string)
		switch status {
		case "completed", "done":
			completed++
		case "in_progress":
			if title, _ := item["title"].(string); title != "" && current == "" {
				current = title
			}
		}
	}
	subtitle := fmt.Sprintf("%d/%d", completed, len(list))
	if current != "" {
		if r := []rune(current); len(r) > 40 {
			current = string(r[:40]) + "…"
		}
		subtitle += " · " + current
	}
	return subtitle
}

// compactToolArgs renders a JSON args payload as a single compact line capped
// at maxLen runes, for codex-style "server.tool(args)" subtitles. Empty or
// no-op payloads collapse to "".
func compactToolArgs(argsJSON string, maxLen int) string {
	s := strings.TrimSpace(argsJSON)
	if s == "" || s == "{}" || s == "null" {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err == nil {
		s = buf.String()
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > maxLen {
		s = string(r[:maxLen]) + "…"
	}
	return s
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

// WebToolResultStreams is the structured stream payload for execute-style tools.
type WebToolResultStreams struct {
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Aggregated string `json:"aggregated,omitempty"`
}

// WebToolResultMeta is structured execution metadata for UI consumers.
type WebToolResultMeta struct {
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	SpillPath  string `json:"spill_path,omitempty"`
}

// WebToolResultPresentation carries UI presentation hints on a tool result.
type WebToolResultPresentation struct {
	Kind        string `json:"kind,omitempty"`
	Title       string `json:"title,omitempty"`
	Subtitle    string `json:"subtitle,omitempty"`
	Collapsible bool   `json:"collapsible,omitempty"`
}

// WebToolResultData carries tool completion info.
// Legacy fields (output / display_output) are always populated for old clients;
// streams/meta/presentation are additive dual-channel fields.
type WebToolResultData struct {
	Name          string                     `json:"name"`
	Output        string                     `json:"output"`
	DisplayOutput string                     `json:"display_output,omitempty"` // clean output for UI display
	Error         string                     `json:"error,omitempty"`
	ToolCallID    string                     `json:"tool_call_id,omitempty"`
	Streams       *WebToolResultStreams      `json:"streams,omitempty"`
	Meta          *WebToolResultMeta         `json:"meta,omitempty"`
	Presentation  *WebToolResultPresentation `json:"presentation,omitempty"`
	// DurationMs is the runner-measured call→result latency (approval wait
	// already subtracted), provided for all tools. It coexists with
	// Meta.DurationMs, which only execute-style tools report (in-sandbox
	// execution time).
	DurationMs int64 `json:"duration_ms,omitempty"`
	// Denied is true when the user rejected this call at the approval prompt.
	// The UI renders it struck-through/muted (declined), not as an error.
	Denied bool `json:"denied,omitempty"`
}

// WebTokenData carries token usage to the browser. Field order/types MUST match
// handler.TokenUsage so OnTokenUpdate's WebTokenData(info) conversion compiles.
// total_tokens is current context occupancy (last call); the rest are
// cumulative for the session.
type WebTokenData struct {
	TotalTokens       int64   `json:"total_tokens"`
	PromptTokens      int64   `json:"prompt_tokens"`
	CompletionTokens  int64   `json:"completion_tokens"`
	CachedTokens      int64   `json:"cached_tokens"`
	ReasoningTokens   int64   `json:"reasoning_tokens"`
	CacheWriteTokens  int64   `json:"cache_write_tokens"`
	CallCount         int64   `json:"call_count"`
	CacheHitRate      float64 `json:"cache_hit_rate"`
	CacheSupported    bool    `json:"cache_supported"`
	ModelContextLimit int     `json:"model_context_limit"`
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

// WebDoneData signals agent completion. Error is a short user-facing summary;
// Detail carries the full raw error text for a collapsible "details" view.
// Stopped marks a user-initiated stop — the UI shows a calm notice, not an error.
type WebDoneData struct {
	Error   string `json:"error,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Stopped bool   `json:"stopped,omitempty"`
}

// WebApprovalRequestData carries an approval request. ToolCallID (when known)
// ties the prompt to the exact pending tool_call row so the UI can paint that
// row as "waiting for approval".
type WebApprovalRequestData struct {
	ID         string `json:"id"`
	ToolName   string `json:"tool_name"`
	ToolArgs   string `json:"tool_args"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	IsExternal bool   `json:"is_external"`
}

// WebAskUserRequestData carries an ask_user question request to web clients.
type WebAskUserRequestData struct {
	ID        string                  `json:"id"`
	Questions []tools.AskUserQuestion `json:"questions"`
}

// WebHandler implements AgentEventHandler by sending events to web clients
// through a channel-based event broker.
type WebHandler struct {
	eventCh chan WebEvent

	mu              sync.Mutex
	approvalCounter int
	pendingApproval map[string]*webPendingApproval

	askUserMu      sync.Mutex
	askUserCounter int
	pendingAskUser map[string]*pendingAskUser
}

// pendingApproval pairs an approval's response channel with the request payload
// so the latter can be re-surfaced to a (re)connecting client via
// /api/approval/pending — mirroring pendingAskUser. The WS approval_request event
// is fire-once, so without retaining the data a reload/reconnect while an
// approval is pending would leave the agent blocked with no card to act on.
type webPendingApproval struct {
	ch   chan ApprovalResponse
	data WebApprovalRequestData
}

// pendingAskUser pairs a question's response channel with the request payload so
// the latter can be re-surfaced to a (re)connecting client via /api/ask/pending.
type pendingAskUser struct {
	ch   chan tools.AskUserBatchResponse
	data WebAskUserRequestData
}

// NewWebHandler creates a handler that sends events to the given channel.
func NewWebHandler() *WebHandler {
	return &WebHandler{
		eventCh:         make(chan WebEvent, 256),
		pendingApproval: make(map[string]*webPendingApproval),
		pendingAskUser:  make(map[string]*pendingAskUser),
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

func (h *WebHandler) OnToolCall(ev ToolCallEvent) {
	data := WebToolCallData{
		Name:        ev.Name,
		Args:        ev.Args,
		ToolCallID:  ev.ToolCallID,
		DisplayInfo: extractToolDisplayInfo(ev.Name, ev.Args),
		BatchID:     ev.BatchID,
		BatchIndex:  ev.BatchIndex,
		BatchSize:   ev.BatchSize,
	}
	if !ev.StartedAt.IsZero() {
		data.StartedAt = ev.StartedAt.UnixMilli()
	}
	h.emit("tool_call", data)
}

func (h *WebHandler) OnToolResult(ev ToolResultEvent) {
	name, output := ev.Name, ev.Output
	errMsg := ""
	if ev.Err != nil {
		errMsg = ev.Err.Error()
	}
	data := WebToolResultData{
		Name:       name,
		Output:     output,
		ToolCallID: ev.ToolCallID,
		Error:      errMsg,
		DurationMs: ev.Duration.Milliseconds(),
		Denied:     ev.Denied,
	}
	// Dual-channel for execute: parse model string into streams/meta for UI.
	if name == "execute" {
		if parsed, ok := tools.ParseExecModelOutput(output); ok {
			data.DisplayOutput = parsed.DisplayBody
			data.Streams = &WebToolResultStreams{
				Stdout:     parsed.Streams.Stdout,
				Stderr:     parsed.Streams.Stderr,
				Aggregated: parsed.Streams.Aggregated,
			}
			data.Meta = &WebToolResultMeta{
				ExitCode:   parsed.Meta.ExitCode,
				DurationMs: parsed.Meta.DurationMs,
				TimedOut:   parsed.Meta.TimedOut,
				Truncated:  parsed.Meta.Truncated,
				SpillPath:  parsed.Meta.SpillPath,
			}
			data.Presentation = &WebToolResultPresentation{
				Kind:        parsed.Presentation.Kind,
				Title:       parsed.Presentation.Title,
				Subtitle:    parsed.Presentation.Subtitle,
				Collapsible: parsed.Presentation.Collapsible,
			}
		} else {
			data.DisplayOutput = cleanToolOutput(name, output)
		}
	} else {
		data.DisplayOutput = cleanToolOutput(name, output)
	}
	h.emit("tool_result", data)
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
	if err == nil {
		h.emit("agent_done", WebDoneData{})
		return
	}
	// User-initiated stop (the runner reports the clean context error): show a
	// calm "stopped" notice, not a red error card.
	if errors.Is(err, context.Canceled) {
		h.emit("agent_done", WebDoneData{Stopped: true})
		return
	}
	// Raw run errors (eino NodeRunError wrapping go-openai API errors) are too
	// noisy for the timeline — send a one-line summary plus the raw detail.
	summary, detail := internalmodel.SummarizeRunError(err)
	h.emit("agent_done", WebDoneData{Error: summary, Detail: detail})
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
	data := WebApprovalRequestData{
		ID:         id,
		ToolName:   req.ToolName,
		ToolArgs:   req.ToolArgs,
		ToolCallID: req.ToolCallID,
		IsExternal: req.IsExternal,
	}
	h.pendingApproval[id] = &webPendingApproval{ch: respCh, data: data}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pendingApproval, id)
		h.mu.Unlock()
	}()

	h.emit("approval_request", data)

	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		return ApprovalResponse{}, ctx.Err()
	}
}

// ResolveApproval resolves a pending approval request. Called by API handler.
// approveAll distinguishes "approve all" (promote the session to auto-approve,
// like the TUI's "Approve All" and ACP's "Allow Always") from a plain
// "approve once" that leaves the session mode untouched. Previously every
// approve was treated as auto, silently flipping the whole session to
// Full access on a single Allow click.
func (h *WebHandler) ResolveApproval(id string, approved, approveAll bool) error {
	h.mu.Lock()
	p, ok := h.pendingApproval[id]
	h.mu.Unlock()

	if !ok {
		return fmt.Errorf("no pending approval with id %q", id)
	}

	mode := ModeManual
	if approved && approveAll {
		mode = ModeAuto
	}

	select {
	case p.ch <- ApprovalResponse{Approved: approved, Mode: mode}:
		return nil
	default:
		return fmt.Errorf("approval %q already resolved", id)
	}
}

// PendingApprovalRequests returns the still-unanswered approval requests so a
// reloaded/reconnecting client can re-surface the approval card (the
// approval_request WS event is fire-once and ephemeral). Without this, a page
// refresh or WS reconnect while an approval is pending would drop the card and
// leave the agent blocked forever. Mirrors PendingAskUserRequests.
func (h *WebHandler) PendingApprovalRequests() []WebApprovalRequestData {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]WebApprovalRequestData, 0, len(h.pendingApproval))
	for _, p := range h.pendingApproval {
		out = append(out, p.data)
	}
	return out
}

// --- Ask-user flow ---

// RequestAskUser emits the question(s) to web clients and blocks until the user
// answers (via the /api/ask endpoint → ResolveAskUser) or the context is
// cancelled. It mirrors RequestApproval: a per-request id keys a one-shot
// response channel so the API handler can route the answer back. This is wired
// into the ask_user tool's BatchRequestFn for the web frontend.
func (h *WebHandler) RequestAskUser(ctx context.Context, questions []tools.AskUserQuestion) (tools.AskUserBatchResponse, error) {
	data := WebAskUserRequestData{Questions: questions}
	h.askUserMu.Lock()
	h.askUserCounter++
	data.ID = fmt.Sprintf("ask_%d", h.askUserCounter)
	respCh := make(chan tools.AskUserBatchResponse, 1)
	h.pendingAskUser[data.ID] = &pendingAskUser{ch: respCh, data: data}
	h.askUserMu.Unlock()

	defer func() {
		h.askUserMu.Lock()
		delete(h.pendingAskUser, data.ID)
		h.askUserMu.Unlock()
	}()

	h.emit("ask_user_request", data)

	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		return tools.AskUserBatchResponse{}, ctx.Err()
	}
}

// ResolveAskUser delivers the user's answers to a pending ask_user request.
// Called by the API handler when the frontend submits answers.
func (h *WebHandler) ResolveAskUser(id string, resp tools.AskUserBatchResponse) error {
	h.askUserMu.Lock()
	p, ok := h.pendingAskUser[id]
	h.askUserMu.Unlock()

	if !ok {
		return fmt.Errorf("no pending ask_user with id %q", id)
	}

	select {
	case p.ch <- resp:
		return nil
	default:
		return fmt.Errorf("ask_user %q already resolved", id)
	}
}

// PendingAskUserRequests returns the still-unanswered ask_user requests so a
// reloaded/reconnecting client can re-surface the question (the ask_user_request
// WS event is fire-once and ephemeral). Without this, a page refresh while a
// question is pending would leave the agent blocked with no way to answer.
func (h *WebHandler) PendingAskUserRequests() []WebAskUserRequestData {
	h.askUserMu.Lock()
	defer h.askUserMu.Unlock()
	out := make([]WebAskUserRequestData, 0, len(h.pendingAskUser))
	for _, p := range h.pendingAskUser {
		out = append(out, p.data)
	}
	return out
}
