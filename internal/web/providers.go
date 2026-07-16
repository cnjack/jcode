package web

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
)

// handleProviderCatalog returns a provider's browsable model catalog for the
// "browse directory" UI. For registry providers it lists the built-in models
// (the official /models endpoint is not reliably complete); for custom
// (OpenAI-compatible) endpoints it queries the live /models endpoint. Each
// entry is flagged added=true when the model is already in the provider's
// config (either as a CustomModelConfig or a registry model that's enabled).
func (s *Server) handleProviderCatalog(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	if providerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id is required"})
		return
	}

	type catalogEntry struct {
		ID          string   `json:"id"`
		Name        string   `json:"name,omitempty"`
		Added       bool     `json:"added"`
		Context     int      `json:"context,omitempty"`
		Reasoning   bool     `json:"reasoning,omitempty"`
		Attachment  bool     `json:"attachment,omitempty"`
		EffortTiers []string `json:"effort_tiers,omitempty"`
		// Custom marks a user-defined model (editable/removable) vs a built-in
		// registry model (toggled via model_state). Surfaced so the catalog row can
		// show edit/remove affordances only on user custom models.
		Custom bool `json:"custom,omitempty"`
	}

	// Collect the set of already-configured model ids for this provider, so each
	// catalog entry can be flagged added/可移除. Custom models come from config;
	// for registry providers, MergeConfigProviders has already merged custom
	// models into the registry, so registry membership is the source of truth.
	configured := make(map[string]bool)
	customSet := make(map[string]*config.CustomModelConfig) // user-defined models by id
	var apiKey, baseURL string
	var headers map[string]string
	cfg, _ := config.LoadConfig()
	if cfg != nil {
		if pc := cfg.GetProviders()[providerID]; pc != nil {
			apiKey, baseURL, headers = pc.APIKey, pc.BaseURL, pc.Headers
			for _, m := range pc.CustomModels {
				configured[m.ID] = true
				cm := m // copy for map value
				customSet[m.ID] = &cm
			}
		}
	}

	// customEntry builds a catalogEntry from a user-defined CustomModelConfig.
	customEntry := func(id string) catalogEntry {
		m := customSet[id]
		e := catalogEntry{ID: id, Added: true, Custom: true}
		if m != nil {
			e.Name = m.Name
			e.Context = m.Context
			e.Reasoning = m.Reasoning
			e.Attachment = m.Attachment
			e.EffortTiers = m.EffortTiers
		}
		return e
	}

	// resolveRegistryBrand finds a registry provider whose brand keyword appears
	// in the given id/url — so a custom endpoint pointing at, say, zhipu's API
	// still surfaces zhipu's models.dev catalog rather than a fragile live
	// /models probe. Returns "" when no brand matches.
	resolveRegistryBrand := func(hint string) string {
		if s.registry == nil {
			return ""
		}
		hint = strings.ToLower(hint)
		for _, rp := range s.registry.ListProviders() {
			// Use the registry id as the brand keyword (e.g. "zhipuai", "openai",
			// "deepseek"); these already encode the brand and are stable.
			brand := strings.ToLower(rp.ID)
			if brand != "" && strings.Contains(hint, brand) {
				return rp.ID
			}
		}
		return ""
	}

	// The catalog defaults to the built-in (models.dev) catalog — this is the
	// reliable source, since many endpoints either lack a /models route or
	// return an incomplete list. Exact id match first; otherwise a brand match
	// on the provider id or base URL (so a custom zhipu endpoint still shows the
	// zhipu catalog).
	registryID := providerID
	if s.registry == nil || !s.registry.HasProvider(registryID) {
		if hint := resolveRegistryBrand(providerID + " " + baseURL); hint != "" {
			registryID = hint
		}
	}
	if s.registry != nil && s.registry.HasProvider(registryID) {
		models := s.registry.ListProviderModels(registryID, true)
		result := make([]catalogEntry, 0, len(models))
		for _, m := range models {
			// A user-defined custom model is merged into the registry by
			// MergeConfigProviders, so it appears here too. For those, build the
			// entry from the stored CustomModelConfig (which carries the
			// user-set name/context/tiers) rather than the derived registry view —
			// otherwise effort tiers and other authored fields are lost.
			if cm := customSet[m.ID]; cm != nil {
				result = append(result, catalogEntry{
					ID:          m.ID,
					Name:        cm.Name,
					Added:       true,
					Context:     cm.Context,
					Reasoning:   cm.Reasoning,
					Attachment:  cm.Attachment,
					EffortTiers: cm.EffortTiers,
					Custom:      true,
				})
				continue
			}
			ctx := 0
			if m.Limit != nil {
				ctx = m.Limit.Context
			}
			result = append(result, catalogEntry{
				ID:         m.ID,
				Name:       m.Name,
				Added:      configured[m.ID] || m.DefaultEnabled,
				Context:    ctx,
				Reasoning:  m.Reasoning,
				Attachment: m.Attachment,
				Custom:     false,
			})
		}
		// Also surface any user-added custom models not in the brand catalog, so
		// the catalog isn't missing models the user explicitly configured.
		for id := range configured {
			found := false
			for _, e := range result {
				if e.ID == id {
					found = true
					break
				}
			}
			if !found {
				result = append(result, customEntry(id))
			}
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	// Truly custom endpoint with no brand match: probe the live /models endpoint
	// as a last resort. Many gateways support the OpenAI-compatible /models list;
	// on any failure we fall back to just the configured models so the catalog is
	// never empty/erroring.
	if baseURL != "" {
		if ids := model.ListProviderModelsLive(r.Context(), apiKey, baseURL, headers); len(ids) > 0 {
			result := make([]catalogEntry, 0, len(ids))
			seen := make(map[string]bool, len(ids))
			for _, id := range ids {
				if seen[id] {
					continue
				}
				seen[id] = true
				if c := customSet[id]; c != nil {
					result = append(result, customEntry(id))
				} else {
					result = append(result, catalogEntry{ID: id, Added: configured[id]})
				}
			}
			writeJSON(w, http.StatusOK, result)
			return
		}
	}

	// No registry brand and no live /models: show the configured custom models
	// (added=true, custom=true) so the catalog reflects what's actually usable.
	result := make([]catalogEntry, 0, len(configured))
	for id := range configured {
		result = append(result, customEntry(id))
	}
	writeJSON(w, http.StatusOK, result)
}

// maskSecret hides a secret for display: first 4 and last 4 chars for longer
// values, "****" for short ones. Used for API keys and header values so the
// list endpoint never returns plaintext credentials.
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > 8 {
		return s[:4] + "..." + s[len(s)-4:]
	}
	return "****"
}

// handleListProviders returns all configured providers (key masked).
func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadConfig()
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	type customModelView struct {
		ID          string   `json:"id"`
		Name        string   `json:"name,omitempty"`
		Reasoning   bool     `json:"reasoning,omitempty"`
		Context     int      `json:"context,omitempty"`
		Attachment  bool     `json:"attachment,omitempty"`
		EffortTiers []string `json:"effort_tiers,omitempty"`
		// Custom marks a user-defined model (editable) vs a built-in registry
		// model surfaced for display (read-only). Omitted/zero ⇒ treated as a
		// user custom model for backward compatibility, but we always set it.
		Custom bool `json:"custom,omitempty"`
	}
	type providerDetail struct {
		ID              string            `json:"id"`
		Name            string            `json:"name,omitempty"` // display name for custom providers
		Custom          bool              `json:"custom,omitempty"`
		APIKeySet       bool              `json:"api_key_set"`
		APIKey          string            `json:"api_key,omitempty"` // masked
		BaseURL         string            `json:"base_url,omitempty"`
		Headers         map[string]string `json:"headers,omitempty"` // values masked
		CustomModels    []customModelView `json:"custom_models,omitempty"`
		Vision          *bool             `json:"vision,omitempty"`
		Thinking        *bool             `json:"thinking,omitempty"`
		ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	}

	result := make([]providerDetail, 0)
	for id, pc := range cfg.GetProviders() {
		detail := providerDetail{
			ID:              id,
			Name:            pc.Name,
			APIKeySet:       pc.APIKey != "",
			BaseURL:         pc.BaseURL,
			Vision:          pc.Vision,
			Thinking:        pc.Thinking,
			ReasoningEffort: pc.ReasoningEffort,
		}
		// A provider is "custom" when it exists only because the user configured
		// it (an OpenAI-compatible endpoint), not as a built-in registry brand.
		// MergeConfigProviders flags those on the registry entry; a configured id
		// with no registry entry at all is custom too. The registry may be nil in
		// setup mode — fall back to "has a display name" as the custom signal.
		if s.registry != nil {
			if prov := s.registry.GetProvider(id); prov != nil {
				detail.Custom = prov.Custom
			} else {
				detail.Custom = true
			}
		} else if pc.Name != "" {
			detail.Custom = true
		}
		if pc.APIKey != "" {
			detail.APIKey = maskSecret(pc.APIKey)
		}
		if len(pc.Headers) > 0 {
			masked := make(map[string]string, len(pc.Headers))
			for k, v := range pc.Headers {
				masked[k] = maskSecret(v)
			}
			detail.Headers = masked
		}
		// Build the card's unified model list. For registry providers this is the
		// built-in (models.dev) models the user has enabled, each marked read-only
		// (Custom=false); the user's CustomModels are then appended and marked
		// editable (Custom=true). The card renders them identically and only
		// surfaces edit/delete affordances on editable rows.
		cms := make([]customModelView, 0)
		seen := make(map[string]bool)
		// Track which ids are user-defined custom models so the registry loop can
		// skip them (they're merged into the registry by MergeConfigProviders but
		// must surface with their authored fields from CustomModelConfig, handled
		// in the custom loop below).
		customIDs := make(map[string]bool, len(pc.CustomModels))
		for _, m := range pc.CustomModels {
			customIDs[m.ID] = true
		}
		if !detail.Custom && s.registry != nil && s.registry.HasProvider(id) {
			for _, m := range s.registry.ListProviderModels(id, true) {
				if !m.DefaultEnabled {
					continue
				}
				if customIDs[m.ID] {
					continue // user custom model — emitted below with authored fields
				}
				if seen[m.ID] {
					continue
				}
				seen[m.ID] = true
				ctx := 0
				if m.Limit != nil {
					ctx = m.Limit.Context
				}
				cms = append(cms, customModelView{
					ID:         m.ID,
					Name:       m.Name,
					Reasoning:  m.Reasoning,
					Context:    ctx,
					Attachment: m.Attachment,
					Custom:     false,
				})
			}
		}
		for _, m := range pc.CustomModels {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			cms = append(cms, customModelView{
				ID:          m.ID,
				Name:        m.Name,
				Reasoning:   m.Reasoning,
				Context:     m.Context,
				Attachment:  m.Attachment,
				EffortTiers: m.EffortTiers,
				Custom:      true,
			})
		}
		if len(cms) > 0 {
			detail.CustomModels = cms
		}
		result = append(result, detail)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })

	writeJSON(w, http.StatusOK, result)
}

// handleAddProvider adds a new provider to the config. For custom
// (non-registry) providers the caller should also send a name; models are
// optional here and are added afterward from the provider card (the dialog only
// captures the connection).
func (s *Server) handleAddProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID              string            `json:"id"`
		APIKey          string            `json:"api_key"`
		BaseURL         string            `json:"base_url,omitempty"`
		Name            string            `json:"name,omitempty"`
		Model           string            `json:"model,omitempty"`
		ModelReasoning  bool              `json:"model_reasoning,omitempty"`
		Headers         map[string]string `json:"headers,omitempty"`
		Vision          *bool             `json:"vision,omitempty"`
		Thinking        *bool             `json:"thinking,omitempty"`
		ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.ID == "" || req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id and api_key are required"})
		return
	}
	if !validReasoningEffort(req.ReasoningEffort) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reasoning_effort"})
		return
	}

	// Serialize config RMW + live publish under cfgMu (see Server.cfgMu).
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = &config.Config{MaxIterations: 1000}
	}
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]*config.ProviderConfig)
	}

	// A custom provider (not in the registry) needs a base URL so requests can
	// be routed. Models are optional at creation time: the provider is created
	// connection-only and models are added afterward from its card, so a brand
	// new custom endpoint can be saved before its model list is known.
	isCustom := s.registry == nil || !s.registry.HasProvider(req.ID)
	if isCustom && req.BaseURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "base_url is required for custom providers"})
		return
	}

	pc := &config.ProviderConfig{
		APIKey:          req.APIKey,
		BaseURL:         req.BaseURL,
		Name:            req.Name,
		Headers:         cleanHeaders(req.Headers),
		Vision:          req.Vision,
		Thinking:        req.Thinking,
		ReasoningEffort: req.ReasoningEffort,
	}
	if isCustom && req.Model != "" {
		pc.CustomModels = []config.CustomModelConfig{{
			ID:        req.Model,
			Name:      req.Model,
			ToolCall:  true,
			Reasoning: req.ModelReasoning,
		}}
	}
	cfg.Providers[req.ID] = pc

	// If there is no active model yet and this is a custom provider with an
	// explicit model, adopt it as the active model so the app can boot.
	if cfg.Model == "" && isCustom && req.Model != "" {
		cfg.Model = req.ID + "/" + req.Model
	}

	if err := config.SaveConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config: " + err.Error()})
		return
	}

	// Publish into the live server so /api/models sees the new provider without a restart.
	s.cfg = cfg
	s.registry = model.NewModelRegistryWithConfig(cfg)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// validReasoningEffort whitelists the thinking-depth values accepted from
// clients. The set mirrors the effort levels models.dev publishes under
// reasoning_options (see internal/model registry). Empty means "unset / omit
// the parameter".
func validReasoningEffort(v string) bool {
	switch v {
	case "", "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}

// cleanHeaders drops rows with an empty key and trims whitespace from both key
// and value, so blank editor rows never reach the saved config and a pasted
// token with a stray trailing space does not silently break auth.
func cleanHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		out[k] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// handleUpdateProvider edits an existing provider, merging secret fields so the
// client may omit unchanged credentials. An empty api_key keeps the stored key;
// a header value left empty keeps the stored value for that key (the list
// endpoint returns masked secrets, so the UI sends blanks for untouched ones).
func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id is required"})
		return
	}
	var req struct {
		APIKey       string            `json:"api_key,omitempty"`
		BaseURL      string            `json:"base_url,omitempty"`
		Name         string            `json:"name,omitempty"`
		Headers      map[string]string `json:"headers,omitempty"`
		CustomModels *[]struct {
			ID          string   `json:"id"`
			Name        string   `json:"name,omitempty"`
			Reasoning   bool     `json:"reasoning,omitempty"`
			Context     int      `json:"context,omitempty"`
			Attachment  bool     `json:"attachment,omitempty"`
			EffortTiers []string `json:"effort_tiers,omitempty"`
		} `json:"custom_models,omitempty"`
		Vision          *bool  `json:"vision,omitempty"`
		Thinking        *bool  `json:"thinking,omitempty"`
		ReasoningEffort string `json:"reasoning_effort,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if !validReasoningEffort(req.ReasoningEffort) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reasoning_effort"})
		return
	}

	// Serialize config RMW + live publish under cfgMu (see Server.cfgMu).
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	cfg, err := config.LoadConfig()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	pc := cfg.GetProviders()[id]
	if pc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found"})
		return
	}

	// Mutate in place so fields not exposed by this endpoint (display name,
	// custom models, deprecated lists) are preserved untouched.
	prevHeaders := pc.Headers
	// base_url uses keep-on-empty semantics (like api_key): the list endpoint
	// masks secrets but returns base_url verbatim, yet a client that doesn't
	// touch the endpoint may still submit an empty value. Overwriting
	// unconditionally would wipe a stored custom endpoint, so only adopt a
	// non-empty incoming value.
	if req.BaseURL != "" {
		pc.BaseURL = req.BaseURL
	}
	pc.Vision = req.Vision
	pc.Thinking = req.Thinking
	pc.ReasoningEffort = req.ReasoningEffort
	if req.Name != "" {
		pc.Name = req.Name
	}
	if req.APIKey != "" {
		pc.APIKey = req.APIKey
	}
	// Merge headers: empty incoming value ⇒ keep the stored secret for that key.
	pc.Headers = nil
	if cleaned := cleanHeaders(req.Headers); len(cleaned) > 0 {
		merged := make(map[string]string, len(cleaned))
		for k, v := range cleaned {
			if v == "" {
				if ov, ok := prevHeaders[k]; ok {
					merged[k] = ov
					continue
				}
			}
			merged[k] = v
		}
		pc.Headers = merged
	}

	// Replace the provider's custom models when the client sends the list (nil ⇒
	// keep existing). Each model's stored Context is preserved by merging on id,
	// ToolCall stays true (matching the add path), and the model currently set as
	// active cannot be dropped so a save can't strand the running app.
	if req.CustomModels != nil {
		prev := make(map[string]config.CustomModelConfig, len(pc.CustomModels))
		for _, m := range pc.CustomModels {
			prev[m.ID] = m
		}
		next := make([]config.CustomModelConfig, 0, len(*req.CustomModels))
		seen := make(map[string]bool, len(*req.CustomModels))
		for _, m := range *req.CustomModels {
			mid := strings.TrimSpace(m.ID)
			if mid == "" || seen[mid] {
				continue
			}
			seen[mid] = true
			cm := config.CustomModelConfig{ID: mid, Name: strings.TrimSpace(m.Name), ToolCall: true, Reasoning: m.Reasoning}
			// Adopt the incoming per-model capability fields when provided;
			// otherwise carry over the previously stored values so an edit that
			// only renames a model doesn't silently drop its context window,
			// vision flag, or configured effort tiers.
			if old, ok := prev[mid]; ok {
				if cm.Context == 0 {
					cm.Context = old.Context
				}
				if !cm.Attachment {
					cm.Attachment = old.Attachment
				}
				if len(cm.EffortTiers) == 0 {
					cm.EffortTiers = old.EffortTiers
				}
			}
			if m.Context > 0 {
				cm.Context = m.Context
			}
			if m.Attachment {
				cm.Attachment = true
			}
			if len(m.EffortTiers) > 0 {
				cm.EffortTiers = m.EffortTiers
			}
			next = append(next, cm)
		}
		// Reject custom model ids that collide with the provider's built-in
		// (registry) models. A duplicate id would shadow or be shadowed by the
		// registry entry, confusing the model picker and catalog. Custom ids
		// may still be edited to their own value (handled by seen dedup above).
		if s.registry != nil {
			if regProv := s.registry.GetProvider(id); regProv != nil {
				for _, cm := range next {
					if _, ok := regProv.Models[cm.ID]; ok {
						// Allow it only if it was already a custom model with this id
						// (editing an existing custom entry in place).
						if _, wasCustom := prev[cm.ID]; !wasCustom {
							writeJSON(w, http.StatusBadRequest, map[string]string{
								"error": "model id '" + cm.ID + "' duplicates a built-in model; choose another id",
							})
							return
						}
					}
				}
			}
		}
		if strings.HasPrefix(cfg.Model, id+"/") {
			active := strings.TrimPrefix(cfg.Model, id+"/")
			if _, wasThere := prev[active]; wasThere && !seen[active] {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot remove the active model; switch to another model first"})
				return
			}
		}
		isCustom := s.registry == nil || !s.registry.HasProvider(id)
		if isCustom && len(next) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "custom providers need at least one model"})
			return
		}
		pc.CustomModels = next
	}

	if cfg.Providers == nil {
		cfg.Providers = make(map[string]*config.ProviderConfig)
	}
	cfg.Providers[id] = pc
	if err := config.SaveConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config: " + err.Error()})
		return
	}

	// Publish the updated config + registry to the live server so the chat model
	// picker (/api/models) and catalog reflect added/edited/removed models
	// without a restart — matching handleSetupComplete's publish step.
	s.cfg = cfg
	s.registry = model.NewModelRegistryWithConfig(cfg)

	// Rebuild the agents of live engines currently running on this provider so
	// connection-level changes (api_key, base_url, headers, vision, thinking,
	// reasoning_effort) take effect immediately. The old agent captured a chat
	// model built from the previous ProviderConfig — without a rebuild, e.g. a
	// cleared vision override would keep silently stripping images until the
	// next model/mode switch. createAgent re-reads the config from disk (already
	// saved above), mirroring the MCP-reload rebuild path.
	s.rebuildEnginesForProvider(id)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// rebuildEnginesForProvider rebuilds the agent of every live engine whose
// current model belongs to the given provider. Rebuild failures are logged and
// skipped: the engine keeps its previous (stale but working) agent rather than
// being left without one.
func (s *Server) rebuildEnginesForProvider(providerID string) {
	s.tasksMu.RLock()
	engines := make([]*Engine, 0, len(s.tasks))
	for _, e := range s.tasks {
		engines = append(engines, e)
	}
	s.tasksMu.RUnlock()
	if a := s.activeEngine(); a != nil {
		found := false
		for _, e := range engines {
			if e == a {
				found = true
				break
			}
		}
		if !found {
			engines = append(engines, a)
		}
	}
	for _, eng := range engines {
		if eng.createAgent == nil {
			continue
		}
		prov, mdl, _ := eng.modelSnapshot()
		if prov != providerID {
			continue
		}
		ag, err := eng.createAgent(prov, mdl)
		if err != nil {
			config.Logger().Printf("[web] provider %s update: agent rebuild failed for task %s: %v", providerID, eng.taskID, err)
			continue
		}
		// Conditional install: a model switch that lands while createAgent runs
		// outside emu built a newer agent from the already-updated config — it
		// must not be clobbered with this now-stale one.
		if !eng.setAgentIfModel(ag, prov, mdl) {
			config.Logger().Printf("[web] provider %s update: task %s switched models mid-rebuild; skipping stale agent", providerID, eng.taskID)
		}
	}
}

// handleDeleteProvider removes a provider from the config.
func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	if providerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id is required"})
		return
	}

	// Serialize RMW with other config writers (cfgMu documents this in Server).
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

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

	activeProvider, _ := cfg.GetProviderModel()
	if activeProvider == providerID {
		// Pick a surviving provider+model so cfg.Model is never left pointing at
		// a deleted provider. Reject when no safe replacement exists.
		nextRef := firstAlternateProviderModel(cfg, s.registry, providerID)
		if nextRef == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete the only provider (or no replacement model available)"})
			return
		}
		cfg.Model = nextRef
	}

	delete(cfg.Providers, providerID)
	if err := config.SaveConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config: " + err.Error()})
		return
	}

	s.cfg = cfg
	s.registry = model.NewModelRegistryWithConfig(cfg)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// firstAlternateProviderModel returns "provider/model" for the first configured
// provider other than skipID that has at least one usable model, or "" if none.
func firstAlternateProviderModel(cfg *config.Config, reg *model.ModelRegistry, skipID string) string {
	if cfg == nil {
		return ""
	}
	// Prefer a registry rebuilt from cfg so custom models on survivors are visible.
	live := model.NewModelRegistryWithConfig(cfg)
	if live == nil {
		live = reg
	}
	for id, pc := range cfg.GetProviders() {
		if id == skipID || pc == nil {
			continue
		}
		if live != nil {
			if models := live.ListProviderModels(id, true); len(models) > 0 {
				return id + "/" + models[0].ID
			}
		}
		if len(pc.CustomModels) > 0 {
			return id + "/" + pc.CustomModels[0].ID
		}
	}
	return ""
}
