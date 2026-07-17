package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/telemetry"
	"github.com/cnjack/jcode/internal/tools"
)

// approvalMiddleware implements adk.ChatModelAgentMiddleware with both
// approval gating and safe tool-error handling (converting panics/errors to
// agent-visible strings instead of aborting the agent loop).
type approvalMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	approvalFunc ApprovalFunc
}

// newApprovalMiddleware creates a ChatModelAgentMiddleware that wraps tool calls
// with approval gating and safe error handling.
func newApprovalMiddleware(approvalFunc ApprovalFunc) adk.ChatModelAgentMiddleware {
	return &approvalMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		approvalFunc:                 approvalFunc,
	}
}

func (m *approvalMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (result string, retErr error) {
		// Recover from panics in tool execution so a single buggy tool
		// cannot crash the entire agent loop.
		defer func() {
			if r := recover(); r != nil {
				result = fmt.Sprintf("Tool execution panicked: %v", r)
				retErr = nil // surface as agent-visible string, not error
			}
		}()

		// Expose the LLM tool-call id to downstream layers (the approval
		// gate keys per-call bookkeeping — denied flag, approval wait time —
		// by this id; the ApprovalFunc signature itself carries no identity).
		ctx = WithToolCallID(ctx, tCtx.CallID)

		subSpan := telemetry.SubSpanFromContext(ctx)

		// Approval gate — traced as a separate "approval" span.
		if m.approvalFunc != nil {
			var finishApproval func(string)
			if subSpan != nil {
				finishApproval = subSpan("approval")
			}

			approved, err := m.approvalFunc(ctx, tCtx.Name, argumentsInJSON)

			if err != nil {
				// A reviewer denial is not an execution error: surface the
				// reviewer's rationale to the model with anti-workaround guidance,
				// distinct from the generic user-rejection message.
				var reviewDenied *ReviewDeniedError
				if errors.As(err, &reviewDenied) {
					msg := reviewDeniedMessage(reviewDenied.Reason)
					if finishApproval != nil {
						finishApproval("auto-review-denied")
					}
					return msg, nil
				}
				msg := fmt.Sprintf("Tool approval error: %v", err)
				if finishApproval != nil {
					finishApproval(msg)
				}
				return msg, nil
			}
			if !approved {
				msg := "Tool execution was rejected by user. " +
					"IMPORTANT: The user has explicitly denied this operation. " +
					"Do NOT attempt to perform the same action using alternative tools, different commands, or workarounds. " +
					"Respect the user's decision and either ask the user how they would like to proceed or move on to a different task."
				if finishApproval != nil {
					finishApproval("rejected")
				}
				return msg, nil
			}
			if finishApproval != nil {
				finishApproval("approved")
			}
		}

		// Execution — traced as a separate "execution" span.
		var finishExec func(string)
		if subSpan != nil {
			finishExec = subSpan("execution")
		}

		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			// Fatal errors (permanently dead executor: container removed,
			// SSH connection gone) abort the run by propagating instead of
			// being folded — every retry would fail identically and burn
			// the iteration budget. Checked BEFORE folding on purpose.
			if tools.IsFatal(err) {
				if finishExec != nil {
					finishExec("fatal: " + err.Error())
				}
				return "", err
			}
			// NOTE: this folding format is load-bearing. The
			// "Tool execution failed:" prefix is matched by
			// internal/handler/acp.go (isToolFailureOutput) and
			// internal/agent/reminder.go (updateErrorStreak), and mirrored
			// by the subagent safeToolMiddleware in
			// internal/tools/subagent.go — keep all of them in sync.
			if result != "" {
				result = fmt.Sprintf("%s\n\nTool execution failed: %v", result, err)
			} else {
				result = fmt.Sprintf("Tool execution failed: %v", err)
			}
			if finishExec != nil {
				finishExec(result)
			}
			return result, nil
		}
		if finishExec != nil {
			finishExec(result)
		}
		return result, nil
	}, nil
}

// WrapEnhancedInvokableToolCall mirrors WrapInvokableToolCall for multimodal
// tools. Keeping this at the approval layer is important even for read-only
// enhanced tools: it preserves hook pre-approval, tool-call identity, progress
// notifications, panic containment, and the agent-visible error contract.
func (m *approvalMiddleware) WrapEnhancedInvokableToolCall(
	ctx context.Context,
	endpoint adk.EnhancedInvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.EnhancedInvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argument *schema.ToolArgument, opts ...tool.Option) (result *schema.ToolResult, retErr error) {
		// Match the plain-tool behavior: a buggy tool must not crash the whole
		// agent loop. A panic has no trustworthy partial result, so replace it.
		defer func() {
			if r := recover(); r != nil {
				result = textToolResult(fmt.Sprintf("Tool execution panicked: %v", r))
				retErr = nil
			}
		}()

		argumentsInJSON := ""
		if argument != nil {
			argumentsInJSON = argument.Text
		}

		// Approval prompts and their wait/denied bookkeeping are keyed by the
		// model-issued call id, exactly as for plain tools.
		ctx = WithToolCallID(ctx, tCtx.CallID)

		subSpan := telemetry.SubSpanFromContext(ctx)

		if m.approvalFunc != nil {
			var finishApproval func(string)
			if subSpan != nil {
				finishApproval = subSpan("approval")
			}

			approved, err := m.approvalFunc(ctx, tCtx.Name, argumentsInJSON)
			if err != nil {
				var reviewDenied *ReviewDeniedError
				if errors.As(err, &reviewDenied) {
					msg := reviewDeniedMessage(reviewDenied.Reason)
					if finishApproval != nil {
						finishApproval("auto-review-denied")
					}
					return textToolResult(msg), nil
				}
				msg := fmt.Sprintf("Tool approval error: %v", err)
				if finishApproval != nil {
					finishApproval(msg)
				}
				return textToolResult(msg), nil
			}
			if !approved {
				msg := "Tool execution was rejected by user. " +
					"IMPORTANT: The user has explicitly denied this operation. " +
					"Do NOT attempt to perform the same action using alternative tools, different commands, or workarounds. " +
					"Respect the user's decision and either ask the user how they would like to proceed or move on to a different task."
				if finishApproval != nil {
					finishApproval("rejected")
				}
				return textToolResult(msg), nil
			}
			if finishApproval != nil {
				finishApproval("approved")
			}
		}

		var finishExec func(string)
		if subSpan != nil {
			finishExec = subSpan("execution")
		}

		result, err := endpoint(ctx, argument, opts...)
		if err != nil {
			if tools.IsFatal(err) {
				if finishExec != nil {
					finishExec("fatal: " + err.Error())
				}
				return nil, err
			}
			failure := fmt.Sprintf("Tool execution failed: %v", err)
			result = appendToolResultContext(result, failure)
			if finishExec != nil {
				finishExec(toolResultText(result))
			}
			return result, nil
		}
		if finishExec != nil {
			finishExec(toolResultText(result))
		}
		return result, nil
	}, nil
}

// reviewDeniedMessage renders the agent-visible result when the automatic
// reviewer denies a call. It names the reviewer (not the user) as the source and
// blocks workaround attempts, mirroring the anti-circumvention guidance codex's
// guardian uses.
func reviewDeniedMessage(reason string) string {
	r := reason
	if r == "" {
		r = "The automatic safety reviewer denied this action due to unacceptable risk."
	}
	return "Tool execution was denied by the automatic safety reviewer.\n" +
		"Reason: " + r + "\n" +
		"IMPORTANT: Do NOT retry the same action via a workaround, an alternative tool, or a rephrased command. " +
		"Proceed only with a materially safer alternative, or stop and ask the user for explicit approval after explaining the risk."
}

// NewTeammateHandlers returns the middleware stack for a teammate agent.
// It includes the approval + safe-tool-error middleware with the given approval function.
func NewTeammateHandlers(approvalFunc ApprovalFunc) []adk.ChatModelAgentMiddleware {
	return []adk.ChatModelAgentMiddleware{
		newApprovalMiddleware(approvalFunc),
	}
}
