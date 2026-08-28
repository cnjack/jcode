package web

import (
	"context"
	"fmt"
	"os"
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
	// rebuildMu serializes schema/prompt rebuilds for this engine. Revision checks
	// protect model/mode switches, while this lock ensures two process-wide
	// settings refreshes cannot both build from one revision and discard whichever
	// configuration happened to finish second.
	rebuildMu sync.Mutex
	// toolOverrideMu serializes GET hydration and two-phase CAS updates so a
	// client never observes the durable revision between fsync and the prepared
	// agent/catalog publication.
	toolOverrideMu sync.Mutex
	// uploadMu serializes task-scoped file persistence with session deletion.
	// uploadGeneration invalidates a request that finished reading its multipart
	// body after the owning session was deleted or reset.
	uploadMu         sync.Mutex
	uploadGeneration uint64
	// retired permanently closes this engine to new run/focus claims. Lifecycle
	// transitions set it while holding Server.tasksMu -> Server.mu; readers use
	// the atomic fast-path after resolving an engine pointer.
	retired atomic.Bool
	// teardownOnce makes resource release idempotent across shutdown, failed
	// publication, and concurrent cleanup paths.
	teardownOnce sync.Once

	// taskID is the task identity (== the recorder's session UUID once a message
	// has been recorded). Empty for a freshly created, not-yet-messaged engine.
	taskID string

	// pwd is the task's working directory, bound at creation. In the target model
	// it is immutable for the task's lifetime; "switching project" creates a new
	// Engine rather than mutating this one's env in place.
	pwd string
	// workspaceKind distinguishes a user-selected project from a JCode-managed
	// no-project workspace. It is immutable for the task's lifetime.
	workspaceKind session.WorkspaceKind

	// --- run state (guarded today by Server.mu; gains its own lock in a later
	// increment once Server.mu's single-run role is gone) ---
	agent         *adk.ChatModelAgent
	agentRevision uint64 // invalidates slow agent rebuilds when model/mode changes concurrently
	history       []adk.Message
	running       atomic.Bool // per-task busy flag (was the global Server.running gate)
	runCancel     context.CancelFunc
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
	agentRole    string // custom top-level agent name; empty = default

	// --- per-task execution context ---
	env           *tools.Env // fresh per task; todo/goal/bg hang off it
	todoStore     *tools.TodoStore
	taskHub       *tools.TaskHub
	approvalState *runner.ApprovalState

	// --- per-task accounting + event emission ---
	recorder        *session.Recorder
	recorderInit    func(*session.Recorder) // decorates lazily-created recorders (see EngineConfig.RecorderInit)
	tokenUsage      *model.TokenUsage
	sessionSnapshot string // git tree hash at run start, for session-scoped diffs
	handler         *handler.WebHandler
	eventHandler    handler.AgentEventHandler // runner handler (may wrap handler in a NotifyingHandler)
	breakdownFn     func() usage.ContextBreakdown
	toolSearchStats func() ToolSearchCounts

	// per-task agent rebuild closures (bound to THIS task's env/model/prompt), so
	// a model or mode switch rebuilds only this task's agent.
	createAgent    func(providerName, modelName string) (*adk.ChatModelAgent, error)
	rebuildForMode func(planMode bool) (*adk.ChatModelAgent, error)
	rebuildForRole func(roleName, providerName, modelName string) (*AgentRoleBuild, error)

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
	TaskID          string
	Pwd             string
	WorkspaceKind   session.WorkspaceKind
	Mode            string
	ProviderName    string
	ModelName       string
	AgentRole       string
	Agent           *adk.ChatModelAgent
	Env             *tools.Env
	TodoStore       *tools.TodoStore
	TaskHub         *tools.TaskHub // per-task persistent agent-task registry handle
	Recorder        *session.Recorder
	TokenUsage      *model.TokenUsage
	ApprovalState   *runner.ApprovalState
	Handler         *handler.WebHandler
	EventHandler    handler.AgentEventHandler
	BreakdownFn     func() usage.ContextBreakdown
	ToolSearchStats func() ToolSearchCounts
	CreateAgent     func(providerName, modelName string) (*adk.ChatModelAgent, error)
	RebuildForMode  func(planMode bool) (*adk.ChatModelAgent, error)
	RebuildForRole  func(roleName, providerName, modelName string) (*AgentRoleBuild, error)
	FlowLoader      *flow.Loader
	// RecorderInit decorates recorders this engine creates AFTER build (lazy
	// creation / session switch in chat.go) so they get the same hooks (e.g.
	// the LLM title refiner) as the recorder built with the task.
	RecorderInit func(*session.Recorder)
}

// AgentRoleBuild is an atomic custom-agent rebuild result. Provider/model are
// the actual model selected after applying the role's optional model override.
type AgentRoleBuild struct {
	Agent    *adk.ChatModelAgent
	Provider string
	Model    string
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
		taskID:          taskID,
		pwd:             c.Pwd,
		workspaceKind:   session.NormalizeWorkspaceKind(c.WorkspaceKind),
		mode:            c.Mode,
		providerName:    c.ProviderName,
		modelName:       c.ModelName,
		agentRole:       c.AgentRole,
		agent:           c.Agent,
		env:             c.Env,
		todoStore:       c.TodoStore,
		taskHub:         c.TaskHub,
		recorder:        c.Recorder,
		tokenUsage:      tu,
		approvalState:   c.ApprovalState,
		handler:         c.Handler,
		eventHandler:    c.EventHandler,
		breakdownFn:     c.BreakdownFn,
		toolSearchStats: c.ToolSearchStats,
		createAgent:     c.CreateAgent,
		rebuildForMode:  c.RebuildForMode,
		rebuildForRole:  c.RebuildForRole,
		flowLoader:      c.FlowLoader,
		recorderInit:    c.RecorderInit,
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

// tryStartEngine serializes a task's idle->running claim with foreground
// changes. Guarded Desktop reopen requests take s.mu exclusively while they
// verify require_idle and focus their candidate, so a run claim can never land
// between that final check and the pointer swap.
func (s *Server) tryStartEngine(eng *Engine) bool {
	if eng == nil {
		return false
	}
	s.tasksMu.RLock()
	s.mu.RLock()
	canonical := !eng.retired.Load() && s.canonicalEngineLocked(eng)
	started := canonical && eng.running.CompareAndSwap(false, true)
	s.mu.RUnlock()
	s.tasksMu.RUnlock()
	return started
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

// ensureAgentAvailable lazily repairs an Engine whose selected model could not
// be constructed when the task engine was built. This keeps the control plane
// reachable for account reauthentication and provider/model changes without
// ever executing on a silent fallback. Rebuilds are serialized with the normal
// model/mode paths so a successful recovery cannot overwrite a concurrent user
// selection.
func (e *Engine) ensureAgentAvailable() error {
	e.emu.Lock()
	if e.agent != nil {
		e.emu.Unlock()
		return nil
	}
	e.emu.Unlock()

	if e.createAgent == nil {
		return fmt.Errorf("selected model agent is unavailable")
	}

	e.rebuildMu.Lock()
	defer e.rebuildMu.Unlock()

	e.emu.Lock()
	if e.agent != nil {
		e.emu.Unlock()
		return nil
	}
	provider, modelName := e.providerName, e.modelName
	e.emu.Unlock()

	ag, err := e.createAgent(provider, modelName)
	if err != nil {
		return fmt.Errorf("create agent for selected model %s/%s: %w", provider, modelName, err)
	}
	if ag == nil {
		return fmt.Errorf("create agent for selected model %s/%s returned nil", provider, modelName)
	}
	e.applyModelSwitch(ag, provider, modelName)
	return nil
}

// agentBuildSnapshot captures the inputs and revision for an asynchronous
// agent rebuild. Call installAgentIfRevision with the returned revision: a
// concurrent model/mode/skill change must win instead of being overwritten by
// a slower rebuild that used stale inputs.
func (e *Engine) agentBuildSnapshot() (provider, model, modeStr string, revision uint64) {
	e.emu.Lock()
	defer e.emu.Unlock()
	return e.providerName, e.modelName, e.mode, e.agentRevision
}

// curMode returns the engine's mode under emu.
func (e *Engine) curMode() string {
	e.emu.Lock()
	defer e.emu.Unlock()
	return e.mode
}

// recordModeChange commits authorization state before a caller publishes it to
// the live engine or UI. The recorder pointer is snapshotted under emu, while
// the potentially slow fsync runs without blocking unrelated history reads.
func (e *Engine) recordModeChange(modeStr string) error {
	e.emu.Lock()
	recorder := e.recorder
	e.emu.Unlock()
	if recorder == nil {
		return fmt.Errorf("session recorder is unavailable")
	}
	return recorder.RecordModeChangeStrict(modeStr)
}

func (e *Engine) curAgentRole() string {
	e.emu.Lock()
	defer e.emu.Unlock()
	return e.agentRole
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
// re-tags the recorder with the new provider/model pair.
func (e *Engine) applyModelSwitch(ag *adk.ChatModelAgent, provider, model string) {
	e.emu.Lock()
	e.agent = ag
	e.agentRevision++
	e.providerName = provider
	e.modelName = model
	rec := e.recorder
	e.emu.Unlock()
	if rec != nil {
		rec.SetProviderModel(provider, model)
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
	e.agentRevision++
}

func (e *Engine) applyAgentRoleSwitch(roleName string, built *AgentRoleBuild) {
	e.emu.Lock()
	e.agentRole = roleName
	if built != nil && built.Agent != nil {
		e.agent = built.Agent
		e.providerName = built.Provider
		e.modelName = built.Model
	}
	e.agentRevision++
	rec := e.recorder
	e.emu.Unlock()
	if rec != nil {
		rec.SetAgent(roleName)
		if built != nil && built.Provider != "" && built.Model != "" {
			rec.SetProviderModel(built.Provider, built.Model)
		}
	}
}

// setAgent swaps just the agent under emu (MCP reload, skill toggle, setup).
func (e *Engine) setAgent(ag *adk.ChatModelAgent) {
	e.emu.Lock()
	defer e.emu.Unlock()
	e.agent = ag
	e.agentRevision++
}

// installAgentIfRevision atomically installs a rebuilt agent only if no other
// operation changed the engine's agent inputs or agent since the build began.
func (e *Engine) installAgentIfRevision(ag *adk.ChatModelAgent, revision uint64) bool {
	e.emu.Lock()
	defer e.emu.Unlock()
	if e.agentRevision != revision {
		return false
	}
	e.agent = ag
	e.agentRevision++
	return true
}

// resolveEngine returns the engine for taskID, or the active engine when taskID
// is empty (legacy / no-task_id callers). Returns nil when taskID is unknown.
func (s *Server) resolveEngine(taskID string) *Engine {
	if taskID == "" {
		eng := s.activeEngine()
		if eng != nil && eng.retired.Load() {
			return nil
		}
		return eng
	}
	s.tasksMu.RLock()
	eng := s.tasks[taskID]
	s.tasksMu.RUnlock()
	if eng != nil && eng.retired.Load() {
		eng = nil
	}
	if eng == nil {
		// The active engine may not be in the map yet under its session UUID
		// (a brand-new chat whose recorder UUID the client already knows).
		if a := s.activeEngine(); a != nil && !a.retired.Load() && a.taskID == taskID {
			return a
		}
	}
	return eng
}

// maxLiveEngines bounds the number of concurrently-live task engines so a
// client cannot mint unbounded engines (fd/goroutine/agent accumulation).
const maxLiveEngines = 64

var errTooManyTasks = fmt.Errorf("too many concurrent tasks")
var errTaskAlreadyRegistered = fmt.Errorf("task engine is already registered")
var errEngineRetired = fmt.Errorf("task engine is retired")

// registerEngine adds eng to the tasks map (keyed by task id), publishes its
// pump-cancel under tasksMu (so teardown observes it), and starts its event
// pump. Idempotent for an already-registered engine. Returns errTooManyTasks if
// registering a NEW engine would exceed the live-engine cap.
func (s *Server) registerEngine(eng *Engine) error {
	if eng == nil {
		return nil
	}
	if eng.retired.Load() {
		return errEngineRetired
	}
	s.prepareEngineRegistration(eng)
	if eng.taskID == "" {
		return nil
	}
	s.tasksMu.Lock()
	if eng.retired.Load() {
		s.tasksMu.Unlock()
		return errEngineRetired
	}
	existing, existed := s.tasks[eng.taskID]
	if existed {
		s.tasksMu.Unlock()
		if existing == eng {
			return nil
		}
		return errTaskAlreadyRegistered
	}
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

func (s *Server) prepareEngineRegistration(eng *Engine) {
	if eng == nil {
		return
	}
	if eng.handler != nil {
		eng.handler.SetModePromotionCallback(func() error {
			return s.syncModeAfterApproval(eng, true, true)
		})
	}
	if eng.env == nil {
		return
	}
	statusSource, ok := eng.env.Exec.(tools.RemoteConnectionStatusSource)
	if !ok {
		return
	}
	taskID := eng.taskID
	statusSource.SetRemoteConnectionStatusHandler(func(status tools.RemoteConnectionStatus) {
		// Reconnect state is task-scoped control-plane data. It bypasses the
		// WebHandler pump because it originates below the agent runner, but uses
		// the same task-tagged broker envelope so task subscriptions still apply.
		if taskID == "" || s.wsBroker == nil {
			return
		}
		s.wsBroker.Broadcast(WSEvent{
			TaskID: taskID,
			Type:   "remote_connection_status",
			Data:   status,
		})
	})
}

// publishEngineCandidate atomically installs a fully built and hydrated engine.
// expected is nil for a cold/new task and the currently published engine for an
// idle reconnect. The old runtime is never removed before its replacement is
// ready, so concurrent task resolution cannot observe an empty or unhydrated
// conversation. Callers serialize this with taskCreateMu.
func (s *Server) publishEngineCandidate(eng, expected *Engine) error {
	if eng == nil || eng.taskID == "" {
		return fmt.Errorf("cannot publish an engine without a task id")
	}
	if eng.retired.Load() {
		return errEngineRetired
	}
	s.prepareEngineRegistration(eng)

	base := s.rootCtx()
	if base == nil {
		base = context.Background()
	}
	pumpCtx, cancel := context.WithCancel(base)
	eng.pumpCancel = cancel

	s.tasksMu.Lock()
	existing, exists := s.tasks[eng.taskID]
	s.mu.Lock()
	if eng.retired.Load() {
		s.mu.Unlock()
		s.tasksMu.Unlock()
		cancel()
		return errEngineRetired
	}
	activeMatchesExpected := expected != nil && s.Engine == expected
	if expected == nil {
		if exists {
			s.mu.Unlock()
			s.tasksMu.Unlock()
			cancel()
			return errTaskAlreadyRegistered
		}
		if len(s.tasks) >= maxLiveEngines {
			s.mu.Unlock()
			s.tasksMu.Unlock()
			cancel()
			return errTooManyTasks
		}
	} else {
		if expected.taskID != eng.taskID {
			s.mu.Unlock()
			s.tasksMu.Unlock()
			cancel()
			return fmt.Errorf("replacement task id %s does not match %s", eng.taskID, expected.taskID)
		}
		if (exists && existing != expected) || (!exists && !activeMatchesExpected) {
			s.mu.Unlock()
			s.tasksMu.Unlock()
			cancel()
			return fmt.Errorf("task engine changed during activation")
		}
		if expected.retired.Load() {
			s.mu.Unlock()
			s.tasksMu.Unlock()
			cancel()
			return fmt.Errorf("conversation %s runtime is retired", eng.taskID)
		}
		if expected.running.Load() {
			s.mu.Unlock()
			s.tasksMu.Unlock()
			cancel()
			return fmt.Errorf("conversation %s is running; refusing to replace its runtime", eng.taskID)
		}
		expected.retired.Store(true)
	}
	s.tasks[eng.taskID] = eng
	if activeMatchesExpected {
		// Replacing an unhealthy runtime for the same foreground identity is not a
		// focus change. Keep Desktop attached to the repaired engine.
		s.Engine = eng
	}
	s.mu.Unlock()
	s.tasksMu.Unlock()

	s.startPump(pumpCtx, eng)
	if expected != nil {
		if s.ptyMgr != nil {
			s.ptyMgr.closeForTask(expected.taskID)
		}
		expected.teardown()
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
	eng, err := s.assembleLocalEngine(taskID, pwd, modeStr, factory)
	if err != nil {
		return nil, err
	}
	if err := s.registerEngine(eng); err != nil {
		eng.teardown()
		return nil, err
	}
	return eng, nil
}

// assembleLocalEngine constructs a candidate without publishing it. Activation
// uses this to finish hydration before the task becomes resolvable.
func (s *Server) assembleLocalEngine(taskID, pwd, modeStr string, factory func(taskID, pwd, mode string) (*EngineConfig, error)) (*Engine, error) {
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
		roleName := cur.curAgentRole()
		if roleName != "" && eng.rebuildForRole != nil {
			prov, mdl, _ := eng.modelSnapshot()
			if built, agErr := eng.rebuildForRole(roleName, prov, mdl); agErr == nil {
				eng.applyAgentRoleSwitch(roleName, built)
			}
		}
	}
	return eng, nil
}

// assembleRemoteEngine constructs an unpublished remote candidate. The caller
// retains ownership of exec until the candidate is atomically published.
func (s *Server) assembleRemoteEngine(taskID string, exec tools.RemoteExecutor, remotePwd, modeStr string) (*Engine, error) {
	if s.newRemoteEngine == nil {
		return nil, fmt.Errorf("remote task creation is not supported")
	}
	ec, err := s.newRemoteEngine(taskID, exec, remotePwd, modeStr)
	if err != nil {
		return nil, err
	}
	eng := newEngine(ec)
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
	if err := s.registerEngine(eng); err != nil {
		if canonical := s.resolveEngine(eng.taskID); canonical != nil {
			if canonical != eng {
				eng.teardown()
			}
			eng = canonical
		} else {
			eng.teardown()
			return
		}
	}
	prev, ok := s.swapActiveEngine(eng, nil, false, false)
	if !ok {
		return
	}
	s.finishActiveEngineSwap(prev, eng)
}

// canonicalEngineLocked reports whether eng is still the live registry
// occurrence. The caller holds tasksMu and s.mu (read or write).
func (s *Server) canonicalEngineLocked(eng *Engine) bool {
	if eng == nil || eng.retired.Load() {
		return false
	}
	if eng.taskID == "" {
		return s.Engine == eng
	}
	return s.tasks[eng.taskID] == eng
}

// swapActiveEngine validates the target and commits one foreground pointer
// update under the global lifecycle lock order: tasksMu -> s.mu. When guarded,
// expectedSessionID/requireIdle are revalidated in the same critical section.
func (s *Server) swapActiveEngine(
	eng *Engine,
	expectedSessionID *string,
	requireIdle bool,
	guarded bool,
) (*Engine, bool) {
	if eng == nil {
		return nil, false
	}
	s.tasksMu.RLock()
	s.mu.Lock()
	if !s.canonicalEngineLocked(eng) {
		s.mu.Unlock()
		s.tasksMu.RUnlock()
		return nil, false
	}
	if guarded && s.activeEngineGuardFailureLocked(expectedSessionID, requireIdle) != "" {
		s.mu.Unlock()
		s.tasksMu.RUnlock()
		return nil, false
	}
	prev := s.Engine
	s.Engine = eng
	s.mu.Unlock()
	s.tasksMu.RUnlock()
	return prev, true
}

type activeEngineGuardFailure string

const (
	activeEngineChanged activeEngineGuardFailure = "session_changed"
	activeEngineBusy    activeEngineGuardFailure = "session_running"
)

// activeEngineGuardFailureLocked checks a conditional foreground update. The
// caller holds s.mu for reading or writing. A pointer distinguishes an omitted
// expected_session_id from an explicitly expected empty id.
func (s *Server) activeEngineGuardFailureLocked(expectedSessionID *string, requireIdle bool) activeEngineGuardFailure {
	current := s.Engine
	currentID := ""
	if current != nil {
		currentID = current.taskID
	}
	if expectedSessionID != nil && currentID != *expectedSessionID {
		return activeEngineChanged
	}
	if requireIdle && current != nil && current.running.Load() {
		return activeEngineBusy
	}
	return ""
}

// snapshotActiveEngineGuard turns require_idle-only into an identity guard by
// capturing the current task id in the same read section as the preflight.
// This prevents a slow fresh-task build started on A from overwriting a later
// idle navigation to B.
func (s *Server) snapshotActiveEngineGuard(
	expectedSessionID *string,
	requireIdle bool,
) (*string, activeEngineGuardFailure) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if expectedSessionID == nil && requireIdle {
		currentID := ""
		if current := s.Engine; current != nil {
			currentID = current.taskID
		}
		expectedSessionID = &currentID
	}
	return expectedSessionID, s.activeEngineGuardFailureLocked(expectedSessionID, requireIdle)
}

// focusEngineGuarded atomically revalidates the Desktop reopen preconditions
// and focuses eng. Run starts take s.mu for reading via tryStartEngine, while
// every foreground navigation takes it for writing, so neither can cross the
// final guard/swap boundary.
func (s *Server) focusEngineGuarded(
	eng *Engine,
	expectedSessionID *string,
	requireIdle bool,
) activeEngineGuardFailure {
	if eng == nil {
		return activeEngineChanged
	}
	s.tasksMu.RLock()
	s.mu.Lock()
	if !s.canonicalEngineLocked(eng) {
		s.mu.Unlock()
		s.tasksMu.RUnlock()
		return activeEngineChanged
	}
	if failure := s.activeEngineGuardFailureLocked(expectedSessionID, requireIdle); failure != "" {
		s.mu.Unlock()
		s.tasksMu.RUnlock()
		return failure
	}
	prev := s.Engine
	s.Engine = eng
	s.mu.Unlock()
	s.tasksMu.RUnlock()
	s.finishActiveEngineSwap(prev, eng)
	return ""
}

type engineRetirePolicy struct {
	requireInactive   bool
	requireIdle       bool
	requireUnrecorded bool
}

// retireEngine atomically validates and detaches one exact engine occurrence.
// Resource teardown happens later, outside all server locks. All lifecycle
// mutations follow tasksMu -> s.mu -> emu, matching tryStart/focus/publish.
func (s *Server) retireEngine(eng *Engine, policy engineRetirePolicy) bool {
	if eng == nil {
		return false
	}
	s.tasksMu.Lock()
	s.mu.Lock()
	if eng.retired.Load() {
		s.mu.Unlock()
		s.tasksMu.Unlock()
		return false
	}
	if eng.taskID != "" && s.tasks[eng.taskID] != eng {
		s.mu.Unlock()
		s.tasksMu.Unlock()
		return false
	}
	if policy.requireInactive && s.Engine == eng {
		s.mu.Unlock()
		s.tasksMu.Unlock()
		return false
	}
	if policy.requireIdle && eng.running.Load() {
		s.mu.Unlock()
		s.tasksMu.Unlock()
		return false
	}
	if policy.requireUnrecorded {
		eng.emu.Lock()
		recorded := eng.recorder != nil && eng.recorder.HasRecording()
		eng.emu.Unlock()
		if recorded {
			s.mu.Unlock()
			s.tasksMu.Unlock()
			return false
		}
	}
	eng.retired.Store(true)
	if eng.taskID != "" {
		delete(s.tasks, eng.taskID)
	}
	s.mu.Unlock()
	s.tasksMu.Unlock()
	return true
}

// discardEngineCandidate removes exactly the unpublished candidate pointer
// produced by a failed guarded request. It never deletes a replacement with
// the same id or an engine another request already focused. A freshly-created
// managed scratch directory is removed only with os.Remove, which fails closed
// if anything wrote into it while the request was in flight.
func (s *Server) discardEngineCandidate(eng *Engine, removeScratch bool) {
	if eng == nil || eng.taskID == "" {
		return
	}
	if !s.retireEngine(eng, engineRetirePolicy{requireInactive: true, requireIdle: true}) {
		return
	}
	eng.teardown()
	if removeScratch {
		_ = os.Remove(eng.pwd)
	}
}

func (s *Server) finishActiveEngineSwap(prev, eng *Engine) {
	if prev != nil && prev != eng {
		if s.retireEngine(prev, engineRetirePolicy{
			requireInactive: true, requireIdle: true, requireUnrecorded: true,
		}) {
			prev.teardown()
			if prev.workspaceKind == session.WorkspaceScratch {
				_ = os.Remove(prev.pwd)
			}
		}
	}
	// Remember the foregrounded session per project (keyed by the engine's own
	// pwd, so remote workspaces never clobber the local entry) — health reports
	// it after a restart so clients return to their last conversation. Runs
	// outside s.mu: this is best-effort file I/O.
	session.SaveLastSession(engineProject(eng), eng.taskID)
}

// deleteEngine removes a task engine from the map and tears it down (stops its
// pump, cancels its run, closes its recorder). The active engine is never
// deleted out from under the foreground; callers guard against that.
func (s *Server) deleteEngine(taskID string) {
	s.tasksMu.RLock()
	eng := s.tasks[taskID]
	s.tasksMu.RUnlock()
	if eng != nil && s.retireEngine(eng, engineRetirePolicy{requireInactive: true}) {
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
	e.teardownOnce.Do(func() {
		e.retired.Store(true)
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
		e.toolOverrideMu.Lock()
		e.emu.Lock()
		recorder := e.recorder
		e.emu.Unlock()
		if recorder != nil {
			recorder.Close()
		}
		e.toolOverrideMu.Unlock()
		// Release the remote target, if any: closes the SSH connection or
		// decrements the Docker container ref-count (stopping it on last release).
		// No-op for local engines.
		if e.env != nil {
			_ = e.env.CloseRemote()
			// Close this task's browser session (managed tabs close; extension tabs
			// are detached back to the user). No-op if the task never used browser.
			e.env.CloseBrowser()
			// Same for computer use. This matters more than the browser case: the
			// session holds the app allowlist, so leaving it open would carry a grant
			// the user gave one task into the next one. No-op if unused.
			e.env.CloseComputer()
		}
	})
}

// setTaskStatus broadcasts a global task_status event (so every client's sidebar
// can mark the task running/idle live) and best-effort persists Status +
// UpdatedAt so recency survives a reload. The broadcast carries the task id in
// its DATA (not the envelope TaskID) so it is delivered to all clients, not
// filtered to the task's subscribers. It also carries the task's project path
// and the exact server-side timestamp being persisted, so clients can bump the
// project-level "last activity" clock with the server's value (not the
// browser's clock) without re-deriving either from their local task list.
func (s *Server) setTaskStatus(eng *Engine, running bool) {
	if eng == nil || eng.taskID == "" {
		return
	}
	status := "idle"
	if running {
		status = "running"
	}
	now := time.Now().Format(time.RFC3339)
	s.wsBroker.Broadcast(WSEvent{Type: "task_status", Data: map[string]any{
		"task_id":    eng.taskID,
		"running":    running,
		"status":     status,
		"project":    engineProject(eng),
		"updated_at": now,
	}})
	go func(id, st, ts string) {
		_, _ = session.UpdateSessionMeta(id, func(m *session.SessionMeta) {
			m.Status = st
			m.UpdatedAt = ts
		})
	}(eng.taskID, status, now)
}

// CloseAllEngines tears down every live engine. Called on server shutdown.
func (s *Server) CloseAllEngines() {
	if s.remoteConns != nil {
		s.remoteConns.closeAll()
	}
	s.tasksMu.Lock()
	s.mu.Lock()
	engines := make([]*Engine, 0, len(s.tasks))
	seen := make(map[*Engine]struct{}, len(s.tasks)+1)
	for _, e := range s.tasks {
		if e == nil {
			continue
		}
		e.retired.Store(true)
		engines = append(engines, e)
		seen[e] = struct{}{}
	}
	if active := s.Engine; active != nil {
		if _, ok := seen[active]; !ok {
			active.retired.Store(true)
			engines = append(engines, active)
		}
	}
	s.tasks = make(map[string]*Engine)
	s.Engine = nil
	s.mu.Unlock()
	s.tasksMu.Unlock()
	for _, e := range engines {
		e.teardown()
	}
}
