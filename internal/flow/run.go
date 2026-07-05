package flow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"

	"github.com/cnjack/jcode/internal/config"
)

// run holds the state of one workflow execution. Host functions are methods on
// *run so they close over the run's sink, counters, and loop. Everything that
// touches the goja VM or a goja.Value runs on the single loop goroutine; blocking
// subagent work runs on other goroutines and settles back via loop.RunOnLoop.
type run struct {
	engine *Engine
	wf     Workflow
	meta   Meta
	runID  string
	args   interface{}
	sink   EventSink
	ctx    context.Context

	loop *eventloop.EventLoop
	vm   *goja.Runtime // set on the loop before RunProgram; read off-loop only via Interrupt

	sema        chan struct{} // concurrency limiter (cap = engine.maxConcurrent)
	agentSeq    int64         // agent id sequence
	agentTotal  int64         // total agents launched (for the hard cap)
	spent       int64         // cumulative agent tokens (for budget)
	budgetTotal int64         // token target, 0 = unset

	depth int // nested-workflow depth (workflow() is one level deep)

	done chan flowOutcome
}

type flowOutcome struct {
	result interface{}
	err    error
}

// finish records the terminal outcome exactly once.
func (r *run) finish(o flowOutcome) {
	select {
	case r.done <- o:
	default:
	}
}

// configure installs the field-name mapper, host functions, and globals on the VM.
// Called once on the loop goroutine before RunProgram.
func (r *run) configure(vm *goja.Runtime) {
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	set := func(name string, fn func(goja.FunctionCall) goja.Value) {
		if err := vm.Set(name, fn); err != nil {
			config.Logger().Printf("[flow] set %s: %v", name, err)
		}
	}
	set("agent", r.jsAgent)
	set("phase", r.jsPhase)
	set("log", r.jsLog)
	set("workflow", r.jsWorkflow)
	set("__flowResolve", r.jsFlowResolve)
	set("__flowReject", r.jsFlowReject)

	_ = vm.Set("args", vm.ToValue(r.args))

	budget := vm.NewObject()
	if r.budgetTotal > 0 {
		_ = budget.Set("total", r.budgetTotal)
	} else {
		_ = budget.Set("total", goja.Null())
	}
	_ = budget.Set("spent", func(goja.FunctionCall) goja.Value {
		return vm.ToValue(atomic.LoadInt64(&r.spent))
	})
	_ = budget.Set("remaining", func(goja.FunctionCall) goja.Value {
		if r.budgetTotal <= 0 {
			return vm.ToValue(math.Inf(1))
		}
		rem := r.budgetTotal - atomic.LoadInt64(&r.spent)
		if rem < 0 {
			rem = 0
		}
		return vm.ToValue(rem)
	})
	_ = vm.Set("budget", budget)
}

// jsAgent implements agent(prompt, opts?) -> Promise. It returns a pending promise
// immediately (so the loop stays free to run other thunks — real fan-out), runs the
// blocking subagent on its own goroutine gated by the concurrency semaphore, and
// settles the promise back on the loop goroutine.
func (r *run) jsAgent(call goja.FunctionCall) goja.Value {
	vm := r.vm
	prompt := ""
	if len(call.Arguments) > 0 {
		prompt = call.Argument(0).String()
	}
	var spec AgentSpec
	if len(call.Arguments) > 1 {
		opts := call.Argument(1)
		if opts != nil && !goja.IsUndefined(opts) && !goja.IsNull(opts) {
			if err := vm.ExportTo(opts, &spec); err != nil {
				config.Logger().Printf("[flow] agent opts decode: %v", err)
			}
		}
	}
	spec.Prompt = prompt

	p, resolve, reject := vm.NewPromise()

	if strings.TrimSpace(prompt) == "" {
		_ = reject(vm.ToValue("agent(prompt): prompt must be a non-empty string"))
		return vm.ToValue(p)
	}

	// Hard total-agent cap (runaway-loop backstop).
	if r.engine.maxAgents > 0 && atomic.AddInt64(&r.agentTotal, 1) > int64(r.engine.maxAgents) {
		_ = reject(vm.ToValue(fmt.Sprintf("workflow exceeded the max-agents cap (%d)", r.engine.maxAgents)))
		return vm.ToValue(p)
	}

	id := fmt.Sprintf("agent_%d", atomic.AddInt64(&r.agentSeq, 1))
	label := spec.Label
	if label == "" {
		label = summarize(prompt)
	}
	r.sink.OnAgentStart(r.runID, AgentEvent{ID: id, Label: label, Phase: spec.Phase, Status: "running"})

	// settle centralises the "resolve/reject only on the loop" discipline. Called
	// from the worker goroutine; it hops onto the loop before touching the VM.
	settle := func(res AgentResult, err error) {
		// Emit the terminal sink event OUTSIDE RunOnLoop, unconditionally. The sink
		// is concurrency-safe and does not touch the VM, so it must always fire —
		// every OnAgentStart pairs with an OnAgentDone even if the loop is torn down
		// before the promise settles. (If it lived inside the RunOnLoop callback,
		// a timeout/cancel could drop it, leaving a progress UI showing a phantom
		// "running" agent forever.)
		if err != nil {
			r.sink.OnAgentDone(r.runID, AgentEvent{ID: id, Label: label, Phase: spec.Phase, Status: "failed", Err: err.Error()})
		} else {
			atomic.AddInt64(&r.spent, res.Tokens)
			r.sink.OnAgentDone(r.runID, AgentEvent{ID: id, Label: label, Phase: spec.Phase, Status: "done", Tokens: res.Tokens})
		}
		// Settle the JS promise on the loop goroutine — the only place resolve/reject
		// and goja.Values may be touched. Dropped harmlessly if the loop is terminated
		// (the run has already unwound and the sink is already notified above).
		ok := r.loop.RunOnLoop(func(vm *goja.Runtime) {
			if err != nil {
				_ = reject(vm.ToValue(err.Error()))
				return
			}
			var out interface{}
			if res.Structured != nil {
				out = res.Structured
			} else {
				out = res.Text
			}
			_ = resolve(vm.ToValue(out))
		})
		if !ok {
			config.Logger().Printf("[flow] agent %s promise settle dropped: loop terminated (sink already notified)", id)
		}
	}

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				settle(AgentResult{}, fmt.Errorf("agent panicked: %v", rec))
			}
		}()
		// Concurrency gate: block until a slot frees or the run is cancelled.
		select {
		case r.sema <- struct{}{}:
		case <-r.ctx.Done():
			settle(AgentResult{}, r.ctx.Err())
			return
		}
		defer func() { <-r.sema }()

		res, err := r.engine.spawn(r.ctx, spec)
		settle(res, err)
	}()

	return vm.ToValue(p)
}

// jsPhase implements phase(title, detail?).
func (r *run) jsPhase(call goja.FunctionCall) goja.Value {
	title := ""
	if len(call.Arguments) > 0 {
		title = call.Argument(0).String()
	}
	detail := ""
	if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
		detail = call.Argument(1).String()
	}
	r.sink.OnPhase(r.runID, title, detail)
	return goja.Undefined()
}

// jsLog implements log(msg, level?).
func (r *run) jsLog(call goja.FunctionCall) goja.Value {
	msg := ""
	if len(call.Arguments) > 0 {
		msg = call.Argument(0).String()
	}
	level := "info"
	if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) {
		level = call.Argument(1).String()
	}
	r.sink.OnLog(r.runID, level, msg)
	return goja.Undefined()
}

// jsWorkflow implements workflow(name, args?) -> Promise: run another saved
// workflow inline, one level deep, sharing this run's sink. Returns the child's
// result. Nesting deeper than one level throws.
func (r *run) jsWorkflow(call goja.FunctionCall) goja.Value {
	vm := r.vm
	p, resolve, reject := vm.NewPromise()

	if r.depth >= 1 {
		_ = reject(vm.ToValue("workflow() nesting is one level deep: a workflow() call inside a child workflow is not allowed"))
		return vm.ToValue(p)
	}
	if r.engine.resolver == nil {
		_ = reject(vm.ToValue("workflow() is unavailable: no workflow resolver configured"))
		return vm.ToValue(p)
	}
	name := ""
	if len(call.Arguments) > 0 {
		name = call.Argument(0).String()
	}
	child, ok := r.engine.resolver(name)
	if !ok {
		_ = reject(vm.ToValue(fmt.Sprintf("workflow(%q): no such workflow", name)))
		return vm.ToValue(p)
	}
	var childArgs interface{}
	if len(call.Arguments) > 1 {
		childArgs = call.Argument(1).Export()
	}

	settle := func(res interface{}, err error) {
		// Mirror jsAgent: RunOnLoop returns false once the loop is terminated (the
		// run is already unwinding). We can't touch the VM to settle then, but that
		// is harmless — the Go caller has already returned via r.done/ctx.Done, and
		// the abandoned promise dies with the discarded VM.
		ok := r.loop.RunOnLoop(func(vm *goja.Runtime) {
			if err != nil {
				_ = reject(vm.ToValue(err.Error()))
				return
			}
			_ = resolve(vm.ToValue(res))
		})
		if !ok {
			config.Logger().Printf("[flow] nested workflow %q settle dropped: loop terminated", name)
		}
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				settle(nil, fmt.Errorf("nested workflow panicked: %v", rec))
			}
		}()
		res, err := r.engine.runChild(r.ctx, child, childArgs, r.runID+"/"+name, r.depth+1)
		settle(res, err)
	}()
	return vm.ToValue(p)
}

// jsFlowResolve / jsFlowReject are the terminal callbacks wired by wrapSource.
// They run on the loop goroutine, so Export is safe.
func (r *run) jsFlowResolve(call goja.FunctionCall) goja.Value {
	var result interface{}
	if len(call.Arguments) > 0 {
		result = call.Argument(0).Export()
	}
	r.finish(flowOutcome{result: result})
	return goja.Undefined()
}

func (r *run) jsFlowReject(call goja.FunctionCall) goja.Value {
	msg := "workflow failed"
	if len(call.Arguments) > 0 {
		msg = call.Argument(0).String()
	}
	r.finish(flowOutcome{err: errors.New(msg)})
	return goja.Undefined()
}

// summarize makes a short label from a prompt for the progress UI.
func summarize(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}
