package review

import (
	"context"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/tools"
)

// reviewMaxIterations bounds the read-only investigation loop so a review can
// never wander; a handful of targeted reads is plenty of evidence.
const reviewMaxIterations = 8

// investigationGuidance is appended to the system prompt when investigation is
// enabled. It tells the reviewer it MAY gather read-only evidence and must still
// finish with the strict JSON verdict.
const investigationGuidance = `
# Investigation
You MAY call the read-only tools (read, grep, glob) to gather evidence before
deciding — for example, check whether a target path exists and what it contains
before judging a deletion, or inspect a file before concluding a command leaks
secrets. Prefer a few targeted read-only checks; do not go on a fishing
expedition. These tools cannot modify anything. When you have enough evidence,
stop calling tools and reply with ONLY the strict JSON verdict.`

// reviewWithTools is the V2 path: a bounded read-only agent loop lets the
// reviewer gather evidence before deciding. It is strictly read-only (read,
// grep, glob — no shell, no writes, no network), runs within the review
// timeout, and must end with the strict-JSON verdict. Any failure escalates to
// the user, exactly like the single-shot path.
func (e *Engine) reviewWithTools(ctx context.Context, req Request, cm einomodel.ToolCallingChatModel) (Result, reviewMeta) {
	meta := reviewMeta{}

	// A read-only Env rooted at the workspace under judgment. No execute tool is
	// wired, so the reviewer cannot run shell commands or mutate anything.
	env := tools.NewEnv(req.Cwd, e.platform)
	roTools := []tool.BaseTool{
		env.NewReadTool(),
		env.NewGrepTool(),
		env.NewGlobTool(),
	}

	instruction := e.system + "\n" + investigationGuidance
	ag, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "approval-reviewer",
		Description:   "Read-only safety reviewer for one planned tool call",
		Instruction:   instruction,
		Model:         cm,
		ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: roTools}},
		MaxIterations: reviewMaxIterations,
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries:  3,
			IsRetryAble: internalmodel.IsRetryable,
			BackoffFunc: internalmodel.SmartBackoff,
		},
	})
	if err != nil {
		config.Logger().Printf("[review] investigate agent init failed: %v", err)
		meta.failReason = "investigate agent init failed"
		return Result{Outcome: Escalate, Failed: true}, meta
	}

	msgs, calls, runErr := runReviewerAgent(ctx, ag, renderUserPrompt(req))
	meta.calls = calls
	if runErr != nil {
		config.Logger().Printf("[review] investigate run failed: %v", runErr)
		meta.failReason = "investigate run failed"
		return Result{Outcome: Escalate, Failed: true}, meta
	}

	// The verdict is the last assistant message; scan newest→oldest so tool-call
	// chatter before the final JSON is ignored.
	for i := len(msgs) - 1; i >= 0; i-- {
		if a, ok := parseAssessment(msgs[i]); ok {
			meta.userAuth = a.UserAuthorization
			if res, ok := mapOutcome(a); ok {
				return res, meta
			}
		}
	}
	meta.failReason = "no parseable verdict from investigation"
	return Result{Outcome: Escalate, Failed: true}, meta
}

// runReviewerAgent runs one read-only reviewer turn to completion, returning the
// text of each assistant message (so the caller can pick the final verdict), the
// number of model calls, and any run error.
func runReviewerAgent(ctx context.Context, ag *adk.ChatModelAgent, prompt string) ([]string, int64, error) {
	input := &adk.AgentInput{Messages: []adk.Message{schema.UserMessage(prompt)}, EnableStreaming: true}
	iterator := ag.Run(ctx, input)

	var messages []string
	var cur strings.Builder
	var calls int64
	flush := func() {
		if cur.Len() > 0 {
			messages = append(messages, cur.String())
			cur.Reset()
		}
	}
loop:
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			if ctx.Err() != nil {
				return messages, calls, ctx.Err()
			}
			return messages, calls, event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		mo := event.Output.MessageOutput
		if mo.Role != schema.Assistant {
			continue
		}
		// A fresh assistant message begins; bank the previous one.
		flush()
		calls++
		if mo.IsStreaming {
			for {
				chunk, err := mo.MessageStream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					if ctx.Err() != nil {
						return messages, calls, ctx.Err()
					}
					return messages, calls, err
				}
				if chunk != nil {
					cur.WriteString(chunk.Content)
				}
			}
		} else if mo.Message != nil {
			cur.WriteString(mo.Message.Content)
		}
		if ctx.Err() != nil {
			break loop
		}
	}
	flush()
	return messages, calls, nil
}
