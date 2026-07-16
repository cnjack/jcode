package web

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"

	"github.com/cnjack/jcode/internal/flow"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/review"
	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
	"github.com/cnjack/jcode/internal/usage"
)

// Engine is the per-task run state of the web server — one independent top-level
// session (a "task"). It holds exactly the fields that were Server singletons in
// the single-active design: the agent, its conversation history, recorder,
// per-task token tracker, approval axis, working dir, and event handler.
//
// The refactor toward concurrent tasks proceeds in stages:
//   - INC-3 (this): the fields move out of Server into Engine, Server embeds one
//     bootstrap *Engine, and field promotion keeps every existing s.<field>
//     reference compiling and behaving identically (concurrency stays 1).
//   - INC-5: handlers resolve an Engine by task_id (Server.tasks) instead of
//     relying on the promoted bootstrap.
//   - INC-7+: the global run gate is removed so multiple Engines run at once;
//     `running` becomes the per-task busy flag and each Engine gets its own ctx.
//
// Kernel types (session.Recorder, tools.Env, runner.ApprovalState,
// handler.WebHandler, model.TokenUsage) are reused unchanged — parallelism is
// achieved by instantiating one set of them per Engine, never by editing them.
type Engine struct {
	// emu guards this engine's mutable run state (history, recorder, runCancel,
	// sessionSnapshot). Named emu — not mu — so it does not collide with the
	// promoted Server.mu when Engine is embedded in Server.
	emu sync.Mutex

	// taskID is the task identity (== the recorder's session UUID once a message
	// has been recorded). Empty for a freshly created, not-yet-messaged engine.
	taskID string

	// pwd is the task's working directory, bound at creation. In the target model
	// it is immutable for the task's lifetime; "switching project" creates a new
	// Engine rather than mutating this one's env in place.
	pwd string

	// --- run state (guarded today by Server.mu; gains its own lock in a later
	// increment once Server.mu's single-run role is gone) ---
	agent     *adk.ChatModelAgent
	history   []adk.Message
	running   atomic.Bool // per-task busy flag (was the global Server.running gate)
	runCancel context.CancelFunc
	// runGen is bumped (under emu) each time a run installs its runCancel. A run
	// goroutine captures its generation at start and only tears down (clears
	// runCancel, releases running, broadcasts idle) if it is still current — so a
	// finishing run that has already been superseded by the next turn on the same
	// engine does not clobber the new run's cancel and leave the task unstoppable.
	runGen uint64

	// --- per-task model / mode axis ---
	providerName string
	modelName    string
	mode         string // "build" / "plan" / "full_access"

	// --- per-task execution context ---
	env           *tools.Env // fresh per task; todo/goal/bg hang off it
	todoStore     *tools.TodoStore
	approvalState *runner.ApprovalState

	// --- per-task accounting + event emission ---
	recorder        *session.Recorder
	recorderInit    func(*session.Recorder) // decorates lazily-created recorders (see EngineConfig.RecorderInit)
	tokenUsage      *model.TokenUsage
	sessionSnapshot string // git tree hash at run start, for session-scoped diffs
	handler         *handler.WebHandler
	eventHandler    handler.AgentEventHandler // runner handler (may wrap handler in a NotifyingHandler)
	breakdownFn     func() usage.ContextBreakdown

	// per-task agent rebuild closures (bound to THIS task's env/model/prompt), so
	// a model or mode switch rebuilds only this task's agent.
	createAgent    func(providerName, modelName string) (*adk.ChatModelAgent, error)
	rebuildForMode func(planMode bool) (*adk.ChatModelAgent, error)

	// pumpCancel stops this engine's event-forwarding goroutine on teardown.
	pumpCancel context.CancelFunc

	// flowLoader resolves workflow slash commands (/api/slash-commands, slash
	// rewrites) for THIS task's project, so a task in a different project sees its
	// own .jcode/workflows. Shared with this task's workflow_run tool. nil ⇒ the
	// server falls back to its boot loader.
	flowLoader *flow.Loader
}

// EngineConfig carries the per-task pieces a factory (command.buildWebTask)
// produces for one task. The web package owns Engine's unexported fields, so the
// command package hands them over through this exported struct and the server
// assembles the Engine via newEngine.
type EngineConfig struct {
	TaskID         string
	Pwd            string
	Mode           string
	ProviderName   string
	ModelName      string
	Agent          *adk.ChatModelAgent
	Env            *tools.Env
	TodoStore      *tools.TodoStore
	Recorder       *session.Recorder
	TokenUsage     *model.TokenUsage
	ApprovalState  *runner.ApprovalState
	Handler        *handler.WebHandler
	EventHandler   handler.AgentEventHandler
	BreakdownFn    func() usage.ContextBreakdown
	CreateAgent    func(providerName, modelName string) (*adk.ChatModelAgent, error)
	RebuildForMode func(planMode bool) (*adk.ChatModelAgent, error)
	FlowLoader     *flow.Loader
	// RecorderInit decorates recorders this engine creates AFTER build (lazy
	// creation / session switch in chat.go) so they get the same hooks (e.g.
	// the LLM title refiner) as the recorder built with the task.
	RecorderInit func(*session.Recorder)
}

// newEngine assembles an *Engine from the factory-produced config. The engine's
// identity (taskID) is its recorder's session UUID unless an explicit resume id
// was supplied.
func newEngine(c *EngineConfig) *Engine {
	tu := c.TokenUsage
	if tu == nil {
		tu = &model.TokenUsage{}
	}
	taskID := c.TaskID
	if taskID == "" && c.Recorder != nil {
		taskID = c.Recorder.UUID()
	}
	e := &Engine{
		taskID:         taskID,
		pwd:            c.Pwd,
		mode:           c.Mode,
		providerName:   c.ProviderName,
		modelName:      c.ModelName,
		agent:          c.Agent,
		env:            c.Env,
		todoStore:      c.TodoStore,
		recorder:       c.Recorder,
		tokenUsage:     tu,
		approvalState:  c.ApprovalState,
		handler:        c.Handler,
		eventHandler:   c.EventHandler,
		breakdownFn:    c.BreakdownFn,
		createAgent:    c.CreateAgent,
		rebuildForMode: c.RebuildForMode,
		flowLoader:     c.FlowLoader,
		recorderInit:   c.RecorderInit,
	}
	// Give the approval reviewer (when one is installed on this ApprovalState)
	// recent conversation context. Harmless when no reviewer is set.
	if e.approvalState != nil {
		e.approvalState.SetTranscriptFunc(e.recentTranscript)
	}
	return e
}

// recentTranscript snapshots the tail of the conversation for the approval
// reviewer. Reads eng.history under emu, so it is safe to call from the run
// goroutine where approvals happen.
func (e *Engine) recentTranscript() []review.Msg {
	e.emu.Lock()
	defer e.emu.Unlock()
	return review.MsgsFromHistory(e.history)
}

// activeEngine returns the currently-foregrounded engine (the embedded bootstrap
// pointer), read under s.mu so it never tears against setActiveEngine's swap.
// Legacy non-task-routed handlers MUST go through this accessor (and the
// emu-locked Engine helpers below) instead of bare promoted s.<field> reads.
func (s *Server) activeEngine() *Engine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Engine
}

// flowLoaderFor returns the task-scoped workflow loader for eng (so slash
// commands resolve THIS task's project workflows), falling back to the server's
// boot loader when the engine has none — e.g. before any task engine exists.
func (s *Server) flowLoaderFor(eng *Engine) *flow.Loader {
	if eng != nil && eng.flowLoader != nil {
		return eng.flowLoader
	}
	return s.flowLoader
}

// --- emu-guarded accessors for an engine's MUTABLE run-state fields (agent,
// recorder, mode, provider, model). Immutable-after-build fields (pwd, env,
// todoStore, handler, tokenUsage, approvalState, breakdownFn) may be read
// directly off an *Engine snapshot obtained via activeEngine/resolveEngine. ---

// activePwd returns the foreground engine's working directory (or "") via the
// s.mu-guarded accessor, so file/exec/git/pty handlers never read the swapped
// s.Engine pointer bare.
func (s *Server) activePwd() string {
	if eng := s.activeEngine(); eng != nil {
		return eng.pwd
	}
	return ""
}

// activeHandler returns the foreground engine's WebHandler (or nil).
func (s *Server) activeHandler() *handler.WebHandler {
	if eng := s.activeEngine(); eng != nil {
		return eng.handler
	}
	return nil
}

// activeMode returns the foreground engine's mode (or "").
func (s *Server) activeMode() string {
	if eng := s.activeEngine(); eng != nil {
		return eng.curMode()
	}
	return ""
}

// modelSnapshot returns the engine's provider/model/mode under emu.
func (e *Engine) modelSnapshot() (provider, model, modeStr string) {
	e.emu.Lock()
	defer e.emu.Unlock()
	return e.providerName, e.modelName, e.mode
}

// curMode returns the engine's mode under emu.
func (e *Engine) curMode() string {
	e.emu.Lock()
	defer e.emu.Unlock()
	return e.mode
}

// recUUID returns the engine recorder's UUID (or "") under emu.
func (e *Engine) recUUID() string {
	e.emu.Lock()
	defer e.emu.Unlock()
	if e.recorder == nil {
		return ""
	}
	return e.recorder.UUID()
}

// applyModelSwitch swaps the engine's agent + provider/model under emu and
// re-tags the recorder with the new model.
func (e *Engine) applyModelSwitch(ag *adk.ChatModelAgent, provider, model string) {
	e.emu.Lock()
	defer e.emu.Unlock()
	e.agent = ag
	e.providerName = provider
	e.modelName = model
	if e.recorder != nil {
		e.recorder.SetModel(model)
	}
}

// applyModeSwitch sets the engine's mode and (optionally) its rebuilt agent.
func (e *Engine) applyModeSwitch(modeStr string, ag *adk.ChatModelAgent) {
	e.emu.Lock()
	defer e.emu.Unlock()
	e.mode = modeStr
	if ag != nil {
		e.agent = ag
	}
}

// setAgent swaps just the agent under emu (MCP reload, skill toggle, setup).
func (e *Engine) setAgent(ag *adk.ChatModelAgent) {
	e.emu.Lock()
	defer e.emu.Unlock()
	e.agent = ag
}

// setAgentIfModel installs ag only if the engine is still on provider/model.
// Rebuild paths construct agents outside emu; if a model switch lands in that
// window, its (newer) agent must win over the rebuild's stale one. Returns
// whether the agent was installed.
func (e *Engine) setAgentIfModel(ag *adk.ChatModelAgent, provider, model string) bool {
	e.emu.Lock()
	defer e.emu.Unlock()
	if e.providerName != provider || e.modelName != model {
		return false
	}
	e.agent = ag
	return true
}

// resolveEngine returns the engine for taskID, or the active engine when taskID
// is empty (legacy / no-task_id callers). Returns nil when taskID is unknown.
func (s *Server) resolveEngine(taskID string) *Engine {
	if taskID == "" {
		return s.activeEngine()
	}
	s.tasksMu.RLock()
	eng := s.tasks[taskID]
	s.tasksMu.RUnlock()
	if eng == nil {
		// The active engine may not be in the map yet under its session UUID
		// (a brand-new chat whose recorder UUID the client already knows).
		if a := s.activeEngine(); a != nil && a.taskID == taskID {
			return a
		}
	}
	return eng
}

// maxLiveEngines bounds the number of concurrently-live task engines so a
// client cannot mint unbounded engines (fd/goroutine/agent accumulation).
const maxLiveEngines = 64

var errTooManyTasks = fmt.Errorf("too many concurrent tasks")

// registerEngine adds eng to the tasks map (keyed by task id), publishes its
// pump-cancel under tasksMu (so teardown observes it), and starts its event
// pump. Idempotent for an already-registered engine. Returns errTooManyTasks if
// registering a NEW engine would exceed the live-engine cap.
func (s *Server) registerEngine(eng *Engine) error {
	if eng == nil || eng.taskID == "" {
		return nil
	}
	s.tasksMu.Lock()
	_, existed := s.tasks[eng.taskID]
	if !existed && len(s.tasks) >= maxLiveEngines {
		s.tasksMu.Unlock()
		return errTooManyTasks
	}
	var pumpCtx context.Context
	if !existed {
		base := s.rootCtx()
		if base == nil {
			base = context.Background()
		}
		ctx, cancel := context.WithCancel(base)
		eng.pumpCancel = cancel // published under tasksMu; teardown reads it after a tasksMu acquisition
		pumpCtx = ctx
	}
	s.tasks[eng.taskID] = eng
	s.tasksMu.Unlock()
	if pumpCtx != nil {
		s.startPump(pumpCtx, eng)
	}
	return nil
}

// startPump forwards eng's handler events to the WS broker, stamped with the
// engine's task id, until ctx is cancelled (teardown) or the channel closes.
// Each engine gets its own pump so concurrent tasks never serialize on one
// forwarding goroutine.
func (s *Server) startPump(ctx context.Context, eng *Engine) {
	events := eng.handler.Events()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				s.wsBroker.Broadcast(WSEvent{TaskID: eng.taskID, Type: ev.Event, Data: ev.Data})
			}
		}
	}()
}

// buildLocalEngine creates and registers a fresh local task engine.
func (s *Server) buildLocalEngine(taskID, pwd, modeStr string) (*Engine, error) {
	if s.newEngine == nil {
		return nil, fmt.Errorf("task creation is not supported")
	}
	return s.buildLocalEngineWith(taskID, pwd, modeStr, s.newEngine)
}

// buildLocalEngineWith assembles, model-inherits, and registers a local task
// engine using the supplied factory. The factory is a parameter so automation
// runs can pass the headless factory (which drops interactive tools) while
// sharing all the registration/model-inheritance plumbing with normal tasks.
func (s *Server) buildLocalEngineWith(taskID, pwd, modeStr string, factory func(taskID, pwd, mode string) (*EngineConfig, error)) (*Engine, error) {
	ec, err := factory(taskID, pwd, modeStr)
	if err != nil {
		return nil, err
	}
	eng := newEngine(ec)
	// Inherit the foreground task's current model selection rather than reverting
	// to the startup default (the factory bakes in startup provider/model).
	if cur := s.activeEngine(); cur != nil && eng.createAgent != nil {
		prov, mdl, _ := cur.modelSnapshot()
		if prov != "" && (prov != eng.providerName || mdl != eng.modelName) {
			if ag, agErr := eng.createAgent(prov, mdl); agErr == nil {
				eng.applyModelSwitch(ag, prov, mdl)
			}
		}
	}
	if err := s.registerEngine(eng); err != nil {
		eng.teardown()
		return nil, err
	}
	return eng, nil
}

// buildRemoteEngine creates and registers a fresh remote (SSH or Docker) task engine.
func (s *Server) buildRemoteEngine(taskID string, exec tools.RemoteExecutor, remotePwd, modeStr string) (*Engine, error) {
	if s.newRemoteEngine == nil {
		return nil, fmt.Errorf("remote task creation is not supported")
	}
	ec, err := s.newRemoteEngine(taskID, exec, remotePwd, modeStr)
	if err != nil {
		return nil, err
	}
	eng := newEngine(ec)
	if err := s.registerEngine(eng); err != nil {
		eng.teardown()
		return nil, err
	}
	return eng, nil
}

// setActiveEngine makes eng the foreground engine (the one the promoted legacy
// handlers operate on). It reclaims the OUTGOING engine only when it is an unused
// throwaway (idle and never recorded) — e.g. a "new chat" the user navigated
// away from without typing — so the new-chat/switch-project path doesn't leak an
// engine each time. A running or already-recorded task is kept (real background
// work). The pointer swap is guarded by s.mu, matching activeEngine's read.
func (s *Server) setActiveEngine(eng *Engine) {
	if eng == nil {
		return
	}
	_ = s.registerEngine(eng)
	s.mu.Lock()
	prev := s.Engine
	s.Engine = eng
	s.mu.Unlock()
	if prev != nil && prev != eng {
		// Re-check running INSIDE emu, together with the recorder check, rather
		// than via an unlocked pre-check: a run starting on prev concurrently
		// (running flips true, runCancel set under emu) must not be torn down. The
		// folded check only ever makes reclaim more conservative — at worst it
		// leaks an idle throwaway engine, never cancels a live run.
		reclaim := false
		prev.emu.Lock()
		if !prev.running.Load() && (prev.recorder == nil || !prev.recorder.HasRecording()) {
			reclaim = true
		}
		prev.emu.Unlock()
		if reclaim {
			s.deleteEngine(prev.taskID)
		}
	}
}

// deleteEngine removes a task engine from the map and tears it down (stops its
// pump, cancels its run, closes its recorder). The active engine is never
// deleted out from under the foreground; callers guard against that.
func (s *Server) deleteEngine(taskID string) {
	s.tasksMu.Lock()
	eng := s.tasks[taskID]
	delete(s.tasks, taskID)
	s.tasksMu.Unlock()
	if eng != nil {
		eng.teardown()
	}
}

// teardown stops the engine's event pump, cancels any in-flight run, waits for
// it to drain, then closes the recorder — so the recorder is never closed under
// a live writer (which would truncate the session file). Shared resources (push
// notifiers, registry, skill loader, MCP clients) are owned by the Server and
// deliberately left untouched. Callers reach teardown only after a tasksMu
// acquisition (deleteEngine/CloseAllEngines), which establishes happens-before
// with registerEngine's pumpCancel publication.
func (e *Engine) teardown() {
	if e.pumpCancel != nil {
		e.pumpCancel()
	}
	e.emu.Lock()
	c := e.runCancel
	e.emu.Unlock()
	if c != nil {
		c()
	}
	// Best-effort wait for the run goroutine to flip running=false so its final
	// RecordAssistant lands before we close.
	for i := 0; i < 200 && e.running.Load(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if e.recorder != nil {
		e.recorder.Close()
	}
	// Release the remote target, if any: closes the SSH connection or
	// decrements the Docker container ref-count (stopping it on last release).
	// No-op for local engines.
	if e.env != nil {
		_ = e.env.CloseRemote()
		// Close this task's browser session (managed tabs close; extension tabs
		// are detached back to the user). No-op if the task never used browser.
		e.env.CloseBrowser()
	}
}

// setTaskStatus broadcasts a global task_status event (so every client's sidebar
// can mark the task running/idle live) and best-effort persists Status +
// UpdatedAt so recency survives a reload. The broadcast carries the task id in
// its DATA (not the envelope TaskID) so it is delivered to all clients, not
// filtered to the task's subscribers.
func (s *Server) setTaskStatus(eng *Engine, running bool) {
	if eng == nil || eng.taskID == "" {
		return
	}
	status := "idle"
	if running {
		status = "running"
	}
	s.wsBroker.Broadcast(WSEvent{Type: "task_status", Data: map[string]any{
		"task_id": eng.taskID,
		"running": running,
		"status":  status,
	}})
	go func(id, st string) {
		_, _ = session.UpdateSessionMeta(id, func(m *session.SessionMeta) {
			m.Status = st
			m.UpdatedAt = time.Now().Format(time.RFC3339)
		})
	}(eng.taskID, status)
}

// CloseAllEngines tears down every live engine. Called on server shutdown.
func (s *Server) CloseAllEngines() {
	s.tasksMu.Lock()
	engines := make([]*Engine, 0, len(s.tasks))
	for _, e := range s.tasks {
		engines = append(engines, e)
	}
	s.tasks = make(map[string]*Engine)
	s.tasksMu.Unlock()
	for _, e := range engines {
		e.teardown()
	}
}
