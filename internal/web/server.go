// Package web implements the jcode web server and API.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/eino/adk"
	"github.com/cnjack/jcode/internal/automation"
	"github.com/cnjack/jcode/internal/browser"
	"github.com/cnjack/jcode/internal/channel"
	"github.com/cnjack/jcode/internal/computer"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/flow"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/skills"
	"github.com/cnjack/jcode/internal/telemetry"
	"github.com/cnjack/jcode/internal/tools"
	"github.com/cnjack/jcode/internal/usage"
	utils "github.com/cnjack/jcode/internal/util"
)

// Server is the jcode web server.
type Server struct {
	// Engine is the bootstrap/active task's run state. Its fields (agent, history,
	// recorder, pwd, env, tokenUsage, approvalState, handler, …) are PROMOTED onto
	// Server, so existing s.<field> accesses resolve to s.Engine.<field> while
	// there is a single active task. Per-task routing (Server.tasks) supersedes
	// this promotion in a later increment. Always non-nil after NewServer.
	*Engine

	// tasks holds every live task engine keyed by task id (session UUID). Wired in
	// the routing increment; the bootstrap Engine above is the current de-facto
	// entry until then.
	tasks   map[string]*Engine
	tasksMu sync.RWMutex

	port        int
	host        string
	openBrowser bool
	wsBroker    *WSBroker

	// authToken, when requireAuth is set, must be presented as a bearer token on
	// every non-exempt request (see authMiddleware in auth.go). requireAuth is
	// enabled when the server binds to a non-loopback host.
	authToken   string
	requireAuth bool

	// mu guards the shared-server maps and, during the single-active transition,
	// the bootstrap Engine's run state (the role that moves to a per-Engine lock
	// once tasks truly run in parallel).
	mu sync.RWMutex

	// ctxPtr holds the server-level context (set in Start), used for background
	// agent work. Stored atomically because the automation scheduler/manual-run
	// goroutines (launched by command.runWebServer before Start) may read it
	// concurrently with Start's write. Read via rootCtx.
	ctxPtr atomic.Pointer[context.Context]

	// Dependencies set during initialization.
	tracer   *telemetry.LangfuseTracer
	cfg      *config.Config
	cfgMu    sync.Mutex // serializes read-modify-write SaveConfig from concurrent handlers
	registry *model.ModelRegistry

	// newEngine builds a fresh, fully-isolated task engine (its own env, agent,
	// recorder, handler, approval state) at the given pwd/mode. This is how a new
	// concurrent task — or a "switch project" — gets its run state without
	// mutating any other task's. taskID is non-empty when resuming an existing
	// session. nil in setup mode.
	newEngine func(taskID, pwd, mode string) (*EngineConfig, error)

	// newRemoteEngine is newEngine's remote sibling: it builds a task engine bound
	// to a remote executor (SSH or Docker) instead of a local pwd.
	newRemoteEngine func(taskID string, executor tools.RemoteExecutor, remotePwd, mode string) (*EngineConfig, error)

	// newAutomationEngine builds a headless task engine for automation runs: like
	// newEngine but drops interactive tools (ask_user) so an unattended run can't
	// stall waiting on a human. Falls back to newEngine when unset (back-compat).
	newAutomationEngine func(taskID, pwd, mode string) (*EngineConfig, error)

	// remoteConns holds SSH connections established by the remote-connect wizard
	// that have not yet been bound to the live env (keyed by connection id).
	remoteConns *remoteConnRegistry

	// PTY manager for terminal sessions.
	ptyMgr *ptyManager

	// skillLoader provides skill listing for slash commands.
	skillLoader *skills.Loader

	// flowLoader provides workflow listing for slash commands (e.g. /repo-audit).
	flowLoader *flow.Loader

	// reloadMCP re-establishes MCP connections from the given server map and
	// swaps in the fresh tool set (the agent is rebuilt by the caller). nil
	// when MCP hot-reload is unavailable. Returns per-server statuses.
	reloadMCP func(servers map[string]*config.MCPServer) ([]tools.MCPStatus, error)

	// mcpStatuses is the most recent per-server connection status, used by the
	// management UI. Guarded by mu.
	mcpStatuses map[string]tools.MCPStatus

	// mcpLogins tracks in-progress/finished OAuth logins per server name. Guarded by mu.
	mcpLogins map[string]*mcpLoginState

	// wechatClient is the optional WeChat channel client.
	wechatClient channel.Channel

	// needsSetup is true when no providers are configured. The server starts in
	// setup mode and exposes setup API endpoints while blocking chat operations.
	needsSetup bool
	version    string

	// usageStore backs the global usage-statistics endpoint. nil falls back to
	// usage.Default(); tests inject a temp-dir store.
	usageStore *usage.Store

	// automations is the automation definition/run store (nil in setup mode).
	// Run execution reuses the Engine via automationRunner; the periodic
	// scheduler is owned by command.runWebServer.
	automations *automation.Store

	// autoRunMu guards autoRunInflight, the set of automation ids with a manual
	// run currently in flight on this server. It is the manual-run analogue of the
	// scheduler's own inflight guard: without it a double-click (or two clients)
	// would launch parallel agent sessions mutating the same project directory.
	autoRunMu       sync.Mutex
	autoRunInflight map[string]bool

	// browserMgr is the process-wide browser-use manager (extension bridge +
	// managed Chrome). Shared with per-task Envs so the settings UI and the
	// agent's browser_* tools drive the same Chrome. nil disables browser use.
	browserMgr *browser.Manager

	// computerMgr is the process-wide computer-use manager, shared with per-task
	// Envs so the settings UI and the agent's computer_* tools see one backend and
	// one view of what is granted. nil disables computer use.
	computerMgr *computer.Manager

	// bleController toggles the BLE status channel live (from the settings
	// endpoint) without an app restart. nil when BLE is not compiled in.
	bleController BLEController
}

// BLEController lets the settings endpoint start/stop the BLE status channel at
// runtime. Implemented by *ble.Proxy; kept as an interface so this package does
// not depend on the ble concrete type.
type BLEController interface {
	Enable()
	Disable()
}

// ServerConfig holds the configuration for creating a new Server.
type ServerConfig struct {
	Port                int
	Host                string
	OpenBrowser         bool
	Pwd                 string
	Version             string
	Agent               *adk.ChatModelAgent
	CreateAgent         func(providerName, modelName string) (*adk.ChatModelAgent, error)
	RebuildForMode      func(planMode bool) (*adk.ChatModelAgent, error)
	NewEngine           func(taskID, pwd, mode string) (*EngineConfig, error)                                             // factory for new concurrent task engines (local)
	NewRemoteEngine     func(taskID string, executor tools.RemoteExecutor, remotePwd, mode string) (*EngineConfig, error) // remote sibling of NewEngine (SSH or Docker)
	NewAutomationEngine func(taskID, pwd, mode string) (*EngineConfig, error)                                             // headless sibling of NewEngine for automation runs (drops interactive tools)
	InitialMode         string                                                                                            // unified startup mode string ("approval"/"plan"/"full_access")
	TodoStore           *tools.TodoStore
	Recorder            *session.Recorder
	Tracer              *telemetry.LangfuseTracer
	Env                 *tools.Env
	ProviderName        string
	ModelName           string
	Config              *config.Config
	Registry            *model.ModelRegistry
	ApprovalState       *runner.ApprovalState
	SkillLoader         *skills.Loader
	FlowLoader          *flow.Loader
	ReloadMCP           func(servers map[string]*config.MCPServer) ([]tools.MCPStatus, error) // optional: hot-reload MCP tools
	InitialMCPStatuses  []tools.MCPStatus                                                     // statuses from the startup MCP load
	WechatClient        channel.Channel                                                       // optional WeChat channel
	WebHandler          *handler.WebHandler                                                   // optional: pre-created handler for sharing with tools
	EventHandler        handler.AgentEventHandler                                             // optional: handler for runner (e.g. NotifyingHandler)
	NeedsSetup          bool                                                                  // true when no providers are configured (setup mode)
	TokenUsage          *model.TokenUsage                                                     // optional: shared token tracker (created when nil)
	ContextBreakdownFn  func() usage.ContextBreakdown                                         // optional: live per-task context breakdown
	Automations         *automation.Store                                                     // optional: automation store (nil in setup mode)
	AuthToken           string                                                                // bearer token required on non-exempt requests when RequireAuth is set
	RequireAuth         bool                                                                  // enforce token auth (set when bound to a non-loopback host)
	BrowserManager      *browser.Manager                                                      // optional: process-wide browser-use manager shared with per-task Envs
	ComputerManager     *computer.Manager                                                     // optional: process-wide computer-use manager shared with per-task Envs
	BLEController       BLEController                                                         // optional: live BLE status-channel toggle (desktop builds)
}

// NewServer creates a new web server.
func NewServer(cfg *ServerConfig) *Server {
	h := cfg.WebHandler
	if h == nil {
		h = handler.NewWebHandler()
	}
	var eh handler.AgentEventHandler = h
	if cfg.EventHandler != nil {
		eh = cfg.EventHandler
	}
	// The bootstrap Engine carries the per-task run state of the initial session.
	boot := &Engine{
		pwd:            cfg.Pwd,
		handler:        h,
		agent:          cfg.Agent,
		todoStore:      cfg.TodoStore,
		recorder:       cfg.Recorder,
		env:            cfg.Env,
		providerName:   cfg.ProviderName,
		modelName:      cfg.ModelName,
		mode:           mode.Parse(cfg.InitialMode).String(),
		approvalState:  cfg.ApprovalState,
		eventHandler:   eh,
		tokenUsage:     cfg.TokenUsage,
		breakdownFn:    cfg.ContextBreakdownFn,
		createAgent:    cfg.CreateAgent,
		rebuildForMode: cfg.RebuildForMode,
	}
	if boot.tokenUsage == nil {
		boot.tokenUsage = &model.TokenUsage{}
	}
	// The engine's identity is its recorder's session UUID; this is the task_id
	// stamped on its events and the key in the tasks map.
	if boot.taskID == "" && boot.recorder != nil {
		boot.taskID = boot.recorder.UUID()
	}
	s := &Server{
		Engine:              boot,
		tasks:               make(map[string]*Engine),
		port:                cfg.Port,
		host:                cfg.Host,
		openBrowser:         cfg.OpenBrowser,
		version:             cfg.Version,
		wsBroker:            NewWSBroker(),
		newEngine:           cfg.NewEngine,
		newRemoteEngine:     cfg.NewRemoteEngine,
		newAutomationEngine: cfg.NewAutomationEngine,
		remoteConns:         newRemoteConnRegistry(),
		tracer:              cfg.Tracer,
		cfg:                 cfg.Config,
		registry:            cfg.Registry,
		ptyMgr:              newPTYManager(),
		skillLoader:         cfg.SkillLoader,
		flowLoader:          cfg.FlowLoader,
		reloadMCP:           cfg.ReloadMCP,
		mcpStatuses:         make(map[string]tools.MCPStatus),
		mcpLogins:           make(map[string]*mcpLoginState),
		wechatClient:        cfg.WechatClient,
		needsSetup:          cfg.NeedsSetup,
		automations:         cfg.Automations,
		autoRunInflight:     make(map[string]bool),
		authToken:           cfg.AuthToken,
		requireAuth:         cfg.RequireAuth,
		browserMgr:          cfg.BrowserManager,
		computerMgr:         cfg.ComputerManager,
		bleController:       cfg.BLEController,
	}
	// The bootstrap engine is registered (and its pump started) in Start, once
	// the root context exists.
	for _, st := range cfg.InitialMCPStatuses {
		s.mcpStatuses[st.Name] = st
	}

	// TodoStore/GoalStore → recorder/handler wiring is done PER TASK in the engine
	// factory (command.buildWebTask), so each engine binds its OWN recorder and
	// handler. (The bootstrap engine is built by that same factory.)

	return s
}

// Handler returns the underlying WebHandler for external wiring (e.g. approval routing).
func (s *Server) Handler() *handler.WebHandler {
	return s.activeHandler()
}

// rootCtx returns the server-level context set by Start, or nil before Start
// has run. Background goroutines (the automation scheduler/manual runs) must
// tolerate a nil result, which means the server has not started serving yet.
func (s *Server) rootCtx() context.Context {
	if p := s.ctxPtr.Load(); p != nil {
		return *p
	}
	return nil
}

// Start starts the web server. Blocks until context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	s.ctxPtr.Store(&ctx)
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/auth/verify", s.handleAuthVerify)
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("POST /api/stop", s.handleStop)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("POST /api/sessions", s.handleNewSession)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("GET /api/todos", s.handleGetTodos)
	mux.HandleFunc("GET /api/goal", s.handleGetGoal)
	mux.HandleFunc("POST /api/goal", s.handleSetGoal)
	mux.HandleFunc("DELETE /api/goal", s.handleClearGoal)
	mux.HandleFunc("POST /api/approval", s.handleApproval)
	mux.HandleFunc("GET /api/approval/pending", s.handlePendingApproval)
	mux.HandleFunc("POST /api/ask", s.handleAskUser)
	mux.HandleFunc("GET /api/ask/pending", s.handlePendingAskUser)
	mux.HandleFunc("GET /api/files", s.handleListFiles)
	mux.HandleFunc("GET /api/files/content", s.handleReadFile)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/workspace", s.handleWorkspace)
	mux.HandleFunc("GET /api/git/branches", s.handleGitBranches)
	mux.HandleFunc("POST /api/git/checkout", s.handleGitCheckout)
	mux.HandleFunc("GET /api/tasks", s.handleListAllTasks)
	mux.HandleFunc("PATCH /api/tasks/{id}", s.handleUpdateTask)
	mux.HandleFunc("GET /api/usage/stats", s.handleUsageStats)
	mux.HandleFunc("GET /api/tasks/{id}/stats", s.handleTaskStats)
	mux.HandleFunc("GET /api/models", s.handleListModels)
	mux.HandleFunc("POST /api/model", s.handleSwitchModel)
	mux.HandleFunc("POST /api/small-model", s.handleSetSmallModel)
	mux.HandleFunc("POST /api/mode", s.handleSwitchMode)
	mux.HandleFunc("POST /api/exec", s.handleExec)
	mux.HandleFunc("GET /api/diff", s.handleDiff)
	mux.HandleFunc("GET /api/mcp", s.handleListMCP)
	mux.HandleFunc("POST /api/mcp/servers", s.handleCreateMCP)
	mux.HandleFunc("PUT /api/mcp/servers/{name}", s.handleUpdateMCP)
	mux.HandleFunc("DELETE /api/mcp/servers/{name}", s.handleDeleteMCP)
	mux.HandleFunc("POST /api/mcp/{name}/toggle", s.handleToggleMCP)
	mux.HandleFunc("POST /api/mcp/{name}/login", s.handleMCPLogin)
	mux.HandleFunc("GET /api/mcp/{name}/login/status", s.handleMCPLoginStatus)
	mux.HandleFunc("GET /api/ssh", s.handleListSSH)
	mux.HandleFunc("POST /api/remote/connect", s.handleRemoteConnect)
	mux.HandleFunc("POST /api/remote/list-dir", s.handleRemoteListDir)
	mux.HandleFunc("POST /api/remote/bind", s.handleRemoteBind)
	mux.HandleFunc("POST /api/remote/cancel", s.handleRemoteCancel)
	mux.HandleFunc("POST /api/remote/save-alias", s.handleRemoteSaveAlias)
	mux.HandleFunc("GET /api/docker/containers", s.handleListContainers)
	mux.HandleFunc("POST /api/remote/save-docker-alias", s.handleRemoteSaveDockerAlias)
	mux.HandleFunc("GET /api/automations", s.handleListAutomations)
	mux.HandleFunc("POST /api/automations", s.handleCreateAutomation)
	mux.HandleFunc("GET /api/automations/runs", s.handleListAutomationRuns)
	mux.HandleFunc("GET /api/automations/{id}", s.handleGetAutomation)
	mux.HandleFunc("PUT /api/automations/{id}", s.handleUpdateAutomation)
	mux.HandleFunc("DELETE /api/automations/{id}", s.handleDeleteAutomation)
	mux.HandleFunc("POST /api/automations/{id}/run", s.handleRunAutomation)
	mux.HandleFunc("GET /api/automation-templates", s.handleAutomationTemplates)
	mux.HandleFunc("GET /api/browser/status", s.handleBrowserStatus)
	mux.HandleFunc("POST /api/browser/config", s.handleBrowserConfig)
	mux.HandleFunc("GET /api/browser/ext/ws", s.handleBrowserExtWS)
	mux.HandleFunc("GET /api/browser/shots/{id}", s.handleBrowserShot)
	mux.HandleFunc("GET /api/computer/status", s.handleComputerStatus)
	mux.HandleFunc("POST /api/computer/config", s.handleComputerConfig)
	mux.HandleFunc("POST /api/computer/permissions", s.handleComputerPermissionRequest)
	mux.HandleFunc("GET /api/computer/shots/{id}", s.handleComputerShot)
	mux.HandleFunc("GET /api/approval-review-config", s.handleGetApprovalReviewConfig)
	mux.HandleFunc("POST /api/approval-review-config", s.handleSetApprovalReviewConfig)
	mux.HandleFunc("GET /api/skills", s.handleListSkills)
	mux.HandleFunc("POST /api/skills/{name}/toggle", s.handleToggleSkill)
	mux.HandleFunc("GET /api/slash-commands", s.handleSlashCommands)
	mux.HandleFunc("GET /api/browse", s.handleBrowse)
	mux.HandleFunc("POST /api/project/switch", s.handleSwitchProject)
	mux.HandleFunc("POST /api/project/validate", s.handleValidatePaths)
	mux.HandleFunc("POST /api/pty", s.handleCreatePTY)
	mux.HandleFunc("GET /api/pty", s.handleListPTY)
	mux.HandleFunc("DELETE /api/pty/{id}", s.handleKillPTY)
	mux.HandleFunc("GET /api/pty/{id}/ws", s.handlePTYWebSocket)
	mux.HandleFunc("GET /api/approval/mode", s.handleGetApprovalMode)
	mux.HandleFunc("POST /api/approval/mode", s.handleSetApprovalMode)
	mux.HandleFunc("GET /api/channel", s.handleChannelStatus)
	mux.HandleFunc("POST /api/channel/login", s.handleChannelLogin)
	mux.HandleFunc("POST /api/channel/logout", s.handleChannelLogout)
	mux.HandleFunc("POST /api/channel/enable", s.handleChannelEnable)
	mux.HandleFunc("POST /api/channel/disable", s.handleChannelDisable)
	mux.HandleFunc("GET /api/channel/ble", s.handleChannelBLEStatus)
	mux.HandleFunc("POST /api/channel/ble", s.handleSetChannelBLE)

	// Setup API — available in setup mode (no provider configured yet).
	mux.HandleFunc("GET /api/setup/providers", s.handleSetupProviders)
	mux.HandleFunc("GET /api/setup/providers/{id}/models", s.handleSetupProviderModels)
	mux.HandleFunc("POST /api/setup/complete", s.handleSetupComplete)
	mux.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/setup/validate", s.handleSetupValidate)

	// Provider management API — add/remove providers after initial setup.
	mux.HandleFunc("GET /api/providers", s.handleListProviders)
	mux.HandleFunc("POST /api/providers", s.handleAddProvider)
	mux.HandleFunc("PUT /api/providers/{id}", s.handleUpdateProvider)
	mux.HandleFunc("DELETE /api/providers/{id}", s.handleDeleteProvider)
	// Browse a provider's model catalog. For registry providers this returns the
	// built-in model list; for custom endpoints it queries the live /models
	// endpoint. Each entry is flagged added=true when already configured.
	mux.HandleFunc("GET /api/providers/{id}/models", s.handleProviderCatalog)

	// History management.
	mux.HandleFunc("POST /api/history/truncate", s.handleTruncateHistory)

	// Model state API — favorites & recent.
	mux.HandleFunc("GET /api/model-state", s.handleGetModelState)
	mux.HandleFunc("POST /api/model-state/favorite", s.handleToggleFavorite)
	mux.HandleFunc("POST /api/model-state/enabled", s.handleToggleModelEnabled)
	mux.HandleFunc("POST /api/model-state/effort", s.handleSetModelEffort)

	// Serve embedded frontend (SPA with fallback to index.html)
	mux.Handle("GET /", newSPAHandler())

	// Auth (token) then CORS. corsMiddleware MUST stay the outer wrapper so
	// OPTIONS preflights are answered there and never reach authMiddleware
	// without an Authorization header. authMiddleware is a no-op unless
	// requireAuth is set (i.e. bound to a non-loopback host).
	corsHandler := corsMiddleware(s.authMiddleware(mux))

	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	srv := &http.Server{
		Addr:    addr,
		Handler: corsHandler,
	}

	// Register the bootstrap engine (adds it to the tasks map + starts its event
	// pump). New task engines register themselves on creation.
	if s.Engine != nil {
		_ = s.registerEngine(s.Engine)
	}

	// Graceful shutdown on context cancellation.
	go func() {
		<-ctx.Done()
		s.ptyMgr.closeAll()
		s.wsBroker.Close()
		_ = srv.Shutdown(context.Background())
	}()

	config.Logger().Printf("[web] server starting on http://%s", addr)
	fmt.Printf("🌐 jcode web server running at http://%s\n", addr)
	fmt.Printf("   Press Ctrl+C to stop\n")

	// Use net.Listen + srv.Serve so we can open the browser right after
	// the port is bound (ListenAndServe would delay until first request).
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	// Open browser after the port is bound.
	if s.openBrowser {
		url := fmt.Sprintf("http://%s", addr)
		go openBrowser(url)
	}

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// --- API Handlers ---

// currentModelSupportsImage checks if the given engine's selected model supports
// image input.
func (s *Server) currentModelSupportsImage(eng *Engine) bool {
	if s.registry == nil || eng == nil {
		return false
	}
	provider, mdl, _ := eng.modelSnapshot()
	_, m, ok := s.registry.LookupModel(provider, mdl)
	return ok && m.SupportsImageInput()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()

	if s.needsSetup || eng == nil {
		pwd := ""
		if eng != nil {
			pwd = eng.pwd
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":        "needs_setup",
			"version":       s.version,
			"pwd":           pwd,
			"provider":      "",
			"model":         "",
			"mode":          "build",
			"session_id":    "",
			"running":       false,
			"needs_setup":   true,
			"auth_required": s.requireAuth,
		})
		return
	}

	provider, mdl, modeStr := eng.modelSnapshot()
	sessionID := eng.recUUID()
	// After a restart the bootstrap engine is a fresh throwaway (no recording,
	// not running) whose UUID has no history to restore. Report the project's
	// last foregrounded session instead so clients boot straight back into the
	// conversation that was open when the app was closed. Once the live engine
	// has real state it always reports its own UUID.
	eng.emu.Lock()
	throwaway := (eng.recorder == nil || !eng.recorder.HasRecording()) && !eng.running.Load()
	eng.emu.Unlock()
	if throwaway {
		if last := session.LoadLastSession(eng.pwd); last != "" {
			sessionID = last
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"version":       s.version,
		"pwd":           eng.pwd,
		"provider":      provider,
		"model":         mdl,
		"mode":          modeStr,
		"session_id":    sessionID,
		"running":       eng.running.Load(),
		"image_support": s.currentModelSupportsImage(eng),
		"auth_required": s.requireAuth,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()
	if eng == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no active task"})
		return
	}
	full := eng.tokenUsage.GetFull()
	provider, mdl, modeStr := eng.modelSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"running":    eng.running.Load(),
		"ws_clients": s.wsBroker.ClientCount(),
		"pwd":        eng.pwd,
		"provider":   provider,
		"model":      mdl,
		"mode":       modeStr,
		// Live token snapshot so a client reconnecting between turns can render
		// the context bar + cache hit rate without waiting for the next
		// token_update WS event. total_tokens = current context occupancy.
		"token": map[string]any{
			"total_tokens":        eng.tokenUsage.GetLastTotal(),
			"prompt_tokens":       full.PromptTokens,
			"completion_tokens":   full.CompletionTokens,
			"cached_tokens":       full.CachedTokens,
			"reasoning_tokens":    full.ReasoningTokens,
			"cache_write_tokens":  full.CacheWriteTokens,
			"call_count":          full.CallCount,
			"cache_hit_rate":      eng.tokenUsage.CacheHitRate(),
			"cache_supported":     eng.tokenUsage.CacheObserved(),
			"model_context_limit": s.currentModelContextLimit(eng),
		},
	})
}

// currentModelContextLimit resolves the context window of the given engine's
// selected model, or 0 if unknown.
func (s *Server) currentModelContextLimit(eng *Engine) int {
	if s.registry == nil || s.cfg == nil || eng == nil {
		return 0
	}
	provider, mdl, _ := eng.modelSnapshot()
	return model.ResolveContextLimit(s.registry, s.cfg, provider, mdl)
}

// handleWorkspace returns lightweight git workspace info (branch + dirty) for
// the current project so the web UI can show the real branch name. Diff stats
// are fetched separately via /api/diff. Empty branch = not a git repo.
func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	// Use the request context so the git commands are cancelled if the client
	// disconnects (CodeRabbit review feedback on PR #82).
	// `branch --show-current` (not `rev-parse --abbrev-ref HEAD`) so a freshly
	// initialised repo with no commits still reports its unborn branch (e.g.
	// "main") instead of the literal "HEAD".
	branchCmd := exec.CommandContext(r.Context(), "git", "branch", "--show-current")
	branchCmd.Dir = s.activePwd()
	branchCmd.Env = utils.ScrubbedGitEnv()
	branchOut, _ := branchCmd.Output()
	branch := strings.TrimSpace(string(branchOut))

	statusCmd := exec.CommandContext(r.Context(), "git", "status", "--porcelain")
	statusCmd.Dir = s.activePwd()
	statusCmd.Env = utils.ScrubbedGitEnv()
	statusOut, _ := statusCmd.Output()
	dirty := strings.TrimSpace(string(statusOut)) != ""

	writeJSON(w, http.StatusOK, map[string]any{
		"branch": branch,
		"dirty":  dirty,
	})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// isAllowedWebOrigin decides whether a browser request's Origin is trusted.
//
// The server (especially the always-on desktop sidecar) exposes agent control —
// i.e. shell/file tools — over loopback with no auth token, so an unconditional
// `Access-Control-Allow-Origin: *` plus a WebSocket CheckOrigin that returns
// true would let any website the user visits drive the agent or read its live
// event stream via ws://127.0.0.1:<port>. We gate on Origin instead:
//
//   - empty Origin (curl, native client, same-origin navigations) → allow
//   - Origin equal to the request's own Host (same-origin) → allow; this covers
//     local-browser, the desktop webview, and LAN access via `--host 0.0.0.0`
//   - Origin whose host is loopback → allow; this covers the Vite dev proxy
//     (localhost:5173 → 127.0.0.1:<port>)
//   - Origin host "tauri.localhost" → allow; this is the Windows/Linux Tauri
//     shell origin when the desktop app serves the page itself and the frontend
//     reaches the API over an absolute loopback URL (macOS uses tauri://localhost,
//     already covered by the loopback "localhost" case above).
//
// A page on https://evil.com cannot forge its Origin, so it falls through to
// false and is blocked. This is intentionally not a full auth solution (a local
// process can still reach the port); it closes the cross-origin *website* vector.
func isAllowedWebOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Host == r.Host {
		return true
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1", "tauri.localhost":
		return true
	}
	return false
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// CORS response headers alone are not an authorization boundary: a hostile
		// page can send a "simple" no-cors POST whose response is unreadable but
		// whose side effect still happens. Reject an untrusted browser Origin before
		// any API handler can mutate config, start an agent, or control the Mac.
		if origin != "" && !isAllowedWebOrigin(r) {
			http.Error(w, "cross-origin request denied", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// openBrowser opens the given URL in the user's default browser.
func openBrowser(url string) {
	if err := utils.OpenURL(url); err != nil {
		config.Logger().Printf("[web] failed to open browser: %v", err)
	}
}
