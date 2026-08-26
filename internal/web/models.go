package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cnjack/jcode/internal/cloud"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/providerauth"
	"github.com/cnjack/jcode/internal/providertools"
)

func modelModalities(m *model.RegistryModel) (input, output []string) {
	if m != nil && m.Modalities != nil {
		input = append([]string(nil), m.Modalities.Input...)
		output = append([]string(nil), m.Modalities.Output...)
	}
	if len(input) == 0 {
		input = []string{"text"}
		if m != nil && m.Attachment {
			input = append(input, "image")
		}
	}
	if len(output) == 0 {
		output = []string{"text"}
	}
	return input, output
}

func hasModality(modalities []string, target string) bool {
	for _, item := range modalities {
		if item == target {
			return true
		}
	}
	return false
}

func appendModality(modalities []string, value string) []string {
	if hasModality(modalities, value) {
		return modalities
	}
	return append(modalities, value)
}

func splitModelReference(ref string) (provider, modelID string) {
	provider, modelID, ok := strings.Cut(ref, "/")
	if !ok || provider == "" || modelID == "" {
		return "", ""
	}
	return provider, modelID
}

func configuredImageAvailability(pc *config.ProviderConfig, imageModel providertools.ImageModel) string {
	managedXAI := pc != nil && pc.Auth != nil && pc.Auth.Method == string(providerauth.MethodXAIOAuth) &&
		imageModel.Provider == "xai"
	if pc == nil || (pc.APIKey == "" && !managedXAI) {
		return "unsupported"
	}
	if managedXAI {
		manager, err := providerauth.Default(config.ConfigDir())
		if err != nil || manager.ValidateBinding(context.Background(), providerauth.Binding{
			Method: providerauth.MethodXAIOAuth, AccountID: pc.Auth.AccountID,
		}) != nil {
			return "unsupported"
		}
	}
	if !imageModel.Supported {
		return "unknown"
	}
	if !imageModel.Builtin {
		normalized, err := validateImageEndpoint(pc.ImageEndpoint)
		if err != nil || normalized.Protocol != imageModel.Protocol {
			return "unknown"
		}
	}
	return "supported"
}

func imageModelSelectable(cfg *config.Config, providerID, modelID string) bool {
	if cfg == nil {
		return false
	}
	pc := cfg.GetProviders()[providerID]
	for _, candidate := range providertools.ImageModels(cfg) {
		if candidate.Provider == providerID && candidate.ID == modelID && configuredImageAvailability(pc, candidate) == "supported" {
			return true
		}
	}
	return false
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	curProvider, curModel := "", ""
	taskID := r.URL.Query().Get("task_id")
	eng := s.resolveEngine(taskID)
	if taskID != "" && eng == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if eng != nil {
		curProvider, curModel, _ = eng.modelSnapshot()
	}
	// Snapshot pointers under cfgMu — setup/provider handlers reassign s.cfg and
	// s.registry under that lock after SaveConfig.
	s.cfgMu.Lock()
	cfg := s.cfg
	s.cfgMu.Unlock()
	if cfg == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"current":       map[string]string{"provider": curProvider, "model": curModel},
			"current_image": map[string]string{"provider": "", "model": ""},
			"providers":     []any{},
		})
		return
	}

	type modelInfo struct {
		ID                     string                  `json:"id"`
		Name                   string                  `json:"name"`
		ToolCall               bool                    `json:"tool_call"`
		ContextLimit           int                     `json:"context_limit,omitempty"`
		Reasoning              bool                    `json:"reasoning,omitempty"`
		Recommended            bool                    `json:"recommended,omitempty"`
		DefaultEnabled         bool                    `json:"default_enabled,omitempty"`
		Enabled                bool                    `json:"enabled"`
		ImageSupport           bool                    `json:"image_support,omitempty"`
		ReasoningOptions       []model.ReasoningOption `json:"reasoning_options,omitempty"`
		InputModalities        []string                `json:"input_modalities"`
		OutputModalities       []string                `json:"output_modalities"`
		CapabilityAvailability string                  `json:"capability_availability"`
		ImageSizes             []string                `json:"image_sizes,omitempty"`
		ImageAspectRatios      []string                `json:"image_aspect_ratios,omitempty"`
		ImageResolutions       []string                `json:"image_resolutions,omitempty"`
	}
	type providerInfo struct {
		ID        string      `json:"id"`
		Name      string      `json:"name"`
		Kind      string      `json:"kind"`
		Source    string      `json:"source"`
		Scope     string      `json:"scope,omitempty"`
		ScopeID   string      `json:"scope_id,omitempty"`
		ScopeName string      `json:"scope_name,omitempty"`
		Custom    bool        `json:"custom,omitempty"`
		Models    []modelInfo `json:"models"`
	}

	modelState, _ := config.LoadModelState()

	// Rebuild the registry from the live config so models added at runtime
	// (custom models saved via the providers API) appear immediately in the chat
	// model picker. The startup registry (s.registry) is a snapshot and would not
	// reflect these additions until a restart.
	registry := model.NewModelRegistryWithConfig(cfg)

	var result []providerInfo
	configuredProviders := cfg.GetProviders()
	imageModelsByProvider := make(map[string][]providertools.ImageModel)
	for _, imageModel := range providertools.ImageModels(cfg) {
		imageModelsByProvider[imageModel.Provider] = append(imageModelsByProvider[imageModel.Provider], imageModel)
	}
	providerIndexes := make(map[string]int, len(configuredProviders))
	for _, rp := range registry.ListProviders() {
		pc, configured := configuredProviders[rp.ID]
		if !configured {
			continue
		}
		// Return the complete model catalog. Consumers derive the chat picker from
		// output:text + tool_call and the image picker from resolved output:image;
		// filtering to tool-call models here would hide registry image candidates.
		models := registry.ListProviderModels(rp.ID, false)
		if len(models) == 0 && len(imageModelsByProvider[rp.ID]) == 0 {
			continue
		}
		pi := providerInfo{
			ID: rp.ID, Name: rp.Name, Kind: rp.ID, Source: "desktop", Custom: rp.Custom,
		}
		modelIndexes := make(map[string]int, len(models))
		for _, m := range models {
			ctx := 0
			if m.Limit != nil {
				ctx = m.Limit.Context
			}
			ref := config.ModelRef{Provider: rp.ID, Model: m.ID}
			enabled := modelState.IsModelEnabled(ref, m.DefaultEnabled)
			inputModalities, outputModalities := modelModalities(m)
			availability := "unsupported"
			if hasModality(outputModalities, "image") {
				availability = "unknown"
			}
			pi.Models = append(pi.Models, modelInfo{
				ID: m.ID, Name: m.Name, ToolCall: m.ToolCall, ContextLimit: ctx,
				Reasoning: m.Reasoning, Recommended: m.Recommended,
				DefaultEnabled: m.DefaultEnabled, Enabled: enabled,
				ImageSupport:     hasModality(inputModalities, "image"),
				ReasoningOptions: m.ReasoningOptions,
				InputModalities:  inputModalities, OutputModalities: outputModalities,
				CapabilityAvailability: availability,
			})
			modelIndexes[m.ID] = len(pi.Models) - 1
		}
		if pc != nil {
			for _, imageModel := range imageModelsByProvider[rp.ID] {
				availability := configuredImageAvailability(pc, imageModel)
				if index, exists := modelIndexes[imageModel.ID]; exists {
					entry := &pi.Models[index]
					entry.OutputModalities = appendModality(entry.OutputModalities, "image")
					entry.CapabilityAvailability = availability
					entry.ImageSizes = append([]string(nil), imageModel.Sizes...)
					entry.ImageAspectRatios = append([]string(nil), imageModel.AspectRatios...)
					entry.ImageResolutions = append([]string(nil), imageModel.Resolutions...)
					continue
				}
				pi.Models = append(pi.Models, modelInfo{
					ID: imageModel.ID, Name: imageModel.Name, Enabled: true, DefaultEnabled: true,
					InputModalities: []string{"text"}, OutputModalities: []string{"image"},
					CapabilityAvailability: availability,
					ImageSizes:             append([]string(nil), imageModel.Sizes...),
					ImageAspectRatios:      append([]string(nil), imageModel.AspectRatios...),
					ImageResolutions:       append([]string(nil), imageModel.Resolutions...),
				})
			}
		}
		providerIndexes[rp.ID] = len(result)
		result = append(result, pi)
	}
	// An image-only custom provider may have no chat models and therefore no
	// runtime registry entry. Keep its explicitly configured image catalog
	// visible without teaching the chat registry to route those models.
	for providerID, pc := range configuredProviders {
		imageModels := imageModelsByProvider[providerID]
		if _, seen := providerIndexes[providerID]; seen || pc == nil || len(imageModels) == 0 {
			continue
		}
		name := pc.Name
		if name == "" {
			name = providerID
		}
		pi := providerInfo{ID: providerID, Name: name, Kind: providerID, Source: "desktop", Custom: true}
		for _, imageModel := range imageModels {
			pi.Models = append(pi.Models, modelInfo{
				ID: imageModel.ID, Name: imageModel.Name, Enabled: true, DefaultEnabled: true,
				InputModalities: []string{"text"}, OutputModalities: []string{"image"},
				CapabilityAvailability: configuredImageAvailability(pc, imageModel),
				ImageSizes:             append([]string(nil), imageModel.Sizes...),
				ImageAspectRatios:      append([]string(nil), imageModel.AspectRatios...),
				ImageResolutions:       append([]string(nil), imageModel.Resolutions...),
			})
		}
		providerIndexes[providerID] = len(result)
		result = append(result, pi)
	}
	// Older clients recorded live models discovered from an API-key custom
	// provider only in model_state.json. Those explicit refs are valid runtime
	// routes (the provider supplies the endpoint and credentials), but they do not
	// exist in the config-backed registry and would therefore disappear from the
	// picker after a refresh. Project them as conservative text/tool models. New
	// enables are also persisted into CustomModels by handleToggleModelEnabled;
	// this fallback keeps existing installations working without a migration that
	// mutates config during a GET. Keep it limited to legacy API-key profiles with
	// no declared chat or image models: once config has an authored catalog, it is
	// authoritative and removals from the provider editor must stay removed.
	for _, ref := range modelState.EnabledModels {
		pc, configured := configuredProviders[ref.Provider]
		if !configured || pc == nil || ref.Provider == "" || ref.Model == "" ||
			pc.Auth != nil || strings.TrimSpace(pc.BaseURL) == "" || pc.HasConfiguredChatModels() ||
			pc.ImageEndpoint != nil || !modelState.IsModelEnabled(ref, false) ||
			!providerIsCustom(registry, ref.Provider) {
			continue
		}
		providerIndex, exists := providerIndexes[ref.Provider]
		if !exists {
			name := pc.Name
			if name == "" {
				name = ref.Provider
			}
			providerIndex = len(result)
			providerIndexes[ref.Provider] = providerIndex
			result = append(result, providerInfo{
				ID: ref.Provider, Name: name, Kind: ref.Provider, Source: "desktop", Custom: true,
			})
		}
		alreadyListed := false
		for _, candidate := range result[providerIndex].Models {
			if candidate.ID == ref.Model {
				alreadyListed = true
				break
			}
		}
		if alreadyListed {
			continue
		}
		result[providerIndex].Models = append(result[providerIndex].Models, modelInfo{
			ID: ref.Model, Name: ref.Model, ToolCall: true, Enabled: true,
			InputModalities: []string{"text"}, OutputModalities: []string{"text"},
			CapabilityAvailability: "unsupported",
		})
	}
	imageProvider, imageModel := splitModelReference(cfg.ImageModel)

	response := map[string]any{
		"current":       map[string]string{"provider": curProvider, "model": curModel},
		"current_image": map[string]string{"provider": imageProvider, "model": imageModel},
		"providers":     result,
	}

	// Cloud providers are an additional catalog. A Cloud outage or logged-out
	// Desktop must never hide or disable local providers.
	if s.cloudStatus().LoggedIn {
		cloudCtx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		cloudModels, err := cloud.ListConfiguredCloudModels(cloudCtx, cfg)
		if err != nil {
			response["cloud_error"] = err.Error()
		} else {
			byProvider := make(map[string]int)
			for _, cloudModel := range cloudModels {
				providerRef := cloud.CloudProviderRef(cloudModel.ProviderID)
				index, exists := byProvider[providerRef]
				if !exists {
					result = append(result, providerInfo{
						ID: providerRef, Name: cloudModel.ProviderName,
						Kind: cloudModel.Kind, Source: "cloud",
						Scope: cloudModel.Scope, ScopeID: cloudModel.ScopeID,
						ScopeName: cloudModel.ScopeName,
					})
					index = len(result) - 1
					byProvider[providerRef] = index
				}
				cloudInputModalities := []string{"text"}
				if cloudModel.Capabilities.Image {
					cloudInputModalities = append(cloudInputModalities, "image")
				}
				result[index].Models = append(result[index].Models, modelInfo{
					ID: cloudModel.ModelID, Name: cloudModel.ModelName,
					ToolCall:       cloudModel.Capabilities.Tools,
					ContextLimit:   cloudModel.ContextWindow,
					Reasoning:      cloudModel.Capabilities.Reasoning,
					DefaultEnabled: true, Enabled: true,
					ImageSupport:     cloudModel.Capabilities.Image,
					ReasoningOptions: cloudModel.ReasoningOptions,
					InputModalities:  cloudInputModalities,
					OutputModalities: []string{"text"}, CapabilityAvailability: "unsupported",
				})
			}
			response["providers"] = result
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSwitchModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		TaskID   string `json:"task_id,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	eng := s.resolveEngine(req.TaskID)
	if eng == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	// No running gate: applyModelSwitch swaps eng.agent under eng.emu (the lock the
	// run reads it under), so a mid-run switch is safe and takes effect next turn —
	// consistent with mode/approval switching.
	if req.Provider == "" || req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and model are required"})
		return
	}
	// Treat selecting the active model as a no-op. Cloud composers include a
	// model reference while applying model-scoped effort, and older clients may
	// resend their visible selection on every message. Rebuilding the agent and
	// broadcasting model_changed in that case only creates duplicate timeline
	// rows and unnecessary config writes.
	curProvider, curModel, _ := eng.modelSnapshot()
	if curProvider == req.Provider && curModel == req.Model {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Rebuild THIS task's agent for the new model and swap it in under eng.emu
	// (the same lock submitMessage uses to read the agent). Keep history.
	eng.rebuildMu.Lock()
	ag, err := eng.createAgent(req.Provider, req.Model)
	if err != nil {
		eng.rebuildMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	eng.applyModelSwitch(ag, req.Provider, req.Model)
	eng.rebuildMu.Unlock()
	if req.TaskID != "" {
		s.wsBroker.Broadcast(WSEvent{Type: "model_changed", TaskID: eng.taskID, Data: map[string]string{
			"provider": req.Provider,
			"model":    req.Model,
		}})
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Persist the selection so a restart resumes on this model — matches the
	// TUI model picker, which writes cfg.Model on every switch. In-place on the
	// shared cfg under cfgMu (same discipline as handleSetSmallModel).
	s.cfgMu.Lock()
	s.mu.Lock()
	var persistErr error
	if s.cfg != nil {
		prevModel := s.cfg.Model
		s.cfg.Model = req.Provider + "/" + req.Model
		if err := config.SaveConfig(s.cfg); err != nil {
			// Keep memory consistent with disk: a failed save must not leave the
			// live config advertising a value that won't survive a restart.
			s.cfg.Model = prevModel
			persistErr = err
		}
	}
	s.mu.Unlock()
	s.cfgMu.Unlock()
	if persistErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config: " + persistErr.Error()})
		return
	}
	s.syncAccountSettingsBestEffort(r)

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
		Mode   string `json:"mode"`
		TaskID string `json:"task_id,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	// Accept only the four canonical unified mode ids.
	switch req.Mode {
	case "approval", "plan", "auto", "full_access":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be 'approval', 'plan', 'auto', or 'full_access'"})
		return
	}
	sm := mode.Parse(req.Mode)

	eng := s.resolveEngine(req.TaskID)
	if eng == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	// No running gate: applyModeSwitch writes eng.agent under eng.emu, the same
	// lock submitMessage reads it under, so a mid-run switch is safe and simply
	// takes effect on the next turn (matching TUI/ACP and the "Allow all" path).
	// Serialize rebuild, durable commit, and publication with every other mode or
	// schema rebuild for this task. Re-check after acquiring the lock because a
	// concurrent Allow-all may have completed while this request was waiting.
	eng.rebuildMu.Lock()
	if eng.curMode() == sm.String() {
		eng.rebuildMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "mode": sm.String()})
		return
	}

	// Rebuild the candidate agent first, but do not publish it yet. The durable
	// mode journal is the commit point: a write/fsync failure leaves the previous
	// agent, approval axis, mode selector, and websocket state untouched.
	var newAg *adk.ChatModelAgent
	if eng.rebuildForMode != nil {
		ag, err := eng.rebuildForMode(sm.IsPlan())
		if err != nil {
			eng.rebuildMu.Unlock()
			config.Logger().Printf("[web] mode switch agent rebuild error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to switch mode"})
			return
		}
		newAg = ag
	}
	if err := eng.recordModeChange(sm.String()); err != nil {
		eng.rebuildMu.Unlock()
		config.Logger().Printf("[web] mode journal commit failed for task %s: %v", eng.taskID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to persist mode change"})
		return
	}
	if eng.approvalState != nil {
		eng.approvalState.SetSessionMode(sm) // approval axis (Full access → auto)
	}
	eng.applyModeSwitch(sm.String(), newAg)
	eng.rebuildMu.Unlock()

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
		"small_model":    cfg.SmallModel,
		"image_model":    cfg.ImageModel,
		"media":          cfg.Media,
		"language":       cfg.Language,
		"theme":          cfg.Theme,
		"max_iterations": cfg.MaxIterations,
	})
}

// handleSetImageModel sets or clears the independent image-generation role.
// Availability remains resolver-driven: storing a model reference does not by
// itself register generate_image when its endpoint/protocol is unresolved.
func (s *Server) handleSetImageModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if (req.Provider == "") != (req.Model == "") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and model must both be set, or both empty to clear"})
		return
	}

	ref := ""
	if req.Provider != "" {
		ref = req.Provider + "/" + req.Model
	}

	s.cfgMu.Lock()
	latest, err := config.MutateConfig(func(cfg *config.Config) error {
		if req.Provider != "" {
			_, ok := cfg.GetProviders()[req.Provider]
			if !ok {
				return newConfigMutationHTTPError(http.StatusBadRequest, "unknown provider: "+req.Provider)
			}
			if !imageModelSelectable(cfg, req.Provider, req.Model) {
				return newConfigMutationHTTPError(http.StatusBadRequest, "image model is not supported by this provider profile")
			}
		}
		cfg.ImageModel = ref
		return nil
	})
	if err == nil {
		s.publishConfigSnapshotLocked(latest)
	}
	s.cfgMu.Unlock()
	if err != nil {
		writeConfigMutationError(w, err)
		return
	}

	// The tool catalog is built into each live agent. Rebuild all task agents so
	// changing an image provider takes effect even when chat uses another one.
	if err := s.rebuildToolAgents(); err != nil {
		s.logProviderApplyFailure(req.Provider, "image model update", err)
		writeSavedButNotApplied(w, "image model selection")
		return
	}
	if s.wsBroker != nil {
		s.wsBroker.Broadcast(WSEvent{Type: "image_model_changed", Data: map[string]string{
			"provider": req.Provider,
			"model":    req.Model,
		}})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleSetSmallModel sets or clears config.small_model (both fields empty =
// clear). It mutates the live shared config IN PLACE (the pointer captured by
// buildWebTask's closure and read by ModelFactory/automation at call time) —
// the s.cfg = fresh reassign pattern would leave those readers on the old
// value until restart. Same discipline as handleBrowserConfig.
func (s *Server) handleSetSmallModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if (req.Provider == "") != (req.Model == "") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and model must both be set, or both empty to clear"})
		return
	}

	ref := ""
	if req.Provider != "" {
		ref = req.Provider + "/" + req.Model
	}

	s.cfgMu.Lock()
	s.mu.Lock()
	if s.cfg == nil {
		s.mu.Unlock()
		s.cfgMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config unavailable"})
		return
	}
	if ref != "" {
		_, isCloud := cloud.ParseCloudProviderRef(req.Provider)
		if _, ok := s.cfg.GetProviders()[req.Provider]; !ok && !isCloud {
			s.mu.Unlock()
			s.cfgMu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown provider: " + req.Provider})
			return
		}
	}
	prev := s.cfg.SmallModel
	s.cfg.SmallModel = ref
	err := config.SaveConfig(s.cfg)
	if err != nil {
		// Keep memory consistent with disk: a failed save must not leave the
		// live config advertising a value that won't survive a restart.
		s.cfg.SmallModel = prev
	}
	s.mu.Unlock()
	s.cfgMu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config: " + err.Error()})
		return
	}
	s.syncAccountSettingsBestEffort(r)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "small_model": ref})
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
	s.syncProviderConfigsBestEffort()

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
	managedConfigChanged := false
	customConfigChanged := false
	if req.Enabled {
		var err error
		managedConfigChanged, err = s.ensureManagedModelConfigured(r.Context(), req.Provider, req.Model)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if !managedConfigChanged {
			customConfigChanged, err = s.ensureCustomModelConfigured(req.Provider, req.Model)
			if err != nil {
				config.Logger().Printf(
					"[models] custom model persistence failed provider=%q model=%q: %v",
					req.Provider, req.Model, err,
				)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save custom model"})
				return
			}
		}
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
	if managedConfigChanged || customConfigChanged {
		if err := s.rebuildProviderDependents(req.Provider, "enable discovered model"); err != nil {
			writeSavedButNotApplied(w, "provider model")
			return
		}
	}
	s.syncProviderConfigsBestEffort()

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": req.Enabled,
	})
}

// ensureCustomModelConfigured persists a model selected from a custom
// API-key provider's live /models catalog. Visibility state alone cannot teach
// the config-backed registry about a previously unknown model, so without this
// row the switch appears to save but the chat picker has nothing to render.
func (s *Server) ensureCustomModelConfigured(providerID, modelID string) (bool, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return false, fmt.Errorf("load config for custom provider %q: %w", providerID, err)
	}
	provider := cfg.GetProviders()[providerID]
	if provider == nil || provider.Auth != nil || strings.TrimSpace(provider.BaseURL) == "" ||
		!providerIsCustom(model.NewModelRegistryWithConfig(cfg), providerID) {
		return false, nil
	}
	for _, existing := range provider.CustomModels {
		if existing.ID == modelID {
			return false, nil
		}
	}

	configChanged := false
	s.cfgMu.Lock()
	configLocked := true
	defer func() {
		if configLocked {
			s.cfgMu.Unlock()
		}
	}()
	latest, err := config.MutateConfig(func(current *config.Config) error {
		pc := current.GetProviders()[providerID]
		if pc == nil || pc.Auth != nil || strings.TrimSpace(pc.BaseURL) == "" {
			return errors.New("custom provider configuration changed while enabling model")
		}
		for _, existing := range pc.CustomModels {
			if existing.ID == modelID {
				return nil
			}
		}
		pc.CustomModels = append(pc.CustomModels, config.CustomModelConfig{
			ID: modelID, Name: modelID, ToolCall: true,
		})
		configChanged = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("persist custom model %q for provider %q: %w", modelID, providerID, err)
	}
	s.publishConfigSnapshotLocked(latest)
	s.cfgMu.Unlock()
	configLocked = false
	return configChanged, nil
}

func (s *Server) ensureManagedModelConfigured(
	ctx context.Context,
	providerID string,
	modelID string,
) (bool, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return false, err
	}
	provider := cfg.GetProviders()[providerID]
	if provider == nil || provider.Auth == nil {
		return false, nil
	}
	existingManaged := false
	for _, existing := range provider.CustomModels {
		if existing.ID == modelID {
			if !existing.Managed {
				return false, nil
			}
			existingManaged = true
			break
		}
	}
	// A provider-native static model needs only a model-state override; it is
	// already available to the runtime registry without a managed metadata row.
	if !existingManaged && s.registry != nil {
		if registryProvider := s.registry.GetProvider(providerID); registryProvider != nil &&
			registryProvider.Models[modelID] != nil {
			return false, nil
		}
	}
	method, err := parseProviderAuthMethod(provider.Auth.Method)
	if err != nil {
		return false, err
	}
	service, err := s.providerAuthService()
	if err != nil {
		return false, err
	}
	liveModels, err := service.Models(ctx, providerauth.Binding{
		Method: method, AccountID: provider.Auth.AccountID,
	})
	if err != nil {
		return false, err
	}
	var discovered *providerauth.Model
	for i := range liveModels {
		if liveModels[i].Kind == providerauth.ModelKindChat && liveModels[i].ID == modelID {
			discovered = &liveModels[i]
			break
		}
	}
	if discovered == nil {
		return false, fmt.Errorf("model %q is no longer available for this account", modelID)
	}
	metadata := managedModelConfigFromLive(s.registry, providerID, *discovered)
	configChanged := false

	s.cfgMu.Lock()
	configLocked := true
	defer func() {
		if configLocked {
			s.cfgMu.Unlock()
		}
	}()
	latest, err := config.MutateConfig(func(current *config.Config) error {
		pc := current.GetProviders()[providerID]
		if pc == nil || pc.Auth == nil || pc.Auth.Method != string(method) ||
			pc.Auth.AccountID != provider.Auth.AccountID {
			return errors.New("managed provider authentication changed while enabling model")
		}
		for index, existing := range pc.CustomModels {
			if existing.ID == modelID {
				if !existing.Managed {
					return nil
				}
				if !reflect.DeepEqual(existing, metadata) {
					pc.CustomModels[index] = metadata
					configChanged = true
				}
				return nil
			}
		}
		pc.CustomModels = append(pc.CustomModels, metadata)
		configChanged = true
		return nil
	})
	if err != nil {
		return false, err
	}
	s.publishConfigSnapshotLocked(latest)
	s.cfgMu.Unlock()
	configLocked = false
	return configChanged, nil
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
	s.syncProviderConfigsBestEffort()

	writeJSON(w, http.StatusOK, map[string]any{
		"effort": req.Effort,
	})
}
