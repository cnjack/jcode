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
