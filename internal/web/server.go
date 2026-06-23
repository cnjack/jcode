// Package web implements the jcode web server and API.
package web

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/gorilla/websocket"

	"github.com/cnjack/jcode/internal/channel"
	"github.com/cnjack/jcode/internal/config"
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

	// mu guards the shared-server maps and, during the single-active transition,
	// the bootstrap Engine's run state (the role that moves to a per-Engine lock
	// once tasks truly run in parallel).
	mu sync.RWMutex

	// Server-level context (from Start), used for background agent work.
	ctx context.Context

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

	// remoteConns holds SSH connections established by the remote-connect wizard
	// that have not yet been bound to the live env (keyed by connection id).
	remoteConns *remoteConnRegistry

	// PTY manager for terminal sessions.
	ptyMgr *ptyManager

	// skillLoader provides skill listing for slash commands.
	skillLoader *skills.Loader

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
}

// ServerConfig holds the configuration for creating a new Server.
type ServerConfig struct {
	Port               int
	Host               string
	OpenBrowser        bool
	Pwd                string
	Version            string
	Agent              *adk.ChatModelAgent
	CreateAgent        func(providerName, modelName string) (*adk.ChatModelAgent, error)
	RebuildForMode     func(planMode bool) (*adk.ChatModelAgent, error)
	NewEngine          func(taskID, pwd, mode string) (*EngineConfig, error)                                             // factory for new concurrent task engines (local)
	NewRemoteEngine    func(taskID string, executor tools.RemoteExecutor, remotePwd, mode string) (*EngineConfig, error) // remote sibling of NewEngine (SSH or Docker)
	InitialMode        string                                                                                            // unified startup mode string ("approval"/"plan"/"full_access")
	TodoStore          *tools.TodoStore
	Recorder           *session.Recorder
	Tracer             *telemetry.LangfuseTracer
	Env                *tools.Env
	ProviderName       string
	ModelName          string
	Config             *config.Config
	Registry           *model.ModelRegistry
	ApprovalState      *runner.ApprovalState
	SkillLoader        *skills.Loader
	ReloadMCP          func(servers map[string]*config.MCPServer) ([]tools.MCPStatus, error) // optional: hot-reload MCP tools
	InitialMCPStatuses []tools.MCPStatus                                                     // statuses from the startup MCP load
	WechatClient       channel.Channel                                                       // optional WeChat channel
	WebHandler         *handler.WebHandler                                                   // optional: pre-created handler for sharing with tools
	EventHandler       handler.AgentEventHandler                                             // optional: handler for runner (e.g. NotifyingHandler)
	NeedsSetup         bool                                                                  // true when no providers are configured (setup mode)
	TokenUsage         *model.TokenUsage                                                     // optional: shared token tracker (created when nil)
	ContextBreakdownFn func() usage.ContextBreakdown                                         // optional: live per-task context breakdown
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
		Engine:          boot,
		tasks:           make(map[string]*Engine),
		port:            cfg.Port,
		host:            cfg.Host,
		openBrowser:     cfg.OpenBrowser,
		version:         cfg.Version,
		wsBroker:        NewWSBroker(),
		newEngine:       cfg.NewEngine,
		newRemoteEngine: cfg.NewRemoteEngine,
		remoteConns:     newRemoteConnRegistry(),
		tracer:          cfg.Tracer,
		cfg:             cfg.Config,
		registry:        cfg.Registry,
		ptyMgr:          newPTYManager(),
		skillLoader:     cfg.SkillLoader,
		reloadMCP:       cfg.ReloadMCP,
		mcpStatuses:     make(map[string]tools.MCPStatus),
		mcpLogins:       make(map[string]*mcpLoginState),
		wechatClient:    cfg.WechatClient,
		needsSetup:      cfg.NeedsSetup,
	}
	// The bootstrap engine is registered (and its pump started) in Start, once
	// s.ctx exists.
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

// Start starts the web server. Blocks until context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	s.ctx = ctx
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("GET /api/health", s.handleHealth)
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
	mux.HandleFunc("DELETE /api/providers/{id}", s.handleDeleteProvider)

	// History management.
	mux.HandleFunc("POST /api/history/truncate", s.handleTruncateHistory)

	// Model state API — favorites & recent.
	mux.HandleFunc("GET /api/model-state", s.handleGetModelState)
	mux.HandleFunc("POST /api/model-state/favorite", s.handleToggleFavorite)
	mux.HandleFunc("POST /api/model-state/enabled", s.handleToggleModelEnabled)

	// Serve embedded frontend (SPA with fallback to index.html)
	mux.Handle("GET /", newSPAHandler())

	// CORS middleware
	corsHandler := corsMiddleware(mux)

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
	if !ok || m == nil || m.Modalities == nil {
		return false
	}
	for _, mod := range m.Modalities.Input {
		if mod == "image" {
			return true
		}
	}
	return false
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()

	if s.needsSetup || eng == nil {
		pwd := ""
		if eng != nil {
			pwd = eng.pwd
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":      "needs_setup",
			"version":     s.version,
			"pwd":         pwd,
			"provider":    "",
			"model":       "",
			"mode":        "build",
			"session_id":  "",
			"running":     false,
			"needs_setup": true,
		})
		return
	}

	provider, mdl, modeStr := eng.modelSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"version":       s.version,
		"pwd":           eng.pwd,
		"provider":      provider,
		"model":         mdl,
		"mode":          modeStr,
		"session_id":    eng.recUUID(),
		"running":       eng.running.Load(),
		"image_support": s.currentModelSupportsImage(eng),
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
	branchOut, _ := branchCmd.Output()
	branch := strings.TrimSpace(string(branchOut))

	statusCmd := exec.CommandContext(r.Context(), "git", "status", "--porcelain")
	statusCmd.Dir = s.activePwd()
	statusOut, _ := statusCmd.Output()
	dirty := strings.TrimSpace(string(statusOut)) != ""

	writeJSON(w, http.StatusOK, map[string]any{
		"branch": branch,
		"dirty":  dirty,
	})
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.needsSetup {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "setup required: please configure a provider first"})
		return
	}

	var req struct {
		Message   string      `json:"message"`
		Images    []chatImage `json:"images,omitempty"`     // optional: base64-encoded images
		Mode      string      `json:"mode,omitempty"`       // "build" or "plan"
		SessionID string      `json:"session_id,omitempty"` // optional: the task (session) to run
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 20<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	modeStr := req.Mode
	if modeStr == "" {
		modeStr = s.activeMode()
	}

	// Resolve (or lazily create) the engine for this task. Different tasks run
	// concurrently; the per-task running flag only blocks double-running the SAME
	// task.
	eng, err := s.engineForChat(req.SessionID, modeStr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !eng.running.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "this task is already processing a request",
		})
		return
	}

	sessionID := s.submitMessage(eng, req.Message, modeStr, "", req.SessionID, req.Images)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "processing", "session_id": sessionID})
}

// engineForChat resolves the engine a chat request targets. An empty task id (or
// one matching the active task) uses the active engine; a known live task uses
// its engine; an unknown id lazily spins up a fresh engine for it (a new task or
// the first message of a not-yet-live task), rooted at the active task's pwd.
func (s *Server) engineForChat(taskID, modeStr string) (*Engine, error) {
	if eng := s.resolveEngine(taskID); eng != nil {
		return eng, nil
	}
	pwd := ""
	if a := s.activeEngine(); a != nil {
		pwd = a.pwd
	}
	return s.buildLocalEngine(taskID, pwd, modeStr)
}

// chatImage represents a base64-encoded image in a chat request.
type chatImage struct {
	Data     string `json:"data"`       // base64 data (without data: prefix)
	MimeType string `json:"media_type"` // e.g. "image/png", "image/jpeg"
}

// SubmitMessage submits a message for agent processing from an external source
// (e.g. WeChat inbound message). Returns false if the agent is busy.
func (s *Server) SubmitMessage(message, source string) bool {
	eng := s.activeEngine()
	if eng == nil {
		return false
	}
	if !eng.running.CompareAndSwap(false, true) {
		return false
	}
	s.submitMessage(eng, message, eng.curMode(), source, "", nil)
	return true
}

// submitMessage is the shared implementation for starting an agent run.
// source is an optional label (e.g. "wechat") for the user_message event.
// sessionID is an optional session identifier from the client to ensure
// continuity — if the current recorder has a different UUID, resume the
// correct session instead of creating a new one.
// images is an optional list of base64-encoded images to include in the message.
// The caller must have already set eng.running to true (via CompareAndSwap).
// Returns the session_id of the recorder used.
func (s *Server) submitMessage(eng *Engine, message, mode, source, sessionID string, images []chatImage) string {
	// Slash command rewrite: if the original message starts with "/", check for
	// skill slash commands and rewrite to load_skill instruction (same pattern as
	// ACP/TUI). This must happen BEFORE the plan-mode prefix is applied, otherwise
	// HasPrefix("/"…) would fail against the prefixed string.
	agentMsg := message
	if strings.HasPrefix(message, "/") {
		cmd := strings.TrimPrefix(message, "/")
		parts := strings.SplitN(cmd, " ", 2)
		cmdName := parts[0]
		if s.skillLoader != nil {
			if sk := s.skillLoader.GetBySlash("/" + cmdName); sk != nil {
				userInput := ""
				if len(parts) > 1 {
					userInput = parts[1]
				}
				var sb strings.Builder
				fmt.Fprintf(&sb, "Use the load_skill tool with name=%q and follow its instructions.", sk.Name)
				if userInput != "" {
					sb.WriteString("\n\nAdditional context: ")
					sb.WriteString(userInput)
				}
				agentMsg = sb.String()
			}
		}
	}

	// Plan mode no longer needs an inline prompt prefix: the agent is rebuilt with
	// the read-only plan system prompt + tool set on mode switch (handleSwitchMode),
	// matching TUI/ACP. The mode arg is retained for the recorder/event context.
	_ = mode

	// Emit user_message event for external sources (e.g. WeChat) so web clients see it.
	// Web-originated messages are already added by the frontend's sendMessage().
	if source != "" {
		eng.handler.Emit("user_message", map[string]string{
			"content": message,
			"source":  source,
		})
	}

	// Ensure a recorder exists (lazy creation on first message).
	// If the client provided a session_id and the current recorder differs,
	// resume the client's session to prevent creating a duplicate.
	eng.emu.Lock()
	if eng.recorder == nil {
		rec, _ := session.NewRecorder(eng.pwd, eng.providerName, eng.modelName)
		if sessionID != "" {
			rec.SetUUID(sessionID)
		}
		eng.recorder = rec
	} else if sessionID != "" && eng.recorder.UUID() != sessionID {
		// Client is continuing a session that doesn't match the current recorder.
		// Resume the client's session to keep all messages together.
		eng.recorder.Close()
		rec, _ := session.NewRecorder(eng.pwd, eng.providerName, eng.modelName)
		rec.SetUUID(sessionID)
		eng.recorder = rec
	}
	recorder := eng.recorder
	eng.emu.Unlock()

	// Record user message.
	if recorder != nil {
		var entryImages []session.EntryImage
		for _, img := range images {
			entryImages = append(entryImages, session.EntryImage{
				MimeType: img.MimeType,
				Data:     img.Data,
			})
		}
		recorder.RecordUser(agentMsg, entryImages...)
	}

	// Build the user message — include images as multimodal content if provided.
	var userMsg *schema.Message
	if len(images) > 0 {
		parts := make([]schema.MessageInputPart, 0, len(images)+1)
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeText,
			Text: agentMsg,
		})
		for _, img := range images {
			data := img.Data
			parts = append(parts, schema.MessageInputPart{
				Type: schema.ChatMessagePartTypeImageURL,
				Image: &schema.MessageInputImage{
					MessagePartCommon: schema.MessagePartCommon{
						MIMEType:   img.MimeType,
						Base64Data: &data,
					},
				},
			})
		}
		userMsg = &schema.Message{
			Role:                  schema.User,
			Content:               agentMsg,
			UserInputMultiContent: parts,
		}
	} else {
		userMsg = schema.UserMessage(agentMsg)
	}

	eng.emu.Lock()
	eng.history = append(eng.history, userMsg)
	history := make([]adk.Message, len(eng.history))
	copy(history, eng.history)
	agent := eng.agent
	eng.emu.Unlock()

	// Stream response via WebSocket — run agent in background. Each task derives
	// its own cancellable context so /stop cancels only that task.
	runCtx, runCancel := context.WithCancel(s.ctx)
	eng.emu.Lock()
	eng.runGen++
	gen := eng.runGen
	eng.runCancel = runCancel
	eng.emu.Unlock()

	go func() {
		s.setTaskStatus(eng, true)
		defer func() {
			// Tear down only if this run is still the current one. If a newer turn
			// on the same engine has already taken over (runGen advanced) it now
			// owns running/runCancel — leave them so /stop still reaches the live
			// run and we don't broadcast a spurious idle for it. Releasing running
			// inside the same emu section that clears runCancel also closes the
			// gate↔cancel interleave window the run-start CAS relies on.
			eng.emu.Lock()
			superseded := eng.runGen != gen
			if !superseded {
				eng.runCancel = nil
				eng.running.Store(false)
			}
			eng.emu.Unlock()
			if !superseded {
				s.setTaskStatus(eng, false)
			}
		}()

		// Take a git snapshot before the agent run for session diff tracking.
		s.takeSessionSnapshot(eng)

		resp := runner.Run(runCtx, agent, history, eng.eventHandler, recorder, eng.todoStore, eng.env.GoalStore, s.tracer, eng.tokenUsage)
		if resp != "" {
			eng.emu.Lock()
			eng.history = append(eng.history, &schema.Message{Role: schema.Assistant, Content: resp})
			eng.emu.Unlock()
		}
	}()

	return recorder.UUID()
}

// handleListAllTasks returns every session across all projects (flat list,
// each tagged with its project path) so the web sidebar can render a
// Workspace > Project > Task tree without switching the active project.
func (s *Server) handleListAllTasks(w http.ResponseWriter, r *http.Request) {
	all, err := session.ListAllSessions()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Snapshot which task ids are currently running (live engines) so the sidebar
	// can show a running indicator even on a fresh page load.
	running := make(map[string]bool)
	s.tasksMu.RLock()
	for id, e := range s.tasks {
		if e != nil && e.running.Load() {
			running[id] = true
		}
	}
	s.tasksMu.RUnlock()

	type taskItem struct {
		UUID      string `json:"uuid"`
		Project   string `json:"project"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at,omitempty"`
		Provider  string `json:"provider"`
		Model     string `json:"model"`
		Title     string `json:"title,omitempty"`
		Pinned    bool   `json:"pinned"`
		Archived  bool   `json:"archived"`
		Unread    bool   `json:"unread"`
		Status    string `json:"status,omitempty"`
		Running   bool   `json:"running"`
	}
	items := make([]taskItem, 0)
	for project, metas := range all {
		for _, m := range metas {
			items = append(items, taskItem{
				UUID:      m.UUID,
				Project:   project,
				CreatedAt: m.StartTime,
				UpdatedAt: m.UpdatedAt,
				Provider:  m.Provider,
				Model:     m.Model,
				Title:     m.Title,
				Pinned:    m.Pinned,
				Archived:  m.Archived,
				Unread:    m.Unread,
				Status:    m.Status,
				Running:   running[m.UUID],
			})
		}
	}
	writeJSON(w, http.StatusOK, items)
}

// handleUpdateTask applies a partial metadata update (pin/archive/unread/title)
// to a task by uuid across all projects.
func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Pinned   *bool   `json:"pinned"`
		Archived *bool   `json:"archived"`
		Unread   *bool   `json:"unread"`
		Title    *string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	meta, err := session.UpdateSessionMeta(id, func(m *session.SessionMeta) {
		if req.Pinned != nil {
			m.Pinned = *req.Pinned
		}
		if req.Archived != nil {
			m.Archived = *req.Archived
		}
		if req.Unread != nil {
			m.Unread = *req.Unread
		}
		if req.Title != nil {
			m.Title = *req.Title
		}
		m.UpdatedAt = time.Now().Format(time.RFC3339)
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if meta == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	metas, err := session.ListSessions(s.activePwd())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type sessionItem struct {
		UUID      string `json:"uuid"`
		CreatedAt string `json:"created_at"`
		Provider  string `json:"provider"`
		Model     string `json:"model"`
		Title     string `json:"title,omitempty"`
	}

	items := make([]sessionItem, 0, len(metas))
	for _, m := range metas {
		items = append(items, sessionItem{
			UUID:      m.UUID,
			CreatedAt: m.StartTime,
			Provider:  m.Provider,
			Model:     m.Model,
			Title:     m.Title,
		})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entries, err := session.LoadSession(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session id is required"})
		return
	}
	// Tear down the live engine for this task (if any) so its run is cancelled and
	// resources reclaimed. The active foreground engine is left in place — but its
	// recorder is reset to a fresh session so post-delete writes don't land in the
	// now-unlinked file (silent data loss).
	if eng := s.resolveEngine(id); eng != nil {
		eng.emu.Lock()
		cancel := eng.runCancel
		eng.emu.Unlock()
		if cancel != nil {
			cancel()
		}
		if eng != s.activeEngine() {
			s.deleteEngine(id)
		} else {
			// Active task: wait for the cancelled run to drain so its final
			// RecordAssistant/usage writes land before we close + reset the recorder
			// (a post-close write would re-create and truncate the file).
			for i := 0; i < 200 && eng.running.Load(); i++ {
				time.Sleep(5 * time.Millisecond)
			}
			eng.emu.Lock()
			if eng.recorder != nil && eng.recorder.UUID() == id {
				eng.recorder.Close()
				eng.recorder = nil
				eng.history = nil
			}
			eng.emu.Unlock()
		}
	}

	// Resolve the owning project across all projects: a task deleted from the
	// sidebar tree may not belong to the active project.
	if _, err := session.DeleteSessionByUUID(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleTruncateHistory(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()
	if eng == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no active task"})
		return
	}
	if eng.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent is currently running"})
		return
	}

	var req struct {
		// BeforeUserMessage: keep all history entries that come before the
		// Nth user message (0-indexed). Everything from that user message
		// onward is discarded. Pass 0 to clear everything.
		BeforeUserMessage int `json:"before_user_message"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	// Capture the recorder under eng.emu (same lock submitMessage uses) but do
	// file I/O outside the lock.
	eng.emu.Lock()
	rec := eng.recorder
	eng.emu.Unlock()
	sessionID := ""
	if rec != nil {
		sessionID = rec.UUID()
	}

	// Persist first — if the file rewrite fails we abort without touching
	// the in-memory history so state never diverges.
	if rec != nil {
		if err := rec.TruncateAtUserMessage(req.BeforeUserMessage); err != nil {
			config.Logger().Printf("[truncate] rewrite session file failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to truncate session file"})
			return
		}
	}

	// Now truncate in-memory history under eng.emu.
	eng.emu.Lock()
	truncAt := 0
	if req.BeforeUserMessage > 0 {
		userCount := 0
		truncAt = len(eng.history) // default: keep all
		for i, msg := range eng.history {
			if msg.Role == schema.User {
				if userCount == req.BeforeUserMessage {
					truncAt = i
					break
				}
				userCount++
			}
		}
	}
	if truncAt == 0 {
		eng.history = nil
	} else {
		eng.history = eng.history[:truncAt]
	}
	eng.emu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"session_id": sessionID,
	})
}

func (s *Server) handleNewSession(w http.ResponseWriter, r *http.Request) {
	// Parse optional resume session ID + project. Creating a task no longer
	// blocks on "is the agent running" — tasks run concurrently.
	var req struct {
		SessionID string `json:"session_id,omitempty"`
		Pwd       string `json:"pwd,omitempty"`
	}
	// The body is optional (empty = brand-new task → EOF), but a non-empty
	// malformed body should be rejected rather than creating a zero-value task.
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Already-live task: just focus it (do not disturb its run).
	if req.SessionID != "" {
		if eng := s.resolveEngine(req.SessionID); eng != nil {
			s.setActiveEngine(eng)
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "session_id": eng.taskID})
			return
		}
	}

	if s.newEngine == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task creation is not supported"})
		return
	}

	// Each new/resumed task gets its OWN engine (env, agent, recorder, handler),
	// so it runs independently of every other task.
	pwd := req.Pwd
	if pwd == "" {
		if a := s.activeEngine(); a != nil {
			pwd = a.pwd
		}
	}
	eng, err := s.buildLocalEngine(req.SessionID, pwd, s.activeMode())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Resume: hydrate the fresh engine with the persisted conversation/todos/goal.
	if req.SessionID != "" {
		entries, lerr := session.LoadSession(req.SessionID)
		if lerr != nil {
			// Stale/nonexistent session id: don't silently register a phantom empty
			// engine under it — tear the just-built engine down and report not-found.
			s.deleteEngine(eng.taskID)
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		st := session.ReconstructState(entries)
		eng.emu.Lock()
		eng.history = st.History
		eng.emu.Unlock()
		if eng.todoStore != nil {
			items := make([]tools.TodoItem, len(st.Todos))
			for i, t := range st.Todos {
				items[i] = tools.TodoItem{ID: t.ID, Title: t.Title, Status: tools.TodoStatus(t.Status)}
			}
			eng.todoStore.Update(items)
		}
		if eng.env != nil && eng.env.GoalStore != nil {
			eng.env.GoalStore.RestoreFromSnapshot(st.Goal)
			if eng.handler != nil {
				eng.handler.Emit("goal_update", eng.env.GoalStore.Get())
			}
		}
	}

	s.setActiveEngine(eng)

	// Brand-new task: tell its view to start clean.
	if req.SessionID == "" {
		s.wsBroker.Broadcast(WSEvent{TaskID: eng.taskID, Type: "session_reset", Data: map[string]string{}})
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "session_id": eng.taskID})
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	curProvider, curModel := "", ""
	if eng := s.activeEngine(); eng != nil {
		curProvider, curModel, _ = eng.modelSnapshot()
	}
	if s.registry == nil || s.cfg == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"current":   map[string]string{"provider": curProvider, "model": curModel},
			"providers": []any{},
		})
		return
	}

	type modelInfo struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		ToolCall       bool   `json:"tool_call"`
		ContextLimit   int    `json:"context_limit,omitempty"`
		Reasoning      bool   `json:"reasoning,omitempty"`
		Recommended    bool   `json:"recommended,omitempty"`
		DefaultEnabled bool   `json:"default_enabled,omitempty"`
		Enabled        bool   `json:"enabled"`
		ImageSupport   bool   `json:"image_support,omitempty"`
	}
	type providerInfo struct {
		ID     string      `json:"id"`
		Name   string      `json:"name"`
		Models []modelInfo `json:"models"`
	}

	modelState, _ := config.LoadModelState()

	var result []providerInfo
	configuredProviders := s.cfg.GetProviders()
	for _, rp := range s.registry.ListProviders() {
		if _, configured := configuredProviders[rp.ID]; !configured {
			continue
		}
		models := s.registry.ListProviderModels(rp.ID, true)
		if len(models) == 0 {
			continue
		}
		pi := providerInfo{ID: rp.ID, Name: rp.Name}
		for _, m := range models {
			ctx := 0
			if m.Limit != nil {
				ctx = m.Limit.Context
			}
			ref := config.ModelRef{Provider: rp.ID, Model: m.ID}
			enabled := modelState.IsModelEnabled(ref, m.DefaultEnabled)
			imageSupport := false
			if m.Modalities != nil {
				for _, mod := range m.Modalities.Input {
					if mod == "image" {
						imageSupport = true
						break
					}
				}
			}
			pi.Models = append(pi.Models, modelInfo{
				ID: m.ID, Name: m.Name, ToolCall: m.ToolCall, ContextLimit: ctx,
				Reasoning: m.Reasoning, Recommended: m.Recommended,
				DefaultEnabled: m.DefaultEnabled, Enabled: enabled,
				ImageSupport: imageSupport,
			})
		}
		result = append(result, pi)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"current":   map[string]string{"provider": curProvider, "model": curModel},
		"providers": result,
	})
}

func (s *Server) handleSwitchModel(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()
	if eng == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no active task"})
		return
	}
	// No running gate: applyModelSwitch swaps eng.agent under eng.emu (the lock the
	// run reads it under), so a mid-run switch is safe and takes effect next turn —
	// consistent with mode/approval switching.

	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Provider == "" || req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and model are required"})
		return
	}

	// Rebuild THIS task's agent for the new model and swap it in under eng.emu
	// (the same lock submitMessage uses to read the agent). Keep history.
	ag, err := eng.createAgent(req.Provider, req.Model)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	eng.applyModelSwitch(ag, req.Provider, req.Model)

	// Track in recent models.
	if state, err := config.LoadModelState(); err == nil {
		state.AddRecent(config.ModelRef{Provider: req.Provider, Model: req.Model})
		_ = config.SaveModelState(state)
	}

	s.wsBroker.Broadcast(WSEvent{Type: "model_changed", TaskID: eng.taskID, Data: map[string]string{
		"provider": req.Provider,
		"model":    req.Model,
	}})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSwitchMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	// Accept only the three canonical unified mode ids.
	switch req.Mode {
	case "approval", "plan", "full_access":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be 'approval', 'plan', or 'full_access'"})
		return
	}
	sm := mode.Parse(req.Mode)

	eng := s.activeEngine()
	if eng == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no active task"})
		return
	}
	// No running gate: applyModeSwitch writes eng.agent under eng.emu, the same
	// lock submitMessage reads it under, so a mid-run switch is safe and simply
	// takes effect on the next turn (matching TUI/ACP and the "Allow all" path).

	// Rebuild this task's agent FIRST. If the rebuild fails, abort without
	// changing the mode/approval axis — otherwise plan mode could be reported while
	// a write-capable agent stays live.
	var newAg *adk.ChatModelAgent
	if eng.rebuildForMode != nil {
		ag, err := eng.rebuildForMode(sm.IsPlan())
		if err != nil {
			config.Logger().Printf("[web] mode switch agent rebuild error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to switch mode"})
			return
		}
		newAg = ag
	}
	if eng.approvalState != nil {
		eng.approvalState.SetSessionMode(sm) // approval axis (Full access → auto)
	}
	eng.applyModeSwitch(sm.String(), newAg)

	s.wsBroker.Broadcast(WSEvent{Type: "mode_changed", TaskID: eng.taskID, Data: map[string]string{
		"mode": sm.String(),
	}})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": sm.String()})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadConfig()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Return safe subset: no API keys.
	providerName, modelName := cfg.GetProviderModel()
	writeJSON(w, http.StatusOK, map[string]any{
		"provider":       providerName,
		"model":          modelName,
		"max_iterations": cfg.MaxIterations,
	})
}

func (s *Server) handleGetTodos(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()
	if eng == nil || eng.todoStore == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, eng.todoStore.Items())
}

// handleGetGoal returns the current session goal (or null when none is set).
func (s *Server) handleGetGoal(w http.ResponseWriter, _ *http.Request) {
	eng := s.activeEngine()
	if eng == nil || eng.env == nil || eng.env.GoalStore == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, eng.env.GoalStore.Get())
}

// handleSetGoal sets (or replaces) the session goal. Unless start=false, it also
// kicks off an agent run so work begins immediately.
func (s *Server) handleSetGoal(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()
	if eng == nil || eng.env == nil || eng.env.GoalStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "goals not available"})
		return
	}
	var req struct {
		Objective string `json:"objective"`
		Start     *bool  `json:"start,omitempty"` // default true
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	objective, err := tools.ValidateGoalObjective(req.Objective)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	g := eng.env.GoalStore.Set(objective)

	if req.Start == nil || *req.Start {
		// Start working immediately when idle; if busy, the continuation guard
		// will pick the goal up after the current run finishes. Targets the active
		// task.
		if eng.running.CompareAndSwap(false, true) {
			s.submitMessage(eng, tools.GoalKickoffPrompt(objective), eng.curMode(), "", "", nil)
		}
	}
	writeJSON(w, http.StatusOK, g)
}

// handleClearGoal removes the session goal.
func (s *Server) handleClearGoal(w http.ResponseWriter, _ *http.Request) {
	if eng := s.activeEngine(); eng != nil && eng.env != nil && eng.env.GoalStore != nil {
		eng.env.GoalStore.Clear()
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (s *Server) handleApproval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string `json:"id"`
		TaskID     string `json:"task_id"`
		Approved   bool   `json:"approved"`
		ApproveAll bool   `json:"approve_all"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	// Route the resolve to the requesting task's handler. resolveEngine maps an
	// empty task_id to the active task (legacy clients) but a NON-empty unknown id
	// to nil — so a stray id can't resolve against the active task's handler-local
	// approval ids.
	reng := s.resolveEngine(req.TaskID)
	if reng == nil || reng.handler == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such task"})
		return
	}
	if err := reng.handler.ResolveApproval(req.ID, req.Approved, req.ApproveAll); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// "Allow all" promotes that task to auto-approve (the runner flips its
	// ApprovalState on resolve). Mirror it onto that task's mode + selector.
	s.syncModeAfterApproval(reng, req.Approved, req.ApproveAll)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// syncModeAfterApproval reflects an approve-all promotion onto the server's
// user-facing mode state and notifies connected clients. A plain single approve
// (or a deny) leaves the mode untouched. The runner's ApprovalState is the
// source of truth for the approval axis; this only projects it onto the unified
// selector the frontend renders.
func (s *Server) syncModeAfterApproval(eng *Engine, approved, approveAll bool) {
	if !approved || !approveAll || eng == nil {
		return
	}
	sm := mode.FullAccess
	eng.applyModeSwitch(sm.String(), nil)
	s.wsBroker.Broadcast(WSEvent{Type: "mode_changed", TaskID: eng.taskID, Data: map[string]string{
		"mode": sm.String(),
	}})
}

// handlePendingApproval returns approval requests still awaiting a decision.
// The frontend pulls this after rebuilding the timeline (page reload / session
// resume / WS reconnect) so an in-flight approval is re-attached as a card
// instead of leaving the agent blocked forever.
func (s *Server) handlePendingApproval(w http.ResponseWriter, r *http.Request) {
	// Empty task_id → active task; non-empty unknown → empty (don't leak another
	// task's pending requests under a stray id).
	eng := s.resolveEngine(r.URL.Query().Get("task_id"))
	if eng == nil || eng.handler == nil {
		writeJSON(w, http.StatusOK, []handler.WebApprovalRequestData{})
		return
	}
	writeJSON(w, http.StatusOK, eng.handler.PendingApprovalRequests())
}

// handleAskUser resolves a pending ask_user request with the user's answers,
// routed back to the blocked tool via WebHandler.ResolveAskUser. The "answers"
// array is parallel to the questions the frontend received in ask_user_request:
// each carries the question header plus either a free-text answer or selected
// option labels.
func (s *Server) handleAskUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		TaskID  string `json:"task_id"`
		Answers []struct {
			QuestionHeader string   `json:"question_header"`
			Answer         string   `json:"answer"`
			Selected       []string `json:"selected"`
		} `json:"answers"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	resp := tools.AskUserBatchResponse{}
	for _, a := range req.Answers {
		resp.Answers = append(resp.Answers, tools.AskUserAnswer{
			QuestionHeader: a.QuestionHeader,
			Answer:         a.Answer,
			Selected:       a.Selected,
		})
	}

	// Route the answer to the requesting task's handler. Empty task_id → active;
	// non-empty unknown → reject (ids are handler-local).
	eng := s.resolveEngine(req.TaskID)
	if eng == nil || eng.handler == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such task"})
		return
	}
	if err := eng.handler.ResolveAskUser(req.ID, resp); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handlePendingAskUser returns ask_user questions still awaiting an answer.
// The frontend pulls this after rebuilding the timeline (page reload / session
// resume) so an in-flight question is re-attached to its tool card instead of
// leaving the agent blocked forever.
func (s *Server) handlePendingAskUser(w http.ResponseWriter, r *http.Request) {
	eng := s.resolveEngine(r.URL.Query().Get("task_id"))
	if eng == nil || eng.handler == nil {
		writeJSON(w, http.StatusOK, []handler.WebAskUserRequestData{})
		return
	}
	writeJSON(w, http.StatusOK, eng.handler.PendingAskUserRequests())
}

// withinWorkspace reports whether abs is the workspace root or strictly inside
// it. Uses filepath.Rel rather than strings.HasPrefix so a sibling like /repo2
// can't escape /repo, and an empty root rejects everything.
func withinWorkspace(root, abs string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	pwd := s.activePwd()
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = pwd
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(pwd, dir)
	}

	// Prevent path traversal / sibling escape.
	abs := filepath.Clean(dir)
	if !withinWorkspace(pwd, abs) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type fileItem struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
	}

	items := make([]fileItem, 0, len(entries))
	for _, e := range entries {
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		items = append(items, fileItem{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  size,
		})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	pwd := s.activePwd()
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(pwd, abs)
	}

	// Prevent path traversal / sibling escape.
	abs = filepath.Clean(abs)
	if !withinWorkspace(pwd, abs) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path outside workspace"})
		return
	}

	content, err := os.ReadFile(abs)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	// Limit file size to 1MB.
	if len(content) > 1<<20 {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": "file too large (>1MB)",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"path":    abs,
		"content": string(content),
	})
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}

	ctx, cancel := context.WithTimeout(s.ctx, 30*1e9) // 30 seconds
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", req.Command)
	cmd.Dir = s.activePwd()

	output, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	// Truncate output to 256KB
	out := string(output)
	if len(out) > 256*1024 {
		out = out[:256*1024] + "\n... (truncated)"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"output":    out,
		"exit_code": exitCode,
	})
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "working"
	}

	// "session" mode: diff between snapshot taken at agent run start and current state.
	if mode == "session" {
		s.handleSessionDiff(w, r)
		return
	}

	var args []string
	switch mode {
	case "staged":
		args = []string{"diff", "--cached", "--no-color"}
	case "branch":
		args = []string{"diff", "HEAD~1", "--no-color"}
	default: // "working"
		args = []string{"diff", "--no-color"}
	}

	cmd := exec.CommandContext(s.ctx, "git", args...)
	cmd.Dir = s.activePwd()
	output, _ := cmd.CombinedOutput()

	// Parse diff into structured entries
	type diffEntry struct {
		File      string `json:"file"`
		Patch     string `json:"patch"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
		Status    string `json:"status"` // "M", "A", "D"
	}

	var entries []diffEntry
	rawDiff := string(output)

	// Also get changed file list for status
	statCmd := exec.CommandContext(s.ctx, "git", "diff", "--stat", "--no-color")
	switch mode {
	case "staged":
		statCmd = exec.CommandContext(s.ctx, "git", "diff", "--cached", "--stat", "--no-color")
	case "branch":
		statCmd = exec.CommandContext(s.ctx, "git", "diff", "HEAD~1", "--stat", "--no-color")
	}
	statCmd.Dir = s.activePwd()
	_, _ = statCmd.CombinedOutput()

	// Parse unified diff into per-file entries
	sections := splitDiffByFile(rawDiff)
	for _, sec := range sections {
		adds, dels := countDiffLines(sec.patch)
		entries = append(entries, diffEntry{
			File:      sec.file,
			Patch:     sec.patch,
			Additions: adds,
			Deletions: dels,
			Status:    sec.status,
		})
	}

	if entries == nil {
		entries = []diffEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"mode":    mode,
		"entries": entries,
	})
}

type diffSection struct {
	file   string
	patch  string
	status string
}

func splitDiffByFile(raw string) []diffSection {
	var sections []diffSection
	lines := strings.Split(raw, "\n")
	var current *diffSection
	var patchLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			// Flush previous
			if current != nil {
				current.patch = strings.Join(patchLines, "\n")
				sections = append(sections, *current)
			}
			// Parse file name from "diff --git a/foo b/foo"
			parts := strings.SplitN(line, " b/", 2)
			file := ""
			if len(parts) == 2 {
				file = parts[1]
			}
			current = &diffSection{file: file, status: "M"}
			patchLines = []string{line}
		} else if current != nil {
			patchLines = append(patchLines, line)
			if strings.HasPrefix(line, "new file") {
				current.status = "A"
			} else if strings.HasPrefix(line, "deleted file") {
				current.status = "D"
			}
		}
	}
	if current != nil {
		current.patch = strings.Join(patchLines, "\n")
		sections = append(sections, *current)
	}
	return sections
}

func countDiffLines(patch string) (adds, dels int) {
	scanner := bufio.NewScanner(strings.NewReader(patch))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			adds++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			dels++
		}
	}
	return
}

// takeSessionSnapshot records the current git working tree state
// so that session-scoped diffs can be computed later.
func (s *Server) takeSessionSnapshot(eng *Engine) {
	if eng == nil {
		return
	}
	// Use "git stash create" to get a tree-ish of the current state without
	// actually stashing. If there are no changes, use HEAD.
	cmd := exec.CommandContext(s.ctx, "git", "stash", "create")
	cmd.Dir = eng.pwd
	out, err := cmd.Output()
	snapshot := strings.TrimSpace(string(out))
	if err != nil || snapshot == "" {
		// No local changes — use HEAD as baseline
		cmd2 := exec.CommandContext(s.ctx, "git", "rev-parse", "HEAD")
		cmd2.Dir = eng.pwd
		out2, _ := cmd2.Output()
		snapshot = strings.TrimSpace(string(out2))
	}
	eng.emu.Lock()
	eng.sessionSnapshot = snapshot
	eng.emu.Unlock()
}

// handleSessionDiff computes the diff between the session start snapshot and current state.
func (s *Server) handleSessionDiff(w http.ResponseWriter, _ *http.Request) {
	// Capture the active engine ONCE so the snapshot and the working dir come
	// from the same task's repo even if the active engine is swapped between the
	// two reads (otherwise we could diff engine A's snapshot against engine B's
	// tree). eng.pwd is immutable after creation, so reading it bare is safe.
	eng := s.activeEngine()
	snapshot := ""
	pwd := ""
	if eng != nil {
		eng.emu.Lock()
		snapshot = eng.sessionSnapshot
		eng.emu.Unlock()
		pwd = eng.pwd
	}

	type diffEntry struct {
		File      string `json:"file"`
		Patch     string `json:"patch"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
		Status    string `json:"status"`
	}

	if snapshot == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"mode":    "session",
			"entries": []diffEntry{},
		})
		return
	}

	// Diff from snapshot to current working tree
	cmd := exec.CommandContext(s.ctx, "git", "diff", snapshot, "--no-color")
	cmd.Dir = pwd
	output, _ := cmd.CombinedOutput()

	var entries []diffEntry
	sections := splitDiffByFile(string(output))
	for _, sec := range sections {
		adds, dels := countDiffLines(sec.patch)
		entries = append(entries, diffEntry{
			File:      sec.file,
			Patch:     sec.patch,
			Additions: adds,
			Deletions: dels,
			Status:    sec.status,
		})
	}

	if entries == nil {
		entries = []diffEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"mode":    "session",
		"entries": entries,
	})
}

// mcpLoginState tracks an in-progress or finished OAuth login for a server.
type mcpLoginState struct {
	Status  string `json:"status"` // pending | authorized | error | needs_client_id
	AuthURL string `json:"auth_url,omitempty"`
	Message string `json:"message,omitempty"`
}

// mcpServerView is the JSON shape returned for one MCP server in the list and
// CRUD responses — enough for the management UI's status badges and edit form.
type mcpServerView struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     []string          `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Timeout int               `json:"timeout,omitempty"`
	Enabled bool              `json:"enabled"`
	OAuth   bool              `json:"oauth"`    // OAuth enabled for this server
	HasAuth bool              `json:"has_auth"` // a token is stored
	Status  string            `json:"status"`   // connected | needs_auth | error | disabled | configured
	Error   string            `json:"error,omitempty"`
}

// mcpServerReq is the request body for creating/updating an MCP server.
type mcpServerReq struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"` // local|stdio|http|sse
	URL     string            `json:"url"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     []string          `json:"env"`
	Headers map[string]string `json:"headers"`
	Timeout int               `json:"timeout"`
	OAuth   *struct {
		Enabled      bool     `json:"enabled"`
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		Scopes       []string `json:"scopes"`
	} `json:"oauth"`
}

// serverFromReq builds a config.MCPServer from a request body, normalizing the
// transport ("local" → "stdio") and preserving any existing OAuth token state.
func serverFromReq(req *mcpServerReq) (*config.MCPServer, error) {
	srv := &config.MCPServer{
		Headers:        req.Headers,
		TimeoutSeconds: req.Timeout,
	}
	t := req.Type
	if t == "local" {
		t = "stdio"
	}
	switch t {
	case "http", "sse":
		if req.URL == "" {
			return nil, fmt.Errorf("url is required for %s servers", t)
		}
		srv.Type = t
		srv.URL = req.URL
	case "stdio", "":
		if req.Command == "" {
			return nil, fmt.Errorf("command is required for local servers")
		}
		srv.Type = "stdio"
		srv.Command = req.Command
		srv.Args = req.Args
		srv.Env = req.Env
	default:
		return nil, fmt.Errorf("unknown server type %q (use local, http, or sse)", req.Type)
	}
	if req.OAuth != nil && (req.OAuth.Enabled || req.OAuth.ClientID != "" || len(req.OAuth.Scopes) > 0) {
		srv.OAuth = &config.MCPOAuthConfig{
			Enabled:      req.OAuth.Enabled || req.OAuth.ClientID != "",
			ClientID:     req.OAuth.ClientID,
			ClientSecret: req.OAuth.ClientSecret,
			Scopes:       req.OAuth.Scopes,
		}
	}
	return srv, nil
}

// reloadMCPAndRebuild reconnects MCP servers from the current config and
// rebuilds the live agent so new tools take effect without a restart.
func (s *Server) reloadMCPAndRebuild() error {
	if s.reloadMCP != nil {
		s.mu.RLock()
		servers := s.cfg.MCPServers
		s.mu.RUnlock()
		statuses, err := s.reloadMCP(servers)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.mcpStatuses = make(map[string]tools.MCPStatus, len(statuses))
		for _, st := range statuses {
			s.mcpStatuses[st.Name] = st
		}
		s.mu.Unlock()
	}
	if !s.needsSetup {
		// Rebuild the foreground task's agent so the new MCP tools take effect.
		if eng := s.activeEngine(); eng != nil && eng.createAgent != nil {
			prov, mod, _ := eng.modelSnapshot()
			ag, err := eng.createAgent(prov, mod)
			if err != nil {
				return err
			}
			eng.setAgent(ag)
		}
	}
	return nil
}

// mcpServerStatus derives the UI status string for a server from its config and
// last-known connection status.
func (s *Server) mcpServerStatus(name string, srv *config.MCPServer) (status, errMsg string) {
	if srv.Disabled {
		return "disabled", ""
	}
	st, ok := s.mcpStatuses[name]
	switch {
	case !ok:
		return "configured", ""
	case st.NeedsAuth:
		return "needs_auth", ""
	case st.Running:
		return "connected", ""
	case st.Error != nil:
		return "error", st.Error.Error()
	default:
		return "configured", ""
	}
}

func (s *Server) handleListMCP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	servers := make(map[string]mcpServerView)
	if s.cfg != nil {
		for name, srv := range s.cfg.MCPServers {
			status, errMsg := s.mcpServerStatus(name, srv)
			servers[name] = mcpServerView{
				Name:    name,
				Type:    srv.Type,
				URL:     srv.URL,
				Command: srv.Command,
				Args:    srv.Args,
				Env:     srv.Env,
				Headers: srv.Headers,
				Timeout: srv.TimeoutSeconds,
				Enabled: !srv.Disabled,
				OAuth:   srv.OAuth != nil && srv.OAuth.Enabled,
				HasAuth: tools.HasMCPOAuthToken(name),
				Status:  status,
				Error:   errMsg,
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
}

func (s *Server) handleCreateMCP(w http.ResponseWriter, r *http.Request) {
	var req mcpServerReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<18)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	srv, err := serverFromReq(&req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	if s.cfg.MCPServers == nil {
		s.cfg.MCPServers = make(map[string]*config.MCPServer)
	}
	if _, exists := s.cfg.MCPServers[req.Name]; exists {
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a server with that name already exists"})
		return
	}
	s.cfg.MCPServers[req.Name] = srv
	if err := config.SaveConfig(s.cfg); err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Unlock()

	if err := s.reloadMCPAndRebuild(); err != nil {
		config.Logger().Printf("[web] mcp create reload failed: %v", err)
	}
	s.wsBroker.Broadcast(WSEvent{Type: "mcp_changed", Data: map[string]string{"name": req.Name}})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "name": req.Name})
}

func (s *Server) handleUpdateMCP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req mcpServerReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<18)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	srv, err := serverFromReq(&req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	existing, ok := s.cfg.MCPServers[name]
	if !ok {
		s.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not found"})
		return
	}
	// Preserve disabled flag and any already-obtained OAuth client id/secret so
	// editing other fields doesn't drop a working registration.
	srv.Disabled = existing.Disabled
	if srv.OAuth != nil && existing.OAuth != nil {
		if srv.OAuth.ClientID == "" {
			srv.OAuth.ClientID = existing.OAuth.ClientID
		}
		if srv.OAuth.ClientSecret == "" {
			srv.OAuth.ClientSecret = existing.OAuth.ClientSecret
		}
	}
	s.cfg.MCPServers[name] = srv
	if err := config.SaveConfig(s.cfg); err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Unlock()

	if err := s.reloadMCPAndRebuild(); err != nil {
		config.Logger().Printf("[web] mcp update reload failed: %v", err)
	}
	s.wsBroker.Broadcast(WSEvent{Type: "mcp_changed", Data: map[string]string{"name": name}})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "name": name})
}

func (s *Server) handleDeleteMCP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	if _, ok := s.cfg.MCPServers[name]; !ok {
		s.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not found"})
		return
	}
	delete(s.cfg.MCPServers, name)
	delete(s.mcpStatuses, name)
	delete(s.mcpLogins, name)
	if err := config.SaveConfig(s.cfg); err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Unlock()

	_ = tools.DeleteMCPOAuthToken(name)
	if err := s.reloadMCPAndRebuild(); err != nil {
		config.Logger().Printf("[web] mcp delete reload failed: %v", err)
	}
	s.wsBroker.Broadcast(WSEvent{Type: "mcp_changed", Data: map[string]string{"name": name}})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleToggleMCP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	s.mu.Lock()
	srv, ok := s.cfg.MCPServers[name]
	if !ok {
		s.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not found"})
		return
	}
	srv.Disabled = !req.Enabled
	if err := config.SaveConfig(s.cfg); err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Unlock()

	if err := s.reloadMCPAndRebuild(); err != nil {
		config.Logger().Printf("[web] mcp toggle reload failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "name": name, "enabled": req.Enabled})
}

// handleMCPLogin starts the OAuth authorization flow for an HTTP/SSE server in
// the background and opens the user's browser. Progress is polled via
// handleMCPLoginStatus.
func (s *Server) handleMCPLogin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	srv, ok := s.cfg.MCPServers[name]
	if !ok {
		s.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not found"})
		return
	}
	if srv.URL == "" || (srv.Type != "http" && srv.Type != "sse") {
		s.mu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "OAuth login only applies to http/sse servers"})
		return
	}
	if existing := s.mcpLogins[name]; existing != nil && existing.Status == "pending" {
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a login is already in progress"})
		return
	}
	if srv.OAuth == nil {
		srv.OAuth = &config.MCPOAuthConfig{Enabled: true}
	}
	s.mcpLogins[name] = &mcpLoginState{Status: "pending"}
	s.mu.Unlock()

	go s.runMCPLogin(name)
	writeJSON(w, http.StatusOK, map[string]any{"status": "pending"})
}

func (s *Server) setMCPLogin(name, status, msg string) {
	s.mu.Lock()
	st := s.mcpLogins[name]
	if st == nil {
		st = &mcpLoginState{}
		s.mcpLogins[name] = st
	}
	st.Status = status
	st.Message = msg
	s.mu.Unlock()
}

func (s *Server) runMCPLogin(name string) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()

	s.mu.RLock()
	srv := s.cfg.MCPServers[name]
	s.mu.RUnlock()
	if srv == nil {
		s.setMCPLogin(name, "error", "server not found")
		return
	}

	err := tools.PerformMCPOAuthLogin(ctx, name, srv, func(authURL string) {
		s.mu.Lock()
		if st := s.mcpLogins[name]; st != nil {
			st.AuthURL = authURL
		}
		s.mu.Unlock()
		s.wsBroker.Broadcast(WSEvent{Type: "mcp_login", Data: map[string]string{"name": name, "auth_url": authURL}})
		openBrowser(authURL)
	})
	if err != nil {
		status := "error"
		if errors.Is(err, tools.ErrOAuthNeedsClientID) {
			status = "needs_client_id"
		}
		s.setMCPLogin(name, status, err.Error())
		config.Logger().Printf("[web] mcp login %q failed: %v", name, err)
		return
	}

	// Persist the (possibly dynamically registered) client id and enabled flag.
	s.mu.Lock()
	if saveErr := config.SaveConfig(s.cfg); saveErr != nil {
		config.Logger().Printf("[web] mcp login %q: save config failed: %v", name, saveErr)
	}
	s.mu.Unlock()

	if reErr := s.reloadMCPAndRebuild(); reErr != nil {
		config.Logger().Printf("[web] mcp login %q: reload failed: %v", name, reErr)
	}
	s.setMCPLogin(name, "authorized", "")
	s.wsBroker.Broadcast(WSEvent{Type: "mcp_changed", Data: map[string]string{"name": name}})
}

func (s *Server) handleMCPLoginStatus(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.RLock()
	st := s.mcpLogins[name]
	s.mu.RUnlock()
	if st == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "idle"})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		dir = home
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type folderItem struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	var folders []folderItem
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip hidden folders
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		folders = append(folders, folderItem{
			Name: e.Name(),
			Path: filepath.Join(abs, e.Name()),
		})
	}
	if folders == nil {
		folders = []folderItem{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"current": abs,
		"folders": folders,
	})
}

func (s *Server) handleCreatePTY(w http.ResponseWriter, r *http.Request) {
	pwd, owner := "", ""
	var dockerExec *tools.DockerExecutor
	if eng := s.activeEngine(); eng != nil {
		pwd, owner = eng.pwd, eng.taskID
		// A container-bound engine gets a terminal INSIDE the container; SSH and
		// local engines keep a local shell (SSH-in-terminal remains a known gap).
		if eng.env != nil {
			if de, ok := eng.env.Exec.(*tools.DockerExecutor); ok {
				dockerExec = de
			}
		}
	}

	var (
		id  string
		err error
	)
	if dockerExec != nil {
		// createDocker acquires its own container ref (so an env switch can't stop
		// the container under a live terminal) and resolves the shared client itself.
		id, err = s.ptyMgr.createDocker(dockerExec.ContainerID(), pwd, owner)
	} else {
		id, err = s.ptyMgr.create(pwd, owner)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *Server) handleListPTY(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.ptyMgr.list()})
}

func (s *Server) handleKillPTY(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.ptyMgr.kill(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePTYWebSocket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.ptyMgr.serveWS(w, r, id)
}

// handleValidatePaths reports which of the given local paths no longer exist (or
// are not directories). The web UI keeps its workspace list in localStorage and
// can't stat the disk itself, so it calls this to prune dead workspaces from the
// picker instead of letting the user click one and hit "path does not exist".
// Callers send local paths only; ssh:// labels can't be stat'd here and would be
// wrongly reported missing, so they must be filtered out client-side.
func (s *Server) handleValidatePaths(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	missing := []string{}
	for _, p := range req.Paths {
		if p == "" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			// Only a confirmed not-exist means the workspace is gone. Transient
			// errors (permission, NFS hiccup) are inconclusive — keep the path
			// rather than silently dropping a still-valid workspace from the picker.
			if os.IsNotExist(err) {
				missing = append(missing, p)
			}
			continue
		}
		if !info.IsDir() {
			missing = append(missing, p)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"missing": missing})
}

func (s *Server) handleSwitchProject(w http.ResponseWriter, r *http.Request) {
	// No running gate: "switch project" builds a NEW independent engine and leaves
	// the previous task running in the background — switching to another task while
	// one is chatting is the whole point of concurrent tasks.
	if s.newEngine == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "project switching is not supported",
		})
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	// Validate path exists and is a directory.
	info, err := os.Stat(req.Path)
	if err != nil || !info.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path does not exist or is not a directory"})
		return
	}

	// Snapshot the outgoing task once, build the new engine BEFORE tearing down its
	// PTYs — a failed build must not kill the current task's terminals.
	prevTaskID, curMode := "", ""
	if cur := s.activeEngine(); cur != nil {
		prevTaskID, curMode = cur.taskID, cur.curMode()
	}

	// "Switch project" = build a fresh engine rooted at the new path and make it
	// active. This replaces in-place env mutation, so no other live task's
	// execution context is disturbed.
	eng, err := s.buildLocalEngine("", req.Path, curMode)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("failed to switch project: %v", err),
		})
		return
	}
	s.ptyMgr.closeForTask(prevTaskID) // outgoing task's PTYs only
	s.setActiveEngine(eng)

	// Reset todos for the (now empty) active task view.
	if eng.todoStore != nil {
		eng.todoStore.Update(nil)
	}

	// Broadcast project change to clients.
	s.wsBroker.Broadcast(WSEvent{
		Type: "project_switched",
		Data: map[string]string{
			"pwd": req.Path,
		},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"pwd":    req.Path,
	})
}

func (s *Server) handleGetApprovalMode(w http.ResponseWriter, r *http.Request) {
	autoApprove := false
	if eng := s.activeEngine(); eng != nil && eng.approvalState != nil {
		autoApprove = eng.approvalState.GetMode() == handler.ModeAuto
	}
	writeJSON(w, http.StatusOK, map[string]any{"auto_approve": autoApprove})
}

func (s *Server) handleSetApprovalMode(w http.ResponseWriter, r *http.Request) {
	eng := s.activeEngine()
	if eng == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no active task"})
		return
	}
	// No running gate: the rebuild is emu-safe and applies next turn, consistent
	// with the "Allow all" approval path which also flips full_access mid-run.
	var req struct {
		AutoApprove bool `json:"auto_approve"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	// Legacy endpoint: auto-approve now maps onto the unified mode (Full access vs
	// Approval). Both are non-plan, so rebuild to the full tool set for consistency.
	sm := mode.Approval
	if req.AutoApprove {
		sm = mode.FullAccess
	}
	// Rebuild first; abort the toggle if the rebuild fails (don't desync the
	// reported mode from the live agent).
	var newAg *adk.ChatModelAgent
	if eng.rebuildForMode != nil {
		ag, err := eng.rebuildForMode(false)
		if err != nil {
			config.Logger().Printf("[web] approval mode agent rebuild error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to set approval mode"})
			return
		}
		newAg = ag
	}
	if eng.approvalState != nil {
		eng.approvalState.SetSessionMode(sm)
	}
	eng.applyModeSwitch(sm.String(), newAg)
	// Persist as the default startup mode so the preference survives restarts —
	// resolveStartupMode reads cfg.DefaultMode. cfgMu serializes the config RMW.
	s.cfgMu.Lock()
	if s.cfg != nil {
		s.cfg.DefaultMode = sm.String()
		if err := config.SaveConfig(s.cfg); err != nil {
			config.Logger().Printf("[web] approval mode save config failed: %v", err)
		}
	}
	s.cfgMu.Unlock()

	s.wsBroker.Broadcast(WSEvent{
		Type:   "approval_mode_changed",
		TaskID: eng.taskID,
		Data:   map[string]any{"auto_approve": req.AutoApprove},
	})
	// Also emit the unified mode event so updated clients keep their selector synced.
	s.wsBroker.Broadcast(WSEvent{Type: "mode_changed", TaskID: eng.taskID, Data: map[string]string{"mode": sm.String()}})
	writeJSON(w, http.StatusOK, map[string]any{"auto_approve": req.AutoApprove})
}

// --- WebSocket handler ---

// CheckOrigin rejects cross-origin WebSocket handshakes from untrusted web
// pages (see isAllowedWebOrigin); without this any website could open a socket
// to the loopback server and read the agent's live event stream.
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: isAllowedWebOrigin,
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		config.Logger().Printf("[ws] upgrade error: %v", err)
		return
	}

	id, client, unsub := s.wsBroker.Register(conn)
	config.Logger().Printf("[ws] client %d connected", id)

	// Write pump: send events to client.
	go client.writePump()

	// Read pump: handle incoming messages.
	defer func() {
		unsub()
		_ = conn.Close()
		config.Logger().Printf("[ws] client %d disconnected", id)
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var incoming WSIncoming
		if err := json.Unmarshal(msg, &incoming); err != nil {
			continue
		}
		s.handleWSMessage(client, incoming)
	}
}

func (s *Server) handleWSMessage(client *WSClient, msg WSIncoming) {
	switch msg.Type {
	case "ping":
		// Unicast the pong to the pinging client (broadcasting it woke every
		// client unnecessarily).
		if data, err := json.Marshal(WSEvent{Type: "pong"}); err == nil {
			client.send(data)
		}
	case "subscribe":
		var data struct {
			TaskIDs []string `json:"task_ids"`
		}
		if json.Unmarshal(msg.Data, &data) == nil {
			client.subscribe(data.TaskIDs)
		}
	case "unsubscribe":
		var data struct {
			TaskIDs []string `json:"task_ids"`
		}
		if json.Unmarshal(msg.Data, &data) == nil {
			client.unsubscribe(data.TaskIDs)
		}
	case "approval":
		var data struct {
			ID         string `json:"id"`
			TaskID     string `json:"task_id"`
			Approved   bool   `json:"approved"`
			ApproveAll bool   `json:"approve_all"`
		}
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			return
		}
		// Empty task_id → active task (legacy); non-empty unknown → drop (ids are
		// handler-local and could collide with another task's).
		reng := s.resolveEngine(data.TaskID)
		if reng == nil || reng.handler == nil {
			return
		}
		if err := reng.handler.ResolveApproval(data.ID, data.Approved, data.ApproveAll); err != nil {
			config.Logger().Printf("[ws] resolve approval failed for id=%q: %v", data.ID, err)
			return
		}
		// Same mode-sync as the POST path: an "allow all" over WS must also
		// update the selector pill the user is looking at.
		s.syncModeAfterApproval(reng, data.Approved, data.ApproveAll)
	}
}

// --- Stop handler ---

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	// Cancel only the targeted task. task_id comes via query or JSON body; absent,
	// fall back to the active task (legacy clients).
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		var req struct {
			TaskID string `json:"task_id"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
		taskID = req.TaskID
	}

	eng := s.resolveEngine(taskID)
	if eng == nil || !eng.running.Load() {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_running"})
		return
	}

	eng.emu.Lock()
	cancel := eng.runCancel
	eng.emu.Unlock()
	if cancel != nil {
		cancel()
	}

	// Notify clients on that task's channel.
	eng.handler.OnAgentDone(fmt.Errorf("stopped by user"))

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// --- SSH list handler ---

func (s *Server) handleListSSH(w http.ResponseWriter, r *http.Request) {
	type sshItem struct {
		Name string `json:"name"`
		Addr string `json:"addr"`
		Path string `json:"path,omitempty"`
	}

	var items []sshItem
	if s.cfg != nil {
		for _, a := range s.cfg.SSHAliases {
			items = append(items, sshItem{
				Name: a.Name,
				Addr: a.Addr,
				Path: a.Path,
			})
		}
	}
	if items == nil {
		items = []sshItem{}
	}

	current := "local"
	if eng := s.activeEngine(); eng != nil && eng.env != nil && eng.env.IsRemote() {
		current = "ssh"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"current": current,
		"aliases": items,
	})
}

// --- Skills list handler (for slash commands) ---

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	type skillItem struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Slash       string `json:"slash"`
		Builtin     bool   `json:"builtin"`
		Source      string `json:"source"` // builtin | local
		Enabled     bool   `json:"enabled"`
	}

	var items []skillItem
	if s.skillLoader != nil {
		for _, sk := range s.skillLoader.All() {
			source := "local"
			if sk.Builtin {
				source = "builtin"
			}
			items = append(items, skillItem{
				Name:        sk.Name,
				Description: sk.Description,
				Slash:       sk.Slash,
				Builtin:     sk.Builtin,
				Source:      source,
				Enabled:     s.skillLoader.IsEnabled(sk.Name),
			})
		}
	}
	if items == nil {
		items = []skillItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

// handleToggleSkill enables/disables a skill, persisting to config and updating
// the loader + agent so the change takes effect immediately.
func (s *Server) handleToggleSkill(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if s.skillLoader == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "skills unavailable"})
		return
	}

	// cfgMu (not s.mu) serializes the cfg read-modify-write+save, so concurrent
	// approval-mode / MCP / skill saves can't clobber each other in memory or on
	// disk.
	s.cfgMu.Lock()
	// Rebuild the disabled set from config.
	disabled := make(map[string]bool, len(s.cfg.DisabledSkills))
	for _, n := range s.cfg.DisabledSkills {
		disabled[n] = true
	}
	if req.Enabled {
		delete(disabled, name)
	} else {
		disabled[name] = true
	}
	list := make([]string, 0, len(disabled))
	for n := range disabled {
		list = append(list, n)
	}
	sort.Strings(list)
	s.cfg.DisabledSkills = list
	if err := config.SaveConfig(s.cfg); err != nil {
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.cfgMu.Unlock()

	s.skillLoader.SetDisabled(list)
	// Rebuild the foreground task's agent so the system prompt (skill descriptions)
	// and load_skill tool reflect the change on the next run.
	if !s.needsSetup {
		if eng := s.activeEngine(); eng != nil && eng.createAgent != nil {
			prov, mod, _ := eng.modelSnapshot()
			if ag, err := eng.createAgent(prov, mod); err == nil {
				eng.setAgent(ag)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "name": name, "enabled": req.Enabled})
}

// handleSlashCommands returns skill slash commands for the web frontend
// autocomplete menu. Built-in commands (/setting, /model, /ssh, etc.) are
// excluded because the web UI provides dedicated controls for those features
// and submitMessage only dispatches skill-based slash commands.
func (s *Server) handleSlashCommands(w http.ResponseWriter, r *http.Request) {
	type slashItem struct {
		Slash       string `json:"slash"`
		Description string `json:"description"`
		Type        string `json:"type"` // "skill"
	}

	var items []slashItem
	if s.skillLoader != nil {
		for _, sk := range s.skillLoader.SlashCommands() {
			items = append(items, slashItem{
				Slash:       sk.Slash,
				Description: sk.Description,
				Type:        "skill",
			})
		}
	}

	if items == nil {
		items = []slashItem{}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Slash < items[j].Slash
	})

	writeJSON(w, http.StatusOK, items)
}

// --- Setup & Provider Management Handlers ---

// handleSetupStatus returns whether the server is in setup mode.
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"needs_setup": s.needsSetup,
	})
}

// handleSetupValidate tests connectivity to a provider with the given API key.
func (s *Server) handleSetupValidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api_key is required"})
		return
	}

	baseURL := req.BaseURL
	if baseURL == "" && s.registry != nil {
		baseURL = s.registry.GetProviderAPI(req.Provider)
	}
	if baseURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no base URL available for this provider"})
		return
	}

	if err := model.ValidateProvider(r.Context(), req.APIKey, baseURL); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"valid": true,
	})
}

// handleSetupProviders returns all available providers from the registry.
func (s *Server) handleSetupProviders(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	type providerItem struct {
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		Doc        string   `json:"doc,omitempty"`
		API        string   `json:"api,omitempty"`
		Env        []string `json:"env,omitempty"`
		Configured bool     `json:"configured"`
		Tag        string   `json:"tag,omitempty"` // "recommended", "free", "local"
	}

	providers := s.registry.ListProviders()
	cfg, _ := config.LoadConfig()
	configured := map[string]bool{}
	if cfg != nil {
		for k := range cfg.GetProviders() {
			configured[k] = true
		}
	}

	// Provider tags for recommendation.
	tags := map[string]string{
		"openai":    "recommended",
		"anthropic": "recommended",
		"ollama":    "local",
	}

	result := make([]providerItem, 0, len(providers))
	for _, p := range providers {
		result = append(result, providerItem{
			ID:         p.ID,
			Name:       p.Name,
			Doc:        p.Doc,
			API:        p.API,
			Env:        p.Env,
			Configured: configured[p.ID],
			Tag:        tags[p.ID],
		})
	}

	// Sort: configured first, then by tag (recommended > local > ""), then by name.
	sort.SliceStable(result, func(i, j int) bool {
		ri, rj := result[i], result[j]
		if ri.Configured != rj.Configured {
			return ri.Configured
		}
		tagOrder := map[string]int{"recommended": 0, "local": 1, "": 2}
		oi := tagOrder[ri.Tag]
		oj := tagOrder[rj.Tag]
		if oi != oj {
			return oi < oj
		}
		return ri.Name < rj.Name
	})

	writeJSON(w, http.StatusOK, result)
}

// handleSetupProviderModels returns models for a specific provider from the registry.
func (s *Server) handleSetupProviderModels(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	if providerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id is required"})
		return
	}

	if s.registry == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	models := s.registry.ListProviderModels(providerID, true)
	type modelItem struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		ToolCall     bool   `json:"tool_call"`
		ContextLimit int    `json:"context_limit,omitempty"`
		Reasoning    bool   `json:"reasoning,omitempty"`
	}

	result := make([]modelItem, 0, len(models))
	for _, m := range models {
		ctx := 0
		if m.Limit != nil {
			ctx = m.Limit.Context
		}
		result = append(result, modelItem{
			ID:           m.ID,
			Name:         m.Name,
			ToolCall:     m.ToolCall,
			ContextLimit: ctx,
			Reasoning:    m.Reasoning,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// handleSetupComplete handles the initial setup submission.
// It saves the provider config and creates the agent.
func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Provider == "" || req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and model are required"})
		return
	}

	// Build or update config.
	var cfg *config.Config
	cfg, err := config.LoadConfig()
	if err != nil {
		// First time — create fresh config.
		cfg = &config.Config{
			MaxIterations: 1000,
		}
	}

	if cfg.Providers == nil {
		cfg.Providers = make(map[string]*config.ProviderConfig)
	}
	cfg.Providers[req.Provider] = &config.ProviderConfig{
		APIKey:  req.APIKey,
		BaseURL: req.BaseURL,
	}
	cfg.Model = req.Provider + "/" + req.Model

	if err := config.SaveConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config: " + err.Error()})
		return
	}

	// Create the foreground task's agent with the new config.
	eng := s.activeEngine()
	if eng == nil || eng.createAgent == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no active task to configure"})
		return
	}
	ag, err := eng.createAgent(req.Provider, req.Model)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create agent: " + err.Error()})
		return
	}
	eng.applyModelSwitch(ag, req.Provider, req.Model)
	// Publish the new config + registry to the live server so endpoints
	// (/api/models, context-limit, etc.) reflect the just-configured provider
	// without a restart.
	s.cfgMu.Lock()
	s.cfg = cfg
	s.registry = model.NewModelRegistryWithConfig(cfg)
	s.cfgMu.Unlock()
	s.mu.Lock()
	s.needsSetup = false
	s.mu.Unlock()

	// Notify clients that setup is complete.
	s.wsBroker.Broadcast(WSEvent{Type: "model_changed", TaskID: eng.taskID, Data: map[string]string{
		"provider": req.Provider,
		"model":    req.Model,
	}})

	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"provider": req.Provider,
		"model":    req.Model,
	})
}

// handleListProviders returns all configured providers (key masked).
func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadConfig()
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	type providerDetail struct {
		ID        string `json:"id"`
		APIKeySet bool   `json:"api_key_set"`
		APIKey    string `json:"api_key,omitempty"` // masked
		BaseURL   string `json:"base_url,omitempty"`
	}

	result := make([]providerDetail, 0)
	for id, pc := range cfg.GetProviders() {
		detail := providerDetail{
			ID:        id,
			APIKeySet: pc.APIKey != "",
			BaseURL:   pc.BaseURL,
		}
		if pc.APIKey != "" {
			// Mask API key: show first 4 and last 4 chars.
			key := pc.APIKey
			if len(key) > 8 {
				detail.APIKey = key[:4] + "..." + key[len(key)-4:]
			} else {
				detail.APIKey = "****"
			}
		}
		result = append(result, detail)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })

	writeJSON(w, http.StatusOK, result)
}

// handleAddProvider adds a new provider to the config.
func (s *Server) handleAddProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		APIKey  string `json:"api_key"`
		BaseURL string `json:"base_url,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.ID == "" || req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id and api_key are required"})
		return
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = &config.Config{MaxIterations: 1000}
	}
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]*config.ProviderConfig)
	}
	cfg.Providers[req.ID] = &config.ProviderConfig{
		APIKey:  req.APIKey,
		BaseURL: req.BaseURL,
	}
	if err := config.SaveConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeleteProvider removes a provider from the config.
func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	if providerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id is required"})
		return
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	providers := cfg.GetProviders()
	if providers == nil || providers[providerID] == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found"})
		return
	}

	// Don't allow deleting the active provider.
	activeProvider, _ := cfg.GetProviderModel()
	if activeProvider == providerID {
		remaining := 0
		for k := range providers {
			if k != providerID {
				remaining++
			}
		}
		if remaining == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete the only provider"})
			return
		}
	}

	delete(cfg.Providers, providerID)
	if err := config.SaveConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleGetModelState returns the recent, favorite, and visibility settings.
func (s *Server) handleGetModelState(w http.ResponseWriter, r *http.Request) {
	state, err := config.LoadModelState()
	if err != nil {
		state = &config.ModelState{}
	}
	type modelRefJSON struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}

	recent := make([]modelRefJSON, 0, len(state.Recent))
	for _, r := range state.Recent {
		recent = append(recent, modelRefJSON{Provider: r.Provider, Model: r.Model})
	}
	favorites := make([]modelRefJSON, 0, len(state.Favorite))
	for _, r := range state.Favorite {
		favorites = append(favorites, modelRefJSON{Provider: r.Provider, Model: r.Model})
	}
	enabledModels := make([]modelRefJSON, 0, len(state.EnabledModels))
	for _, r := range state.EnabledModels {
		enabledModels = append(enabledModels, modelRefJSON{Provider: r.Provider, Model: r.Model})
	}
	disabledModels := make([]modelRefJSON, 0, len(state.DisabledModels))
	for _, r := range state.DisabledModels {
		disabledModels = append(disabledModels, modelRefJSON{Provider: r.Provider, Model: r.Model})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"recent":          recent,
		"favorite":        favorites,
		"enabled_models":  enabledModels,
		"disabled_models": disabledModels,
	})
}

// handleToggleFavorite toggles a model in the favorites list.
func (s *Server) handleToggleFavorite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Provider == "" || req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and model are required"})
		return
	}

	state, err := config.LoadModelState()
	if err != nil {
		state = &config.ModelState{}
	}
	nowFavorite := state.ToggleFavorite(config.ModelRef{Provider: req.Provider, Model: req.Model})
	if err := config.SaveModelState(state); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"favorite": nowFavorite,
	})
}

// handleToggleModelEnabled toggles whether a model is shown in the model selector.
func (s *Server) handleToggleModelEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Enabled  bool   `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Provider == "" || req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and model are required"})
		return
	}

	state, err := config.LoadModelState()
	if err != nil {
		state = &config.ModelState{}
	}
	state.SetModelEnabled(config.ModelRef{Provider: req.Provider, Model: req.Model}, req.Enabled)
	if err := config.SaveModelState(state); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": req.Enabled,
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
		// Only reflect CORS headers for trusted origins; a disallowed cross-origin
		// request gets none, so the browser blocks the response (and its preflight).
		if origin != "" && isAllowedWebOrigin(r) {
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
