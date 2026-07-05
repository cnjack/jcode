package agent

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"

	"github.com/cnjack/jcode/internal/hooks"
)

// hookToolMiddleware fires the tool-related hooks around a tool invocation. It is
// inserted twice into the chain (see NewAgent):
//
//   - pre  (post=false): sits OUTSIDE the approval middleware. It runs PreToolUse,
//     which can deny the call, rewrite its arguments, inject context, or mark the
//     call pre-approved so the approval gate skips its user prompt.
//   - post (post=true):  sits INSIDE the approval middleware, wrapping the raw
//     tool. It sees the real execution error (the approval layer folds errors into
//     strings), so it can distinguish PostToolUse from PostToolUseFailure and
//     rewrite the result.
//
// The dispatcher is read from the context (injected by the command surface), so a
// session with no hooks configured makes these middlewares free no-ops.
type hookToolMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	post bool
}

func newPreHookMiddleware() adk.ChatModelAgentMiddleware {
	return &hookToolMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, post: false}
}

func newPostHookMiddleware() adk.ChatModelAgentMiddleware {
	return &hookToolMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, post: true}
}

func (m *hookToolMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if m.post {
		return m.wrapPost(endpoint, tCtx), nil
	}
	return m.wrapPre(endpoint, tCtx), nil
}

// wrapPre handles PreToolUse.
func (m *hookToolMiddleware) wrapPre(endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) adk.InvokableToolCallEndpoint {
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		disp := hooks.DispatcherFromContext(ctx)
		if !disp.Configured(hooks.PreToolUse) {
			return endpoint(ctx, args, opts...)
		}
		dec := disp.Fire(ctx, hooks.PreToolUse, hooks.Payload{
			ToolName:  tCtx.Name,
			ToolInput: json.RawMessage(args),
		})
		if dec.Denied() {
			return hookDenyMessage(dec.Reason), nil
		}
		if len(dec.UpdatedInput) > 0 {
			args = string(dec.UpdatedInput)
		}
		if dec.Permission == hooks.PermAllow {
			// Let the inner approval gate skip its user prompt for this call.
			ctx = hooks.WithPreApproved(ctx)
		}
		result, err := endpoint(ctx, args, opts...)
		if dec.AdditionalContext != "" {
			result = appendHookContext(result, dec.AdditionalContext)
		}
		return result, err
	}
}

// wrapPost handles PostToolUse / PostToolUseFailure.
func (m *hookToolMiddleware) wrapPost(endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) adk.InvokableToolCallEndpoint {
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, args, opts...)

		event := hooks.PostToolUse
		if err != nil {
			event = hooks.PostToolUseFailure
		}
		disp := hooks.DispatcherFromContext(ctx)
		if !disp.Configured(event) {
			return result, err
		}
		dec := disp.Fire(ctx, event, hooks.Payload{
			ToolName:     tCtx.Name,
			ToolInput:    json.RawMessage(args),
			ToolResponse: result,
		})
		if dec.ModifiedResult != nil {
			result = *dec.ModifiedResult
		}
		if dec.AdditionalContext != "" {
			result = appendHookContext(result, dec.AdditionalContext)
		}
		return result, err
	}
}

// hookDenyMessage is returned to the model when a PreToolUse hook blocks a tool.
// It mirrors the approval-rejection wording so the model does not try to work
// around the policy.
func hookDenyMessage(reason string) string {
	msg := "Tool execution was blocked by a hook policy."
	if reason != "" {
		msg += " Reason: " + reason
	}
	msg += " IMPORTANT: Do NOT retry this or attempt a workaround with a different tool or command. " +
		"Respect the policy and either choose a different approach or ask the user how to proceed."
	return msg
}

func appendHookContext(result, ctxText string) string {
	if result == "" {
		return ctxText
	}
	return result + "\n\n" + ctxText
}
