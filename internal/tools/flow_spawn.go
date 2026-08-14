package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/flow"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/telemetry"
)

// This file wires jcode's agent machinery to the pure flow engine. It lives in the
// tools package (not flow) so that flow stays free of tools/model imports and the
// dependency runs one way: tools → flow → config.

const (
	flowAgentMaxIter      = 40
	flowAgentTypeReasoner = "reasoner"
	flowStructRetry       = 2 // extra attempts to coax schema-valid JSON
)

// FlowSpawnDeps carry what NewFlowSpawn needs to build and run a subagent. Env and
// ModelFactory are required.
type FlowSpawnDeps struct {
	Env          *Env
	ModelFactory *internalmodel.ModelFactory
	Recorder     *session.Recorder
	Tracer       *telemetry.LangfuseTracer
	AgentRoles   map[string]config.AgentRoleConfig
}

// NewFlowSpawn builds the flow.SpawnFunc used by the CLI/TUI/web/ACP frontends. It
// reuses the adk agent machinery: a fresh child Env, tools chosen by agent type,
// and the session's model factory (spec.Model overrides; "" = session default).
func NewFlowSpawn(deps FlowSpawnDeps) flow.SpawnFunc {
	return func(ctx context.Context, spec flow.AgentSpec) (flow.AgentResult, error) {
		if deps.Env == nil || deps.ModelFactory == nil {
			return flow.AgentResult{}, fmt.Errorf("flow spawn: Env and ModelFactory are required")
		}
		roleName := strings.TrimSpace(spec.AgentType)
		if roleName == "" {
			roleName = AgentTypeExplore
		}
		profile, instructions, roleModel, err := resolveFlowAgentProfile(roleName, deps.AgentRoles)
		if err != nil {
			return flow.AgentResult{}, err
		}
		modelRef := spec.Model
		if roleModel != "" {
			modelRef = roleModel
		}
		maxIterations, err := resolveFlowAgentMaxIterations(spec.MaxIterations)
		if err != nil {
			return flow.AgentResult{}, err
		}
		cm, err := deps.ModelFactory.GetModel(ctx, modelRef)
		if err != nil {
			return flow.AgentResult{}, fmt.Errorf("resolve model %q: %w", modelRef, err)
		}
		childEnv := deps.Env.CloneForSubagent()
		agentTools := flowTools(childEnv, profile)
		instruction := flowAgentSystemPrompt(profile, childEnv.Pwd(), childEnv.Exec.Platform())
		if instructions != "" {
			instruction += "\n\n## Custom role: " + roleName + "\n" + instructions
		}

		schemaJSON := ""
		if spec.Schema != nil {
			if b, mErr := json.Marshal(spec.Schema); mErr == nil {
				schemaJSON = string(b)
			}
		}
		attempts := 1
		if schemaJSON != "" {
			attempts = flowStructRetry + 1
		}

		var tokens int64
		var lastText string
		var structured interface{}
		for attempt := 0; attempt < attempts; attempt++ {
			prompt := spec.Prompt
			if schemaJSON != "" {
				prompt = spec.Prompt + "\n\n---\nReturn ONLY a single JSON value that conforms to this JSON Schema. No prose, no markdown code fences:\n" + schemaJSON
				if attempt > 0 {
					prompt += "\n\n(Your previous reply was not valid JSON for the schema. Output ONLY the JSON value.)"
				}
			}
			ag, aerr := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
				Name:          "flow-agent",
				Description:   spec.Label,
				Instruction:   instruction,
				Model:         cm,
				ToolsConfig:   adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: agentTools}},
				MaxIterations: maxIterations,
				Handlers:      flowAgentHandlers(),
				ModelRetryConfig: &adk.ModelRetryConfig{
					MaxRetries:  3,
					IsRetryAble: internalmodel.IsRetryable,
					BackoffFunc: internalmodel.SmartBackoff,
				},
			})
			if aerr != nil {
				return flow.AgentResult{}, fmt.Errorf("create flow agent: %w", aerr)
			}
			text, tok, rerr := runFlowAgent(ctx, ag, []adk.Message{schema.UserMessage(prompt)})
			tokens += tok
			lastText = text
			if err := ctx.Err(); err != nil {
				return flow.AgentResult{}, err
			}
			// A real agent-run error (API failure after retries, stream error) must
			// fail the workflow — don't return partial text as a success, and don't
			// spend the structured-output retries on it.
			if rerr != nil {
				return flow.AgentResult{}, fmt.Errorf("flow agent %q failed: %w", spec.Label, rerr)
			}
			if schemaJSON == "" {
				break
			}
			if v, ok := extractFlowJSON(text); ok {
				structured = v
				break
			}
		}
		if schemaJSON != "" && structured == nil {
			return flow.AgentResult{}, fmt.Errorf("error_max_structured_output_retries: no schema-valid JSON after %d attempt(s)", attempts)
		}
		return flow.AgentResult{Text: strings.TrimSpace(lastText), Structured: structured, Tokens: tokens}, nil
	}
}

func resolveFlowAgentProfile(roleName string, roles map[string]config.AgentRoleConfig) (profile, instructions, model string, err error) {
	// reasoner is a reserved built-in capability: a same-named custom role
	// must not silently turn a verifier that promises no tools into a writer.
	if roleName == flowAgentTypeReasoner {
		return flowAgentTypeReasoner, "", "", nil
	}
	if role, ok := roles[roleName]; ok {
		return AgentTypeGeneral, role.Instructions, role.Model, nil
	}
	if roleName == AgentTypeExplore || roleName == AgentTypeGeneral || roleName == AgentTypeCoordinator {
		return roleName, "", "", nil
	}
	return "", "", "", fmt.Errorf("unknown agent type %q", roleName)
}

func resolveFlowAgentMaxIterations(requested int) (int, error) {
	if requested == 0 {
		return flowAgentMaxIter, nil
	}
	if requested < 1 || requested > flowAgentMaxIter {
		return 0, fmt.Errorf("flow agent maxIterations must be between 1 and %d (got %d)", flowAgentMaxIter, requested)
	}
	return requested, nil
}

// flowTools mirrors the subagent tool set: reasoner = no tools, explore =
// read-only; general/coordinator also get edit/write/todo.
func flowTools(env *Env, agentType string) []tool.BaseTool {
	if agentType == flowAgentTypeReasoner {
		return []tool.BaseTool{}
	}
	ts := []tool.BaseTool{
		env.NewReadTool(),
		env.NewGrepTool(),
	}
	switch agentType {
	case "general", "coordinator":
		ts = append(ts,
			env.NewExecuteTool(nil),
			env.NewEditTool(),
			env.NewWriteTool(),
			env.NewTodoWriteTool(),
			env.NewTodoReadTool(),
		)
	default:
		ts = append(ts, env.NewPlanExecuteTool())
	}
	return ts
}

func flowAgentHandlers() []adk.ChatModelAgentMiddleware {
	// Flow agents have no approval middleware of their own: the workflow_run
	// parent call is the one-time grant. They still need the same innermost
	// panic/non-fatal error folding as direct subagents.
	return []adk.ChatModelAgentMiddleware{newSafeToolMiddleware()}
}

// runFlowAgent runs one agent turn to completion, returning accumulated assistant
// text, this run's token delta, and any run error. A non-nil error means the agent
// run genuinely failed (API error after retries, stream error) — the caller must
// not treat the partial text as a successful result.
func runFlowAgent(ctx context.Context, ag *adk.ChatModelAgent, messages []adk.Message) (string, int64, error) {
	tokenUsage := &internalmodel.TokenUsage{}
	ctx = internalmodel.WithTokenTracker(ctx, tokenUsage)
	input := &adk.AgentInput{Messages: messages, EnableStreaming: true}

	var sb strings.Builder
	var runErr error
	iterator := ag.Run(ctx, input)
loop:
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			// A cancelled ctx surfaces here as a stream error; report the clean ctx
			// error rather than the noisy wrapped one.
			if ctx.Err() != nil {
				runErr = ctx.Err()
			} else {
				runErr = event.Err
			}
			break
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		mo := event.Output.MessageOutput
		if mo.Role != schema.Assistant {
			continue
		}
		if mo.IsStreaming {
			for {
				chunk, err := mo.MessageStream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					if ctx.Err() != nil {
						runErr = ctx.Err()
					} else {
						runErr = err
					}
					break loop
				}
				if chunk != nil {
					sb.WriteString(chunk.Content)
				}
			}
		} else if mo.Message != nil {
			sb.WriteString(mo.Message.Content)
		}
	}
	_, _, cur := tokenUsage.Get()
	return sb.String(), cur, runErr
}

// extractFlowJSON pulls the first JSON object/array out of text (tolerating fences
// and surrounding prose) and unmarshals it.
func extractFlowJSON(text string) (interface{}, bool) {
	s := strings.TrimSpace(text)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v, true
	}
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return nil, false
	}
	open := s[start]
	closeCh := byte('}')
	if open == '[' {
		closeCh = ']'
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case open:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				if err := json.Unmarshal([]byte(s[start:i+1]), &v); err == nil {
					return v, true
				}
				return nil, false
			}
		}
	}
	return nil, false
}

func flowAgentSystemPrompt(agentType, pwd, platform string) string {
	base := fmt.Sprintf(`You are a workflow agent working on one focused step of a larger orchestrated task.

Current work path: %s
Platform: %s
Date: %s

`, pwd, platform, time.Now().Format("2006-01-02"))
	switch agentType {
	case flowAgentTypeReasoner:
		return base + `Reason only from the context in your prompt. You have no tools and must not request repository access. Return the requested answer directly and concisely.`
	case "general":
		return base + `Complete the specific task in your prompt. You may edit and write files. Keep your scope narrow — only do what was asked. Report what you did concisely.`
	case "coordinator":
		return base + `Complete the task in your prompt, breaking it into steps as needed. You may edit and write files. Report a concise synthesis.`
	default:
		return base + `Search and read code to answer the question in your prompt. Do NOT make any file changes. Report findings concisely.`
	}
}
