// Package web implements the jcode web server and API.
package web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/gorilla/websocket"

	"github.com/cnjack/jcode/internal/channel"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/skills"
	"github.com/cnjack/jcode/internal/telemetry"
	"github.com/cnjack/jcode/internal/tools"
)

// Server is the jcode web server.
type Server struct {
	port     int
	host     string
	pwd      string
	handler  *handler.WebHandler
	broker   *SSEBroker
	wsBroker *WSBroker

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

	// switchProject changes the working directory and rebuilds the agent.
	switchProject func(newPwd string) (*adk.ChatModelAgent, *session.Recorder, error)

	// PTY manager for terminal sessions.
	ptyMgr *ptyManager

	// approvalState controls whether tool calls require approval.
	approvalState *runner.ApprovalState

	// skillLoader provides skill listing for slash commands.
	skillLoader *skills.Loader

	// disabledMCP tracks MCP servers that have been disabled via the UI.
	disabledMCP map[string]bool

	// sessionSnapshot holds the git tree hash at the start of an agent run,
	// used to compute session-scoped diffs (agent changes only).
	sessionSnapshot string

	// wechatClient is the optional WeChat channel client.
	wechatClient channel.Channel

	// eventHandler is the handler passed to the runner — may be a NotifyingHandler
	// wrapping the WebHandler, or the WebHandler itself.
	eventHandler handler.AgentEventHandler
}

// ServerConfig holds the configuration for creating a new Server.
type ServerConfig struct {
	Port          int
	Host          string
	Pwd           string
	Agent         *adk.ChatModelAgent
	CreateAgent   func(providerName, modelName string) (*adk.ChatModelAgent, error)
	SwitchProject func(newPwd string) (*adk.ChatModelAgent, *session.Recorder, error)
	TodoStore     *tools.TodoStore
	Recorder      *session.Recorder
	Tracer        *telemetry.LangfuseTracer
	Env           *tools.Env
	ProviderName  string
	ModelName     string
	Config        *config.Config
	Registry      *model.ModelRegistry
	ApprovalState *runner.ApprovalState
	SkillLoader   *skills.Loader
	WechatClient  channel.Channel           // optional WeChat channel
	WebHandler    *handler.WebHandler       // optional: pre-created handler for sharing with tools
	EventHandler  handler.AgentEventHandler // optional: handler for runner (e.g. NotifyingHandler)
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
		port:          cfg.Port,
		host:          cfg.Host,
		pwd:           cfg.Pwd,
		handler:       h,
		broker:        NewSSEBroker(),
		wsBroker:      NewWSBroker(),
		agent:         cfg.Agent,
		createAgent:   cfg.CreateAgent,
		switchProject: cfg.SwitchProject,
		todoStore:     cfg.TodoStore,
		recorder:      cfg.Recorder,
		tracer:        cfg.Tracer,
		env:           cfg.Env,
		providerName:  cfg.ProviderName,
		modelName:     cfg.ModelName,
		mode:          "build",
		cfg:           cfg.Config,
		registry:      cfg.Registry,
		ptyMgr:        newPTYManager(),
		approvalState: cfg.ApprovalState,
		skillLoader:   cfg.SkillLoader,
		disabledMCP:   make(map[string]bool),
		wechatClient:  cfg.WechatClient,
		eventHandler:  eh,
	}

	// Wire TodoStore → session recording.
	// The callback always accesses s.recorder (protected by s.mu) so that
	// handleNewSession / handleSwitchProject correctly use the latest recorder.
	if cfg.TodoStore != nil {
		cfg.TodoStore.OnUpdate = func(items []tools.TodoItem) {
			s.mu.RLock()
			r := s.recorder
			s.mu.RUnlock()
			if r != nil {
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
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("POST /api/stop", s.handleStop)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("POST /api/sessions", s.handleNewSession)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("GET /api/todos", s.handleGetTodos)
	mux.HandleFunc("POST /api/approval", s.handleApproval)
	mux.HandleFunc("GET /api/files", s.handleListFiles)
	mux.HandleFunc("GET /api/files/content", s.handleReadFile)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/models", s.handleListModels)
	mux.HandleFunc("POST /api/model", s.handleSwitchModel)
	mux.HandleFunc("POST /api/mode", s.handleSwitchMode)
	mux.HandleFunc("POST /api/exec", s.handleExec)
	mux.HandleFunc("GET /api/diff", s.handleDiff)
	mux.HandleFunc("GET /api/mcp", s.handleListMCP)
	mux.HandleFunc("POST /api/mcp/{name}/toggle", s.handleToggleMCP)
	mux.HandleFunc("GET /api/ssh", s.handleListSSH)
	mux.HandleFunc("GET /api/skills", s.handleListSkills)
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

	// Serve embedded frontend (SPA with fallback to index.html)
	mux.Handle("GET /", newSPAHandler())

	// CORS middleware
	corsHandler := corsMiddleware(mux)

	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	srv := &http.Server{
		Addr:    addr,
		Handler: corsHandler,
	}

	// Forward WebHandler events to SSE broker.
	go s.forwardEvents()

	// Graceful shutdown on context cancellation.
	go func() {
		<-ctx.Done()
		s.ptyMgr.closeAll()
		s.broker.Close()
		s.wsBroker.Close()
		_ = srv.Shutdown(context.Background())
	}()

	config.Logger().Printf("[web] server starting on http://%s", addr)
	fmt.Printf("🌐 jcode web server running at http://%s\n", addr)
	fmt.Printf("   Press Ctrl+C to stop\n")

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// forwardEvents reads from the WebHandler event channel and broadcasts to SSE and WebSocket clients.
func (s *Server) forwardEvents() {
	for ev := range s.handler.Events() {
		s.broker.Broadcast(SSEEvent{
			Event: ev.Event,
			Data:  ev.Data,
		})
		s.wsBroker.Broadcast(WSEvent{
			Type: ev.Event,
			Data: ev.Data,
		})
	}
}

// --- API Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"version":  "0.2.0",
		"pwd":      s.pwd,
		"provider": s.providerName,
		"model":    s.modelName,
		"mode":     s.mode,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"running":    s.running.Load(),
		"clients":    s.broker.ClientCount(),
		"ws_clients": s.wsBroker.ClientCount(),
		"pwd":        s.pwd,
		"provider":   s.providerName,
		"model":      s.modelName,
		"mode":       s.mode,
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	events, unsub := s.broker.Subscribe()
	defer unsub()
	ServeSSE(w, r, events)
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "agent is already processing a request",
		})
		return
	}

	var req struct {
		Message string `json:"message"`
		Mode    string `json:"mode,omitempty"` // "build" or "plan"
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = s.mode
	}

	s.submitMessage(req.Message, mode, "")
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "processing"})
}

// SubmitMessage submits a message for agent processing from an external source
// (e.g. WeChat inbound message). Returns false if the agent is busy.
func (s *Server) SubmitMessage(message, source string) bool {
	if s.running.Load() {
		return false
	}
	s.submitMessage(message, s.mode, source)
	return true
}

// submitMessage is the shared implementation for starting an agent run.
// source is an optional label (e.g. "wechat") for the user_message event.
func (s *Server) submitMessage(message, mode, source string) {
	s.running.Store(true)

	// Apply mode prefix if plan mode requested.
	agentMsg := message
	if mode == "plan" {
		agentMsg = "[PLAN MODE — Read-only. Analyze the codebase and create a plan. Do NOT edit, write, or delete any files.]\n\n" + agentMsg
	}

	// Emit user_message event for external sources (e.g. WeChat) so web clients see it.
	// Web-originated messages are already added by the frontend's sendMessage().
	if source != "" {
		s.handler.Emit("user_message", map[string]string{
			"content": message,
			"source":  source,
		})
	}

	// Ensure a recorder exists (lazy creation on first message).
	if s.recorder == nil {
		rec, _ := session.NewRecorder(s.pwd, s.providerName, s.modelName)
		s.recorder = rec
	}

	// Record user message.
	if s.recorder != nil {
		s.recorder.RecordUser(agentMsg)
	}

	s.mu.Lock()
	s.history = append(s.history, schema.UserMessage(agentMsg))
	history := make([]adk.Message, len(s.history))
	copy(history, s.history)
	agent := s.agent
	s.mu.Unlock()

	// Stream response via SSE — run agent in background.
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

		resp := runner.Run(runCtx, agent, history, s.eventHandler, s.recorder, s.todoStore, s.tracer, nil)
		if resp != "" {
			s.mu.Lock()
			s.history = append(s.history, &schema.Message{Role: schema.Assistant, Content: resp})
			s.mu.Unlock()
		}
	}()
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
	if err := session.DeleteSession(s.pwd, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleNewSession(w http.ResponseWriter, r *http.Request) {
	if s.running.Load() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent is currently running"})
		return
	}

	// Parse optional request body for resume session ID.
	var req struct {
		SessionID string `json:"session_id,omitempty"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)

	s.mu.Lock()
	// Close the old recorder.
	if s.recorder != nil {
		s.recorder.Close()
		s.recorder = nil
	}

	// Create new recorder.
	rec, _ := session.NewRecorder(s.pwd, s.providerName, s.modelName)
	if rec != nil {
		// If resuming an existing session, use its UUID.
		if req.SessionID != "" {
			rec.SetUUID(req.SessionID)
		}
		s.recorder = rec
	}

	// Prepare todo items while holding the lock, but apply them after unlocking
	// to avoid deadlock: todoStore.Update → OnUpdate → s.mu.RLock.
	var updateTodos bool
	var todoItems []tools.TodoItem
	if req.SessionID != "" {
		// Resuming: load prior conversation into history so the agent has context.
		entries, _ := session.LoadSession(req.SessionID)
		// Reconstruct full message history (including tool calls/results).
		s.history = session.ReconstructHistory(entries)
		// Restore todos from the last snapshot in the session.
		if s.todoStore != nil {
			var lastTodos []session.TodoSnapshotItem
			for _, e := range entries {
				if e.Type == session.EntryTodoSnapshot {
					lastTodos = e.Todos
				}
			}
			if len(lastTodos) > 0 {
				updateTodos = true
				todoItems = make([]tools.TodoItem, len(lastTodos))
				for i, t := range lastTodos {
					todoItems[i] = tools.TodoItem{ID: t.ID, Title: t.Title, Status: tools.TodoStatus(t.Status)}
				}
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

	// Notify clients. When resuming an existing session, do NOT broadcast session_reset
	// (which would wipe the UI that the frontend is about to repopulate from history).
	if req.SessionID == "" {
		s.broker.Broadcast(SSEEvent{Event: "session_reset", Data: map[string]string{}})
		s.wsBroker.Broadcast(WSEvent{Type: "session_reset", Data: map[string]string{}})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
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
		ID           string `json:"id"`
		Name         string `json:"name"`
		ToolCall     bool   `json:"tool_call"`
		ContextLimit int    `json:"context_limit,omitempty"`
	}
	type providerInfo struct {
		ID     string      `json:"id"`
		Name   string      `json:"name"`
		Models []modelInfo `json:"models"`
	}

	var result []providerInfo
	providers := s.cfg.GetProviders()
	for name := range providers {
		models := s.registry.ListProviderModels(name, true)
		if len(models) == 0 {
			continue
		}
		pi := providerInfo{ID: name, Name: name}
		for _, m := range models {
			ctx := 0
			if m.Limit != nil {
				ctx = m.Limit.Context
			}
			pi.Models = append(pi.Models, modelInfo{
				ID: m.ID, Name: m.Name, ToolCall: m.ToolCall, ContextLimit: ctx,
			})
		}
		result = append(result, pi)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })

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

	// Notify clients.
	s.broker.Broadcast(SSEEvent{Event: "model_changed", Data: map[string]string{
		"provider": req.Provider,
		"model":    req.Model,
	}})
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
	if req.Mode != "build" && req.Mode != "plan" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be 'build' or 'plan'"})
		return
	}
	s.mode = req.Mode

	s.broker.Broadcast(SSEEvent{Event: "mode_changed", Data: map[string]string{
		"mode": req.Mode,
	}})
	s.wsBroker.Broadcast(WSEvent{Type: "mode_changed", Data: map[string]string{
		"mode": req.Mode,
	}})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": req.Mode})
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

func (s *Server) handleApproval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		Approved bool   `json:"approved"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if err := s.handler.ResolveApproval(req.ID, req.Approved); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

func (s *Server) handleListMCP(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil || s.cfg.MCPServers == nil {
		writeJSON(w, http.StatusOK, map[string]any{"servers": map[string]any{}})
		return
	}

	type mcpInfo struct {
		Type    string `json:"type"`
		Command string `json:"command,omitempty"`
		URL     string `json:"url,omitempty"`
		Status  string `json:"status"`
		Enabled bool   `json:"enabled"`
	}

	servers := make(map[string]mcpInfo)
	for name, srv := range s.cfg.MCPServers {
		servers[name] = mcpInfo{
			Type:    srv.Type,
			Command: srv.Command,
			URL:     srv.URL,
			Status:  "configured",
			Enabled: !s.disabledMCP[name],
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
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

	if s.cfg == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no config loaded"})
		return
	}

	if req.Enabled {
		delete(s.disabledMCP, name)
	} else {
		s.disabledMCP[name] = true
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "name": name, "enabled": req.Enabled})
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
	s.broker.Broadcast(SSEEvent{
		Event: "project_switched",
		Data: map[string]string{
			"pwd": req.Path,
		},
	})
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
	if s.approvalState != nil {
		s.approvalState.SetSessionApproval(req.AutoApprove)
	}
	s.broker.Broadcast(SSEEvent{
		Event: "approval_mode_changed",
		Data:  map[string]any{"auto_approve": req.AutoApprove},
	})
	s.wsBroker.Broadcast(WSEvent{
		Type: "approval_mode_changed",
		Data: map[string]any{"auto_approve": req.AutoApprove},
	})
	writeJSON(w, http.StatusOK, map[string]any{"auto_approve": req.AutoApprove})
}

// --- WebSocket handler ---

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
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
			ID       string `json:"id"`
			Approved bool   `json:"approved"`
		}
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			return
		}
		_ = s.handler.ResolveApproval(data.ID, data.Approved)
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
		Slash       string `json:"slash,omitempty"`
	}

	var items []skillItem
	if s.skillLoader != nil {
		for _, sk := range s.skillLoader.All() {
			items = append(items, skillItem{
				Name:        sk.Name,
				Description: sk.Description,
				Slash:       sk.Slash,
			})
		}
	}
	if items == nil {
		items = []skillItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
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
