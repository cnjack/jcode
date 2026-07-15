package review

import (
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// maxHistoryMsgs bounds how much of the conversation tail is handed to the
// reviewer as evidence. It matches maxTranscriptMsgs (the renderer's own cap) so
// the frontends and the renderer agree on the window.
const maxHistoryMsgs = maxTranscriptMsgs

// MsgsFromHistory converts the tail of an agent conversation into reviewer
// evidence: the last maxHistoryMsgs non-empty messages, with the system prompt
// dropped (it is jcode's own instructions, not evidence of user intent).
//
// All three frontends (ACP, TUI, web) feed the reviewer through this one
// function so their notion of "recent conversation" cannot drift. Callers are
// responsible for holding whatever lock guards their history slice.
func MsgsFromHistory(history []adk.Message) []Msg {
	msgs := history
	if len(msgs) > maxHistoryMsgs {
		msgs = msgs[len(msgs)-maxHistoryMsgs:]
	}
	out := make([]Msg, 0, len(msgs))
	for _, m := range msgs {
		if m == nil || m.Content == "" {
			continue
		}
		role := "user"
		switch m.Role {
		case schema.Assistant:
			role = "assistant"
		case schema.Tool:
			role = "tool"
		case schema.System:
			continue
		}
		out = append(out, Msg{Role: role, Content: m.Content})
	}
	return out
}
