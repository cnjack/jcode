package session

import (
	"encoding/json"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// entryToUserMessage converts a session Entry into a schema.Message,
// restoring multimodal content (images) when present.
func entryToUserMessage(e Entry) *schema.Message {
	if len(e.Images) == 0 {
		return schema.UserMessage(e.Content)
	}
	parts := make([]schema.MessageInputPart, 0, len(e.Images)+1)
	if e.Content != "" {
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeText,
			Text: e.Content,
		})
	}
	for _, img := range e.Images {
		data := img.Data
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{
				MessagePartCommon: schema.MessagePartCommon{
					MIMEType:   img.MimeType,
					Base64Data: &data,
				},
			},
		})
	}
	return &schema.Message{
		Role:                  schema.User,
		Content:               e.Content,
		UserInputMultiContent: parts,
	}
}

// ReconstructHistory converts a slice of recorded session entries back into
// LLM history messages suitable for resuming a conversation.
// It reconstructs tool call and tool result messages so that resumed sessions
// retain full context.
//
// Subagent-internal entries (tool_call / tool_result / assistant recorded
// between subagent_start and subagent_result) are skipped — only the main
// agent's own messages are included.
//
// Because the runner records assistant text AFTER tool calls in the JSONL,
// an EntryAssistant that follows tool-call entries is merged back into the
// preceding assistant message as its Content field.
func ReconstructHistory(entries []Entry) []adk.Message {
	var msgs []adk.Message
	var subagentDepth int
	for _, e := range entries {
		switch e.Type {
		case EntrySubagentStart:
			subagentDepth++
			continue
		case EntrySubagentResult:
			if subagentDepth > 0 {
				subagentDepth--
			}
			continue
		}
		// Skip entries that belong to a running subagent.
		if subagentDepth > 0 {
			continue
		}

		switch e.Type {
		case EntryUser:
			msgs = append(msgs, entryToUserMessage(e))
		case EntryAssistant:
			if e.Content != "" {
				// The runner records assistant text after tool calls, so the
				// preceding message may already be an assistant with ToolCalls
				// but empty Content. Merge into it when possible.
				if n := len(msgs); n > 0 {
					if last := msgs[n-1]; last.Role == schema.Assistant && last.Content == "" && len(last.ToolCalls) > 0 {
						last.Content = e.Content
						continue
					}
				}
				msgs = append(msgs, &schema.Message{Role: schema.Assistant, Content: e.Content})
			}
		case EntryToolCall:
			tc := schema.ToolCall{
				ID:       e.ToolCallID,
				Function: schema.FunctionCall{Name: e.Name, Arguments: e.Args},
			}
			// Merge into preceding assistant message if it exists, otherwise create one.
			if n := len(msgs); n > 0 {
				if last := msgs[n-1]; last.Role == schema.Assistant {
					last.ToolCalls = append(last.ToolCalls, tc)
					continue
				}
			}
			msgs = append(msgs, &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{tc}})
		case EntryToolResult:
			msgs = append(msgs, schema.ToolMessage(e.Output, e.ToolCallID, schema.WithToolName(e.Name)))
		}
	}
	return msgs
}

// toolPlaceholders maps tool names to actionable placeholder messages.
// These tell the model what happened and how to recover the data.
var toolPlaceholders = map[string]string{
	"read":    "[File was read previously. Use the read tool again if needed.]",
	"grep":    "[Search was performed. Run grep again for current results.]",
	"execute": "[Command was executed. Run it again if you need fresh output.]",
}

// defaultPlaceholder is used for tools not in the map above.
const defaultPlaceholder = "[Old tool output cleared. Re-run the tool if needed.]"

// PruneOldToolOutputs replaces old tool result outputs with actionable
// placeholders, protecting the most recent turns from pruning.
// This implements the Tier 1.5 "placeholder compression" strategy:
// recent tool outputs are preserved verbatim; older ones are replaced
// with hints telling the model how to recover the data.
//
// protectTurns is the number of recent user turns to protect (default 2).
// Returns the pruned messages slice (same backing array, modified in place).
func PruneOldToolOutputs(msgs []adk.Message, protectTurns int) []adk.Message {
	if protectTurns <= 0 {
		protectTurns = 2
	}

	// Find the protection boundary by counting user messages backwards.
	userCount := 0
	protectFrom := len(msgs) // index from which messages are protected
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == schema.User {
			userCount++
			if userCount >= protectTurns {
				protectFrom = i
				break
			}
		}
	}

	// Replace old tool outputs with placeholders.
	for i := 0; i < protectFrom; i++ {
		msg := msgs[i]
		if msg.Role != schema.Tool {
			continue
		}
		toolName := msg.ToolName
		if toolName == "" {
			// Extract from MultiContent if available.
			for _, tc := range msg.ToolCalls {
				toolName = tc.Function.Name
			}
		}
		placeholder, ok := toolPlaceholders[toolName]
		if !ok {
			placeholder = defaultPlaceholder
		}
		msg.Content = placeholder
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

// GoalSnapshot is the recoverable state of a session goal.
type GoalSnapshot struct {
	Objective  string
	Status     string
	TokensUsed int64
}

// SessionState is the full recoverable state from a session file, including
// conversation history, plan, todos, mode, and environment.
type SessionState struct {
	History      []adk.Message
	Plan         *PlanSnapshot      // nil if no plan events found
	Todos        []TodoSnapshotItem // last todo snapshot, nil if none
	Goal         *GoalSnapshot      // nil if no goal events or last event cleared it
	Mode         string             // last mode (normal/planning/executing), empty = normal
	EnvTarget    string             // last environment (local/ssh alias)
	SystemPrompt string             // recorded system prompt for KV-cache-friendly resume
	EnvInfo      string             // environment snapshot at recording time
}

// ReconstructState rebuilds the full session state from recorded entries.
// It is compact-aware: if a compact entry is found, messages before it are
// replaced with the compact summary.
//
// Subagent-internal entries are skipped (same logic as ReconstructHistory).
func ReconstructState(entries []Entry) *SessionState {
	state := &SessionState{
		EnvTarget: "local",
	}

	var msgs []adk.Message
	var lastTarget string
	var subagentDepth int

	for _, e := range entries {
		// Track subagent boundaries first.
		switch e.Type {
		case EntrySubagentStart:
			subagentDepth++
			continue
		case EntrySubagentResult:
			if subagentDepth > 0 {
				subagentDepth--
			}
			continue
		case EntrySubagentAsync:
			continue
		}

		// Skip entries that belong to a running subagent.
		if subagentDepth > 0 {
			continue
		}

		switch e.Type {
		case EntryUser:
			msgs = append(msgs, entryToUserMessage(e))

		case EntryAssistant:
			if e.Content != "" {
				// Merge into preceding assistant message that has tool calls
				// but empty content (runner records text after tool calls).
				if n := len(msgs); n > 0 {
					if last := msgs[n-1]; last.Role == schema.Assistant && last.Content == "" && len(last.ToolCalls) > 0 {
						last.Content = e.Content
						continue
					}
				}
				msgs = append(msgs, &schema.Message{Role: schema.Assistant, Content: e.Content})
			}

		case EntryToolCall:
			tc := schema.ToolCall{
				ID:       e.ToolCallID,
				Function: schema.FunctionCall{Name: e.Name, Arguments: e.Args},
			}
			merged := false
			if n := len(msgs); n > 0 {
				if last := msgs[n-1]; last.Role == schema.Assistant {
					last.ToolCalls = append(last.ToolCalls, tc)
					merged = true
				}
			}
			if !merged {
				msgs = append(msgs, &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{tc}})
			}
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
			msgs = append(msgs, schema.ToolMessage(e.Output, e.ToolCallID, schema.WithToolName(e.Name)))
			if e.Name == "switch_env" {
				if e.Error == "" && lastTarget != "" {
					state.EnvTarget = lastTarget
				}
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

		case EntryGoalUpdate:
			if e.GoalStatus == "cleared" {
				state.Goal = nil
			} else {
				state.Goal = &GoalSnapshot{
					Objective:  e.GoalObjective,
					Status:     e.GoalStatus,
					TokensUsed: e.GoalTokensUsed,
				}
			}

		case EntryModeChange:
			state.Mode = e.Mode

		case EntrySystemPrompt:
			state.SystemPrompt = e.Content
			state.EnvInfo = e.EnvInfo
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
