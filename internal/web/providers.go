package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model"
	"github.com/cnjack/jcode/internal/providerauth"
	"github.com/cnjack/jcode/internal/providertools"
)

func validateProviderTools(in map[string]config.ProviderToolPolicy) (map[string]config.ProviderToolPolicy, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]config.ProviderToolPolicy, len(in))
	for key, policy := range in {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			return nil, fmt.Errorf("provider tool id cannot be empty")
		}
		if policy.MaxCallsPerTurn < 0 || policy.MaxCallsPerSession < 0 {
			return nil, fmt.Errorf("provider tool %q call limits cannot be negative", key)
		}
		if _, duplicate := out[key]; duplicate {
			return nil, fmt.Errorf("duplicate provider tool id %q", key)
		}
		out[key] = policy
	}
	return out, nil
}

func isASCIIHost(host string) bool {
	for index := range len(host) {
		if host[index] > 0x7f {
			return false
		}
	}
	return true
}

func validateImageEndpoint(in *config.ImageEndpointConfig) (*config.ImageEndpointConfig, error) {
	if in == nil {
		return nil, nil
	}
	out := *in
	out.Protocol = strings.ToLower(strings.TrimSpace(out.Protocol))
	out.BaseURL = strings.TrimSpace(out.BaseURL)
	if out.Protocol == "" {
		return nil, fmt.Errorf("image_endpoint.protocol is required")
	}
	u, err := url.Parse(out.BaseURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, fmt.Errorf("image_endpoint.base_url must be an HTTPS URL without credentials, query, or fragment")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return nil, fmt.Errorf("image_endpoint.base_url host is required")
	}
	// The image client intentionally accepts only canonical ASCII hosts. Reject
	// Unicode here as well so the settings API never reports a configuration as
	// usable that the runtime will subsequently refuse.
	if !isASCIIHost(host) {
		return nil, fmt.Errorf("image_endpoint.base_url host must use canonical ASCII")
	}
	port := u.Port()
	u.Scheme = "https"
	u.Host = host
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	out.BaseURL = u.String()

	seenModels := make(map[string]struct{}, len(out.Models))
	models := make([]config.ImageModelConfig, 0, len(out.Models))
	for _, item := range out.Models {
		item.ID = strings.TrimSpace(item.ID)
		item.Name = strings.TrimSpace(item.Name)
		if item.ID == "" {
			return nil, fmt.Errorf("image_endpoint model id is required")
		}
		if _, duplicate := seenModels[item.ID]; duplicate {
			return nil, fmt.Errorf("duplicate image_endpoint model %q", item.ID)
		}
		seenModels[item.ID] = struct{}{}
		cleanSizes := make([]string, 0, len(item.Sizes))
		seenSizes := make(map[string]struct{}, len(item.Sizes))
		for _, size := range item.Sizes {
			size = strings.TrimSpace(size)
			if size == "" {
				continue
			}
			if _, duplicate := seenSizes[size]; duplicate {
				continue
			}
			seenSizes[size] = struct{}{}
			cleanSizes = append(cleanSizes, size)
		}
		item.Sizes = cleanSizes
		models = append(models, item)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("image_endpoint.models needs at least one model")
	}
	out.Models = models

	hosts := make([]string, 0, len(out.AssetHosts))
	seenHosts := make(map[string]struct{}, len(out.AssetHosts))
	for _, rule := range out.AssetHosts {
		rule = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(rule, ".")))
		base := strings.TrimPrefix(rule, "*.")
		if base == "" || strings.ContainsAny(base, "/:@?#") || strings.Contains(base, "*") ||
			net.ParseIP(base) != nil || !strings.Contains(base, ".") || !isASCIIHost(base) {
			return nil, fmt.Errorf("invalid image_endpoint asset host %q", rule)
		}
		if _, duplicate := seenHosts[rule]; duplicate {
			continue
		}
		seenHosts[rule] = struct{}{}
		hosts = append(hosts, rule)
	}
	out.AssetHosts = hosts
	return &out, nil
}

func decodeOptionalImageEndpoint(raw json.RawMessage) (present bool, endpoint *config.ImageEndpointConfig, err error) {
	if len(raw) == 0 {
		return false, nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true, nil, nil
	}
	var value config.ImageEndpointConfig
	if err := json.Unmarshal(raw, &value); err != nil {
		return true, nil, fmt.Errorf("invalid image_endpoint")
	}
	valuePtr, err := validateImageEndpoint(&value)
	return true, valuePtr, err
}

// decodeOptionalBaseURL gives provider updates an explicit three-state
// contract without changing the legacy keep-on-empty behavior:
//
//   - omitted or "" keeps the stored URL;
//   - null clears it (and lets registry providers fall back to their default);
//   - a non-empty string replaces it.
//
// API keys intentionally keep their existing secret-input semantics and are
// decoded separately.
func decodeOptionalBaseURL(raw json.RawMessage) (present bool, value string, err error) {
	if len(raw) == 0 {
		return false, "", nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true, "", nil
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return true, "", fmt.Errorf("invalid base_url")
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" {
		return false, "", nil
	}
	return true, decoded, nil
}

func providerIsCustom(registry *model.ModelRegistry, providerID string) bool {
	if registry == nil {
		return true
	}
	provider := registry.GetProvider(providerID)
	return provider == nil || provider.Custom
}

func validateCustomProviderRoutes(
	isCustom bool,
	baseURL string,
	hasChatWorkload bool,
	imageEndpoint *config.ImageEndpointConfig,
) error {
	if !isCustom || strings.TrimSpace(baseURL) != "" {
		return nil
	}
	if hasChatWorkload {
		return fmt.Errorf("base_url is required for custom providers with chat models")
	}
	if imageEndpoint == nil {
		return fmt.Errorf("custom providers need a chat base_url or image_endpoint")
	}
	return nil
}

func decodeOptionalBool(raw json.RawMessage, field string) (present bool, value *bool, err error) {
	if len(raw) == 0 {
		return false, nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true, nil, nil
	}
	var decoded bool
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return true, nil, fmt.Errorf("invalid %s", field)
	}
	return true, &decoded, nil
}

// decodeOptionalProviderAuthBinding preserves the update contract's three
// states: omitted keeps the current mode, null selects API-key authentication,
// and an object selects one managed login binding.
func decodeOptionalProviderAuthBinding(
	raw json.RawMessage,
) (present bool, binding *config.ProviderAuthBinding, err error) {
	if len(raw) == 0 {
		return false, nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true, nil, nil
	}
	var decoded config.ProviderAuthBinding
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return true, nil, fmt.Errorf("invalid auth_binding")
	}
	decoded.Method = strings.TrimSpace(decoded.Method)
	decoded.AccountID = strings.TrimSpace(decoded.AccountID)
	if decoded.Method == "" {
		return true, nil, fmt.Errorf("auth_binding.method is required")
	}
	return true, &decoded, nil
}

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
	var providerConfig *config.ProviderConfig
	cfg, _ := config.LoadConfig()
	if cfg != nil {
		if pc := cfg.GetProviders()[providerID]; pc != nil {
			providerConfig = pc
			apiKey, baseURL, headers = pc.APIKey, pc.BaseURL, pc.Headers
			for _, m := range pc.CustomModels {
				configured[m.ID] = true
				cm := m // copy for map value
				customSet[m.ID] = &cm
			}
		}
	}
	modelState, _ := config.LoadModelState()

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
			e.Custom = !m.Managed
			if m.Managed {
				e.Added = modelState.IsModelEnabled(
					config.ModelRef{Provider: providerID, Model: id}, false,
				)
			}
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

	// Managed account providers expose an account- and entitlement-specific
	// catalog. Prefer that live source over the conservative built-in fallback;
	// it is how Copilot and subscription-backed Codex/xAI accounts advertise the
	// models this particular login may actually use.
	if providerConfig != nil && providerConfig.Auth != nil {
		method, parseErr := parseProviderAuthMethod(providerConfig.Auth.Method)
		service, serviceErr := s.providerAuthService()
		if parseErr == nil && serviceErr == nil {
			liveModels, liveErr := service.Models(r.Context(), providerauth.Binding{
				Method: method, AccountID: providerConfig.Auth.AccountID,
			})
			if liveErr == nil && len(liveModels) > 0 {
				result := make([]catalogEntry, 0, len(liveModels)+len(configured))
				seen := make(map[string]bool, len(liveModels))
				for _, live := range liveModels {
					if live.Kind != providerauth.ModelKindChat || live.ID == "" || seen[live.ID] {
						continue
					}
					seen[live.ID] = true
					metadata := managedModelConfigFromLive(s.registry, providerID, live)
					defaultEnabled := configured[live.ID]
					if persisted := customSet[live.ID]; persisted != nil && persisted.Managed {
						defaultEnabled = false
					}
					if s.registry != nil {
						if native := s.registry.GetProvider(providerID); native != nil {
							if static := native.Models[live.ID]; static != nil {
								defaultEnabled = static.DefaultEnabled
							}
						}
					}
					result = append(result, catalogEntry{
						ID:          live.ID,
						Name:        metadata.Name,
						Added:       modelState.IsModelEnabled(config.ModelRef{Provider: providerID, Model: live.ID}, defaultEnabled),
						Context:     metadata.Context,
						Reasoning:   metadata.Reasoning,
						Attachment:  metadata.Attachment,
						EffortTiers: metadata.EffortTiers,
						Custom:      false,
					})
				}
				// Keep previously enabled managed models visible during upstream
				// catalog rollouts so users can disable them or diagnose entitlement
				// changes without hand-editing config.
				for id := range configured {
					if !seen[id] {
						result = append(result, customEntry(id))
					}
				}
				sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
				writeJSON(w, http.StatusOK, result)
				return
			}
			if liveErr != nil {
				config.Logger().Printf(
					"[provider-auth] live model catalog unavailable for %s (error_type=%T); using fallback",
					method, liveErr,
				)
			}
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
				added := true
				if cm.Managed {
					added = modelState.IsModelEnabled(
						config.ModelRef{Provider: providerID, Model: m.ID}, false,
					)
				}
				result = append(result, catalogEntry{
					ID:          m.ID,
					Name:        cm.Name,
					Added:       added,
					Context:     cm.Context,
					Reasoning:   cm.Reasoning,
					Attachment:  cm.Attachment,
					EffortTiers: cm.EffortTiers,
					Custom:      !cm.Managed,
				})
				continue
			}
			ctx := 0
			if m.Limit != nil {
				ctx = m.Limit.Context
			}
			result = append(result, catalogEntry{
				ID:   m.ID,
				Name: m.Name,
				Added: modelState.IsModelEnabled(
					config.ModelRef{Provider: providerID, Model: m.ID},
					m.DefaultEnabled,
				),
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
					result = append(result, catalogEntry{
						ID: id,
						Added: modelState.IsModelEnabled(
							config.ModelRef{Provider: providerID, Model: id}, configured[id],
						),
					})
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

func managedModelConfigFromLive(
	registry *model.ModelRegistry,
	providerID string,
	live providerauth.Model,
) config.CustomModelConfig {
	result := config.CustomModelConfig{
		ID: live.ID, Name: live.Name, ToolCall: true, Managed: true,
		Protocol: string(live.Protocol), Vendor: live.Vendor,
		Attachment: live.Attachment, Context: live.Context,
	}
	if result.Name == "" {
		result.Name = live.ID
	}
	metadata := findManagedModelMetadata(registry, providerID, live.Vendor, live.ID)
	if metadata == nil {
		return result
	}
	// Exact registry rows may supply a nicer display name. Related siblings
	// only donate capabilities — grok-4.6 must not be labeled "Grok 4.5".
	if metadata.ID == live.ID {
		if result.Name == live.ID && metadata.Name != "" {
			result.Name = metadata.Name
		}
		result.ToolCall = metadata.ToolCall
	} else if metadata.ToolCall {
		result.ToolCall = true
	}
	if metadata.Reasoning {
		result.Reasoning = true
	}
	if metadata.Attachment {
		result.Attachment = true
	}
	if result.Context == 0 && metadata.Limit != nil {
		result.Context = metadata.Limit.Context
	}
	for _, option := range metadata.ReasoningOptions {
		if option.Type == "effort" && len(option.Values) > 0 {
			result.EffortTiers = append([]string(nil), option.Values...)
			break
		}
	}
	return result
}

func findManagedModelMetadata(
	registry *model.ModelRegistry,
	providerID string,
	vendor string,
	modelID string,
) *model.RegistryModel {
	if registry == nil {
		return nil
	}
	var related *model.RegistryModel
	for _, candidate := range []string{providerID, strings.ToLower(strings.TrimSpace(vendor))} {
		if candidate == "" {
			continue
		}
		if provider := registry.GetProvider(candidate); provider != nil {
			if metadata := provider.Models[modelID]; metadata != nil {
				return metadata
			}
			if related == nil {
				related = model.RelatedRegistryModel(provider, modelID)
			}
		}
	}
	for _, provider := range registry.ListProviders() {
		if metadata := provider.Models[modelID]; metadata != nil {
			return metadata
		}
	}
	return related
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
		ID              string                               `json:"id"`
		Name            string                               `json:"name,omitempty"` // display name for custom providers
		Custom          bool                                 `json:"custom,omitempty"`
		APIKeySet       bool                                 `json:"api_key_set"`
		APIKey          string                               `json:"api_key,omitempty"` // masked
		AuthBinding     *config.ProviderAuthBinding          `json:"auth_binding,omitempty"`
		AuthStatus      *providerauth.Status                 `json:"auth_status,omitempty"`
		AuthMethods     []string                             `json:"auth_methods,omitempty"`
		BaseURL         string                               `json:"base_url,omitempty"`
		Headers         map[string]string                    `json:"headers,omitempty"` // values masked
		CustomModels    []customModelView                    `json:"custom_models,omitempty"`
		Vision          *bool                                `json:"vision,omitempty"`
		Thinking        *bool                                `json:"thinking,omitempty"`
		ReasoningEffort string                               `json:"reasoning_effort,omitempty"`
		Protocol        string                               `json:"protocol,omitempty"`
		ProviderTools   map[string]config.ProviderToolPolicy `json:"provider_tools,omitempty"`
		ImageEndpoint   *config.ImageEndpointConfig          `json:"image_endpoint,omitempty"`
		Capabilities    []providertools.ProviderCapability   `json:"capabilities"`
	}

	modelState, _ := config.LoadModelState()
	result := make([]providerDetail, 0)
	for id, pc := range cfg.GetProviders() {
		detail := providerDetail{
			ID:              id,
			Name:            pc.Name,
			APIKeySet:       pc.APIKey != "",
			AuthBinding:     pc.Auth,
			AuthStatus:      s.providerAuthStatus(r.Context(), pc.Auth),
			AuthMethods:     providerAuthMethodsForID(s, id),
			BaseURL:         pc.BaseURL,
			Vision:          pc.Vision,
			Thinking:        pc.Thinking,
			ReasoningEffort: pc.ReasoningEffort,
			Protocol:        pc.Protocol,
			ProviderTools:   pc.ProviderTools,
			ImageEndpoint:   pc.ImageEndpoint,
			Capabilities:    providertools.ProviderCapabilities(cfg, id),
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
				if !modelState.IsModelEnabled(config.ModelRef{Provider: id, Model: m.ID}, m.DefaultEnabled) {
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
			if m.Managed && !modelState.IsModelEnabled(config.ModelRef{Provider: id, Model: m.ID}, false) {
				continue
			}
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
				Custom:      !m.Managed,
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
		ID              string                               `json:"id"`
		APIKey          string                               `json:"api_key"`
		BaseURL         string                               `json:"base_url,omitempty"`
		Name            string                               `json:"name,omitempty"`
		Model           string                               `json:"model,omitempty"`
		ModelReasoning  bool                                 `json:"model_reasoning,omitempty"`
		Headers         map[string]string                    `json:"headers,omitempty"`
		Vision          *bool                                `json:"vision,omitempty"`
		Thinking        *bool                                `json:"thinking,omitempty"`
		ReasoningEffort string                               `json:"reasoning_effort,omitempty"`
		Protocol        string                               `json:"protocol,omitempty"`
		ProviderTools   map[string]config.ProviderToolPolicy `json:"provider_tools,omitempty"`
		ImageEndpoint   *config.ImageEndpointConfig          `json:"image_endpoint,omitempty"`
		AuthBinding     *config.ProviderAuthBinding          `json:"auth_binding,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.Model = strings.TrimSpace(req.Model)
	if req.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	normalizedAuthBinding, err := s.validateProviderBinding(r.Context(), req.ID, req.AuthBinding)
	if err != nil {
		writeConfigMutationError(w, err)
		return
	}
	req.AuthBinding = normalizedAuthBinding
	if req.AuthBinding == nil && req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api_key is required for API-key authentication"})
		return
	}
	if req.AuthBinding != nil {
		// Managed drivers own their endpoint and protected headers. Drop values
		// from a stale API-key form instead of persisting dormant credentials or
		// allowing the browser to redirect a bearer token.
		req.APIKey = ""
		req.BaseURL = ""
		req.Headers = nil
		req.Protocol = ""
	}
	if !validReasoningEffort(req.ReasoningEffort) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reasoning_effort"})
		return
	}
	providerTools, err := validateProviderTools(req.ProviderTools)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	imageEndpoint, err := validateImageEndpoint(req.ImageEndpoint)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.AuthBinding != nil && imageEndpoint != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "image_endpoint requires API-key authentication",
		})
		return
	}

	// Serialize config RMW + live publish under cfgMu (see Server.cfgMu).
	s.cfgMu.Lock()
	configLocked := true
	defer func() {
		if configLocked {
			s.cfgMu.Unlock()
		}
	}()

	// Custom chat and image workloads have independent routes. A valid explicit
	// image endpoint is therefore enough to create an image-only provider, while
	// an empty shell (and every configured chat model without a chat route) is
	// rejected.
	isCustom := providerIsCustom(s.registry, req.ID)
	if err := validateCustomProviderRoutes(isCustom, req.BaseURL, req.Model != "", imageEndpoint); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	cfg, err := config.MutateConfigOrCreate(func(cfg *config.Config) error {
		if cfg.Providers == nil {
			cfg.Providers = make(map[string]*config.ProviderConfig)
		}
		pc := &config.ProviderConfig{
			APIKey:          req.APIKey,
			BaseURL:         req.BaseURL,
			Auth:            req.AuthBinding,
			Name:            req.Name,
			Headers:         cleanHeaders(req.Headers),
			Vision:          req.Vision,
			Thinking:        req.Thinking,
			ReasoningEffort: req.ReasoningEffort,
			Protocol:        strings.TrimSpace(req.Protocol),
			ProviderTools:   providerTools,
			ImageEndpoint:   imageEndpoint,
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
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config: " + err.Error()})
		return
	}

	// Publish into the live server so /api/models sees the new provider without a restart.
	s.publishConfigSnapshotLocked(cfg)
	s.cfgMu.Unlock()
	configLocked = false
	applyErr := s.rebuildProviderDependents(req.ID, "add")
	s.syncProviderConfigsBestEffort()
	if applyErr != nil {
		writeSavedButNotApplied(w, "provider configuration")
		return
	}

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
		k = http.CanonicalHeaderKey(strings.TrimSpace(k))
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
		BaseURL      json.RawMessage   `json:"base_url"`
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
		Vision          json.RawMessage                       `json:"vision,omitempty"`
		Thinking        json.RawMessage                       `json:"thinking,omitempty"`
		ReasoningEffort *string                               `json:"reasoning_effort,omitempty"`
		Protocol        *string                               `json:"protocol,omitempty"`
		ProviderTools   *map[string]config.ProviderToolPolicy `json:"provider_tools,omitempty"`
		ImageEndpoint   json.RawMessage                       `json:"image_endpoint,omitempty"`
		AuthBinding     json.RawMessage                       `json:"auth_binding,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.ReasoningEffort != nil && !validReasoningEffort(*req.ReasoningEffort) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid reasoning_effort"})
		return
	}
	visionPresent, vision, err := decodeOptionalBool(req.Vision, "vision")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	thinkingPresent, thinking, err := decodeOptionalBool(req.Thinking, "thinking")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	baseURLPresent, baseURL, err := decodeOptionalBaseURL(req.BaseURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var providerTools map[string]config.ProviderToolPolicy
	if req.ProviderTools != nil {
		var err error
		providerTools, err = validateProviderTools(*req.ProviderTools)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	imageEndpointPresent, imageEndpoint, err := decodeOptionalImageEndpoint(req.ImageEndpoint)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	authBindingPresent, authBinding, err := decodeOptionalProviderAuthBinding(req.AuthBinding)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if authBindingPresent {
		authBinding, err = s.validateProviderBinding(r.Context(), id, authBinding)
		if err != nil {
			writeConfigMutationError(w, err)
			return
		}
	}

	// Serialize config RMW + live publish under cfgMu (see Server.cfgMu).
	s.cfgMu.Lock()
	configLocked := true
	defer func() {
		if configLocked {
			s.cfgMu.Unlock()
		}
	}()

	cfg, err := config.MutateConfig(func(cfg *config.Config) error {
		pc := cfg.GetProviders()[id]
		if pc == nil {
			return newConfigMutationHTTPError(http.StatusNotFound, "provider not found")
		}
		mutationRegistry := model.NewModelRegistryWithConfig(cfg)
		nextAuth := pc.Auth
		if authBindingPresent {
			nextAuth = authBinding
		}
		if nextAuth != nil && imageEndpointPresent && imageEndpoint != nil {
			return newConfigMutationHTTPError(
				http.StatusBadRequest,
				"image_endpoint requires API-key authentication",
			)
		}
		// Mutate in place so fields not exposed by this endpoint (display name,
		// custom models, deprecated lists) are preserved untouched.
		prevHeaders := cleanHeaders(pc.Headers)
		// Omitted/legacy-empty base_url preserves the existing route. Explicit
		// null clears it so a registry provider can return from a proxy to its
		// official default endpoint.
		if baseURLPresent {
			pc.BaseURL = baseURL
		}
		if visionPresent {
			pc.Vision = vision
		}
		if thinkingPresent {
			pc.Thinking = thinking
		}
		if req.ReasoningEffort != nil {
			pc.ReasoningEffort = *req.ReasoningEffort
		}
		if req.Protocol != nil {
			pc.Protocol = strings.TrimSpace(*req.Protocol)
		}
		if req.ProviderTools != nil {
			pc.ProviderTools = providerTools
		}
		if imageEndpointPresent {
			pc.ImageEndpoint = imageEndpoint
		}
		if authBindingPresent {
			pc.Auth = authBinding
			if authBinding != nil {
				pc.APIKey = ""
				pc.BaseURL = ""
				pc.Headers = nil
				pc.Protocol = ""
			}
		}
		if req.Name != "" {
			pc.Name = req.Name
		}
		if req.APIKey != "" && !secretValueUnchanged(req.APIKey, pc.APIKey) {
			pc.APIKey = req.APIKey
		}
		// A missing headers field keeps the whole block. When the client sends a
		// block, it replaces the key set; empty/current-mask values keep each stored
		// value while an explicit {} clears all provider headers.
		if req.Headers != nil {
			pc.Headers = nil
			cleaned := cleanHeaders(req.Headers)
			if len(cleaned) > 0 {
				merged := make(map[string]string, len(cleaned))
				for k, v := range cleaned {
					if ov, ok := prevHeaders[k]; ok {
						if secretValueUnchanged(v, ov) {
							merged[k] = ov
							continue
						}
					}
					merged[k] = v
				}
				pc.Headers = merged
			}
		} else {
			pc.Headers = prevHeaders
		}
		// The generic secret merge above intentionally preserves omitted fields.
		// Re-assert the managed-auth invariant after it so stale masked headers or
		// an api_key submitted by an older UI cannot survive the mode switch.
		if pc.Auth != nil {
			pc.APIKey = ""
			pc.BaseURL = ""
			pc.Headers = nil
			pc.Protocol = ""
			// Image generation currently uses the Provider API key. Managed chat
			// accounts deliberately do not expose or retain one, so keeping this
			// endpoint would create a configuration that can never run.
			pc.ImageEndpoint = nil
		} else if pc.APIKey == "" {
			return newConfigMutationHTTPError(
				http.StatusBadRequest,
				"api_key is required for API-key authentication",
			)
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
			next := make([]config.CustomModelConfig, 0, len(pc.CustomModels)+len(*req.CustomModels))
			seen := make(map[string]bool, len(pc.CustomModels)+len(*req.CustomModels))
			// Account-scoped live models are backend-owned metadata. The custom
			// model editor sends only user-authored rows, so preserve managed rows
			// across an unrelated provider edit instead of silently deleting them.
			for _, existing := range pc.CustomModels {
				if existing.Managed && existing.ID != "" && !seen[existing.ID] {
					seen[existing.ID] = true
					next = append(next, existing)
				}
			}
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
			if mutationRegistry != nil {
				if regProv := mutationRegistry.GetProvider(id); regProv != nil {
					for _, cm := range next {
						if _, ok := regProv.Models[cm.ID]; ok {
							// Allow it only if it was already a custom model with this id
							// (editing an existing custom entry in place).
							if _, wasCustom := prev[cm.ID]; !wasCustom {
								return newConfigMutationHTTPError(
									http.StatusBadRequest,
									"model id '"+cm.ID+"' duplicates a built-in model; choose another id",
								)
							}
						}
					}
				}
			}
			if strings.HasPrefix(cfg.Model, id+"/") {
				active := strings.TrimPrefix(cfg.Model, id+"/")
				if _, wasThere := prev[active]; wasThere && !seen[active] {
					return newConfigMutationHTTPError(http.StatusBadRequest, "cannot remove the active model; switch to another model first")
				}
			}
			isCustom := providerIsCustom(mutationRegistry, id)
			if isCustom && len(next) == 0 && pc.ImageEndpoint == nil {
				return newConfigMutationHTTPError(http.StatusBadRequest, "custom providers need at least one model")
			}
			pc.CustomModels = next
		}
		if err := validateCustomProviderRoutes(
			providerIsCustom(mutationRegistry, id), pc.BaseURL,
			pc.HasConfiguredChatModels() || strings.HasPrefix(cfg.Model, id+"/"),
			pc.ImageEndpoint,
		); err != nil {
			return newConfigMutationHTTPError(http.StatusBadRequest, err.Error())
		}
		if selectedProvider, selectedModel := splitModelReference(cfg.ImageModel); selectedProvider == id &&
			!imageModelSelectable(cfg, selectedProvider, selectedModel) {
			cfg.ImageModel = ""
		}

		if cfg.Providers == nil {
			cfg.Providers = make(map[string]*config.ProviderConfig)
		}
		cfg.Providers[id] = pc
		return nil
	})
	if err != nil {
		writeConfigMutationError(w, err)
		return
	}

	// Publish the updated config + registry to the live server so the chat model
	// picker (/api/models) and catalog reflect added/edited/removed models
	// without a restart — matching handleSetupComplete's publish step.
	s.publishConfigSnapshotLocked(cfg)

	// The independent image role may use a provider different from chat. A
	// trusted provider-search transport stays connected process-wide, while each
	// task injects it only when that provider owns the task's current chat model.
	// Rebuild every live task so both role and per-task provider gates refresh.
	s.cfgMu.Unlock()
	configLocked = false
	applyErr := s.rebuildProviderDependents(id, "update")
	s.syncProviderConfigsBestEffort()
	if applyErr != nil {
		writeSavedButNotApplied(w, "provider configuration")
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

	// Serialize RMW with other config writers (cfgMu documents this in Server).
	s.cfgMu.Lock()
	configLocked := true
	defer func() {
		if configLocked {
			s.cfgMu.Unlock()
		}
	}()

	cfg, err := config.MutateConfig(func(cfg *config.Config) error {
		providers := cfg.GetProviders()
		if providers == nil || providers[providerID] == nil {
			return newConfigMutationHTTPError(http.StatusNotFound, "provider not found")
		}

		activeProvider, _ := cfg.GetProviderModel()
		if activeProvider == providerID {
			// Pick a surviving provider+model so cfg.Model is never left pointing at
			// a deleted provider. Reject when no safe replacement exists.
			nextRef := firstAlternateProviderModel(cfg, model.NewModelRegistryWithConfig(cfg), providerID)
			if nextRef == "" {
				return newConfigMutationHTTPError(
					http.StatusBadRequest,
					"cannot delete the only provider (or no replacement model available)",
				)
			}
			cfg.Model = nextRef
		}

		delete(cfg.Providers, providerID)
		if strings.HasPrefix(cfg.ImageModel, providerID+"/") {
			cfg.ImageModel = ""
		}
		return nil
	})
	if err != nil {
		writeConfigMutationError(w, err)
		return
	}

	s.publishConfigSnapshotLocked(cfg)
	s.cfgMu.Unlock()
	configLocked = false
	applyErr := s.rebuildProviderDependents(providerID, "delete")
	s.syncProviderConfigsBestEffort()
	if applyErr != nil {
		writeSavedButNotApplied(w, "provider configuration")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type configMutationHTTPError struct {
	status  int
	message string
}

func newConfigMutationHTTPError(status int, message string) error {
	return &configMutationHTTPError{status: status, message: message}
}

func (err *configMutationHTTPError) Error() string { return err.message }

func writeConfigMutationError(w http.ResponseWriter, err error) {
	var requestErr *configMutationHTTPError
	if errors.As(err, &requestErr) {
		writeJSON(w, requestErr.status, map[string]string{"error": requestErr.message})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config: " + err.Error()})
}

// publishConfigSnapshotLocked updates the live snapshot without changing its
// address when possible. Some agent factories retain the startup *Config; an
// in-place replacement lets those readers observe the cross-process reload.
// The caller holds cfgMu.
func (s *Server) publishConfigSnapshotLocked(latest *config.Config) {
	if s.cfg == nil {
		s.cfg = latest
	} else if s.cfg != latest {
		*s.cfg = *latest
	}
	s.registry = model.NewModelRegistryWithConfig(s.cfg)
}

// rebuildProviderDependents reconnects the reserved provider MCP before task
// agents are rebuilt. Other provider edits only need an agent rebuild. The
// handlers call this after releasing cfgMu because reloadMCPAndRebuild takes a
// fresh, serialized config snapshot under that lock.
func (s *Server) rebuildProviderDependents(providerID, action string) error {
	var err error
	if providerID == providertools.BigModelCodingProvider {
		err = s.reloadMCPAndRebuild()
	} else {
		err = s.rebuildToolAgents()
	}
	if err != nil {
		s.logProviderApplyFailure(providerID, action, err)
	}
	return err
}

func (s *Server) logProviderApplyFailure(providerID, action string, applyErr error) {
	// Agent/provider errors are not safe to log verbatim: an adapter or remote
	// MCP implementation may echo the previous credential or a signed URL that
	// is no longer present in the just-saved config and therefore cannot be
	// reliably redacted. Preserve the error type for diagnosis, never its text.
	config.Logger().Printf(
		"[web] provider %s %s: dependent runtime rebuild failed (error_type=%T); configuration saved but not applied",
		providerID, action, applyErr,
	)
}

func writeSavedButNotApplied(w http.ResponseWriter, resource string) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"status": "saved_but_not_applied",
		"error":  resource + " was saved, but the running tool catalog could not be rebuilt; retry or restart to apply it",
	})
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
