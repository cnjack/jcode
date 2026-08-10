package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/telemetry"
	"github.com/cnjack/jcode/internal/usage"
)

const (
	AgentTypeExplore     = "explore"
	AgentTypeGeneral     = "general"
	AgentTypeCoordinator = "coordinator"
	subagentMaxIter      = 50
)

// SubagentNotifier receives subagent lifecycle events for TUI display.
type SubagentNotifier func(name, agentType string, done bool, result string, err error)

// SubagentProgressFn receives intermediate progress events (tool calls, tool results)
// from a running subagent so the TUI can display live progress.
type SubagentProgressFn func(agentName, event, toolName, detail string)

// SubagentTokenFn is called after each model turn with the cumulative token delta
// (tokens used by this subagent since it started) so the TUI can display progress.
// agentName identifies the subagent, so parallel subagents can be told apart.
type SubagentTokenFn func(agentName string, totalTokens int64)

// SubagentDeps holds dependencies injected into the subagent tool at creation time.
type SubagentDeps struct {
	ChatModel    model.ToolCallingChatModel
	ModelFactory *internalmodel.ModelFactory // optional, for multi-model subagents
	TaskManager  *SubagentTaskManager        // optional, for async background tasks
	Notifier     SubagentNotifier
	ProgressFn   SubagentProgressFn        // intermediate tool call/result events
	TokenFn      SubagentTokenFn           // optional: token usage update after each model turn
	Recorder     *session.Recorder         // records subagent start/result to session JSONL
	Tracer       *telemetry.LangfuseTracer // optional: Langfuse tracer for nested spans
	AgentRoles   map[string]config.AgentRoleConfig
}

type subagentInput struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	Prompt          string `json:"prompt"`
	AgentType       string `json:"agent_type"`
	Model           string `json:"model"`
	RunInBackground bool   `json:"run_in_background"`
}

// NewSubagentTool creates the "subagent" tool that delegates tasks to a child agent.
func (e *Env) NewSubagentTool(deps *SubagentDeps) tool.InvokableTool {
	if deps == nil {
		deps = &SubagentDeps{}
	}
	// Only advertise the "small" alias when a small model is actually
	// configured; otherwise the alias silently falls back to the parent model
	// and the hint would be noise.
	modelDesc := "Override model for this subagent in 'provider/model' format (optional, uses parent model by default)"
	if deps.ModelFactory != nil && deps.ModelFactory.SmallModelRef() != "" {
		modelDesc = "Override model for this subagent (optional, uses parent model by default). " +
			"Accepts 'provider/model' or 'small' — the configured lightweight model (" + deps.ModelFactory.SmallModelRef() + "). " +
			"Prefer 'small' for mechanical, low-stakes subtasks: targeted searches, file inventories, simple extraction or verification. " +
			"Keep complex reasoning, code writing, and open-ended exploration on the parent model."
	}
	agentTypes := []string{AgentTypeExplore, AgentTypeGeneral, AgentTypeCoordinator}
	roleHelp := ""
	for _, name := range config.AgentRoleNames(deps.AgentRoles) {
		agentTypes = append(agentTypes, name)
		role := deps.AgentRoles[name]
		roleHelp += fmt.Sprintf(" %s: %s.", name, role.Description)
		if role.Model != "" {
			roleHelp += fmt.Sprintf(" Its model is fixed to %q.", role.Model)
		}
	}
	info := &schema.ToolInfo{
		Name: "subagent",
		Desc: "Delegate a task to a subagent that runs in its own context. " +
			"Use for codebase exploration, research, or independent subtasks. " +
			"The subagent returns only its final answer — intermediate tool calls stay out of your context. " +
			"Selecting general or coordinator requests a one-time delegated-write grant before the child starts.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"name": {
				Type: schema.String, Desc: "Short name for the subagent task (1-3 words)", Required: true,
			},
			"description": {
				Type: schema.String, Desc: "Brief description shown in the UI", Required: true,
			},
			"prompt": {
				Type: schema.String, Desc: "Detailed instructions for the subagent. Include all necessary context.", Required: true,
			},
			"agent_type": {
				Type: schema.String,
				Desc: "Agent type: 'explore' (strictly read-only, default), 'general' (write/execute tools), or 'coordinator' " +
					"(write/execute tools plus sub-subagents). Choosing general or coordinator requests a one-time delegated-write grant " +
					"at this parent tool call, including when run_in_background=true." + roleHelp,
				Enum:     agentTypes,
				Required: false,
			},
			"model": {
				Type: schema.String, Desc: modelDesc, Required: false,
			},
			"run_in_background": {
				Type: schema.Boolean, Desc: "If true, run asynchronously and return a task ID immediately. Check result later with task_get.", Required: false,
			},
		}),
	}

	return &subagentTool{env: e, deps: deps, info: info}
}

type subagentTool struct {
	env  *Env
	deps *SubagentDeps
	info *schema.ToolInfo
}

// safeToolMiddleware folds subagent tool panics and errors into model-visible
// strings so a single failing tool cannot kill the whole subagent run. It is a
// local mirror of the error-handling half of the parent agent's
// approvalMiddleware (internal/agent/middleware.go) — the tools package cannot
// import the agent package (circular dependency), so the logic is duplicated.
//
// NOTE: the folding format is load-bearing and MUST stay byte-identical to
// internal/agent/middleware.go: internal/handler/acp.go (isToolFailureOutput)
// and internal/agent/reminder.go (updateErrorStreak) classify failures by the
// "Tool execution failed:" / "Tool execution panicked:" prefixes.
// TestSafeToolMiddleware_ErrorFolded and TestApprovalMiddleware_NonFatalFolded
// lock the two formats together.
type safeToolMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func newSafeToolMiddleware() adk.ChatModelAgentMiddleware {
	return &safeToolMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
}

func (m *safeToolMiddleware) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	_ *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (result string, retErr error) {
		// Recover from panics so a single buggy tool cannot crash the
		// subagent (or, on the async path, the whole process).
		defer func() {
			if r := recover(); r != nil {
				result = fmt.Sprintf("Tool execution panicked: %v", r)
				retErr = nil // surface as agent-visible string, not error
			}
		}()

		result, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			// Fatal errors (#16: executor permanently dead) propagate so
			// the abort reaches the parent run instead of being folded.
			if IsFatal(err) {
				return "", err
			}
			if result != "" {
				return fmt.Sprintf("%s\n\nTool execution failed: %v", result, err), nil
			}
			return fmt.Sprintf("Tool execution failed: %v", err), nil
		}
		return result, nil
	}, nil
}

func (m *safeToolMiddleware) WrapEnhancedInvokableToolCall(
	ctx context.Context,
	endpoint adk.EnhancedInvokableToolCallEndpoint,
	_ *adk.ToolContext,
) (adk.EnhancedInvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argument *schema.ToolArgument, opts ...tool.Option) (result *schema.ToolResult, retErr error) {
		defer func() {
			if r := recover(); r != nil {
				result = enhancedTextResult(fmt.Sprintf("Tool execution panicked: %v", r))
				retErr = nil
			}
		}()

		result, err := endpoint(ctx, argument, opts...)
		if err != nil {
			if IsFatal(err) {
				return nil, err
			}
			failure := fmt.Sprintf("Tool execution failed: %v", err)
			if enhancedText(result) != "" {
				failure = "\n\n" + failure
			}
			if result == nil {
				return enhancedTextResult(failure), nil
			}
			parts := append([]schema.ToolOutputPart(nil), result.Parts...)
			parts = append(parts, schema.ToolOutputPart{Type: schema.ToolPartTypeText, Text: failure})
			return &schema.ToolResult{Parts: parts}, nil
		}
		return result, nil
	}, nil
}

func enhancedTextResult(text string) *schema.ToolResult {
	return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: text}}}
}

func enhancedText(result *schema.ToolResult) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range result.Parts {
		if part.Type == schema.ToolPartTypeText {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func (s *subagentTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return s.info, nil
}

func (s *subagentTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input subagentInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}
	if input.Name == "" || input.Prompt == "" {
		return "", fmt.Errorf("name and prompt are required")
	}
	roleName := strings.TrimSpace(input.AgentType)
	if roleName == "" {
		roleName = AgentTypeExplore
	}
	profile := roleName
	roleInstructions := ""
	modelRef := input.Model
	if role, ok := s.deps.AgentRoles[roleName]; ok {
		profile = AgentTypeGeneral
		roleInstructions = role.Instructions
		if role.Model != "" {
			modelRef = role.Model
		}
	} else if profile != AgentTypeExplore && profile != AgentTypeGeneral && profile != AgentTypeCoordinator {
		return "", fmt.Errorf("unknown agent_type %q", roleName)
	}

	// Check nesting depth.
	if !s.env.CanNest() {
		return "", fmt.Errorf("maximum subagent nesting depth (%d) reached", MaxSubagentDepth)
	}

	// Resolve model. usageModel tracks the concrete override ref (alias
	// expanded) so token usage is attributed to the model that actually ran;
	// empty means "parent model" and falls back to the recorder's model name.
	chatModel := s.deps.ChatModel
	usageModel := ""
	if modelRef != "" && s.deps.ModelFactory != nil {
		m, err := s.deps.ModelFactory.GetModel(ctx, modelRef)
		if err != nil {
			return "", fmt.Errorf("failed to resolve model %q: %w", modelRef, err)
		}
		chatModel = m
		if ref := s.deps.ModelFactory.ResolveRef(modelRef); ref != "" {
			usageModel = internalmodel.BareModelID(ref)
		}
	}

	config.Logger().Printf("[subagent] start name=%q type=%s depth=%d model=%q bg=%v",
		input.Name, roleName, s.env.Depth, modelRef, input.RunInBackground)

	// Record subagent start event to session.
	if s.deps.Recorder != nil {
		s.deps.Recorder.RecordSubagentStart(input.Name, roleName)
	}

	// Notify TUI of subagent start.
	if s.deps.Notifier != nil {
		s.deps.Notifier(input.Name, roleName, false, "", nil)
	}

	// Build the run function that creates and executes the agent.
	runFn := func(runCtx context.Context) (string, error) {
		runCtx = internalmodel.WithProviderSubagent(runCtx)
		childEnv := s.env.CloneForSubagent()
		childTools := s.buildTools(childEnv, profile)
		prompt := subagentSystemPrompt(profile, s.env.Pwd(), s.env.platform)
		if roleInstructions != "" {
			prompt += "\n\n## Custom role: " + roleName + "\n" + roleInstructions
		}

		// Inject Langfuse child trace so subagent spans nest under the parent.
		var middlewares []adk.ChatModelAgentMiddleware
		if s.deps.Tracer != nil {
			runCtx = s.deps.Tracer.WithChildTrace(runCtx, fmt.Sprintf("subagent-%s", input.Name))
			middlewares = append(middlewares, s.deps.Tracer.ChildAgentMiddleware())
		}
		// Safe error handling MUST be appended last (innermost) so Langfuse
		// traces record the folded result, not the raw error/panic.
		middlewares = append(middlewares, newSafeToolMiddleware())

		ag, err := adk.NewChatModelAgent(runCtx, &adk.ChatModelAgentConfig{
			Name:        fmt.Sprintf("subagent-%s", input.Name),
			Description: input.Description,
			Instruction: prompt,
			Model:       chatModel,
			ToolsConfig: adk.ToolsConfig{
				ToolsNodeConfig: compose.ToolsNodeConfig{
					Tools: childTools,
				},
			},
			MaxIterations: subagentMaxIter,
			Handlers:      middlewares,
			ModelRetryConfig: &adk.ModelRetryConfig{
				MaxRetries:  3,
				IsRetryAble: internalmodel.IsRetryable,
				BackoffFunc: internalmodel.SmartBackoff,
			},
		})
		if err != nil {
			return "", fmt.Errorf("failed to create subagent: %w", err)
		}
		result, runErr := s.runSubagent(runCtx, ag, input, usageModel)
		if s.deps.Tracer != nil {
			if runErr != nil {
				s.deps.Tracer.EndChildTrace(runCtx, fmt.Sprintf("error: %v", runErr))
			} else {
				s.deps.Tracer.EndChildTrace(runCtx, result)
			}
		}
		if runErr != nil {
			return "", runErr
		}
		return result, nil
	}

	// Background async path via TaskManager.
	if input.RunInBackground && s.deps.TaskManager != nil {
		task := &SubagentTask{
			Name:      input.Name,
			AgentType: roleName,
			Model:     modelRef,
			Depth:     s.env.Depth + 1,
		}
		taskID, _, err := s.deps.TaskManager.Submit(ctx, task, runFn, true)
		if err != nil {
			if s.deps.Notifier != nil {
				s.deps.Notifier(input.Name, roleName, true, "", err)
			}
			return "", fmt.Errorf("failed to submit background task: %w", err)
		}
		// Record async launch.
		if s.deps.Recorder != nil {
			s.deps.Recorder.RecordSubagentAsync(input.Name, taskID, roleName)
		}
		return fmt.Sprintf("Background task started: %s\nUse task_get with task_id=%q to check status and retrieve result.", taskID, taskID), nil
	}

	// Synchronous execution (default, backward-compatible).
	result, err := runFn(ctx)
	if err != nil {
		if s.deps.Notifier != nil {
			s.deps.Notifier(input.Name, roleName, true, "", err)
		}
		return "", err
	}

	config.Logger().Printf("[subagent] done name=%q len=%d", input.Name, len(result))
	if s.deps.Recorder != nil {
		s.deps.Recorder.RecordSubagentResult(input.Name, result, nil)
	}
	if s.deps.Notifier != nil {
		s.deps.Notifier(input.Name, roleName, true, result, nil)
	}
	return result, nil
}

// runSubagent drives the subagent's event loop. It returns the accumulated
// assistant text and a non-nil error when the run terminated on a model-layer
// failure (event.Err) — tool errors never surface here: the
// safeToolMiddleware already folded them into model-visible strings.
// usageModel is the bare id of an override model ("" → parent model); usage
// events are attributed to it.
func (s *subagentTool) runSubagent(ctx context.Context, ag *adk.ChatModelAgent, input subagentInput, usageModel string) (string, error) {
	agentInput := &adk.AgentInput{
		Messages: []adk.Message{
			schema.UserMessage(input.Prompt),
		},
		EnableStreaming: true,
	}

	// Per-agent token tracker for this subagent run.
	tokenUsage := &internalmodel.TokenUsage{}
	ctx = internalmodel.WithTokenTracker(ctx, tokenUsage)
	reportTokens := func() {
		if s.deps.TokenFn != nil {
			_, _, cur := tokenUsage.Get()
			s.deps.TokenFn(input.Name, cur)
		}
	}

	// Roll this subagent's tokens into the global usage log under the
	// leader's session so subagent-heavy work isn't undercounted — deferred
	// so error returns are counted too. The tracker is fresh per run, so its
	// cumulative snapshot IS this run's delta.
	defer func() {
		if s.deps.Recorder == nil {
			return
		}
		d := tokenUsage.GetFull()
		if d.TotalTokens > 0 {
			evModel := usageModel
			if evModel == "" {
				evModel = s.deps.Recorder.Model()
			}
			usage.RecordEvent(usage.Event{
				Session:    s.deps.Recorder.UUID(),
				Project:    s.deps.Recorder.Project(),
				Model:      evModel,
				Prompt:     d.PromptTokens,
				Completion: d.CompletionTokens,
				Cached:     d.CachedTokens,
				Reasoning:  d.ReasoningTokens,
				CacheWrite: d.CacheWriteTokens,
				Total:      d.TotalTokens,
				Calls:      d.CallCount,
				CacheSeen:  tokenUsage.CacheObserved(),
			})
		}
	}()

	var assistantText strings.Builder
	iterator := ag.Run(ctx, agentInput)
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			// Model-layer failure (retries exhausted) or cancellation:
			// propagate to the parent instead of returning truncated
			// partial text as if the subagent had succeeded.
			config.Logger().Printf("[subagent] %s error: %v", input.Name, event.Err)
			if ctx.Err() != nil {
				return assistantText.String(), ctx.Err()
			}
			return assistantText.String(), fmt.Errorf("subagent %s failed: %w", input.Name, event.Err)
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		mo := event.Output.MessageOutput

		// Forward tool-role events as progress.
		if mo.Role == schema.Tool {
			toolName := mo.ToolName
			if !mo.IsStreaming && mo.Message != nil {
				s.notifyProgress(input.Name, "tool_result", toolName, mo.Message.Content)
			} else if mo.IsStreaming {
				var sb strings.Builder
				var toolCallID string
				for {
					chunk, err := mo.MessageStream.Recv()
					if err == io.EOF {
						break
					}
					if err != nil {
						break
					}
					if chunk != nil {
						sb.WriteString(chunk.Content)
						if toolCallID == "" && chunk.ToolCallID != "" {
							toolCallID = chunk.ToolCallID
						}
					}
				}
				s.notifyProgress(input.Name, "tool_result", toolName, sb.String())
			}
			continue
		}

		if mo.Role != schema.Assistant {
			continue
		}

		if mo.IsStreaming {
			// Accumulate streaming tool call names and arguments across chunks.
			type pendingTC struct {
				name string
				args strings.Builder
			}
			pending := make(map[int]*pendingTC)
			for {
				chunk, err := mo.MessageStream.Recv()
				if err != nil {
					break
				}
				if chunk == nil {
					continue
				}
				for _, tc := range chunk.ToolCalls {
					idx := 0
					if tc.Index != nil {
						idx = *tc.Index
					}
					if tc.Function.Name != "" {
						p := &pendingTC{name: tc.Function.Name}
						p.args.WriteString(tc.Function.Arguments)
						pending[idx] = p
					} else if p, ok := pending[idx]; ok {
						p.args.WriteString(tc.Function.Arguments)
					}
				}
				if chunk.Content != "" {
					assistantText.WriteString(chunk.Content)
				}
			}
			// Notify accumulated tool calls (progress only, not recorded to session).
			for _, p := range pending {
				s.notifyProgress(input.Name, "tool_call", p.name, p.args.String())
			}
			reportTokens()
		} else if mo.Message != nil {
			// Forward tool call events (progress only, not recorded to session).
			for _, tc := range mo.Message.ToolCalls {
				if tc.Function.Name != "" {
					s.notifyProgress(input.Name, "tool_call", tc.Function.Name, tc.Function.Arguments)
				}
			}
			if mo.Message.Content != "" {
				assistantText.WriteString(mo.Message.Content)
			}
			reportTokens()
		}
	}

	return assistantText.String(), nil
}

// notifyProgress sends an intermediate progress event to the TUI if a ProgressFn is set.
func (s *subagentTool) notifyProgress(agentName, event, toolName, detail string) {
	if s.deps.ProgressFn != nil {
		s.deps.ProgressFn(agentName, event, toolName, detail)
	}
}

func (s *subagentTool) buildTools(childEnv *Env, agentType string) []tool.BaseTool {
	tools := []tool.BaseTool{
		childEnv.NewReadTool(),
		childEnv.NewGrepTool(),
	}
	if agentType != AgentTypeGeneral && agentType != AgentTypeCoordinator {
		return append(tools, childEnv.NewPlanExecuteTool())
	}

	// general/coordinator receive arbitrary execute plus write tools under the
	// one-time delegated-write grant approved on the parent subagent call.
	tools = append(tools, childEnv.NewExecuteTool(nil))

	if agentType == AgentTypeGeneral {
		tools = append(tools,
			childEnv.NewEditTool(),
			childEnv.NewWriteTool(),
			childEnv.NewTodoWriteTool(),
			childEnv.NewTodoReadTool(),
		)
	}

	if agentType == AgentTypeCoordinator {
		tools = append(tools,
			childEnv.NewEditTool(),
			childEnv.NewWriteTool(),
			childEnv.NewTodoWriteTool(),
			childEnv.NewTodoReadTool(),
		)
		// Allow coordinator to spawn sub-subagents if depth allows.
		if childEnv.CanNest() {
			tools = append(tools, childEnv.NewSubagentTool(s.deps))
		}
		// Add task management tools if a TaskManager is available.
		if s.deps.TaskManager != nil {
			tools = append(tools,
				NewTaskListTool(s.deps.TaskManager),
				NewTaskGetTool(s.deps.TaskManager),
				NewTaskStopTool(s.deps.TaskManager),
			)
		}
	}

	return tools
}

func subagentSystemPrompt(agentType, pwd, platform string) string {
	base := fmt.Sprintf(`You are a subagent working on a delegated task.

Current work path: %s
Platform: %s
Date: %s

`, pwd, platform, time.Now().Format("2006-01-02"))

	switch agentType {
	case AgentTypeExplore:
		return base + `You are a research/exploration subagent. Your job is to:
- Search and read code to answer the question in your prompt
- Report findings concisely (under 500 words)
- Do NOT make any file changes

Report your findings in a structured format.`
	case AgentTypeGeneral:
		return base + `You are a task subagent. Your job is to:
- Complete the specific task described in your prompt
- Report what you did and any issues encountered
- Keep your scope narrow — only do what was asked`
	case AgentTypeCoordinator:
		return base + `You are a coordinator subagent. Your job is to:
- Break down the task into smaller subtasks
- Delegate subtasks to child subagents using the subagent tool
- Use run_in_background=true for independent parallel tasks
- Monitor task progress with task_list and task_get
- Synthesize results from all subtasks into a final answer
- Stop any stuck tasks with task_stop`
	}
	return base
}
