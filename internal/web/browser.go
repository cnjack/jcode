package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/cnjack/jcode/internal/browser"
	"github.com/cnjack/jcode/internal/config"
)

// extWSURL is the WebSocket URL the extension should dial for this server. Uses
// a loopback host when bound to a wildcard/loopback address.
func (s *Server) extWSURL() string {
	host := s.host
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("ws://%s:%d/api/browser/ext/ws", host, s.port)
}

// SetupNativeMessaging (re)writes the endpoint discovery file with a fresh token
// and installs/refreshes the native-host manifest so the extension can
// auto-connect via chrome.runtime.connectNative. Best-effort; logs on failure.
// Called at startup (when browser use is enabled) and when settings enable it.
func (s *Server) SetupNativeMessaging() {
	if s.browserMgr == nil || !s.browserMgr.GetConfig().Enabled {
		return
	}
	// Reuse one long-lived token across restarts (the extension stores it once);
	// with a stable port the extension reconnects silently, no re-auth.
	token := s.browserMgr.Bridge().StableToken()
	if err := browser.WriteEndpoint(s.extWSURL(), token); err != nil {
		config.Logger().Printf("[browser] write endpoint failed: %v", err)
	}
	binPath, err := os.Executable()
	if err != nil {
		config.Logger().Printf("[browser] resolve executable failed: %v", err)
		return
	}
	if err := browser.InstallNativeHost(binPath); err != nil {
		config.Logger().Printf("[browser] install native host failed: %v", err)
	}
}

// browserConfigToManager maps the persisted config into the manager's Config.
func browserConfigToManager(bc *config.BrowserConfig) browser.Config {
	if bc == nil {
		return browser.Config{Backend: "auto"}
	}
	backend := bc.Backend
	if backend == "" {
		backend = "auto"
	}
	return browser.Config{
		Enabled:    bc.Enabled,
		Backend:    backend,
		ChromePath: bc.ChromePath,
		Headless:   bc.Headless,
		Viewport:   bc.Viewport,
		DevMode:    bc.DevMode,
	}
}

func (s *Server) handleBrowserStatus(w http.ResponseWriter, r *http.Request) {
	if s.browserMgr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	st := s.browserMgr.Status(r.Context())
	// Merge the persisted site permissions/approval so the UI can render them.
	var sitePerms []config.BrowserSitePermission
	var approval map[string]string
	s.mu.Lock()
	if s.cfg != nil && s.cfg.Browser != nil {
		sitePerms = s.cfg.Browser.SitePermissions
		approval = s.cfg.Browser.Approval
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"available":        true,
		"status":           st,
		"site_permissions": sitePerms,
		"approval":         approval,
	})
}

func (s *Server) handleBrowserConfig(w http.ResponseWriter, r *http.Request) {
	if s.browserMgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "browser use unavailable"})
		return
	}
	var req config.BrowserConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Backend == "" {
		req.Backend = "auto"
	}

	s.cfgMu.Lock()
	s.mu.Lock()
	if s.cfg == nil {
		s.mu.Unlock()
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	s.cfg.Browser = &req
	err := config.SaveConfig(s.cfg)
	s.mu.Unlock()
	s.cfgMu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.browserMgr.SetConfig(browserConfigToManager(&req))
	// Enabling browser use should make native auto-connect available without a
	// restart: refresh the endpoint file + native-host manifest now.
	if req.Enabled {
		s.SetupNativeMessaging()
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleBrowserExtWS is the extension bridge websocket. It is auth-exempt (the
// extension authenticates via its own pairing/token in the first frame).
func (s *Server) handleBrowserExtWS(w http.ResponseWriter, r *http.Request) {
	if s.browserMgr == nil {
		http.Error(w, "browser use unavailable", http.StatusServiceUnavailable)
		return
	}
	s.browserMgr.Bridge().HandleWS(w, r)
}

// handleBrowserShot serves a saved screenshot by id.
func (s *Server) handleBrowserShot(w http.ResponseWriter, r *http.Request) {
	if s.browserMgr == nil {
		http.NotFound(w, r)
		return
	}
	id := r.PathValue("id")
	// Path values may include the .png the frontend appends; trim it.
	if len(id) > 4 && id[len(id)-4:] == ".png" {
		id = id[:len(id)-4]
	}
	path := s.browserMgr.ScreenshotPath(id)
	if path == "" {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}
