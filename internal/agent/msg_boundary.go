package agent

import "github.com/cloudwego/eino/schema"

// findToolBoundary adjusts a split index so that it does not land between an
// assistant message with ToolCalls and the corresponding tool-result messages.
// Given a desired split at position idx (we want to keep msgs[idx:]),
// it moves idx backwards until the split no longer orphans tool results.
//
// This prevents API errors like "messages parameter is invalid" that occur when tool
// result messages appear without their preceding assistant tool-call message.
func findToolBoundary(msgs []*schema.Message, idx int) int {
	if idx <= 0 || idx >= len(msgs) {
		return idx
	}

	// If msgs[idx] is a tool result, walk backwards to include the assistant
	// message that triggered it.
	for idx > 0 && msgs[idx].Role == schema.Tool {
		idx--
	}

	return idx
}
