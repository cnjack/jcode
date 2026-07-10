package web

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
)

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
		Provider string            `json:"provider"`
		APIKey   string            `json:"api_key"`
		BaseURL  string            `json:"base_url,omitempty"`
		Headers  map[string]string `json:"headers,omitempty"`
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

	res := model.ValidateProviderDetailed(r.Context(), req.APIKey, baseURL, req.Headers)
	if !res.OK {
		writeJSON(w, http.StatusOK, map[string]any{
			"valid":      false,
			"error":      res.Error,
			"error_type": res.ErrorType,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"valid":       true,
		"latency_ms":  res.LatencyMS,
		"model_count": res.ModelCount,
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
		ID               string                  `json:"id"`
		Name             string                  `json:"name"`
		ToolCall         bool                    `json:"tool_call"`
		ContextLimit     int                     `json:"context_limit,omitempty"`
		Reasoning        bool                    `json:"reasoning,omitempty"`
		Attachment       bool                    `json:"attachment,omitempty"`
		ReasoningOptions []model.ReasoningOption `json:"reasoning_options,omitempty"`
	}

	result := make([]modelItem, 0, len(models))
	for _, m := range models {
		ctx := 0
		if m.Limit != nil {
			ctx = m.Limit.Context
		}
		result = append(result, modelItem{
			ID:               m.ID,
			Name:             m.Name,
			ToolCall:         m.ToolCall,
			ContextLimit:     ctx,
			Reasoning:        m.Reasoning,
			Attachment:       m.Attachment,
			ReasoningOptions: m.ReasoningOptions,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// It saves the provider config and creates the agent.
//
// The wizard no longer forces a model selection: for registry providers, a
// default model is auto-picked (DefaultEnabled → Recommended → first). A
// caller-supplied model always wins. Custom (non-registry) providers must send
// a model explicitly since none can be inferred.
func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider        string            `json:"provider"`
		Model           string            `json:"model,omitempty"`
		ModelReasoning  bool              `json:"model_reasoning,omitempty"`
		APIKey          string            `json:"api_key"`
		BaseURL         string            `json:"base_url,omitempty"`
		Name            string            `json:"name,omitempty"` // custom provider display name
		Headers         map[string]string `json:"headers,omitempty"`
		Vision          *bool             `json:"vision,omitempty"`
		Thinking        *bool             `json:"thinking,omitempty"`
		ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Provider == "" || req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and api_key are required"})
		return
	}
	if !validReasoningEffort(req.ReasoningEffort) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reasoning_effort"})
		return
	}

	// Resolve the active model. An explicit model always wins; otherwise try to
	// auto-pick a default for registry providers. Custom providers (not in the
	// registry) cannot infer a model and require one from the caller.
	resolvedModel := req.Model
	isCustom := s.registry == nil || !s.registry.HasProvider(req.Provider)
	if resolvedModel == "" {
		if isCustom {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model is required for custom providers"})
			return
		}
		if s.registry != nil {
			resolvedModel = s.registry.PickDefaultModel(req.Provider)
		}
	}
	if resolvedModel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no default model available for this provider; please choose one"})
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
	setupPC := &config.ProviderConfig{
		APIKey:          req.APIKey,
		BaseURL:         req.BaseURL,
		Name:            req.Name,
		Headers:         cleanHeaders(req.Headers),
		Vision:          req.Vision,
		Thinking:        req.Thinking,
		ReasoningEffort: req.ReasoningEffort,
	}
	// For a custom provider, persist the model as a custom model so it survives
	// a model switch (otherwise it exists only as the active-model string and
	// vanishes from the picker once changed).
	if isCustom && resolvedModel != "" {
		setupPC.CustomModels = []config.CustomModelConfig{{
			ID:        resolvedModel,
			Name:      resolvedModel,
			ToolCall:  true,
			Reasoning: req.ModelReasoning,
		}}
	}
	cfg.Providers[req.Provider] = setupPC
	cfg.Model = req.Provider + "/" + resolvedModel

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
	ag, err := eng.createAgent(req.Provider, resolvedModel)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create agent: " + err.Error()})
		return
	}
	eng.applyModelSwitch(ag, req.Provider, resolvedModel)
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
		"model":    resolvedModel,
	}})

	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"provider": req.Provider,
		"model":    resolvedModel,
	})
}
