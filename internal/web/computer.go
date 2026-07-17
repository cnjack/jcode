package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"

	"github.com/cnjack/jcode/internal/computer"
	"github.com/cnjack/jcode/internal/config"
)

// computerConfigPayload is the public REST contract. It deliberately does not
// contain the deprecated config.ComputerConfig.Backend migration field: there
// is one production implementation, the macOS native helper.
type computerConfigPayload struct {
	Enabled            bool                           `json:"enabled"`
	Approval           map[string]string              `json:"approval,omitempty"`
	AppPermissions     []config.ComputerAppPermission `json:"app_permissions,omitempty"`
	MaxActionsPerBatch int                            `json:"max_actions_per_batch,omitempty"`
	ClipboardRead      bool                           `json:"clipboard_read,omitempty"`
	ClipboardWrite     bool                           `json:"clipboard_write,omitempty"`
	SystemKeyCombos    bool                           `json:"system_key_combos,omitempty"`
}

type computerStatusPayload struct {
	Enabled         bool                     `json:"enabled"`
	Available       bool                     `json:"available"`
	Blocker         string                   `json:"blocker"`
	Detail          string                   `json:"detail,omitempty"`
	MaxBatch        int                      `json:"max_batch"`
	Tiers           map[string]string        `json:"tiers,omitempty"`
	Helper          computer.HelperStatus    `json:"helper"`
	Accessibility   computer.PermissionState `json:"accessibility"`
	ScreenRecording computer.PermissionState `json:"screen_recording"`
}

func computerStatusForAPI(st computer.Status) computerStatusPayload {
	return computerStatusPayload{
		Enabled:         st.Enabled,
		Available:       st.Available,
		Blocker:         st.Blocker,
		Detail:          st.Detail,
		MaxBatch:        st.MaxBatch,
		Tiers:           st.Tiers,
		Helper:          st.Helper,
		Accessibility:   st.AccessibilityPermission,
		ScreenRecording: st.ScreenRecordingPermission,
	}
}

func computerConfigForAPI(c *config.ComputerConfig) computerConfigPayload {
	if c == nil {
		return computerConfigPayload{}
	}
	approval := make(map[string]string, len(c.Approval))
	for class, policy := range c.Approval {
		approval[class] = policy
	}
	return computerConfigPayload{
		Enabled:            c.Enabled,
		Approval:           approval,
		AppPermissions:     append([]config.ComputerAppPermission(nil), c.AppPermissions...),
		MaxActionsPerBatch: c.MaxActionsPerBatch,
		ClipboardRead:      c.ClipboardRead,
		ClipboardWrite:     c.ClipboardWrite,
		SystemKeyCombos:    c.SystemKeyCombos,
	}
}

func (p computerConfigPayload) storedConfig() *config.ComputerConfig {
	return &config.ComputerConfig{
		Enabled:            p.Enabled,
		Approval:           p.Approval,
		AppPermissions:     p.AppPermissions,
		MaxActionsPerBatch: p.MaxActionsPerBatch,
		ClipboardRead:      p.ClipboardRead,
		ClipboardWrite:     p.ClipboardWrite,
		SystemKeyCombos:    p.SystemKeyCombos,
	}
}

func (s *Server) handleComputerStatus(w http.ResponseWriter, r *http.Request) {
	supported := computer.Supported()
	s.mu.RLock()
	var stored *config.ComputerConfig
	if s.cfg != nil {
		stored = s.cfg.Computer
	}
	apiConfig := computerConfigForAPI(stored)
	s.mu.RUnlock()

	response := map[string]any{
		"supported": supported,
		"platform":  runtime.GOOS,
		"available": supported && s.computerMgr != nil, // older UI compatibility
		"config":    apiConfig,
	}
	if !supported {
		response["reason"] = computer.UnsupportedReason()
		response["status"] = map[string]any{
			"enabled":          apiConfig.Enabled,
			"available":        false,
			"blocker":          "unsupported",
			"detail":           computer.UnsupportedReason(),
			"max_batch":        normalizedComputerBatch(apiConfig.MaxActionsPerBatch),
			"helper":           map[string]any{"installed": false, "connected": false},
			"accessibility":    computer.PermissionUnknown,
			"screen_recording": computer.PermissionUnknown,
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	if s.computerMgr == nil {
		response["reason"] = "The native Computer Use helper is unavailable in this session"
		response["status"] = map[string]any{
			"enabled":          apiConfig.Enabled,
			"available":        false,
			"blocker":          "no_helper",
			"detail":           response["reason"],
			"max_batch":        normalizedComputerBatch(apiConfig.MaxActionsPerBatch),
			"helper":           map[string]any{"installed": false, "connected": false},
			"accessibility":    computer.PermissionUnknown,
			"screen_recording": computer.PermissionUnknown,
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	response["status"] = computerStatusForAPI(s.computerMgr.Status(r.Context()))
	writeJSON(w, http.StatusOK, response)
}

func normalizedComputerBatch(value int) int {
	if value <= 0 {
		return 20
	}
	return value
}

func (s *Server) handleComputerConfig(w http.ResponseWriter, r *http.Request) {
	var req computerConfigPayload
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": computerConfigDecodeError(err)})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return
	}
	if err := validateComputerEnable(req.Enabled, computer.Supported()); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateComputerPermissions(req.AppPermissions); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	stored := req.storedConfig()
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	s.mu.Lock()
	if s.cfg == nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	previous := s.cfg.Computer
	s.cfg.Computer = stored
	err := config.SaveConfig(s.cfg)
	if err != nil {
		// The disk write is the commit point. Restore the live config pointer so
		// GET/status cannot claim a failed disable succeeded while the Manager is
		// still running under the previous policy.
		s.cfg.Computer = previous
	}
	s.mu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if s.computerMgr != nil {
		// SetConfig is a native-action boundary: it waits for the in-flight UI
		// operation and invalidates the live policy before this request returns.
		s.computerMgr.SetConfig(computer.FromConfig(stored))
	}
	response := map[string]any{"status": "ok", "config": computerConfigForAPI(stored)}
	// cfgMu intentionally remains held through live policy publication and tool
	// rebuild. Without this, concurrent saves can commit B to disk but publish A
	// to the Manager when A's slower rebuild resumes out of order.
	if err := s.rebuildComputerAgent(); err != nil {
		config.Logger().Printf("[computer] config saved but active-agent tool refresh failed: %v", err)
		response["warning_code"] = "agent_refresh_failed"
	}
	writeJSON(w, http.StatusOK, response)
}

func validateComputerEnable(enabled, supported bool) error {
	if enabled && !supported {
		return fmt.Errorf("%s", computer.UnsupportedReason())
	}
	return nil
}

func computerConfigDecodeError(err error) string {
	if err == io.EOF {
		return "request body is empty"
	}
	return "invalid computer config: " + err.Error()
}

func validateComputerPermissions(perms []config.ComputerAppPermission) error {
	for _, p := range perms {
		if p.Tier == "" {
			continue
		}
		t, ok := computer.ParseTier(p.Tier)
		if !ok {
			return fmt.Errorf("unknown tier %s for %s (want read, click or full)", p.Tier, p.BundleID)
		}
		if def := computer.DefaultTier(p.BundleID); t > def {
			return fmt.Errorf("%s is a %s-tier app and cannot be loosened to %s; tier overrides may only tighten",
				p.BundleID, def.String(), t.String())
		}
	}
	return nil
}

// rebuildComputerAgent refreshes fixed tool schemas for every live task, so
// enabling or disabling Computer Use takes effect without an app restart or a
// foreground-task switch. Runtime policy revocation is already immediate; this
// keeps the model-visible tool surface in sync as well.
func (s *Server) rebuildComputerAgent() error {
	if s.needsSetup {
		return nil
	}
	seen := map[*Engine]struct{}{}
	engines := make([]*Engine, 0, 1)
	if active := s.activeEngine(); active != nil {
		seen[active] = struct{}{}
		engines = append(engines, active)
	}
	s.tasksMu.RLock()
	for _, eng := range s.tasks {
		if eng == nil {
			continue
		}
		if _, exists := seen[eng]; exists {
			continue
		}
		seen[eng] = struct{}{}
		engines = append(engines, eng)
	}
	s.tasksMu.RUnlock()

	var rebuildErrors []error
	for _, eng := range engines {
		if eng.createAgent == nil {
			continue
		}
		provider, modelName, _, revision := eng.agentBuildSnapshot()
		ag, err := eng.createAgent(provider, modelName)
		if err != nil {
			rebuildErrors = append(rebuildErrors, fmt.Errorf("task %s: %w", eng.taskID, err))
			continue
		}
		// A concurrent model/mode/skill switch already installed an agent built
		// from newer inputs. Discard this stale result instead of rolling that
		// task back to the old tool schema.
		eng.installAgentIfRevision(ag, revision)
	}
	return errors.Join(rebuildErrors...)
}

// handleComputerShot serves a saved screenshot by id from the file handle that
// Manager validated and opened under the cross-process cache lock.
func (s *Server) handleComputerShot(w http.ResponseWriter, r *http.Request) {
	if s.computerMgr == nil {
		http.NotFound(w, r)
		return
	}
	id := r.PathValue("id")
	if len(id) > 4 && id[len(id)-4:] == ".png" {
		id = id[:len(id)-4]
	}
	f, err := s.computerMgr.OpenScreenshot(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, id+".png", info.ModTime(), f)
}
