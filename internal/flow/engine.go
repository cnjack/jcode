package flow

import (
	"context"
	"fmt"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

// Default caps, matching Claude Code's dynamic workflows.
const (
	DefaultMaxConcurrent = 16   // concurrent agents
	DefaultMaxAgents     = 1000 // hard total per run (runaway backstop)
	DefaultTimeout       = 30 * time.Minute
)

// Engine runs workflows. It depends only on a SpawnFunc and an EventSink, so it
// never imports the tools/model packages — the real spawn adapter is wired by the
// command layer, and tests inject a fake. One Engine can run many workflows.
type Engine struct {
	spawn    SpawnFunc
	sink     EventSink
	resolver func(name string) (Workflow, bool) // for workflow(); nil disables nesting

	maxConcurrent int
	maxAgents     int
	timeout       time.Duration
}

// Option configures an Engine.
type Option func(*Engine)

// WithConcurrency sets the max number of concurrent agents (default 16).
func WithConcurrency(n int) Option {
	return func(e *Engine) {
		if n > 0 {
			e.maxConcurrent = n
		}
	}
}

// WithMaxAgents sets the hard per-run agent cap (default 1000).
func WithMaxAgents(n int) Option {
	return func(e *Engine) {
		if n > 0 {
			e.maxAgents = n
		}
	}
}

// WithTimeout sets the per-run wall-clock timeout (default 30m). Zero disables it.
func WithTimeout(d time.Duration) Option {
	return func(e *Engine) { e.timeout = d }
}

// WithResolver enables workflow() nesting by resolving a name to a Workflow.
func WithResolver(fn func(name string) (Workflow, bool)) Option {
	return func(e *Engine) { e.resolver = fn }
}

// New builds an Engine. sink may be nil (defaults to NopSink).
func New(spawn SpawnFunc, sink EventSink, opts ...Option) *Engine {
	if sink == nil {
		sink = NopSink{}
	}
	e := &Engine{
		spawn:         spawn,
		sink:          sink,
		maxConcurrent: DefaultMaxConcurrent,
		maxAgents:     DefaultMaxAgents,
		timeout:       DefaultTimeout,
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// RunOptions carry per-run inputs.
type RunOptions struct {
	RunID       string      // stable id; generated if empty
	Args        interface{} // becomes the `args` global (JSON value)
	BudgetTotal int64       // token target for budget.total; 0 = unset
}

// Run executes a workflow to completion and returns its final value (whatever the
// script `return`s) or an error. It blocks until the run finishes, the context is
// cancelled, or the timeout fires. Safe to call concurrently for different runs.
func (e *Engine) Run(ctx context.Context, wf Workflow, opts RunOptions) (interface{}, error) {
	runID := opts.RunID
	if runID == "" {
		runID = newRunID(wf.Meta.Name)
	}
	return e.run(ctx, wf, opts.Args, runID, 0, opts.BudgetTotal)
}

// runChild runs a nested workflow (called from workflow()). It shares the sink but
// gets its own loop; depth guards against deeper nesting.
func (e *Engine) runChild(ctx context.Context, wf Workflow, args interface{}, runID string, depth int) (interface{}, error) {
	return e.run(ctx, wf, args, runID, depth, 0)
}

func (e *Engine) run(ctx context.Context, wf Workflow, args interface{}, runID string, depth int, budgetTotal int64) (result interface{}, err error) {
	meta := wf.Meta
	if meta.Name == "" {
		if m, perr := ParseMeta(wf.Source); perr == nil {
			meta = m
		}
	}

	// Run-scoped cancellable context: cancelling it aborts every in-flight agent
	// spawn (so a timeout/cancel/normal-exit stops agents promptly instead of
	// letting them keep burning tokens after the run has ended). Cancelled on every
	// exit path via defer, and eagerly by the watchdog on timeout.
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	r := &run{
		engine:      e,
		wf:          wf,
		meta:        meta,
		runID:       runID,
		args:        args,
		sink:        e.sink,
		ctx:         WithRunID(runCtx, runID),
		sema:        make(chan struct{}, e.maxConcurrent),
		budgetTotal: budgetTotal,
		depth:       depth,
		done:        make(chan flowOutcome, 1),
	}

	program, cerr := goja.Compile(safeName(meta.Name)+".flow.js", wrapSource(wf.Source), false)
	if cerr != nil {
		err = fmt.Errorf("compiling workflow: %w", cerr)
		e.sink.OnRunDone(runID, nil, err)
		return nil, err
	}

	e.sink.OnRunStart(RunInfo{RunID: runID, Name: meta.Name, Meta: meta})

	loop := eventloop.NewEventLoop(eventloop.EnableConsole(false))
	r.loop = loop
	// Start() keeps the loop alive (jobCount seeded to 1) until we Terminate it, so
	// in-flight worker goroutines can RunOnLoop to settle their promises. Run() would
	// exit as soon as the microtask queue drained — before any agent finished.
	loop.Start()
	// Terminate the loop on EVERY exit path so no goroutine/timer leaks. Registered
	// before the handshake so an early return (ctx already done) still tears down.
	// Once we return, any in-flight worker that settles onto a terminated loop is a
	// harmless no-op: the Go caller already has its result and the VM is discarded.
	defer loop.Terminate()

	vmReady := make(chan *goja.Runtime, 1)
	loop.RunOnLoop(func(vm *goja.Runtime) {
		r.vm = vm
		r.configure(vm)
		vmReady <- vm
		if _, rerr := vm.RunProgram(program); rerr != nil {
			// A synchronous throw (e.g. the wrapper wiring itself failed). Normal
			// completion arrives via __flowResolve/__flowReject instead.
			r.finish(flowOutcome{err: fmt.Errorf("running workflow: %w", rerr)})
		}
	})
	// Wait for the VM handle, but never block forever: if ctx is (or becomes)
	// cancelled before the loop runs our job, bail out cleanly.
	var vm *goja.Runtime
	select {
	case vm = <-vmReady:
	case <-ctx.Done():
		e.sink.OnRunDone(runID, nil, ctx.Err())
		return nil, ctx.Err()
	}

	// Watchdog: on timeout, BOTH interrupt any running JS AND finish the run, so the
	// select below always unblocks — even when the loop is idle waiting on agents
	// that never return (interrupt alone can't unwedge an idle loop). finish() is
	// idempotent, so a normal completion racing the timeout still wins cleanly.
	if e.timeout > 0 {
		timer := time.AfterFunc(e.timeout, func() {
			r.finish(flowOutcome{err: fmt.Errorf("workflow timed out after %s", e.timeout)})
			runCancel() // abort in-flight agent spawns so they stop and emit OnAgentDone
			vm.Interrupt(fmt.Sprintf("workflow timed out after %s", e.timeout))
		})
		defer timer.Stop()
	}

	select {
	case out := <-r.done:
		e.sink.OnRunDone(runID, out.result, out.err)
		return out.result, out.err
	case <-ctx.Done():
		// Cancel: interrupt the VM and unwind. The deferred Terminate drops any
		// further RunOnLoop settles from worker goroutines (harmless — we've returned).
		vm.Interrupt("workflow cancelled")
		e.sink.OnRunDone(runID, nil, ctx.Err())
		return nil, ctx.Err()
	}
}

// newRunID builds a run id. It avoids time-based randomness inside the workflow
// (that would break determinism) but the id itself is host-side, so a monotonic
// clock is fine here.
func newRunID(name string) string {
	return fmt.Sprintf("flow_%s_%d", safeName(name), time.Now().UnixNano())
}

func safeName(name string) string {
	if name == "" {
		return "workflow"
	}
	out := make([]rune, 0, len(name))
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}
