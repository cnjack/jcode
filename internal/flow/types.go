// Package flow implements jcode's dynamic-workflow engine: user- or agent-authored
// JavaScript orchestration scripts that fan work out across many subagents, run
// them in parallel/pipeline, and collect structured results — the plan lives in
// code (à la Claude Code Dynamic Workflows / Qoder CLI Workflows).
//
// The engine is a thin goja (JavaScript) control-flow shell over a hand-written Go
// execution core. The script itself does no I/O (no shell/filesystem/network);
// every side effect happens through the agent() primitive, which spawns a subagent
// that goes through jcode's normal tools, permissions, and hooks.
//
// Design doc: internal-doc/dynamic-workflow-design.md
package flow

import "context"

// Meta is the exported `meta` block at the top of a workflow script. It must be a
// pure literal object (no computed values) so it can be parsed without running the
// body. Name becomes the /<name> slash command.
type Meta struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	WhenToUse   string  `json:"whenToUse"`
	Phases      []Phase `json:"phases"`
}

// Phase is one entry in meta.phases — a titled progress group.
type Phase struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Scope describes where a workflow was loaded from. Project wins over user wins
// over builtin on name collisions (mirrors skills.Loader precedence).
type Scope string

const (
	ScopeBuiltin Scope = "builtin"
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopeInline  Scope = "inline" // generated on the fly, not saved
)

// Workflow is a loaded workflow definition.
type Workflow struct {
	Meta   Meta
	Source string // full .js source (including the meta block)
	Path   string // filesystem path ("" for inline/builtin)
	Scope  Scope
}

// AgentSpec describes a single agent() call from the script. Prompt is the first
// positional argument; the rest come from the opts object (mapped by json tag).
type AgentSpec struct {
	Prompt    string      `json:"-"`
	Label     string      `json:"label"`
	Phase     string      `json:"phase"`
	Model     string      `json:"model"`     // "provider/model" override; "" = session default
	AgentType string      `json:"agentType"` // explore|general|coordinator; "" = explore
	Schema    interface{} `json:"schema"`    // JSON Schema (as a JS object) → structured output
	Isolation string      `json:"isolation"` // "worktree" (recognised; isolation is a later phase)
}

// AgentResult is what a single agent() call returns. When Schema was set and the
// subagent produced valid structured output, Structured is non-nil and is what the
// script sees; otherwise the script sees Text.
type AgentResult struct {
	Text       string
	Structured interface{}
	Tokens     int64
}

// SpawnFunc runs one subagent to completion and returns its result. The engine is
// built around this seam so it never imports the tools/model packages directly:
// the real implementation (spawn.go) reuses jcode's adk agent machinery, and tests
// inject a deterministic fake. Implementations must honour ctx cancellation.
type SpawnFunc func(ctx context.Context, spec AgentSpec) (AgentResult, error)

// RunInfo identifies a workflow run for the event sink.
type RunInfo struct {
	RunID string
	Name  string
	Meta  Meta
}

// AgentEvent carries per-agent progress to the sink.
type AgentEvent struct {
	ID     string // stable within a run, e.g. "agent_7"
	Label  string
	Phase  string
	Status string // "running" | "done" | "failed"
	Tokens int64
	Detail string // last tool / short note
	Err    string
}

// EventSink receives run progress. It is frontend-agnostic: the headless CLI, TUI,
// Web, and ACP each provide an implementation. All methods must be safe for
// concurrent use — the engine calls them from the loop goroutine, but the sink may
// fan out to other goroutines. Sinks must not block for long (they run on the loop).
type EventSink interface {
	OnRunStart(r RunInfo)
	OnPhase(runID, title, detail string)
	OnAgentStart(runID string, a AgentEvent)
	OnAgentDone(runID string, a AgentEvent)
	OnLog(runID, level, msg string)
	OnRunDone(runID string, result interface{}, err error)
}

// NopSink is an EventSink that discards everything.
type NopSink struct{}

func (NopSink) OnRunStart(RunInfo)                   {}
func (NopSink) OnPhase(string, string, string)       {}
func (NopSink) OnAgentStart(string, AgentEvent)      {}
func (NopSink) OnAgentDone(string, AgentEvent)       {}
func (NopSink) OnLog(string, string, string)         {}
func (NopSink) OnRunDone(string, interface{}, error) {}

// FuncSink adapts individual callbacks into an EventSink; unset fields are no-ops.
// Handy for the CLI and tests.
type FuncSink struct {
	RunStart   func(RunInfo)
	Phase      func(runID, title, detail string)
	AgentStart func(runID string, a AgentEvent)
	AgentDone  func(runID string, a AgentEvent)
	Log        func(runID, level, msg string)
	RunDone    func(runID string, result interface{}, err error)
}

func (s FuncSink) OnRunStart(r RunInfo) {
	if s.RunStart != nil {
		s.RunStart(r)
	}
}
func (s FuncSink) OnPhase(runID, title, detail string) {
	if s.Phase != nil {
		s.Phase(runID, title, detail)
	}
}
func (s FuncSink) OnAgentStart(runID string, a AgentEvent) {
	if s.AgentStart != nil {
		s.AgentStart(runID, a)
	}
}
func (s FuncSink) OnAgentDone(runID string, a AgentEvent) {
	if s.AgentDone != nil {
		s.AgentDone(runID, a)
	}
}
func (s FuncSink) OnLog(runID, level, msg string) {
	if s.Log != nil {
		s.Log(runID, level, msg)
	}
}
func (s FuncSink) OnRunDone(runID string, result interface{}, err error) {
	if s.RunDone != nil {
		s.RunDone(runID, result, err)
	}
}

var _ EventSink = NopSink{}
var _ EventSink = FuncSink{}

// contextKey is unexported to avoid collisions.
type contextKey string

const runIDKey contextKey = "flow.runID"

// WithRunID tags a context with the current run ID (used by spawn for journaling
// scope and logging).
func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDKey, runID)
}

// RunIDFromContext returns the run ID tagged on ctx, or "".
func RunIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(runIDKey).(string); ok {
		return v
	}
	return ""
}
