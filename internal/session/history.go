package session

import (
	"encoding/json"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// ReconstructHistory converts a slice of recorded session entries back into
// LLM history messages suitable for resuming a conversation.
// Only user and assistant text messages are included; tool calls are omitted
// because reconstructing matching tool-call IDs is non-trivial.
func ReconstructHistory(entries []Entry) []adk.Message {
	var msgs []adk.Message
	for _, e := range entries {
		switch e.Type {
		case EntryUser:
			msgs = append(msgs, schema.UserMessage(e.Content))
		case EntryAssistant:
			if e.Content != "" {
				msgs = append(msgs, &schema.Message{Role: schema.Assistant, Content: e.Content})
			}
		}
	}
	return msgs
}

// PlanSnapshot holds the last known plan state from a session.
type PlanSnapshot struct {
	Status   string
	Title    string
	Content  string
	Feedback string
}

// SessionState is the full recoverable state from a session file, including
// conversation history, plan, todos, mode, and environment.
type SessionState struct {
	History   []adk.Message
	Plan      *PlanSnapshot      // nil if no plan events found
	Todos     []TodoSnapshotItem // last todo snapshot, nil if none
	Mode      string             // last mode (normal/planning/executing), empty = normal
	EnvTarget string             // last environment (local/ssh alias)
}

// ReconstructState rebuilds the full session state from recorded entries.
// It is compact-aware: if a compact entry is found, messages before it are
// replaced with the compact summary.
func ReconstructState(entries []Entry) *SessionState {
	state := &SessionState{
		EnvTarget: "local",
	}

	var msgs []adk.Message
	var lastTarget string

	for _, e := range entries {
		switch e.Type {
		case EntryUser:
			msgs = append(msgs, schema.UserMessage(e.Content))

		case EntryAssistant:
			if e.Content != "" {
				msgs = append(msgs, &schema.Message{Role: schema.Assistant, Content: e.Content})
			}

		case EntryCompact:
			// Discard accumulated history and use the compact summary as base.
			msgs = []adk.Message{
				&schema.Message{Role: schema.System, Content: e.Summary},
			}

		case EntryPlanUpdate:
			if state.Plan == nil {
				state.Plan = &PlanSnapshot{}
			}
			state.Plan.Status = e.PlanStatus
			if e.PlanTitle != "" {
				state.Plan.Title = e.PlanTitle
			}
			if e.PlanContent != "" {
				state.Plan.Content = e.PlanContent
			}
			state.Plan.Feedback = e.Feedback

		case EntryTodoSnapshot:
			state.Todos = e.Todos

		case EntryModeChange:
			state.Mode = e.Mode

		case EntryToolCall:
			if e.Name == "switch_env" {
				type args struct {
					Target string `json:"target"`
				}
				var a args
				if err := json.Unmarshal([]byte(e.Args), &a); err == nil {
					lastTarget = a.Target
				}
			}

		case EntryToolResult:
			if e.Name == "switch_env" {
				if e.Error == "" && lastTarget != "" {
					state.EnvTarget = lastTarget
				}
			}
		}
	}

	state.History = msgs
	return state
}

// GetLastEnvironment scans the session entries to find the last successful switch_env call,
// and returns the target environment alias. If none is found, it returns "local".
func GetLastEnvironment(entries []Entry) string {
	lastEnv := "local"
	var lastTarget string

	for _, e := range entries {
		if e.Type == EntryToolCall && e.Name == "switch_env" {
			// Extract target from args
			type args struct {
				Target string `json:"target"`
			}
			var a args
			if err := json.Unmarshal([]byte(e.Args), &a); err == nil {
				lastTarget = a.Target
			}
		} else if e.Type == EntryToolResult && e.Name == "switch_env" {
			if e.Error == "" && lastTarget != "" {
				lastEnv = lastTarget
			}
		}
	}
	return lastEnv
}
