package handler

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cnjack/jcode/internal/tui"
)

// TUIHandler adapts AgentEventHandler to a BubbleTea *tea.Program.
// It translates every interface method into a p.Send(msg) call using
// the existing TUI message types.
type TUIHandler struct {
	p                    *tea.Program
	artifactPathResolver func(string) (string, error)
}

// SetArtifactPathResolver lets the local TUI show where a managed result was
// saved without putting an absolute host path into the model-visible tool
// result or the session JSONL.
func (h *TUIHandler) SetArtifactPathResolver(resolve func(string) (string, error)) {
	h.artifactPathResolver = resolve
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

func (h *TUIHandler) OnToolCall(ev ToolCallEvent) {
	info := extractToolDisplayInfo(ev.Name, ev.Args)
	h.p.Send(tui.ToolCallMsg{
		Name:       ev.Name,
		Args:       ev.Args,
		Title:      info.Title,
		Subtitle:   info.Subtitle,
		ToolCallID: ev.ToolCallID,
		BatchID:    ev.BatchID,
		BatchIndex: ev.BatchIndex,
		BatchSize:  ev.BatchSize,
		StartedAt:  ev.StartedAt,
		Standalone: ev.Surface == ToolSurfaceStandalone,
	})
}

func (h *TUIHandler) OnToolResult(ev ToolResultEvent) {
	output := ev.Output
	if ev.Name == "generate_image" && ev.Outcome == ToolOutcomeSucceeded && len(ev.Artifacts) > 0 {
		ref := ev.Artifacts[0]
		details := fmt.Sprintf("%s, %dx%d, %d bytes", ref.MediaType, ref.Width, ref.Height, ref.Size)
		if h.artifactPathResolver != nil {
			if resolved, err := h.artifactPathResolver(ref.ID); err == nil && strings.TrimSpace(resolved) != "" {
				details = "JCode engine path: " + resolved + "\n" + details
			}
		}
		output = strings.TrimSpace(output) + "\n" + details
	}
	h.p.Send(tui.ToolResultMsg{
		Name:       ev.Name,
		Output:     output,
		ToolCallID: ev.ToolCallID,
		Err:        ev.Err,
		Denied:     ev.Denied,
		Duration:   ev.Duration,
	})
}

func (h *TUIHandler) OnToolProgress(ev ToolProgressEvent) {
	h.p.Send(tui.ToolProgressMsg{
		Name: ev.Name, ToolCallID: ev.ToolCallID, Phase: string(ev.Phase),
	})
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
	allowApproveAll := req.ApprovalClass == ""
	args := req.ToolArgs
	isBillable := req.ApprovalClass != "" || req.BillableSummary != nil
	var options []tui.ToolApprovalOption
	if isBillable {
		if _, _, err := BillableApprovalOptionIDs(req.Options); err != nil {
			return ApprovalResponse{}, err
		}
		args = formatBillableApprovalSummary(req.BillableSummary)
		options = make([]tui.ToolApprovalOption, 0, len(req.Options))
		for _, option := range req.Options {
			options = append(options, tui.ToolApprovalOption{
				ID: option.ID, Label: option.Label, Kind: option.Kind,
			})
		}
	}
	respCh := make(chan tui.ToolApprovalResponse, 1)
	msg := tui.ToolApprovalRequestMsg{
		Name:            req.ToolName,
		Args:            args,
		Resp:            respCh,
		IsExternal:      req.IsExternal,
		IsBillable:      isBillable,
		WorkerName:      req.WorkerName,
		WorkerColor:     req.WorkerColor,
		AllowApproveAll: allowApproveAll,
		Options:         options,
	}

	// p.Send() blocks on BubbleTea's unbuffered message channel.
	// When multiple tool calls need approval concurrently, the second
	// Send would block until the first approval dialog is fully processed,
	// causing unnecessary serialization. Send in a goroutine so that
	// this goroutine can immediately proceed to wait on respCh.
	go h.p.Send(msg)

	select {
	case resp := <-respCh:
		return ApprovalResponse{
			Approved:         resp.Approved,
			Mode:             ApprovalMode(resp.Mode),
			ResolvedOptionID: resp.ResolvedOptionID,
		}, nil
	case <-ctx.Done():
		return ApprovalResponse{}, ctx.Err()
	}
}
