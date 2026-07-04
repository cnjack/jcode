package memory

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
)

// UsageMiddleware observes every tool call and, when the call reads a file
// under the memory root, bumps that file's usage counter. This is the
// zero-model-compliance usage feedback channel (design §3.2): no citation
// blocks, no prompt cooperation — plain argument sniffing in Go.
type usageMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

// NewUsageMiddleware returns the middleware; safe to add unconditionally
// (it is a no-op for tool calls that never touch the memory root).
func NewUsageMiddleware() adk.ChatModelAgentMiddleware {
	return &usageMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
}

func (m *usageMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, argumentsInJSON, opts...)
		// Account only after a successful execution: a rejected or failed
		// call is not evidence the memory was actually used. Pipeline-internal
		// agents run with accounting disabled (see WithoutUsageAccounting).
		// Fire-and-forget: usage accounting takes a file lock and rewrites
		// state.json, which must never block or slow the tool-call hot path.
		if err == nil && !accountingDisabled(ctx) && argsMayHitMemory(argumentsInJSON) {
			go func() {
				defer func() { _ = recover() }()
				recordArgsUsage(argumentsInJSON)
			}()
		}
		return result, err
	}, nil
}

// argument keys that carry paths in jcode's built-in tools.
var pathKeys = map[string]bool{
	"file_path": true, "path": true, "dir": true, "directory": true, "root": true,
}

// argsMayHitMemory is a cheap pre-filter so the common case (no memory path
// in the args) never even spawns a goroutine.
func argsMayHitMemory(argumentsInJSON string) bool {
	return strings.Contains(argumentsInJSON, "memory")
}

func recordArgsUsage(argumentsInJSON string) {
	root := Root()
	var args map[string]any
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return
	}
	for k, v := range args {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if pathKeys[k] {
			if strings.HasPrefix(s, root) {
				RecordUsage(s)
			}
			continue
		}
		if k == "command" {
			// shell command: credit any whitespace-separated token that
			// points into the memory root (quotes stripped).
			for _, tok := range strings.Fields(s) {
				tok = strings.Trim(tok, `"'`)
				if strings.HasPrefix(tok, root) {
					RecordUsage(tok)
				}
			}
		}
	}
}
