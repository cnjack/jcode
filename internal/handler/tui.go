package handler

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/cnjack/jcode/internal/tui"
)

// TUIHandler adapts AgentEventHandler to a BubbleTea *tea.Program.
// It translates every interface method into a p.Send(msg) call using
// the existing TUI message types.
type TUIHandler struct {
	p *tea.Program
}

// NewTUIHandler creates a handler backed by a BubbleTea program.
func NewTUIHandler(p *tea.Program) *TUIHandler {
	return &TUIHandler{p: p}
}

// SetProgram replaces the underlying BubbleTea program (e.g. after
// the program is created lazily).
func (h *TUIHandler) SetProgram(p *tea.Program) {
	h.p = p
}

// --- Output events ---

func (h *TUIHandler) OnAgentText(text string) {
	h.p.Send(tui.AgentTextMsg{Text: text})
}

func (h *TUIHandler) OnToolCall(name, args, _ string) {
	info := extractToolDisplayInfo(name, args)
	h.p.Send(tui.ToolCallMsg{
		Name:     name,
		Args:     args,
		Title:    info.Title,
		Subtitle: info.Subtitle,
	})
}

func (h *TUIHandler) OnToolResult(name, output, _ string, err error) {
	h.p.Send(tui.ToolResultMsg{Name: name, Output: output, Err: err})
}

func (h *TUIHandler) OnTodoUpdate() {
	h.p.Send(tui.TodoUpdateMsg{})
}

// --- Lifecycle ---

func (h *TUIHandler) OnAgentStart() {
	// TUI sets thinking=true at prompt submit time already, no additional action needed.
}

func (h *TUIHandler) OnAgentDone(err error) {
	h.p.Send(tui.AgentDoneMsg{Err: err})
}

func (h *TUIHandler) OnTokenUpdate(info TokenUsage) {
	h.p.Send(tui.TokenUpdateMsg{
		TotalTokens:       info.TotalTokens,
		ModelContextLimit: info.ModelContextLimit,
	})
}

// --- Approval flow ---

func (h *TUIHandler) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	respCh := make(chan tui.ToolApprovalResponse, 1)
	h.p.Send(tui.ToolApprovalRequestMsg{
		Name:        req.ToolName,
		Args:        req.ToolArgs,
		Resp:        respCh,
		IsExternal:  req.IsExternal,
		WorkerName:  req.WorkerName,
		WorkerColor: req.WorkerColor,
	})

	select {
	case resp := <-respCh:
		return ApprovalResponse{
			Approved: resp.Approved,
			Mode:     ApprovalMode(resp.Mode),
		}, nil
	case <-ctx.Done():
		return ApprovalResponse{}, ctx.Err()
	}
}
