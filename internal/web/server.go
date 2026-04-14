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

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/telemetry"
	"github.com/cnjack/jcode/internal/tools"
)

// Server is the jcode web server.
type Server struct {
	port    int
	host    string
	pwd     string
	handler *handler.WebHandler
	broker  *SSEBroker

	mu      sync.RWMutex
	agent   *adk.ChatModelAgent
	history []adk.Message
	running atomic.Bool

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
}

// NewServer creates a new web server.
func NewServer(cfg *ServerConfig) *Server {
	h := handler.NewWebHandler()
	return &Server{
		port:          cfg.Port,
		host:          cfg.Host,
		pwd:           cfg.Pwd,
		handler:       h,
		broker:        NewSSEBroker(),
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
	}
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
	mux.HandleFunc("POST /api/chat", s.handleChat)
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
	mux.HandleFunc("GET /api/browse", s.handleBrowse)
	mux.HandleFunc("POST /api/project/switch", s.handleSwitchProject)
	mux.HandleFunc("POST /api/pty", s.handleCreatePTY)
	mux.HandleFunc("GET /api/pty", s.handleListPTY)
	mux.HandleFunc("DELETE /api/pty/{id}", s.handleKillPTY)
	mux.HandleFunc("GET /api/pty/{id}/ws", s.handlePTYWebSocket)
	mux.HandleFunc("GET /api/approval/mode", s.handleGetApprovalMode)
	mux.HandleFunc("POST /api/approval/mode", s.handleSetApprovalMode)

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

// forwardEvents reads from the WebHandler event channel and broadcasts to SSE clients.
func (s *Server) forwardEvents() {
	for ev := range s.handler.Events() {
		s.broker.Broadcast(SSEEvent{
			Event: ev.Event,
			Data:  ev.Data,
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
		"running":  s.running.Load(),
		"clients":  s.broker.ClientCount(),
		"pwd":      s.pwd,
		"provider": s.providerName,
		"model":    s.modelName,
		"mode":     s.mode,
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

	s.running.Store(true)

	// Apply mode prefix if plan mode requested.
	message := req.Message
	mode := req.Mode
	if mode == "" {
		mode = s.mode
	}
	if mode == "plan" {
		message = "[PLAN MODE — Read-only. Analyze the codebase and create a plan. Do NOT edit, write, or delete any files.]\n\n" + message
	}

	// Record user message.
	if s.recorder != nil {
		s.recorder.RecordUser(message)
	}

	s.mu.Lock()
	s.history = append(s.history, schema.UserMessage(message))
	history := make([]adk.Message, len(s.history))
	copy(history, s.history)
	agent := s.agent
	s.mu.Unlock()

	// Stream response via SSE — run agent in background.
	// Use server context, not request context (which is canceled when the response is sent).
	go func() {
		defer s.running.Store(false)
		resp := runner.Run(s.ctx, agent, history, s.handler, s.recorder, s.todoStore, s.tracer)
		if resp != "" {
			s.mu.Lock()
			s.history = append(s.history, &schema.Message{Role: schema.Assistant, Content: resp})
			s.mu.Unlock()
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "processing"})
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
	}

	items := make([]sessionItem, 0, len(metas))
	for _, m := range metas {
		items = append(items, sessionItem{
			UUID:      m.UUID,
			CreatedAt: m.StartTime,
			Provider:  m.Provider,
			Model:     m.Model,
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

	type entryItem struct {
		Type    string `json:"type"`
		Content string `json:"content,omitempty"`
		Name    string `json:"name,omitempty"`
	}

	items := make([]entryItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, entryItem{
			Type:    string(e.Type),
			Content: e.Content,
			Name:    e.Name,
		})
	}
	writeJSON(w, http.StatusOK, items)
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

	// Close the old recorder.
	if s.recorder != nil {
		s.recorder.Close()
	}

	// Create a new recorder.
	rec, _ := session.NewRecorder(s.pwd, s.providerName, s.modelName)
	s.recorder = rec

	// Reset conversation history.
	s.mu.Lock()
	s.history = nil
	s.mu.Unlock()

	// Reset todos.
	if s.todoStore != nil {
		s.todoStore.Update(nil)
	}

	// Notify SSE clients.
	s.broker.Broadcast(SSEEvent{Event: "session_reset", Data: map[string]string{
		"session_id": rec.UUID(),
	}})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"session_id": rec.UUID(),
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
	s.history = nil // reset history on model change
	s.mu.Unlock()

	// Reset recorder for new model.
	if s.recorder != nil {
		s.recorder.Close()
	}
	rec, _ := session.NewRecorder(s.pwd, req.Provider, req.Model)
	s.recorder = rec

	// Notify clients.
	s.broker.Broadcast(SSEEvent{Event: "model_changed", Data: map[string]string{
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

func (s *Server) handleListMCP(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil || s.cfg.MCPServers == nil {
		writeJSON(w, http.StatusOK, map[string]any{"servers": map[string]any{}})
		return
	}

	type mcpInfo struct {
		Type    string `json:"type"`
		Command string `json:"command,omitempty"`
		URL     string `json:"url,omitempty"`
		Status  string `json:"status"` // "configured"
	}

	servers := make(map[string]mcpInfo)
	for name, srv := range s.cfg.MCPServers {
		servers[name] = mcpInfo{
			Type:    srv.Type,
			Command: srv.Command,
			URL:     srv.URL,
			Status:  "configured",
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

	if s.cfg.MCPServers == nil {
		s.cfg.MCPServers = make(map[string]*config.MCPServer)
	}

	if !req.Enabled {
		// Mark as disabled by removing from config (in-memory only)
		delete(s.cfg.MCPServers, name)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "name": name})
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

	// Broadcast project change to SSE clients.
	s.broker.Broadcast(SSEEvent{
		Event: "project_switched",
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
	writeJSON(w, http.StatusOK, map[string]any{"auto_approve": req.AutoApprove})
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
