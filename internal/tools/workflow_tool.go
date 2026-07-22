package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/flow"
	internalmodel "github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/telemetry"
)

// WorkflowToolDeps injects what the workflow_run tool needs to spawn agents and
// stream progress. ModelFactory is required.
type WorkflowToolDeps struct {
	ModelFactory *internalmodel.ModelFactory
	Recorder     *session.Recorder
	Tracer       *telemetry.LangfuseTracer
	Sink         flow.EventSink // optional: TUI/web progress; nil = discard
	Loader       *flow.Loader   // optional: for `name` lookup + nested workflow(); built fresh if nil
	AgentRoles   map[string]config.AgentRoleConfig
}

const workflowToolAPIDoc = `The script is plain JavaScript with top-level await. Start it with:

  export const meta = { name, description, phases: [{title, detail}], whenToUse };

Then orchestrate with these injected primitives (all agents run through jcode's normal tools/permissions):
  - agent(prompt, opts?) -> Promise : spawn ONE subagent; returns its final text, or a
        validated object when opts.schema (a JSON Schema) is set. opts: {label, phase,
        model:"provider/model", agentType:"explore"|"general"|"coordinator", schema}.
  - parallel(thunks) -> Promise : run an array of () => agent(...) concurrently, BARRIER
        (waits for all); a throwing thunk resolves to null, so .filter(Boolean).
  - pipeline(items, ...stages) -> Promise : run each item through the stages, NO barrier
        between stages; each stage gets (prevResult, originalItem, index).
  - phase(title, detail?) / log(msg, level?) : progress markers shown in the UI.
  - workflow(name, args?) -> Promise : run another saved workflow (one level deep).
  - args : the JSON value passed in the "args" field. budget : {total, spent(), remaining()}.
Use normal JS for control flow (loops, map/filter, if). Return the final result at the end.
Date.now(), Math.random(), and argless new Date() THROW (runs must be deterministic).
The script itself has NO file/shell access — do all reading/writing/running inside agent() calls.`

type workflowRunInput struct {
	Name   string          `json:"name"`
	Script string          `json:"script"`
	Args   json.RawMessage `json:"args"`
}

// NewWorkflowRunTool creates the "workflow_run" tool: the agent runs a saved
// workflow by name, or writes an inline JavaScript orchestration script and runs
// it. Intermediate subagent work stays out of the agent's context; only the final
// result comes back. This is the "plan lives in code" entry point on all frontends.
func (e *Env) NewWorkflowRunTool(deps *WorkflowToolDeps) tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "workflow_run",
		Desc: "Run a dynamic workflow to fan a large, multi-step task out across many subagents " +
			"(repo audits, broad research, migrations, multi-perspective review). Provide either a " +
			"saved workflow `name`, or an inline JavaScript `script` you author. Intermediate agent " +
			"work stays out of your context — only the final result returns.\n\n" + workflowToolAPIDoc,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"name": {
				Type: schema.String, Desc: "Name of a saved workflow to run (see workflow list). Omit if providing `script`.", Required: false,
			},
			"script": {
				Type: schema.String, Desc: "Inline workflow JavaScript (must start with `export const meta = {...}`). Omit if providing `name`.", Required: false,
			},
			"args": {
				Type: schema.Object, Desc: "Optional JSON value passed to the workflow as the `args` global.", Required: false,
			},
		}),
	}
	return &workflowRunTool{env: e, deps: deps, info: info}
}

type workflowRunTool struct {
	env  *Env
	deps *WorkflowToolDeps
	info *schema.ToolInfo
}

func (t *workflowRunTool) Info(_ context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *workflowRunTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var in workflowRunInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}
	if in.Name == "" && in.Script == "" {
		return "", fmt.Errorf("one of `name` or `script` is required")
	}
	if in.Name != "" && in.Script != "" {
		return "", fmt.Errorf("provide exactly one of `name` or `script`, not both")
	}
	if t.deps == nil || t.deps.ModelFactory == nil {
		return "", fmt.Errorf("workflow_run is not available in this context (no model factory)")
	}

	loader := t.deps.Loader
	if loader == nil {
		loader = flow.NewLoader()
		if pwd := t.env.Pwd(); pwd != "" {
			loader.LoadProject(pwd)
		}
	}

	// Resolve the workflow.
	var wf flow.Workflow
	if in.Script != "" {
		// Pre-flight the agent-authored script: parse its meta block and compile the
		// whole body as JavaScript before spawning anything. A syntax error comes
		// back here so the agent can fix the script instead of a run half-starting.
		if err := flow.Validate(in.Script); err != nil {
			return "", fmt.Errorf("invalid workflow script: %w", err)
		}
		meta, _ := flow.ParseMeta(in.Script) // already validated above
		wf = flow.Workflow{Meta: meta, Source: in.Script, Scope: flow.ScopeInline}
	} else {
		got, ok := loader.Get(in.Name)
		if !ok {
			return "", fmt.Errorf("workflow %q not found", in.Name)
		}
		wf = got
	}

	var runArgs interface{}
	if len(in.Args) > 0 {
		if err := json.Unmarshal(in.Args, &runArgs); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
	}

	sink := t.deps.Sink
	if sink == nil {
		sink = flow.NopSink{}
	}
	spawn := NewFlowSpawn(FlowSpawnDeps{
		Env:          t.env,
		ModelFactory: t.deps.ModelFactory,
		Recorder:     t.deps.Recorder,
		Tracer:       t.deps.Tracer,
		AgentRoles:   t.deps.AgentRoles,
	})
	eng := flow.New(spawn, sink, flow.WithResolver(loader.Resolve))

	config.Logger().Printf("[workflow_run] start name=%q inline=%v", wf.Meta.Name, in.Script != "")
	result, err := eng.Run(ctx, wf, flow.RunOptions{Args: runArgs})
	if err != nil {
		return "", fmt.Errorf("workflow %q failed: %w", wf.Meta.Name, err)
	}
	return formatWorkflowResult(wf.Meta.Name, result), nil
}

func formatWorkflowResult(name string, result interface{}) string {
	switch v := result.(type) {
	case nil:
		return fmt.Sprintf("Workflow %q completed (no return value).", name)
	case string:
		return v
	default:
		if b, err := json.MarshalIndent(v, "", "  "); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}
