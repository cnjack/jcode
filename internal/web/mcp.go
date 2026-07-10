package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/tools"
)

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

func cloneMCPServers(in map[string]*config.MCPServer) map[string]*config.MCPServer {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*config.MCPServer, len(in))
	for name, srv := range in {
		if srv == nil {
			out[name] = nil
			continue
		}
		cp := *srv
		cp.Args = append([]string(nil), srv.Args...)
		cp.Env = append([]string(nil), srv.Env...)
		if srv.Headers != nil {
			cp.Headers = make(map[string]string, len(srv.Headers))
			for k, v := range srv.Headers {
				cp.Headers[k] = v
			}
		}
		if srv.OAuth != nil {
			oa := *srv.OAuth
			oa.Scopes = append([]string(nil), srv.OAuth.Scopes...)
			cp.OAuth = &oa
		}
		out[name] = &cp
	}
	return out
}

// ReloadMCPInBackground connects configured MCP servers without blocking web
// startup. Slow or unreachable MCP servers should update settings/tool state
// when they finish, never delay /api/health or the desktop window.
func (s *Server) ReloadMCPInBackground() {
	if s.reloadMCP == nil {
		return
	}
	go func() {
		config.Logger().Printf("[web] loading MCP tools in background")
		if err := s.reloadMCPAndRebuild(); err != nil {
			config.Logger().Printf("[web] background MCP reload failed: %v", err)
		} else {
			config.Logger().Printf("[web] background MCP reload finished")
		}
		s.wsBroker.Broadcast(WSEvent{Type: "mcp_changed", Data: map[string]string{"source": "startup"}})
	}()
}

// reloadMCPAndRebuild reconnects MCP servers from the current config and
// rebuilds the live agent so new tools take effect without a restart.
func (s *Server) reloadMCPAndRebuild() error {
	if s.reloadMCP != nil {
		s.mu.RLock()
		servers := cloneMCPServers(s.cfg.MCPServers)
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
	ctx, cancel := context.WithTimeout(s.rootCtx(), 5*time.Minute)
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
