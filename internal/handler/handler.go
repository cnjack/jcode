// Package handler defines the AgentEventHandler interface that decouples the
// agent runner from any specific UI implementation (TUI, ACP, Web, etc.).
//
// All agent-loop events flow through this interface. Concrete implementations
// adapt the events to the target transport (BubbleTea, WebSocket, ACP JSON-RPC…).
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
	OnToolCall(name, args, toolCallID string)

	// OnToolResult is called when a tool execution completes.
	OnToolResult(name, output, toolCallID string, err error)

	// OnTodoUpdate is called when the todo store is mutated.
	OnTodoUpdate()

	// --- Lifecycle events ---

	// OnAgentStart is called when the agent begins processing a user prompt,
	// before any LLM call is made. Use this to show a "thinking" / "working"
	// indicator immediately, rather than waiting for the first text chunk.
	OnAgentStart()

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

// TokenUsage carries token usage info to the UI surfaces.
//
// TotalTokens is the LAST call's total — i.e. current context-window
// occupancy, used to drive the context-usage bar. The remaining token counters
// (Prompt/Completion/Cached/Reasoning/CacheWrite/CallCount) are CUMULATIVE for
// the run's tracker, and CacheHitRate is the cumulative cached/prompt ratio.
// CacheSupported is false when the provider never reported any cached tokens,
// so the UI can show "—" instead of a misleading 0%.
//
// NOTE: the field order/types here must stay identical to WebTokenData
// (internal/handler/web.go) so OnTokenUpdate's direct struct conversion keeps
// compiling.
type TokenUsage struct {
	TotalTokens       int64
	PromptTokens      int64
	CompletionTokens  int64
	CachedTokens      int64
	ReasoningTokens   int64
	CacheWriteTokens  int64
	CallCount         int64
	CacheHitRate      float64
	CacheSupported    bool
	ModelContextLimit int // 0 if unknown
}

// ApprovalRequest describes a tool that needs user permission.
type ApprovalRequest struct {
	ToolName    string
	ToolArgs    string
	ToolCallID  string // unique ID of this tool invocation (from the LLM)
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
