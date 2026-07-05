// Package hooks implements jcode's user-configurable hook system: external
// commands fired at key points of the agent loop (before/after a tool runs, on
// session start, when the agent is about to stop, etc.).
//
// The package is a dependency-free leaf: internal/agent, internal/runner and the
// command surfaces depend on it, never the other way around. This keeps the hook
// dispatcher transport-agnostic so TUI, Web and ACP all share one implementation.
//
// The on-disk schema and the stdin/stdout protocol mirror Claude Code / Qoder so
// users can reuse familiar configs. See internal-doc/hooks-design.md.
package hooks

import "encoding/json"

// Event is a hook trigger point.
type Event string

const (
	SessionStart       Event = "SessionStart"
	UserPromptSubmit   Event = "UserPromptSubmit"
	PreToolUse         Event = "PreToolUse"
	PostToolUse        Event = "PostToolUse"
	PostToolUseFailure Event = "PostToolUseFailure"
	PreCompact         Event = "PreCompact"
	PostCompact        Event = "PostCompact"
	Stop               Event = "Stop"
)

// Blockable reports whether an event's hooks may block/deny (via exit code 2 or a
// structured decision). Non-blockable events can still inject context or modify
// results, but cannot halt the operation.
func (e Event) Blockable() bool {
	switch e {
	case UserPromptSubmit, PreToolUse, Stop:
		return true
	}
	return false
}

// Permission is a PreToolUse decision.
type Permission string

const (
	PermNone  Permission = "" // hook expressed no opinion
	PermAllow Permission = "allow"
	PermDeny  Permission = "deny"
	PermAsk   Permission = "ask"
)

// Payload is the JSON handed to a hook process over stdin. Field names match the
// Claude Code contract so existing hook scripts work unchanged.
type Payload struct {
	SessionID      string          `json:"session_id,omitempty"`
	TranscriptPath string          `json:"transcript_path,omitempty"`
	CWD            string          `json:"cwd,omitempty"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name,omitempty"`
	ToolInput      json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse   string          `json:"tool_response,omitempty"`
	Prompt         string          `json:"prompt,omitempty"`
	StopHookActive bool            `json:"stop_hook_active,omitempty"`
	Trigger        string          `json:"trigger,omitempty"` // PreCompact/PostCompact reason
}

// Decision is the folded outcome of firing all of an event's matching hooks.
// The zero value means "no hook had any opinion — proceed normally".
type Decision struct {
	// Permission is the folded PreToolUse verdict. PermDeny wins over everything.
	Permission Permission
	// Block is true when a blockable event decided to halt (exit 2 / decision=block).
	Block bool
	// Reason explains a deny/block, surfaced to the agent or user.
	Reason string
	// UpdatedInput, if non-nil, replaces tool_input before execution (PreToolUse).
	UpdatedInput json.RawMessage
	// ModifiedResult, if non-nil, replaces tool_response after execution (PostToolUse).
	ModifiedResult *string
	// AdditionalContext is text to inject into the model context.
	AdditionalContext string
	// SystemMessage is a non-blocking note surfaced to the user.
	SystemMessage string
}

// Denied reports whether the decision blocks the operation.
func (d Decision) Denied() bool { return d.Block || d.Permission == PermDeny }

// hookSpecificOutput mirrors the nested stdout JSON object a hook may print.
type hookSpecificOutput struct {
	HookEventName            string          `json:"hookEventName,omitempty"`
	PermissionDecision       string          `json:"permissionDecision,omitempty"` // allow|deny|ask
	PermissionDecisionReason string          `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             json.RawMessage `json:"updatedInput,omitempty"`
	ModifiedResult           *string         `json:"modifiedResult,omitempty"`
	AdditionalContext        string          `json:"additionalContext,omitempty"`
}

// hookOutput is the full stdout JSON envelope (parsed only on exit code 0).
type hookOutput struct {
	Continue           *bool               `json:"continue,omitempty"` // false → block (Stop/UserPromptSubmit)
	Decision           string              `json:"decision,omitempty"` // "block" | "approve"
	Reason             string              `json:"reason,omitempty"`
	SystemMessage      string              `json:"systemMessage,omitempty"`
	SuppressOutput     bool                `json:"suppressOutput,omitempty"`
	HookSpecificOutput *hookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// HookSpec is a single configured hook command.
type HookSpec struct {
	Type    string `json:"type"`              // v1 supports "command"
	Command string `json:"command"`           // shell command (run via `sh -c`)
	Timeout int    `json:"timeout,omitempty"` // seconds; 0 → dispatcher default
	Async   bool   `json:"async,omitempty"`   // fire-and-forget (non-blockable events only)
}

// HookGroup binds a matcher to a set of hook commands.
type HookGroup struct {
	Matcher string     `json:"matcher,omitempty"`
	Hooks   []HookSpec `json:"hooks"`
}

// Config is one parsed hooks.json file: event name → matcher groups.
type Config struct {
	Hooks map[string][]HookGroup `json:"hooks"`
}
