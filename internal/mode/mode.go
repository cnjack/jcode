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
	// Ask is step-by-step collaboration: the full tool set is available but
	// non-trivial tool calls gate through the per-call approval dialog. This is
	// jcode's historical default (normal tools + manual approval).
	Ask SessionMode = iota
	// Plan is read-only exploration: the agent gets the plan system prompt and a
	// read-only tool subset, produces a structured plan, then executes on
	// approval.
	Plan
	// Autopilot is end-to-end execution: the full tool set is available and every
	// tool call is auto-approved with no interruption.
	Autopilot
)

// IsPlan reports whether this mode uses the read-only plan tool/prompt axis.
func (m SessionMode) IsPlan() bool { return m == Plan }

// AutoApprove reports whether this mode auto-approves every tool call.
func (m SessionMode) AutoApprove() bool { return m == Autopilot }

// String returns the canonical wire/persistence value.
func (m SessionMode) String() string {
	switch m {
	case Plan:
		return "plan"
	case Autopilot:
		return "autopilot"
	default:
		return "ask"
	}
}

// Label returns the human-facing display name for selector UIs.
func (m SessionMode) Label() string {
	switch m {
	case Plan:
		return "Plan"
	case Autopilot:
		return "Autopilot"
	default:
		return "Ask"
	}
}

// Next returns the next mode in the selector cycle: Ask → Plan → Autopilot → Ask.
func (m SessionMode) Next() SessionMode {
	switch m {
	case Ask:
		return Plan
	case Plan:
		return Autopilot
	default:
		return Ask
	}
}

// Parse converts a persisted/wire string to a SessionMode, tolerating the legacy
// values written before the unified selector existed so old sessions and old
// clients round-trip sanely. Unknown values fall back to Ask (the safe default).
func Parse(s string) SessionMode {
	switch s {
	case "plan", "planning":
		return Plan
	case "autopilot", "auto":
		return Autopilot
	case "ask", "agent", "build", "normal", "executing", "manual", "":
		return Ask
	default:
		return Ask
	}
}
