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
	"sync/atomic"
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
	utils "github.com/cnjack/jcode/internal/util"
)

// Server is the jcode web server.
type Server struct {
	port        int
	host        string
	openBrowser bool
	pwd         string
	handler     *handler.WebHandler
	wsBroker    *WSBroker

	mu      sync.RWMutex
	agent   *adk.ChatModelAgent
	history []adk.Message
	running atomic.Bool

	// Cancel function for the currently running agent, protected by mu.
	runCancel context.CancelFunc

	// Server-level context (from Start), used for background agent work.
	ctx context.Context

	// Active model info.
	providerName string
	modelName    string
	mode         string // "build" or "plan"

	// Dependencies set during initialization.
	todoStore *tools.TodoStore
	recorder  *session.Recorder
	tracer    *telemetry.LangfuseTracer
	env       *tools.Env
	cfg       *config.Config
	registry  *model.ModelRegistry

	// createAgent rebuilds the agent (after config changes).
	// Accepts provider and model names so the caller can switch models.
	createAgent func(providerName, modelName string) (*adk.ChatModelAgent, error)

	// rebuildForMode re-assembles the agent for a session-mode change, swapping
	// the tool/prompt axis (Plan = read-only) while reusing the live chat model.
	rebuildForMode func(planMode bool) (*adk.ChatModelAgent, error)

	// switchProject changes the working directory and rebuilds the agent.
	switchProject func(newPwd string) (*adk.ChatModelAgent, *session.Recorder, error)

	// switchToRemote binds the shared env to a remote SSH executor and rebuilds
	// the agent (mirrors switchProject for remote targets).
	switchToRemote func(executor *tools.SSHExecutor, remotePwd string) (*adk.ChatModelAgent, *session.Recorder, error)

	// remoteConns holds SSH connections established by the remote-connect wizard
	// that have not yet been bound to the live env (keyed by connection id).
	remoteConns *remoteConnRegistry

	// PTY manager for terminal sessions.
	ptyMgr *ptyManager

	// approvalState controls whether tool calls require approval.
	approvalState *runner.ApprovalState

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

	// sessionSnapshot holds the git tree hash at the start of an agent run,
	// used to compute session-scoped diffs (agent changes only).
	sessionSnapshot string

	// wechatClient is the optional WeChat channel client.
	wechatClient channel.Channel

	// eventHandler is the handler passed to the runner — may be a NotifyingHandler
	// wrapping the WebHandler, or the WebHandler itself.
	eventHandler handler.AgentEventHandler

	// needsSetup is true when no providers are configured. The server starts in
	// setup mode and exposes setup API endpoints while blocking chat operations.
	needsSetup bool
	version    string

	// tokenUsage tracks per-call token totals for the agent runs, used for
	// usage display (goal status, token updates).
	tokenUsage *model.TokenUsage
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
	InitialMode        string // unified startup mode string ("ask"/"plan"/"autopilot")
	SwitchProject      func(newPwd string) (*adk.ChatModelAgent, *session.Recorder, error)
	SwitchToRemote     func(executor *tools.SSHExecutor, remotePwd string) (*adk.ChatModelAgent, *session.Recorder, error)
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
	s := &Server{
		port:           cfg.Port,
		host:           cfg.Host,
		openBrowser:    cfg.OpenBrowser,
		pwd:            cfg.Pwd,
		version:        cfg.Version,
		handler:        h,
		wsBroker:       NewWSBroker(),
		agent:          cfg.Agent,
		createAgent:    cfg.CreateAgent,
		rebuildForMode: cfg.RebuildForMode,
		switchProject:  cfg.SwitchProject,
		switchToRemote: cfg.SwitchToRemote,
		remoteConns:    newRemoteConnRegistry(),
		todoStore:      cfg.TodoStore,
		recorder:       cfg.Recorder,
		tracer:         cfg.Tracer,
		env:            cfg.Env,
		providerName:   cfg.ProviderName,
		modelName:      cfg.ModelName,
		mode:           mode.Parse(cfg.InitialMode).String(),
		cfg:            cfg.Config,
		registry:       cfg.Registry,
		ptyMgr:         newPTYManager(),
		approvalState:  cfg.ApprovalState,
		skillLoader:    cfg.SkillLoader,
		reloadMCP:      cfg.ReloadMCP,
		mcpStatuses:    make(map[string]tools.MCPStatus),
		mcpLogins:      make(map[string]*mcpLoginState),
		wechatClient:   cfg.WechatClient,
		eventHandler:   eh,
		needsSetup:     cfg.NeedsSetup,
		tokenUsage:     cfg.TokenUsage,
	}
	if s.tokenUsage == nil {
		s.tokenUsage = &model.TokenUsage{}
	}
	for _, st := range cfg.InitialMCPStatuses {
		s.mcpStatuses[st.Name] = st
	}

	// Wire TodoStore → session recording.
	// The callback always accesses s.recorder (protected by s.mu) so that
	// handleNewSession / handleSwitchProject correctly use the latest recorder.
	if cfg.TodoStore != nil {
		cfg.TodoStore.OnUpdate = func(items []tools.TodoItem) {
			s.mu.RLock()
			r := s.recorder
			s.mu.RUnlock()
			// Only record into a session that already has real content (a user
			// message created the file). Otherwise an ambient todo reset — e.g.
			// clearing the previous session's todos when starting fresh — would
			// be the first write, creating + indexing a phantom empty session.
			if r != nil && r.HasRecording() {
				snapItems := make([]session.TodoSnapshotItem, len(items))
				for i, it := range items {
					snapItems[i] = session.TodoSnapshotItem{
						ID: it.ID, Title: it.Title, Status: string(it.Status),
					}
				}
				r.RecordTodoSnapshot(snapItems)
			}
		}
	}

	// Wire GoalStore → session recording, mirroring the TodoStore wiring above.
	if cfg.Env != nil && cfg.Env.GoalStore != nil {
		cfg.Env.GoalStore.OnUpdate = func(g *tools.Goal) {
			s.mu.RLock()
			r := s.recorder
			s.mu.RUnlock()
			// Same guard as the todo hook: a goal change must not be the first
			// write that creates + indexes an otherwise-empty session (e.g.
			// clearing the previous session's goal on reset). Always emit to the
			// UI, but only persist once the session has real content.
			if r != nil && r.HasRecording() {
				tools.GoalRecorderHook(r)(g)
			}
			if s.handler != nil {
				s.handler.Emit("goal_update", g)
			}
		}
	}

	return s
}

// Handler returns the underlying WebHandler for external wiring (e.g. approval routing).
func (s *Server) Handler() *handler.WebHandler {
	return s.handler
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
	mux.HandleFunc("GET /api/skills", s.handleListSkills)
	mux.HandleFunc("POST /api/skills/{name}/toggle", s.handleToggleSkill)
	mux.HandleFunc("GET /api/slash-commands", s.handleSlashCommands)
	mux.HandleFunc("GET /api/browse", s.handleBrowse)
	mux.HandleFunc("POST /api/project/switch", s.handleSwitchProject)
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

	// Forward WebHandler events to WebSocket clients.
	go s.forwardEvents()

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

// forwardEvents reads from the WebHandler event channel and broadcasts to WebSocket clients.
func (s *Server) forwardEvents() {
	for ev := range s.handler.Events() {
		s.wsBroker.Broadcast(WSEvent{
			Type: ev.Event,
			Data: ev.Data,
		})
	}
}

// --- API Handlers ---

// currentModelSupportsImage checks if the currently selected model supports image input.
func (s *Server) currentModelSupportsImage() bool {
	if s.registry == nil {
		return false
	}
	_, m, ok := s.registry.LookupModel(s.providerName, s.modelName)
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
	s.mu.RLock()
	sessionID := ""
	if s.recorder != nil {
		sessionID = s.recorder.UUID()
	}
	s.mu.RUnlock()

	if s.needsSetup {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":      "needs_setup",
			"version":     s.version,
			"pwd":         s.pwd,
			"provider":    "",
			"model":       "",
			"mode":        "build",
			"session_id":  "",
			"running":     false,
			"needs_setup": true,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"version":       s.version,
		"pwd":           s.pwd,
		"provider":      s.providerName,
		"model":         s.modelName,
		"mode":          s.mode,
		"session_id":    sessionID,
		"running":       s.running.Load(),
		"image_support": s.currentModelSupportsImage(),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"running":    s.running.Load(),
		"ws_clients": s.wsBroker.ClientCount(),
		"pwd":        s.pwd,
		"provider":   s.providerName,
		"model":      s.modelName,
		"mode":       s.mode,
	})
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
	branchCmd.Dir = s.pwd
	branchOut, _ := branchCmd.Output()
	branch := strings.TrimSpace(string(branchOut))

	statusCmd := exec.CommandContext(r.Context(), "git", "status", "--porcelain")
	statusCmd.Dir = s.pwd
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

	// Use CompareAndSwap to atomically check and set running, preventing
	// two concurrent requests from both entering submitMessage.
	if !s.running.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "agent is already processing a request",
		})
		return
	}
	// running is now true; submitMessage will proceed without re-setting it.

	var req struct {
		Message   string      `json:"message"`
		Images    []chatImage `json:"images,omitempty"`     // optional: base64-encoded images
		Mode      string      `json:"mode,omitempty"`       // "build" or "plan"
		SessionID string      `json:"session_id,omitempty"` // optional: continue existing session
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 20<<20)).Decode(&req); err != nil {
		s.running.Store(false)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		s.running.Store(false)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = s.mode
	}

	sessionID := s.submitMessage(req.Message, mode, "", req.SessionID, req.Images)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "processing", "session_id": sessionID})
}

// chatImage represents a base64-encoded image in a chat request.
type chatImage struct {
	Data     string `json:"data"`       // base64 data (without data: prefix)
	MimeType string `json:"media_type"` // e.g. "image/png", "image/jpeg"
}

// SubmitMessage submits a message for agent processing from an external source
// (e.g. WeChat inbound message). Returns false if the agent is busy.
func (s *Server) SubmitMessage(message, source string) bool {
	if !s.running.CompareAndSwap(false, true) {
		return false
	}
	s.submitMessage(message, s.mode, source, "", nil)
	return true
}

// submitMessage is the shared implementation for starting an agent run.
// source is an optional label (e.g. "wechat") for the user_message event.
// sessionID is an optional session identifier from the client to ensure
// continuity — if the current recorder has a different UUID, resume the
// correct session instead of creating a new one.
// images is an optional list of base64-encoded images to include in the message.
// The caller must have already set s.running to true (via CompareAndSwap).
// Returns the session_id of the recorder used.
func (s *Server) submitMessage(message, mode, source, sessionID string, images []chatImage) string {
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
		s.handler.Emit("user_message", map[string]string{
			"content": message,
			"source":  source,
		})
	}

	// Ensure a recorder exists (lazy creation on first message).
	// If the client provided a session_id and the current recorder differs,
	// resume the client's session to prevent creating a duplicate.
	s.mu.Lock()
	if s.recorder == nil {
		rec, _ := session.NewRecorder(s.pwd, s.providerName, s.modelName)
		if sessionID != "" {
			rec.SetUUID(sessionID)
		}
		s.recorder = rec
	} else if sessionID != "" && s.recorder.UUID() != sessionID {
		// Client is continuing a session that doesn't match the current recorder.
		// Resume the client's session to keep all messages together.
		s.recorder.Close()
		rec, _ := session.NewRecorder(s.pwd, s.providerName, s.modelName)
		rec.SetUUID(sessionID)
		s.recorder = rec
	}
	recorder := s.recorder
	s.mu.Unlock()

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

	s.mu.Lock()
	s.history = append(s.history, userMsg)
	history := make([]adk.Message, len(s.history))
	copy(history, s.history)
	agent := s.agent
	s.mu.Unlock()

	// Stream response via WebSocket — run agent in background.
	runCtx, runCancel := context.WithCancel(s.ctx)
	s.mu.Lock()
	s.runCancel = runCancel
	s.mu.Unlock()

	go func() {
		defer func() {
			s.running.Store(false)
			s.mu.Lock()
			s.runCancel = nil
			s.mu.Unlock()
		}()

		// Take a git snapshot before the agent run for session diff tracking.
		s.takeSessionSnapshot()

		resp := runner.Run(runCtx, agent, history, s.eventHandler, recorder, s.todoStore, s.env.GoalStore, s.tracer, s.tokenUsage)
		if resp != "" {
			s.mu.Lock()
			s.history = append(s.history, &schema.Message{Role: schema.Assistant, Content: resp})
			s.mu.Unlock()
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
	type taskItem struct {
		UUID      string `json:"uuid"`
		Project   string `json:"project"`
		CreatedAt string `json:"created_at"`
		Provider  string `json:"provider"`
		Model     string `json:"model"`
		Title     string `json:"title,omitempty"`
		Pinned    bool   `json:"pinned"`
		Archived  bool   `json:"archived"`
		Unread    bool   `json:"unread"`
		Status    string `json:"status,omitempty"`
	}
	items := make([]taskItem, 0)
	for project, metas := range all {
		for _, m := range metas {
			items = append(items, taskItem{
				UUID:      m.UUID,
				Project:   project,
				CreatedAt: m.StartTime,
				Provider:  m.Provider,
				Model:     m.Model,
				Title:     m.Title,
				Pinned:    m.Pinned,
				Archived:  m.Archived,
				Unread:    m.Unread,
				Status:    m.Status,
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
	metas, err := session.ListSessions(s.pwd)
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
	// Resolve the owning project across all projects: a task deleted from the
	// sidebar tree may not belong to the active project (s.pwd).
	if _, err := session.DeleteSessionByUUID(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleTruncateHistory(w http.ResponseWriter, r *http.Request) {
	if s.running.Load() {
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

	// Capture the recorder reference under the lock but do file I/O outside
	// so we don't block other goroutines.
	s.mu.Lock()
	rec := s.recorder
	sessionID := ""
	if rec != nil {
		sessionID = rec.UUID()
	}
	s.mu.Unlock()

	// Persist first — if the file rewrite fails we abort without touching
	// the in-memory history so state never diverges.
	if rec != nil {
		if err := rec.TruncateAtUserMessage(req.BeforeUserMessage); err != nil {
			config.Logger().Printf("[truncate] rewrite session file failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to truncate session file"})
			return
		}
	}

	// Now truncate in-memory history.
	s.mu.Lock()
	truncAt := 0
	if req.BeforeUserMessage > 0 {
		userCount := 0
		truncAt = len(s.history) // default: keep all
		for i, msg := range s.history {
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
		s.history = nil
	} else {
		s.history = s.history[:truncAt]
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"session_id": sessionID,
	})
}

func (s *Server) handleNewSession(w http.ResponseWriter, r *http.Request) {
	// Parse optional request body for resume session ID.
	var req struct {
		SessionID string `json:"session_id,omitempty"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)

	// Only block creating a brand-new session while the agent is running.
	// Resuming (loading) an existing session is always allowed — the web UI
	// may refresh at any time and needs to restore its view.
	if req.SessionID == "" && s.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent is currently running"})
		return
	}

	// When resuming while running, skip recorder/history replacement — just
	// return the requested session_id so the frontend can populate the UI
	// from the session entries (which it already fetched via GET).
	if req.SessionID != "" && s.running.Load() {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "ok",
			"session_id": req.SessionID,
		})
		return
	}

	s.mu.Lock()
	// Close the old recorder.
	if s.recorder != nil {
		s.recorder.Close()
		s.recorder = nil
	}

	// Only create a recorder when resuming an existing session.
	// For brand-new conversations the recorder is created lazily in
	// submitMessage on the first actual user message, which avoids
	// persisting empty sessions.
	if req.SessionID != "" {
		rec, _ := session.NewRecorder(s.pwd, s.providerName, s.modelName)
		if rec != nil {
			rec.SetUUID(req.SessionID)
			s.recorder = rec
		}
	}

	// Prepare todo items while holding the lock, but apply them after unlocking
	// to avoid deadlock: todoStore.Update → OnUpdate → s.mu.RLock.
	var updateTodos bool
	var todoItems []tools.TodoItem
	var resuming bool
	var goalSnap *session.GoalSnapshot
	if req.SessionID != "" {
		resuming = true
		// Resuming: load prior conversation into history so the agent has context.
		entries, _ := session.LoadSession(req.SessionID)
		// Reconstruct full state (message history, todos, goal).
		st := session.ReconstructState(entries)
		s.history = st.History
		goalSnap = st.Goal
		// Always update (an empty list clears leftovers from the previous session).
		if s.todoStore != nil {
			updateTodos = true
			todoItems = make([]tools.TodoItem, len(st.Todos))
			for i, t := range st.Todos {
				todoItems[i] = tools.TodoItem{ID: t.ID, Title: t.Title, Status: tools.TodoStatus(t.Status)}
			}
		}
	} else {
		s.history = nil
		// Mark that todos should be reset.
		if s.todoStore != nil {
			updateTodos = true
		}
	}
	s.mu.Unlock()

	// Apply todo updates outside the lock to avoid deadlock with OnUpdate callback.
	if updateTodos && s.todoStore != nil {
		s.todoStore.Update(todoItems)
	}

	// Apply the session's goal state outside the lock. Restore is silent (no
	// OnUpdate, so nothing is re-recorded into the session file); broadcast
	// the new state to clients explicitly. A brand-new session always resets
	// the store so a goal from the previous session does not leak across.
	if s.env != nil && s.env.GoalStore != nil {
		if resuming {
			s.env.GoalStore.RestoreFromSnapshot(goalSnap)
		} else {
			s.env.GoalStore.Restore(nil)
		}
		if s.handler != nil {
			s.handler.Emit("goal_update", s.env.GoalStore.Get())
		}
	}

	// Notify clients. When resuming an existing session, do NOT broadcast session_reset
	// (which would wipe the UI that the frontend is about to repopulate from history).
	if req.SessionID == "" {
		s.wsBroker.Broadcast(WSEvent{Type: "session_reset", Data: map[string]string{}})
	}

	s.mu.RLock()
	newSessionID := ""
	if s.recorder != nil {
		newSessionID = s.recorder.UUID()
	}
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"session_id": newSessionID,
	})
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil || s.cfg == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"current":   map[string]string{"provider": s.providerName, "model": s.modelName},
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
		"current":   map[string]string{"provider": s.providerName, "model": s.modelName},
		"providers": result,
	})
}

func (s *Server) handleSwitchModel(w http.ResponseWriter, r *http.Request) {
	if s.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent is currently running"})
		return
	}

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

	ag, err := s.createAgent(req.Provider, req.Model)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.mu.Lock()
	s.agent = ag
	s.providerName = req.Provider
	s.modelName = req.Model
	// Keep history — allow continuing the conversation with a different model.
	s.mu.Unlock()

	// Track in recent models.
	if state, err := config.LoadModelState(); err == nil {
		state.AddRecent(config.ModelRef{Provider: req.Provider, Model: req.Model})
		_ = config.SaveModelState(state)
	}

	// Notify clients.
	s.wsBroker.Broadcast(WSEvent{Type: "model_changed", Data: map[string]string{
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
	// Accept the three unified ids plus the legacy "build" alias (→ ask).
	switch req.Mode {
	case "ask", "plan", "autopilot", "build":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be 'ask', 'plan', or 'autopilot'"})
		return
	}
	sm := mode.Parse(req.Mode)

	// Lock around the agent rebuild + mode/approval mutation so we don't race an
	// in-flight submitMessage that reads s.agent under the same lock. The new
	// agent (and tool set) takes effect on the next run, like TUI/ACP.
	s.mu.Lock()
	s.mode = sm.String()
	if s.approvalState != nil {
		s.approvalState.SetSessionMode(sm) // approval axis (Autopilot → auto)
	}
	if s.rebuildForMode != nil {
		if ag, err := s.rebuildForMode(sm.IsPlan()); err == nil {
			s.agent = ag // tool/prompt axis (Plan → read-only)
		} else {
			config.Logger().Printf("[web] mode switch agent rebuild error: %v", err)
		}
	}
	s.mu.Unlock()

	s.wsBroker.Broadcast(WSEvent{Type: "mode_changed", Data: map[string]string{
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
	if s.todoStore == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	items := s.todoStore.Items()
	writeJSON(w, http.StatusOK, items)
}

// handleGetGoal returns the current session goal (or null when none is set).
func (s *Server) handleGetGoal(w http.ResponseWriter, _ *http.Request) {
	if s.env == nil || s.env.GoalStore == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, s.env.GoalStore.Get())
}

// handleSetGoal sets (or replaces) the session goal. Unless start=false, it also
// kicks off an agent run so work begins immediately.
func (s *Server) handleSetGoal(w http.ResponseWriter, r *http.Request) {
	if s.env == nil || s.env.GoalStore == nil {
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
	g := s.env.GoalStore.Set(objective)

	if req.Start == nil || *req.Start {
		// Start working immediately when idle; if busy, the continuation guard
		// will pick the goal up after the current run finishes.
		if s.running.CompareAndSwap(false, true) {
			s.submitMessage(tools.GoalKickoffPrompt(objective), s.mode, "", "", nil)
		}
	}
	writeJSON(w, http.StatusOK, g)
}

// handleClearGoal removes the session goal.
func (s *Server) handleClearGoal(w http.ResponseWriter, _ *http.Request) {
	if s.env != nil && s.env.GoalStore != nil {
		s.env.GoalStore.Clear()
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (s *Server) handleApproval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string `json:"id"`
		Approved   bool   `json:"approved"`
		ApproveAll bool   `json:"approve_all"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if err := s.handler.ResolveApproval(req.ID, req.Approved, req.ApproveAll); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAskUser resolves a pending ask_user request with the user's answers,
// routed back to the blocked tool via WebHandler.ResolveAskUser. The "answers"
// array is parallel to the questions the frontend received in ask_user_request:
// each carries the question header plus either a free-text answer or selected
// option labels.
func (s *Server) handleAskUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
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

	if err := s.handler.ResolveAskUser(req.ID, resp); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handlePendingAskUser returns ask_user questions still awaiting an answer.
// The frontend pulls this after rebuilding the timeline (page reload / session
// resume) so an in-flight question is re-attached to its tool card instead of
// leaving the agent blocked forever.
func (s *Server) handlePendingAskUser(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.handler.PendingAskUserRequests())
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = s.pwd
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(s.pwd, dir)
	}

	// Prevent path traversal.
	abs := filepath.Clean(dir)
	if !strings.HasPrefix(abs, s.pwd) {
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

	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.pwd, abs)
	}

	// Prevent path traversal.
	abs = filepath.Clean(abs)
	if !strings.HasPrefix(abs, s.pwd) {
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
	cmd.Dir = s.pwd

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
	cmd.Dir = s.pwd
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
	statCmd.Dir = s.pwd
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
func (s *Server) takeSessionSnapshot() {
	// Use "git stash create" to get a tree-ish of the current state without
	// actually stashing. If there are no changes, use HEAD.
	cmd := exec.CommandContext(s.ctx, "git", "stash", "create")
	cmd.Dir = s.pwd
	out, err := cmd.Output()
	snapshot := strings.TrimSpace(string(out))
	if err != nil || snapshot == "" {
		// No local changes — use HEAD as baseline
		cmd2 := exec.CommandContext(s.ctx, "git", "rev-parse", "HEAD")
		cmd2.Dir = s.pwd
		out2, _ := cmd2.Output()
		snapshot = strings.TrimSpace(string(out2))
	}
	s.mu.Lock()
	s.sessionSnapshot = snapshot
	s.mu.Unlock()
}

// handleSessionDiff computes the diff between the session start snapshot and current state.
func (s *Server) handleSessionDiff(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	snapshot := s.sessionSnapshot
	s.mu.RUnlock()

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
	cmd.Dir = s.pwd
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
	if s.createAgent != nil && !s.needsSetup {
		s.mu.RLock()
		prov, mod := s.providerName, s.modelName
		s.mu.RUnlock()
		ag, err := s.createAgent(prov, mod)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.agent = ag
		s.mu.Unlock()
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
	id, err := s.ptyMgr.create(s.pwd)
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

func (s *Server) handleSwitchProject(w http.ResponseWriter, r *http.Request) {
	if s.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "agent is running, cannot switch project",
		})
		return
	}
	if s.switchProject == nil {
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

	// Kill all PTY sessions (they were in the old directory).
	s.ptyMgr.closeAll()

	// Call the switchProject callback to rebuild env, prompt, agent.
	ag, rec, err := s.switchProject(req.Path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("failed to switch project: %v", err),
		})
		return
	}

	s.mu.Lock()
	s.pwd = req.Path
	s.agent = ag
	s.recorder = rec
	s.history = nil
	s.mu.Unlock()

	// Reset todos.
	s.todoStore.Update(nil)

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
	if s.approvalState != nil {
		autoApprove = s.approvalState.GetMode() == handler.ModeAuto
	}
	writeJSON(w, http.StatusOK, map[string]any{"auto_approve": autoApprove})
}

func (s *Server) handleSetApprovalMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AutoApprove bool `json:"auto_approve"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	// Legacy endpoint: auto-approve now maps onto the unified mode (Autopilot vs
	// Ask). Both are non-plan, so rebuild to the full tool set for consistency.
	sm := mode.Ask
	if req.AutoApprove {
		sm = mode.Autopilot
	}
	s.mu.Lock()
	s.mode = sm.String()
	if s.approvalState != nil {
		s.approvalState.SetSessionMode(sm)
	}
	if s.rebuildForMode != nil {
		if ag, err := s.rebuildForMode(false); err == nil {
			s.agent = ag
		} else {
			config.Logger().Printf("[web] approval mode agent rebuild error: %v", err)
		}
	}
	s.mu.Unlock()

	s.wsBroker.Broadcast(WSEvent{
		Type: "approval_mode_changed",
		Data: map[string]any{"auto_approve": req.AutoApprove},
	})
	// Also emit the unified mode event so updated clients keep their selector synced.
	s.wsBroker.Broadcast(WSEvent{Type: "mode_changed", Data: map[string]string{"mode": sm.String()}})
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
		s.handleWSMessage(incoming)
	}
}

func (s *Server) handleWSMessage(msg WSIncoming) {
	switch msg.Type {
	case "ping":
		s.wsBroker.Broadcast(WSEvent{Type: "pong"})
	case "approval":
		var data struct {
			ID         string `json:"id"`
			Approved   bool   `json:"approved"`
			ApproveAll bool   `json:"approve_all"`
		}
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			return
		}
		if err := s.handler.ResolveApproval(data.ID, data.Approved, data.ApproveAll); err != nil {
			config.Logger().Printf("[ws] resolve approval failed for id=%q: %v", data.ID, err)
		}
	}
}

// --- Stop handler ---

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if !s.running.Load() {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_running"})
		return
	}

	s.mu.RLock()
	cancel := s.runCancel
	s.mu.RUnlock()

	if cancel != nil {
		cancel()
	}

	// Notify clients.
	s.handler.OnAgentDone(fmt.Errorf("stopped by user"))

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
	if s.env != nil && s.env.IsRemote() {
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

	s.mu.Lock()
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
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Unlock()

	s.skillLoader.SetDisabled(list)
	// Rebuild the agent so the system prompt (skill descriptions) and load_skill
	// tool reflect the change on the next run.
	if s.createAgent != nil && !s.needsSetup {
		s.mu.RLock()
		prov, mod := s.providerName, s.modelName
		s.mu.RUnlock()
		if ag, err := s.createAgent(prov, mod); err == nil {
			s.mu.Lock()
			s.agent = ag
			s.mu.Unlock()
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

	// Create the agent with the new config.
	ag, err := s.createAgent(req.Provider, req.Model)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create agent: " + err.Error()})
		return
	}

	s.mu.Lock()
	s.agent = ag
	s.providerName = req.Provider
	s.modelName = req.Model
	s.needsSetup = false
	s.mu.Unlock()

	// Notify clients that setup is complete.
	s.wsBroker.Broadcast(WSEvent{Type: "model_changed", Data: map[string]string{
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
	case "127.0.0.1", "localhost", "::1":
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
