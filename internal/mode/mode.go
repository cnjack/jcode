// Package mode defines the unified, user-facing session mode that the three
// frontends (TUI, Web, ACP) all present through a single selector.
//
// jcode historically tracked two independent axes:
//   - the tool/prompt axis (normal vs read-only plan), and
//   - the approval axis (approve-each vs auto-approve).
//
// SessionMode collapses those two axes into one three-state selector and is the
// ONLY mode value a frontend ever sets. Each frontend derives the two low-level
// axes from it via the pure helpers below — IsPlan() drives the tool/prompt
// axis and AutoApprove() drives the approval axis. This package imports nothing
// internal on purpose: it is a leaf so every other package can depend on it
// without risking an import cycle.
package mode

// SessionMode is the unified selector value.
type SessionMode int

const (
	// Approval is step-by-step collaboration: the full tool set is available but
	// non-trivial tool calls gate through the per-call approval dialog. This is
	// jcode's historical default (normal tools + manual approval).
	Approval SessionMode = iota
	// Plan is read-only exploration: the agent gets the plan system prompt and a
	// read-only tool subset, produces a structured plan, then executes on
	// approval.
	Plan
	// Auto uses the full tool set and consults an LLM reviewer for every call
	// that would otherwise prompt. The reviewer auto-allows low-risk calls,
	// denies high-risk calls, and escalates uncertain calls back to the user.
	Auto
	// FullAccess is end-to-end execution: the full tool set is available and every
	// tool call is auto-approved with no interruption.
	FullAccess
)

// IsPlan reports whether this mode uses the read-only plan tool/prompt axis.
func (m SessionMode) IsPlan() bool { return m == Plan }

// AutoApprove reports whether this mode auto-approves every tool call.
func (m SessionMode) AutoApprove() bool { return m == FullAccess }

// String returns the canonical wire/persistence value.
func (m SessionMode) String() string {
	switch m {
	case Plan:
		return "plan"
	case Auto:
		return "auto"
	case FullAccess:
		return "full_access"
	default:
		return "approval"
	}
}

// Label returns the human-facing display name for selector UIs.
func (m SessionMode) Label() string {
	switch m {
	case Plan:
		return "Plan"
	case Auto:
		return "Auto"
	case FullAccess:
		return "Full access"
	default:
		return "Ask for approval"
	}
}

// Next returns the next mode in the selector cycle:
// Ask for approval → Plan → Auto → Full access → Ask for approval.
func (m SessionMode) Next() SessionMode {
	switch m {
	case Approval:
		return Plan
	case Plan:
		return Auto
	case Auto:
		return FullAccess
	default:
		return Approval
	}
}

// Parse converts a persisted/wire string to a SessionMode. Unknown and unset
// values fall back to Approval, the safe default.
func Parse(s string) SessionMode {
	switch s {
	case "plan":
		return Plan
	case "auto":
		return Auto
	case "full_access":
		return FullAccess
	case "approval", "":
		return Approval
	default:
		return Approval
	}
}
