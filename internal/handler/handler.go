// Package handler defines the AgentEventHandler interface that decouples the
// agent runner from any specific UI implementation (TUI, ACP, Web, etc.).
//
// All agent-loop events flow through this interface. Concrete implementations
// adapt the events to the target transport (BubbleTea, WebSocket, ACP JSON-RPC…).
package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/toolstate"
)

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
	OnToolCall(ev ToolCallEvent)

	// OnToolResult is called when a tool execution completes.
	OnToolResult(ev ToolResultEvent)

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

// ToolCallEvent describes a single tool invocation announced by the agent.
// All tool calls issued by one assistant message share a BatchID so UIs can
// group concurrent invocations; a single-tool message still forms a batch
// (BatchSize == 1).
type ToolCallEvent struct {
	Name        string
	Args        string
	ToolCallID  string
	Surface     ToolSurface
	Phase       ToolPhase
	OperationID string
	BatchID     string    // batch identity: one assistant message = one batch
	BatchIndex  int       // 0-based position inside the batch
	BatchSize   int       // number of tool calls in the batch
	StartedAt   time.Time // when the runner announced the call
}

// ToolResultEvent describes a completed tool execution.
type ToolResultEvent struct {
	Name       string
	Output     string
	ToolCallID string
	Err        error
	// Duration is result arrival minus call announcement, with any time spent
	// blocked on user approval subtracted (pure execution latency); 0 when
	// unknown.
	Duration time.Duration
	// Denied is true when the user rejected this tool call at the approval
	// prompt. UIs should render this as "declined" (e.g. strikethrough), not
	// as an execution error.
	Denied      bool
	Surface     ToolSurface
	Phase       ToolPhase
	OperationID string
	Outcome     ToolOutcome
	ErrorCode   string
	Provider    string
	Model       string
	Artifacts   []ArtifactRef
}

type ToolSurface = toolstate.Surface

const (
	ToolSurfaceActivity   = toolstate.SurfaceActivity
	ToolSurfaceStandalone = toolstate.SurfaceStandalone
)

type ToolPhase = toolstate.Phase

const (
	ToolPhaseQueued     = toolstate.PhaseQueued
	ToolPhaseGenerating = toolstate.PhaseGenerating
	ToolPhaseSaving     = toolstate.PhaseSaving
	ToolPhaseSucceeded  = toolstate.PhaseSucceeded
	ToolPhaseFailed     = toolstate.PhaseFailed
	ToolPhaseCancelled  = toolstate.PhaseCancelled
	ToolPhaseUncertain  = toolstate.PhaseUncertain
)

type ToolOutcome = toolstate.Outcome

const (
	ToolOutcomeSucceeded = toolstate.OutcomeSucceeded
	ToolOutcomeFailed    = toolstate.OutcomeFailed
	ToolOutcomeCancelled = toolstate.OutcomeCancelled
	ToolOutcomeUncertain = toolstate.OutcomeUncertain
)

// ArtifactRef is the safe, transport-facing subset of an Artifact record.
// It intentionally contains no absolute path or provider source URL.
type ArtifactRef = toolstate.ArtifactRef

// ToolProgressEvent is an additive status update for long-running tools. It is
// delivered through an optional interface so older/custom handlers continue to
// satisfy AgentEventHandler without changes.
type ToolProgressEvent = toolstate.ProgressEvent

type ToolProgressHandler interface {
	OnToolProgress(ToolProgressEvent)
}

func EmitToolProgress(h AgentEventHandler, event ToolProgressEvent) {
	if progress, ok := h.(ToolProgressHandler); ok {
		progress.OnToolProgress(event)
	}
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
	ToolName        string
	ToolArgs        string
	ToolCallID      string // unique ID of this tool invocation (from the LLM)
	IsExternal      bool   // true when accessing paths outside workpath
	WorkerName      string // non-empty for teammate agents
	WorkerColor     string
	ApprovalClass   string
	OperationID     string
	CapabilityKey   string
	Provider        string
	Model           string
	BillableSummary *BillableApprovalSummary
	// Options are runner-issued opaque, one-shot decisions for a structured
	// approval. Transports must present and return these exact IDs; they must
	// never mint replacements or coerce the decision back to a boolean.
	Options []ApprovalOption
	// AllowApproveAll is false for billable/external operations whose grant
	// must be bounded to the exact immutable intent.
	AllowApproveAll bool
}

type ApprovalOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Kind        string `json:"kind,omitempty"`
	Description string `json:"description,omitempty"`
}

// BillableApprovalOptionIDs validates the transport-neutral option contract
// for an externally billable request. Exactly one allow-once and one deny
// option are required; blanket grants and custom decisions are forbidden.
func BillableApprovalOptionIDs(options []ApprovalOption) (allowOnceID, denyID string, err error) {
	if len(options) != 2 {
		return "", "", fmt.Errorf("billable approval requires exactly two opaque options")
	}
	for _, option := range options {
		if strings.TrimSpace(option.ID) == "" {
			return "", "", fmt.Errorf("billable approval option id is required")
		}
		switch option.Kind {
		case "allow_once":
			if allowOnceID != "" {
				return "", "", fmt.Errorf("billable approval has duplicate allow-once options")
			}
			allowOnceID = option.ID
		case "deny":
			if denyID != "" {
				return "", "", fmt.Errorf("billable approval has duplicate deny options")
			}
			denyID = option.ID
		default:
			return "", "", fmt.Errorf("billable approval option kind %q is not allowed", option.Kind)
		}
	}
	if allowOnceID == "" || denyID == "" || allowOnceID == denyID {
		return "", "", fmt.Errorf("billable approval options are invalid")
	}
	return allowOnceID, denyID, nil
}

type BillableApprovalSummary struct {
	Capability   string `json:"capability,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	Size         string `json:"size,omitempty"`
	AspectRatio  string `json:"aspect_ratio,omitempty"`
	Resolution   string `json:"resolution,omitempty"`
	Count        int    `json:"count,omitempty"`
	Billable     bool   `json:"billable,omitempty"`
	HasReference bool   `json:"has_reference,omitempty"`
}

func formatBillableApprovalSummary(summary *BillableApprovalSummary) string {
	if summary == nil {
		return "External provider request"
	}
	count := summary.Count
	if count <= 0 {
		count = 1
	}
	parts := []string{
		"Capability: " + summary.Capability,
		"Provider: " + summary.Provider,
		"Model: " + summary.Model,
		fmt.Sprintf("Count: %d", count),
	}
	if summary.Size != "" {
		parts = append(parts, "Size: "+summary.Size)
	}
	if summary.AspectRatio != "" {
		parts = append(parts, "Aspect ratio: "+summary.AspectRatio)
	}
	if summary.Resolution != "" {
		parts = append(parts, "Resolution: "+summary.Resolution)
	}
	if summary.HasReference {
		parts = append(parts, "Reference image: yes")
	}
	if summary.Billable {
		parts = append(parts, "This request may incur provider charges.")
	}
	return strings.Join(parts, "\n")
}

func billableApprovalTitle(summary *BillableApprovalSummary) string {
	if summary == nil {
		return "Approve external provider request"
	}
	action := "Approve external provider request"
	switch summary.Capability {
	case "image.generate":
		action = "Generate image"
	case "web.search":
		action = "Use provider web search"
	}
	target := strings.Trim(strings.Join([]string{summary.Provider, summary.Model}, " / "), " /")
	if target != "" {
		action += " with " + target
	}
	return action
}

// ApprovalMode mirrors tui.ApprovalMode so that handler consumers don't import tui.
type ApprovalMode int

const (
	ModeManual ApprovalMode = iota
	ModeAuto
)

// ApprovalResponse is what the UI returns for an approval request.
type ApprovalResponse struct {
	Approved         bool
	Mode             ApprovalMode
	ResolvedOptionID string
}
