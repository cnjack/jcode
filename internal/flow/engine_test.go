package flow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSpawn is a deterministic in-process SpawnFunc for engine tests. It records
// concurrency and can rendezvous callers on a barrier to prove real parallelism.
type fakeSpawn struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	calls       int
	specs       []AgentSpec
	barrier     *barrier
	perCall     func(spec AgentSpec) (AgentResult, error)
	sleep       time.Duration
}

func (f *fakeSpawn) fn(ctx context.Context, spec AgentSpec) (AgentResult, error) {
	f.mu.Lock()
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	f.calls++
	f.specs = append(f.specs, spec)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()

	if f.barrier != nil {
		if err := f.barrier.arrive(ctx); err != nil {
			return AgentResult{}, err
		}
	}
	if f.sleep > 0 {
		select {
		case <-time.After(f.sleep):
		case <-ctx.Done():
			return AgentResult{}, ctx.Err()
		}
	}
	if f.perCall != nil {
		return f.perCall(spec)
	}
	return AgentResult{Text: "ok:" + spec.Prompt, Tokens: 7}, nil
}

// barrier blocks each arriving caller until `target` callers are simultaneously
// waiting, then releases them all. It proves N callers ran concurrently: if fewer
// than target ever arrive at once, arrive() blocks until ctx is done.
type barrier struct {
	mu     sync.Mutex
	cond   *sync.Cond
	count  int
	target int
	freed  bool
}

func newBarrier(target int) *barrier {
	b := &barrier{target: target}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *barrier) arrive(ctx context.Context) error {
	b.mu.Lock()
	b.count++
	if b.count >= b.target {
		b.freed = true
		b.cond.Broadcast()
	}
	// Wake on ctx cancellation too.
	stop := context.AfterFunc(ctx, func() { b.cond.Broadcast() })
	defer stop()
	for !b.freed {
		if ctx.Err() != nil {
			b.mu.Unlock()
			return ctx.Err()
		}
		b.cond.Wait()
	}
	b.mu.Unlock()
	return nil
}

// collectSink records events for assertions.
type collectSink struct {
	mu          sync.Mutex
	phases      []string
	logs        []string
	agentStarts int
	agentDones  int
	runDone     bool
	result      interface{}
	runErr      error
}

func (s *collectSink) OnRunStart(RunInfo) {}
func (s *collectSink) OnPhase(_ string, title, _ string) {
	s.mu.Lock()
	s.phases = append(s.phases, title)
	s.mu.Unlock()
}
func (s *collectSink) OnAgentStart(string, AgentEvent) {
	s.mu.Lock()
	s.agentStarts++
	s.mu.Unlock()
}
func (s *collectSink) OnAgentDone(string, AgentEvent) {
	s.mu.Lock()
	s.agentDones++
	s.mu.Unlock()
}
func (s *collectSink) OnLog(_ string, _ string, msg string) {
	s.mu.Lock()
	s.logs = append(s.logs, msg)
	s.mu.Unlock()
}
func (s *collectSink) OnRunDone(_ string, result interface{}, err error) {
	s.mu.Lock()
	s.runDone = true
	s.result = result
	s.runErr = err
	s.mu.Unlock()
}

func runScript(t *testing.T, src string, spawn SpawnFunc, sink EventSink, opts RunOptions, engOpts ...Option) (interface{}, error) {
	t.Helper()
	eng := New(spawn, sink, engOpts...)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wf := Workflow{Source: src, Scope: ScopeInline}
	return eng.Run(ctx, wf, opts)
}

func TestBasicAgentAndReturn(t *testing.T) {
	f := &fakeSpawn{}
	src := `
export const meta = { name: "basic", description: "d" };
const a = await agent("hello");
return { got: a };
`
	res, err := runScript(t, src, f.fn, NopSink{}, RunOptions{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("result is %T, want map: %v", res, res)
	}
	if m["got"] != "ok:hello" {
		t.Fatalf("got = %v, want ok:hello", m["got"])
	}
	if f.calls != 1 {
		t.Fatalf("calls = %d, want 1", f.calls)
	}
}

func TestParallelRealConcurrency(t *testing.T) {
	// 4 agents must run at the same time: the barrier only releases once all 4 are
	// simultaneously in flight. If parallel() serialised them, this would deadlock
	// until the run timeout and fail.
	f := &fakeSpawn{barrier: newBarrier(4)}
	src := `
export const meta = { name: "par", description: "d" };
const rs = await parallel([
  () => agent("a"),
  () => agent("b"),
  () => agent("c"),
  () => agent("d"),
]);
return rs;
`
	res, err := runScript(t, src, f.fn, NopSink{}, RunOptions{}, WithConcurrency(16))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	arr, ok := res.([]interface{})
	if !ok || len(arr) != 4 {
		t.Fatalf("result = %#v, want 4-element array", res)
	}
	if f.maxInFlight != 4 {
		t.Fatalf("maxInFlight = %d, want 4 (real parallelism)", f.maxInFlight)
	}
}

func TestConcurrencyCapEnforced(t *testing.T) {
	// cap = 2, launch 6 agents: never more than 2 in flight at once.
	f := &fakeSpawn{sleep: 15 * time.Millisecond}
	src := `
export const meta = { name: "cap", description: "d" };
const thunks = [];
for (let i = 0; i < 6; i++) thunks.push(() => agent("t" + i));
return await parallel(thunks);
`
	_, err := runScript(t, src, f.fn, NopSink{}, RunOptions{}, WithConcurrency(2))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if f.calls != 6 {
		t.Fatalf("calls = %d, want 6", f.calls)
	}
	if f.maxInFlight > 2 {
		t.Fatalf("maxInFlight = %d, want <= 2 (cap violated)", f.maxInFlight)
	}
}

func TestPipelineStages(t *testing.T) {
	f := &fakeSpawn{perCall: func(spec AgentSpec) (AgentResult, error) {
		return AgentResult{Text: spec.Prompt, Tokens: 1}, nil
	}}
	src := `
export const meta = { name: "pipe", description: "d" };
const out = await pipeline(
  ["x", "y"],
  (item) => agent("stage1:" + item),
  (prev) => agent("stage2:" + prev),
);
return out;
`
	res, err := runScript(t, src, f.fn, NopSink{}, RunOptions{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	arr, _ := res.([]interface{})
	if len(arr) != 2 {
		t.Fatalf("result = %#v, want 2 items", res)
	}
	joined := fmt.Sprint(arr...)
	if !strings.Contains(joined, "stage2:stage1:x") || !strings.Contains(joined, "stage2:stage1:y") {
		t.Fatalf("pipeline stages did not chain: %v", arr)
	}
}

func TestStructuredOutput(t *testing.T) {
	f := &fakeSpawn{perCall: func(spec AgentSpec) (AgentResult, error) {
		if spec.Schema == nil {
			t.Errorf("expected schema to be passed through")
		}
		return AgentResult{Structured: map[string]interface{}{"score": float64(42)}, Tokens: 3}, nil
	}}
	src := `
export const meta = { name: "struct", description: "d" };
const r = await agent("rate it", { schema: { type: "object", properties: { score: { type: "number" } } } });
return r.score;
`
	res, err := runScript(t, src, f.fn, NopSink{}, RunOptions{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	// goja exports whole-valued JS numbers as int64, so compare via string.
	if fmt.Sprint(res) != "42" {
		t.Fatalf("score = %v (%T), want 42", res, res)
	}
}

func TestAgentPassesMaxIterations(t *testing.T) {
	f := &fakeSpawn{perCall: func(spec AgentSpec) (AgentResult, error) {
		if spec.MaxIterations != 5 {
			t.Errorf("max iterations = %d, want 5", spec.MaxIterations)
		}
		return AgentResult{Text: "done"}, nil
	}}
	src := `
export const meta = { name: "bounded", description: "d" };
return await agent("review", { maxIterations: 5 });
`
	if _, err := runScript(t, src, f.fn, NopSink{}, RunOptions{}); err != nil {
		t.Fatalf("run error: %v", err)
	}
}

func TestDeterminismGuards(t *testing.T) {
	for _, expr := range []string{"Date.now()", "Math.random()", "new Date()"} {
		f := &fakeSpawn{}
		src := fmt.Sprintf(`
export const meta = { name: "det", description: "d" };
const x = %s;
return x;
`, expr)
		_, err := runScript(t, src, f.fn, NopSink{}, RunOptions{})
		if err == nil {
			t.Fatalf("%s should throw inside a workflow, got no error", expr)
		}
		if !strings.Contains(err.Error(), "not allowed") {
			t.Fatalf("%s error = %v, want 'not allowed'", expr, err)
		}
	}
}

func TestArgsAndBudget(t *testing.T) {
	f := &fakeSpawn{}
	src := `
export const meta = { name: "ab", description: "d" };
log("total=" + budget.total);
const a = await agent("x");
return { area: args.area, spent: budget.spent(), remaining: budget.remaining() };
`
	sink := &collectSink{}
	res, err := runScript(t, src, f.fn, sink, RunOptions{
		Args:        map[string]interface{}{"area": "auth"},
		BudgetTotal: 100,
	})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	m := res.(map[string]interface{})
	if m["area"] != "auth" {
		t.Fatalf("args.area = %v, want auth", m["area"])
	}
	if m["spent"] != int64(7) {
		t.Fatalf("budget.spent() = %v, want 7", m["spent"])
	}
	if m["remaining"] != int64(93) {
		t.Fatalf("budget.remaining() = %v, want 93", m["remaining"])
	}
}

func TestAgentErrorRejectsPromise(t *testing.T) {
	f := &fakeSpawn{perCall: func(spec AgentSpec) (AgentResult, error) {
		return AgentResult{}, fmt.Errorf("boom")
	}}
	src := `
export const meta = { name: "err", description: "d" };
try {
  await agent("x");
  return "no-error";
} catch (e) {
  return "caught:" + String(e);
}
`
	res, err := runScript(t, src, f.fn, NopSink{}, RunOptions{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	s, _ := res.(string)
	if !strings.HasPrefix(s, "caught:") || !strings.Contains(s, "boom") {
		t.Fatalf("result = %q, want caught:...boom", s)
	}
}

func TestCancellation(t *testing.T) {
	f := &fakeSpawn{sleep: 5 * time.Second} // long enough that cancel wins
	src := `
export const meta = { name: "cancel", description: "d" };
return await agent("slow");
`
	eng := New(f.fn, NopSink{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := eng.Run(ctx, Workflow{Source: src, Scope: ScopeInline}, RunOptions{})
	if err == nil {
		t.Fatalf("expected cancellation error")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("cancellation took too long: %v", time.Since(start))
	}
}

func TestTimeoutUnblocksIdleLoop(t *testing.T) {
	// Regression for the adversarial-review finding: a stuck agent that ignores ctx
	// leaves the loop idle; the watchdog must finish the run, not just interrupt.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	spawn := func(ctx context.Context, spec AgentSpec) (AgentResult, error) {
		<-release // block forever (ignores ctx) until test cleanup
		return AgentResult{Text: "late"}, nil
	}
	eng := New(spawn, NopSink{}, WithTimeout(200*time.Millisecond))
	src := `export const meta = { name: "to", description: "d" };
return await agent("stuck");`
	start := time.Now()
	_, err := eng.Run(context.Background(), Workflow{Source: src, Scope: ScopeInline}, RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout took too long (%v) — watchdog did not unblock the select", elapsed)
	}
}

func TestExportInsideStringNotStripped(t *testing.T) {
	// Regression: stripExports must not remove `export` from inside a string.
	f := &fakeSpawn{perCall: func(spec AgentSpec) (AgentResult, error) {
		return AgentResult{Text: spec.Prompt}, nil
	}}
	src := "export const meta = { name: \"s\", description: \"d\" };\n" +
		"const marker = `\nexport pending stuff\n`;\n" +
		"return await agent(marker);"
	res, err := runScript(t, src, f.fn, NopSink{}, RunOptions{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	s, _ := res.(string)
	if !strings.Contains(s, "export pending stuff") {
		t.Fatalf("the word 'export' inside a template string was stripped: %q", s)
	}
}

func TestSinkAgentEventsPairedOnTimeout(t *testing.T) {
	// Regression for the comparison-run finding: OnAgentStart must always be paired
	// with an OnAgentDone, even when the run times out with the agent still stuck —
	// otherwise a progress UI shows a phantom "running" agent forever.
	spawn := func(ctx context.Context, spec AgentSpec) (AgentResult, error) {
		// A well-behaved agent honours ctx: the run-scoped cancel (fired by the
		// watchdog on timeout) makes it return, which drives OnAgentDone.
		<-ctx.Done()
		return AgentResult{}, ctx.Err()
	}
	sink := &collectSink{}
	eng := New(spawn, sink, WithTimeout(200*time.Millisecond))
	src := `export const meta = { name: "pair", description: "d" };
return await agent("stuck");`
	_, err := eng.Run(context.Background(), Workflow{Source: src, Scope: ScopeInline}, RunOptions{})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	// Poll (not a fixed sleep) until the settle goroutine emits the out-of-loop
	// sink event, so this stays stable on slow CI.
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		sink.mu.Lock()
		starts, dones := sink.agentStarts, sink.agentDones
		sink.mu.Unlock()
		if starts == 1 && dones == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("agent starts/dones = %d/%d, want 1/1 (phantom agent: unpaired start)", starts, dones)
		case <-tick.C:
		}
	}
}

func TestMaxAgentsCap(t *testing.T) {
	f := &fakeSpawn{}
	src := `
export const meta = { name: "maxa", description: "d" };
let ok = 0, failed = 0;
for (let i = 0; i < 5; i++) {
  try { await agent("t" + i); ok++; } catch (e) { failed++; }
}
return { ok: ok, failed: failed };
`
	res, err := runScript(t, src, f.fn, NopSink{}, RunOptions{}, WithMaxAgents(3))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	m := res.(map[string]interface{})
	if m["ok"] != int64(3) || m["failed"] != int64(2) {
		t.Fatalf("cap result = %v, want ok=3 failed=2", m)
	}
}

func TestPhaseAndLogEvents(t *testing.T) {
	f := &fakeSpawn{}
	src := `
export const meta = { name: "ev", description: "d" };
phase("Scan", "find files");
log("scanning");
await agent("x");
phase("Done");
return "ok";
`
	sink := &collectSink{}
	_, err := runScript(t, src, f.fn, sink, RunOptions{})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if len(sink.phases) != 2 || sink.phases[0] != "Scan" || sink.phases[1] != "Done" {
		t.Fatalf("phases = %v, want [Scan Done]", sink.phases)
	}
	if len(sink.logs) == 0 || sink.logs[0] != "scanning" {
		t.Fatalf("logs = %v, want [scanning ...]", sink.logs)
	}
	if sink.agentStarts != 1 || sink.agentDones != 1 {
		t.Fatalf("agent starts/dones = %d/%d, want 1/1", sink.agentStarts, sink.agentDones)
	}
}
