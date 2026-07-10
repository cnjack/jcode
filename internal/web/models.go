package web

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/cloudwego/eino/adk"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/model"
)

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
		ID               string                  `json:"id"`
		Name             string                  `json:"name"`
		ToolCall         bool                    `json:"tool_call"`
		ContextLimit     int                     `json:"context_limit,omitempty"`
		Reasoning        bool                    `json:"reasoning,omitempty"`
		Recommended      bool                    `json:"recommended,omitempty"`
		DefaultEnabled   bool                    `json:"default_enabled,omitempty"`
		Enabled          bool                    `json:"enabled"`
		ImageSupport     bool                    `json:"image_support,omitempty"`
		ReasoningOptions []model.ReasoningOption `json:"reasoning_options,omitempty"`
	}
	type providerInfo struct {
		ID     string      `json:"id"`
		Name   string      `json:"name"`
		Custom bool        `json:"custom,omitempty"`
		Models []modelInfo `json:"models"`
	}

	modelState, _ := config.LoadModelState()

	// Rebuild the registry from the live config so models added at runtime
	// (custom models saved via the providers API) appear immediately in the chat
	// model picker. The startup registry (s.registry) is a snapshot and would not
	// reflect these additions until a restart.
	registry := model.NewModelRegistryWithConfig(s.cfg)

	var result []providerInfo
	configuredProviders := s.cfg.GetProviders()
	for _, rp := range registry.ListProviders() {
		if _, configured := configuredProviders[rp.ID]; !configured {
			continue
		}
		models := registry.ListProviderModels(rp.ID, true)
		if len(models) == 0 {
			continue
		}
		pi := providerInfo{ID: rp.ID, Name: rp.Name, Custom: rp.Custom}
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
				ImageSupport:     imageSupport,
				ReasoningOptions: m.ReasoningOptions,
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
		"recent":           recent,
		"favorite":         favorites,
		"enabled_models":   enabledModels,
		"disabled_models":  disabledModels,
		"effort_overrides": state.EffortOverrides,
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

// handleSetModelEffort records the user's reasoning-effort choice for a single
// model (set from the chat model picker). An empty effort clears the override,
// restoring the provider-level default. The agent is rebuilt so the change
// takes effect on the next turn.
func (s *Server) handleSetModelEffort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Effort   string `json:"effort"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Provider == "" || req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and model are required"})
		return
	}
	if req.Effort != "" && !validReasoningEffort(req.Effort) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid effort"})
		return
	}

	state, err := config.LoadModelState()
	if err != nil {
		state = &config.ModelState{}
	}
	state.SetEffortOverride(config.ModelRef{Provider: req.Provider, Model: req.Model}, req.Effort)
	if err := config.SaveModelState(state); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"effort": req.Effort,
	})
}
