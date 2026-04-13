// Package handler defines the AgentEventHandler interface that decouples the
// agent runner from any specific UI implementation (TUI, ACP, Web, etc.).
//
// All agent-loop events flow through this interface. Concrete implementations
// adapt the events to the target transport (BubbleTea, HTTP/SSE, ACP JSON-RPC…).
package handler

import "context"

// AgentEventHandler is the primary abstraction between the agent runner and the
// presentation layer. It covers three concerns:
//
//  1. Output events  — one-way notifications from agent to UI.
//  2. Approval flow  — bidirectional: agent requests permission, UI responds.
//  3. Lifecycle      — done signals, token usage, etc.
//
// Implementations must be safe for concurrent use; the runner may call methods
// from multiple goroutines (e.g. streaming text while a tool result arrives).
type AgentEventHandler interface {
	// --- Output events ---

	// OnAgentText is called when the agent emits a text chunk (streaming).
	OnAgentText(text string)

	// OnToolCall is called at the beginning of a tool invocation.
	OnToolCall(name, args string)

	// OnToolResult is called when a tool execution completes.
	OnToolResult(name, output string, err error)

	// OnTodoUpdate is called when the todo store is mutated.
	OnTodoUpdate()

	// --- Lifecycle events ---

	// OnAgentDone is called when the agent loop finishes (err may be nil).
	OnAgentDone(err error)

	// OnTokenUpdate reports cumulative token usage after a run.
	OnTokenUpdate(info TokenUsage)

	// --- Approval flow ---

	// RequestApproval asks the UI for tool-execution permission.
	// It blocks until the user responds or ctx is cancelled.
	// Returns (approved, newMode, error).
	RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error)
}

// TokenUsage carries cumulative token counters.
type TokenUsage struct {
	PromptTokens      int64
	CompletionTokens  int64
	TotalTokens       int64
	ModelContextLimit int // 0 if unknown
}

// ApprovalRequest describes a tool that needs user permission.
type ApprovalRequest struct {
	ToolName    string
	ToolArgs    string
	IsExternal  bool   // true when accessing paths outside workpath
	WorkerName  string // non-empty for teammate agents
	WorkerColor string
}

// ApprovalMode mirrors tui.ApprovalMode so that handler consumers don't import tui.
type ApprovalMode int

const (
	ModeManual ApprovalMode = iota
	ModeAuto
)

// ApprovalResponse is what the UI returns for an approval request.
type ApprovalResponse struct {
	Approved bool
	Mode     ApprovalMode
}
