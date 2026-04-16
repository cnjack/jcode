package handler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	acp "github.com/coder/acp-go-sdk"
)

// ACPHandler implements AgentEventHandler by sending ACP SessionUpdate
// notifications through an AgentSideConnection to the connected client.
type ACPHandler struct {
	conn      *acp.AgentSideConnection
	sessionID acp.SessionId

	toolCallCounter atomic.Int64
	mu              sync.Mutex
	// activeToolCall tracks the ToolCallId for the most recently started tool.
	activeToolCall acp.ToolCallId
}

// NewACPHandler creates a handler bound to an ACP connection and session.
func NewACPHandler(conn *acp.AgentSideConnection, sessionID acp.SessionId) *ACPHandler {
	return &ACPHandler{
		conn:      conn,
		sessionID: sessionID,
	}
}

func (h *ACPHandler) nextToolCallID() acp.ToolCallId {
	n := h.toolCallCounter.Add(1)
	return acp.ToolCallId(fmt.Sprintf("tc_%d", n))
}

// --- Output events ---

func (h *ACPHandler) OnAgentText(text string) {
	_ = h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateAgentMessageText(text),
	})
}

func (h *ACPHandler) OnToolCall(name, args, _ string) {
	id := h.nextToolCallID()
	h.mu.Lock()
	h.activeToolCall = id
	h.mu.Unlock()

	_ = h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update: acp.StartToolCall(id, name,
			acp.WithStartStatus(acp.ToolCallStatusInProgress),
			acp.WithStartRawInput(args),
		),
	})
}

func (h *ACPHandler) OnToolResult(name, output, _ string, err error) {
	h.mu.Lock()
	id := h.activeToolCall
	h.mu.Unlock()

	if id == "" {
		return
	}

	status := acp.ToolCallStatusCompleted
	if err != nil {
		status = acp.ToolCallStatusFailed
	}

	var content []acp.ToolCallContent
	if output != "" {
		content = append(content, acp.ToolContent(acp.TextBlock(output)))
	}

	opts := []acp.ToolCallUpdateOpt{
		acp.WithUpdateStatus(status),
	}
	if len(content) > 0 {
		opts = append(opts, acp.WithUpdateContent(content))
	}

	_ = h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateToolCall(id, opts...),
	})
}

func (h *ACPHandler) OnTodoUpdate() {
	// No ACP equivalent; todo state is internal.
}

// --- Lifecycle ---

func (h *ACPHandler) OnAgentDone(err error) {
	// Prompt response is returned by the Prompt method; nothing to send here.
}

func (h *ACPHandler) OnTokenUpdate(info TokenUsage) {
	// ACP does not have a standard token update notification.
}

// --- Approval flow ---

func (h *ACPHandler) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	h.mu.Lock()
	activeID := h.activeToolCall
	h.mu.Unlock()
	if activeID == "" {
		activeID = h.nextToolCallID()
	}

	permResp, err := h.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: h.sessionID,
		ToolCall: acp.RequestPermissionToolCall{
			ToolCallId: activeID,
			Title:      acp.Ptr(req.ToolName),
			RawInput:   req.ToolArgs,
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
