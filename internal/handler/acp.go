package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	acp "github.com/coder/acp-go-sdk"

	"github.com/cnjack/jcode/internal/config"
)

// logACPError logs a failed ACP SessionUpdate to the debug log.
func logACPError(op string, err error) {
	config.Logger().Printf("[acp-handler] %s error: %v", op, err)
}

// ACPHandler implements AgentEventHandler by sending ACP SessionUpdate
// notifications through an AgentSideConnection to the connected client.
type ACPHandler struct {
	conn      *acp.AgentSideConnection
	sessionID acp.SessionId

	toolCallCounter atomic.Int64
	mu              sync.Mutex
	// einoToACP maps Eino tool call IDs to ACP tool call IDs so that
	// OnToolResult can find the correct ACP ID even when multiple tool calls
	// are active concurrently.
	einoToACP map[string]acp.ToolCallId
	// toolArgs caches the raw args JSON by ACP tool call ID so that
	// OnToolResult can build diff content.
	toolArgs map[acp.ToolCallId]string
	// pendingApprovals is a FIFO queue of ACP tool call IDs that have been
	// started but not yet matched to a RequestApproval call. The approval
	// middleware does not pass the Eino tool call ID, so we match by
	// (toolName, toolArgs) in arrival order.
	pendingApprovals []pendingApproval
}

type pendingApproval struct {
	acpID    acp.ToolCallId
	toolName string
	toolArgs string
}

// NewACPHandler creates a handler bound to an ACP connection and session.
func NewACPHandler(conn *acp.AgentSideConnection, sessionID acp.SessionId) *ACPHandler {
	return &ACPHandler{
		conn:      conn,
		sessionID: sessionID,
		einoToACP: make(map[string]acp.ToolCallId),
		toolArgs:  make(map[acp.ToolCallId]string),
	}
}

func (h *ACPHandler) nextToolCallID() acp.ToolCallId {
	n := h.toolCallCounter.Add(1)
	return acp.ToolCallId(fmt.Sprintf("tc_%d", n))
}

// --- Output events ---

func (h *ACPHandler) OnAgentText(text string) {
	if err := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateAgentMessageText(text),
	}); err != nil {
		logACPError("AgentText", err)
	}
}

// toolKindForName maps a jcode tool name to an ACP ToolKind.
func toolKindForName(name string) acp.ToolKind {
	switch name {
	case "read", "glob", "grep", "todoread", "check_background":
		return acp.ToolKindRead
	case "edit", "multi_edit", "write", "todowrite":
		return acp.ToolKindEdit
	case "execute", "background":
		return acp.ToolKindExecute
	default:
		return acp.ToolKindOther
	}
}

// toolCallMeta holds parsed metadata from a tool call's args JSON.
type toolCallMeta struct {
	Path      string
	StartLine int
}

// extractToolCallMeta parses the args JSON once and returns file path and line info.
func extractToolCallMeta(argsJSON string) toolCallMeta {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolCallMeta{}
	}
	var meta toolCallMeta
	for _, key := range []string{"file_path", "path", "file"} {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				meta.Path = s
				break
			}
		}
	}
	for _, key := range []string{"start_line", "line"} {
		if v, ok := args[key]; ok {
			if n, ok := v.(float64); ok {
				meta.StartLine = int(n)
				break
			}
		}
	}
	return meta
}

// parseRawInput converts a JSON args string into a map so that it serializes
// as a JSON object (not a double-escaped string) when assigned to an `any` field.
func parseRawInput(argsJSON string) any {
	if argsJSON == "" {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &obj); err != nil {
		config.Logger().Printf("[acp-handler] parseRawInput: invalid JSON args: %v", err)
		return nil
	}
	return obj
}

func (h *ACPHandler) OnToolCall(name, args, einoToolCallID string) {
	id := h.nextToolCallID()
	h.mu.Lock()
	if einoToolCallID != "" {
		h.einoToACP[einoToolCallID] = id
	}
	h.toolArgs[id] = args
	h.pendingApprovals = append(h.pendingApprovals, pendingApproval{
		acpID: id, toolName: name, toolArgs: args,
	})
	h.mu.Unlock()

	opts := []acp.ToolCallStartOpt{
		acp.WithStartStatus(acp.ToolCallStatusInProgress),
		acp.WithStartRawInput(parseRawInput(args)),
		acp.WithStartKind(toolKindForName(name)),
	}

	// Add file location for file-based tools, with optional line number.
	if meta := extractToolCallMeta(args); meta.Path != "" {
		loc := acp.ToolCallLocation{Path: meta.Path}
		if meta.StartLine > 0 {
			loc.Line = &meta.StartLine
		}
		opts = append(opts, acp.WithStartLocations([]acp.ToolCallLocation{loc}))
	}

	if err := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.StartToolCall(id, name, opts...),
	}); err != nil {
		logACPError("StartToolCall", err)
	}
}

func (h *ACPHandler) OnToolResult(name, output, einoToolCallID string, err error) {
	h.mu.Lock()
	id := h.einoToACP[einoToolCallID]
	cachedArgs := h.toolArgs[id]
	delete(h.einoToACP, einoToolCallID)
	delete(h.toolArgs, id)
	// Drop any still-queued approval entry for this ACP id (e.g. auto-approved
	// tools never go through RequestApproval and would otherwise leak and
	// poison the FIFO fallback on the next approval request).
	if id != "" {
		for i, p := range h.pendingApprovals {
			if p.acpID == id {
				h.pendingApprovals = append(h.pendingApprovals[:i], h.pendingApprovals[i+1:]...)
				break
			}
		}
	}
	h.mu.Unlock()

	if id == "" {
		return
	}

	status := acp.ToolCallStatusCompleted
	if err != nil {
		status = acp.ToolCallStatusFailed
	}

	var content []acp.ToolCallContent
	switch name {
	case "edit":
		content = buildEditDiffContent(cachedArgs, output)
	case "write":
		content = buildWriteDiffContent(cachedArgs, output)
	}
	// Always include the text output as well.
	if output != "" {
		content = append(content, acp.ToolContent(acp.TextBlock(output)))
	}

	opts := []acp.ToolCallUpdateOpt{
		acp.WithUpdateStatus(status),
	}
	if len(content) > 0 {
		opts = append(opts, acp.WithUpdateContent(content))
	}
	// Include output as rawOutput for structured access.
	if output != "" {
		opts = append(opts, acp.WithUpdateRawOutput(output))
	}

	if updateErr := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateToolCall(id, opts...),
	}); updateErr != nil {
		logACPError("UpdateToolCall", updateErr)
	}
}

func (h *ACPHandler) OnTodoUpdate() {
	// No ACP equivalent; todo state is internal.
}

// --- Lifecycle ---

func (h *ACPHandler) OnAgentStart() {
	// ACP does not have a standard "agent started" notification.
}

func (h *ACPHandler) OnAgentDone(err error) {
	// Prompt response is returned by the Prompt method; nothing to send here.
}

func (h *ACPHandler) OnTokenUpdate(info TokenUsage) {
	// ACP does not have a standard token update notification.
}

// --- Approval flow ---

func (h *ACPHandler) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	h.mu.Lock()
	// Find the matching pending tool call by name + args.
	var matchedID acp.ToolCallId
	for i, p := range h.pendingApprovals {
		if p.toolName == req.ToolName && p.toolArgs == req.ToolArgs {
			matchedID = p.acpID
			h.pendingApprovals = append(h.pendingApprovals[:i], h.pendingApprovals[i+1:]...)
			break
		}
	}
	// Fallback: use first pending if no exact match (e.g. args were modified).
	if matchedID == "" && len(h.pendingApprovals) > 0 {
		matchedID = h.pendingApprovals[0].acpID
		h.pendingApprovals = h.pendingApprovals[1:]
	}
	h.mu.Unlock()
	if matchedID == "" {
		matchedID = h.nextToolCallID()
	}

	permResp, err := h.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: h.sessionID,
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: matchedID,
			Title:      acp.Ptr(req.ToolName),
			RawInput:   parseRawInput(req.ToolArgs),
		},
		Options: []acp.PermissionOption{
			{
				OptionId: "allow_once",
				Name:     "Allow",
				Kind:     acp.PermissionOptionKindAllowOnce,
			},
			{
				OptionId: "reject_once",
				Name:     "Deny",
				Kind:     acp.PermissionOptionKindRejectOnce,
			},
			{
				OptionId: "allow_always",
				Name:     "Allow All (auto-approve this session)",
				Kind:     acp.PermissionOptionKindAllowAlways,
			},
		},
	})
	if err != nil {
		return ApprovalResponse{}, err
	}

	if permResp.Outcome.Cancelled != nil {
		return ApprovalResponse{Approved: false, Mode: ModeManual}, nil
	}

	if permResp.Outcome.Selected != nil {
		switch string(permResp.Outcome.Selected.OptionId) {
		case "allow_once":
			return ApprovalResponse{Approved: true, Mode: ModeManual}, nil
		case "allow_always":
			return ApprovalResponse{Approved: true, Mode: ModeAuto}, nil
		case "reject_once":
			return ApprovalResponse{Approved: false, Mode: ModeManual}, nil
		}
	}

	return ApprovalResponse{Approved: false, Mode: ModeManual}, nil
}

// buildEditDiffContent extracts diff information from an edit tool call and
// returns ToolCallContentDiff entries.
func buildEditDiffContent(argsJSON, _ string) []acp.ToolCallContent {
	var args struct {
		FilePath  string `json:"file_path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
		Edits     []struct {
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.FilePath == "" {
		return nil
	}

	var content []acp.ToolCallContent
	if args.OldString != "" || args.NewString != "" {
		content = append(content, acp.ToolDiffContent(args.FilePath, args.NewString, args.OldString))
	}
	for _, e := range args.Edits {
		content = append(content, acp.ToolDiffContent(args.FilePath, e.NewString, e.OldString))
	}
	return content
}

// buildWriteDiffContent creates a diff entry for a write tool call (new file
// creation or full file overwrite).
func buildWriteDiffContent(argsJSON, _ string) []acp.ToolCallContent {
	var args struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.FilePath == "" {
		return nil
	}
	return []acp.ToolCallContent{
		acp.ToolDiffContent(args.FilePath, args.Content),
	}
}
