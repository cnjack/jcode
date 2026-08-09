// Package providertools resolves provider-managed capabilities from exact,
// auditable profiles. It deliberately does not infer private APIs from brand or
// model-name substrings.
package providertools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/imagegen"
)

const (
	ToolImageGeneration = "image_generation"
	ToolWebSearch       = "web_search"

	BigModelCodingProvider = "zhipuai-coding-plan"
	bigModelCodingBaseURL  = "https://open.bigmodel.cn/api/coding/paas/v4"
	bigModelSearchMCPURL   = "https://open.bigmodel.cn/api/mcp/web_search_prime/mcp"
	bigModelSearchMCPName  = "__jcode_provider_bigmodel_search"

	AlibabaTokenPlanCNProvider = "alibaba-token-plan-cn"
	alibabaTokenPlanCNBaseURL  = "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	alibabaTokenPlanCNImageURL = "https://token-plan.cn-beijing.maas.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"
)

func IsProviderSearchMCPServer(name string) bool {
	return name == bigModelSearchMCPName
}

type ImageModel struct {
	Provider  string   `json:"provider"`
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Protocol  string   `json:"protocol"`
	Sizes     []string `json:"sizes,omitempty"`
	Builtin   bool     `json:"builtin"`
	Supported bool     `json:"supported"`
}

type ImageRuntime struct {
	Provider              string
	Model                 string
	Protocol              imagegen.Protocol
	BaseURL               string
	APIKey                string
	AuthMethod            string
	AccountID             string
	Headers               map[string]string
	AssetHosts            []string
	CredentialFingerprint string
	ConfigEpoch           string
	MaxCallsPerTurn       int
	MaxCallsPerSession    int
}

// WebSearchRuntime is deliberately metadata-only. Provider credentials and
// transport details are attached only to the detached MCP runtime returned by
// EffectiveMCPServers; approval and quota code can use this value without
// gaining access to an API key, Authorization header, or endpoint URL.
type WebSearchRuntime struct {
	ProviderProfileID     string
	CredentialFingerprint string
	ConfigEpoch           string
	ModelLabel            string
	MaxCallsPerTurn       int
	MaxCallsPerSession    int
}

// ProviderCapability is a Settings-safe capability snapshot. It contains only
// user-facing mechanism and policy metadata; credentials, endpoint URLs, and
// credential fingerprints are intentionally excluded.
type ProviderCapability struct {
	ID                 string `json:"id"`
	Availability       string `json:"availability"`
	Mechanism          string `json:"mechanism,omitempty"`
	ModelLabel         string `json:"model_label,omitempty"`
	Enabled            bool   `json:"enabled"`
	MaxCallsPerTurn    int    `json:"max_calls_per_turn"`
	MaxCallsPerSession int    `json:"max_calls_per_session"`
}

// ProviderCapabilities returns the manifest-derived Settings view for one
// configured provider. Independent model roles such as image generation are
// intentionally represented by ImageModels instead of this policy surface.
func ProviderCapabilities(cfg *config.Config, providerID string) []ProviderCapability {
	result := make([]ProviderCapability, 0, 1)
	if cfg == nil {
		return result
	}
	provider := cfg.GetProviders()[providerID]
	if provider == nil {
		return result
	}

	if isBigModelCodingProfile(providerID, provider) {
		result = append(result, providerCapability(
			provider, ToolWebSearch, "supported", "mcp_tool", BigModelSearchMCPToolName, 2, 10,
		))
		return result
	}
	return result
}

func providerCapability(
	provider *config.ProviderConfig,
	toolID, availability, mechanism, modelLabel string,
	defaultTurn, defaultSession int,
) ProviderCapability {
	policy := provider.ProviderTools[toolID]
	turn := policy.MaxCallsPerTurn
	if turn <= 0 {
		turn = defaultTurn
	}
	session := policy.MaxCallsPerSession
	if session <= 0 {
		session = defaultSession
	}
	return ProviderCapability{
		ID: toolID, Availability: availability, Mechanism: mechanism,
		ModelLabel: modelLabel, Enabled: policy.Enabled,
		MaxCallsPerTurn: turn, MaxCallsPerSession: session,
	}
}

// ResolveWebSearchRuntime resolves the single supported provider-managed web
// search profile. It fails closed for custom/proxy endpoints, disabled policy,
// or missing credentials and never returns the credential or MCP URL.
func ResolveWebSearchRuntime(cfg *config.Config) (WebSearchRuntime, error) {
	if cfg == nil {
		return WebSearchRuntime{}, fmt.Errorf("web search provider is not configured")
	}
	chatProvider, _ := cfg.GetProviderModel()
	if chatProvider != BigModelCodingProvider {
		return WebSearchRuntime{}, fmt.Errorf("current chat provider does not own the configured web search adapter")
	}
	return ResolveWebSearchTransportRuntime(cfg)
}

// ResolveWebSearchTransportRuntime resolves the detached trusted MCP
// transport independently of the current chat model. A process can host tasks
// using different chat providers, so the transport must remain connected while
// any exact enabled provider profile exists. ResolveWebSearchRuntime remains
// the per-agent injection gate and prevents this endpoint from crossing the
// active chat-provider boundary.
func ResolveWebSearchTransportRuntime(cfg *config.Config) (WebSearchRuntime, error) {
	if cfg == nil {
		return WebSearchRuntime{}, fmt.Errorf("web search provider is not configured")
	}
	provider := cfg.GetProviders()[BigModelCodingProvider]
	if provider == nil || !isBigModelCodingProfile(BigModelCodingProvider, provider) {
		return WebSearchRuntime{}, fmt.Errorf("web search provider profile is not configured")
	}
	if strings.TrimSpace(provider.APIKey) == "" {
		return WebSearchRuntime{}, fmt.Errorf("web search credential is not configured")
	}
	policy, ok := provider.ProviderTools[ToolWebSearch]
	if !ok || !policy.Enabled {
		return WebSearchRuntime{}, fmt.Errorf("web search policy is disabled")
	}

	runtime := WebSearchRuntime{
		ProviderProfileID:     BigModelCodingProvider,
		CredentialFingerprint: shortFingerprint(provider.APIKey),
		ModelLabel:            BigModelSearchMCPToolName,
		MaxCallsPerTurn:       policy.MaxCallsPerTurn,
		MaxCallsPerSession:    policy.MaxCallsPerSession,
	}
	if runtime.MaxCallsPerTurn <= 0 {
		runtime.MaxCallsPerTurn = 2
	}
	if runtime.MaxCallsPerSession <= 0 {
		runtime.MaxCallsPerSession = 10
	}
	runtime.ConfigEpoch = shortFingerprintFields(
		runtime.ProviderProfileID,
		runtime.ModelLabel,
		runtime.CredentialFingerprint,
		strconv.Itoa(runtime.MaxCallsPerTurn),
		strconv.Itoa(runtime.MaxCallsPerSession),
	)
	return runtime, nil
}

// ImageModels returns only models backed by an exact built-in profile or an
// explicit custom ImageEndpoint. Merely having an OpenAI-compatible chat base
// URL never creates an image capability.
func ImageModels(cfg *config.Config) []ImageModel {
	if cfg == nil {
		return nil
	}
	providers := cfg.GetProviders()
	result := make([]ImageModel, 0)
	for providerID, provider := range providers {
		if provider == nil {
			continue
		}
		if isBigModelCodingProfile(providerID, provider) &&
			(provider.ImageEndpoint == nil ||
				!configuredImageModel(provider.ImageEndpoint.Models, "cogview-3-flash")) {
			result = append(result, ImageModel{
				Provider: providerID, ID: "cogview-3-flash", Name: "CogView 3 Flash",
				Protocol: string(imagegen.ProtocolOpenAIImages),
				Sizes:    []string{"1024x1024"}, Builtin: true, Supported: true,
			})
		}
		if isAlibabaTokenPlanCNProfile(providerID, provider) {
			builtin := []ImageModel{
				{
					Provider: providerID, ID: "wan2.7-image", Name: "Wan 2.7 Image",
					Protocol: string(imagegen.ProtocolTokenPlanMultimodal),
					Sizes:    []string{"1024x1024", "720x1280", "1280x720"}, Builtin: true, Supported: true,
				},
				{
					Provider: providerID, ID: "wan2.7-image-pro", Name: "Wan 2.7 Image Pro",
					Protocol: string(imagegen.ProtocolTokenPlanMultimodal),
					Sizes: []string{
						"1024x1024", "720x1280", "1280x720",
						"2048x2048", "1440x2560", "2560x1440",
					},
					Builtin: true, Supported: true,
				},
			}
			for _, model := range builtin {
				if provider.ImageEndpoint == nil || !configuredImageModel(provider.ImageEndpoint.Models, model.ID) {
					result = append(result, model)
				}
			}
		}
		if isManagedXAIProfile(providerID, provider) {
			result = append(result, []ImageModel{
				{
					Provider: providerID, ID: "grok-imagine-image", Name: "Grok Imagine Image",
					Protocol: string(imagegen.ProtocolOpenAIImages), Builtin: true, Supported: true,
				},
				{
					Provider: providerID, ID: "grok-imagine-image-quality", Name: "Grok Imagine Image Quality",
					Protocol: string(imagegen.ProtocolOpenAIImages), Builtin: true, Supported: true,
				},
			}...)
			continue
		}
		if provider.ImageEndpoint == nil {
			continue
		}
		protocol := strings.TrimSpace(provider.ImageEndpoint.Protocol)
		for _, model := range provider.ImageEndpoint.Models {
			id := strings.TrimSpace(model.ID)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(model.Name)
			if name == "" {
				name = id
			}
			result = append(result, ImageModel{
				Provider: providerID, ID: id, Name: name, Protocol: protocol,
				Sizes:     append([]string(nil), model.Sizes...),
				Supported: supportedImageProtocol(protocol),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Provider != result[j].Provider {
			return result[i].Provider < result[j].Provider
		}
		return result[i].ID < result[j].ID
	})
	return dedupeImageModels(result)
}

// ResolveImageRuntime is the execution gate for generate_image. Image
// generation is an independent configured model role, so provider tool policy
// does not enable or disable it. An empty image_model, missing credential,
// unknown model, or unknown protocol fails closed and keeps the tool out.
func ResolveImageRuntime(cfg *config.Config) (ImageRuntime, error) {
	if cfg == nil || strings.TrimSpace(cfg.ImageModel) == "" {
		return ImageRuntime{}, fmt.Errorf("image model is not configured")
	}
	providerID, modelID, ok := strings.Cut(strings.TrimSpace(cfg.ImageModel), "/")
	if !ok || strings.TrimSpace(providerID) == "" || strings.TrimSpace(modelID) == "" {
		return ImageRuntime{}, fmt.Errorf("image_model must use provider/model format")
	}
	provider := cfg.GetProviders()[providerID]
	managedXAI := isManagedXAIProfile(providerID, provider)
	if provider == nil || (strings.TrimSpace(provider.APIKey) == "" && !managedXAI) {
		return ImageRuntime{}, fmt.Errorf("image provider is not configured")
	}
	policy, ok := provider.ProviderTools[ToolImageGeneration]
	if !ok {
		policy = config.ProviderToolPolicy{}
	}
	headers, err := canonicalOutboundHeaders(provider.Headers)
	if err != nil {
		return ImageRuntime{}, err
	}
	headerDigest := canonicalHeaderDigest(headers)

	runtime := ImageRuntime{
		Provider: providerID, Model: modelID, APIKey: provider.APIKey,
		Headers: headers,
		CredentialFingerprint: shortFingerprintFields(
			"api_key", provider.APIKey, "outbound_headers", headerDigest,
		),
		MaxCallsPerTurn:    policy.MaxCallsPerTurn,
		MaxCallsPerSession: policy.MaxCallsPerSession,
	}
	if managedXAI {
		// Managed xAI policy owns the endpoint and protected headers. Ignore any
		// stale hand-edited endpoint/header fields instead of forwarding the OAuth
		// bearer token to configuration-controlled destinations.
		runtime.Headers = nil
		runtime.AuthMethod = provider.Auth.Method
		runtime.AccountID = provider.Auth.AccountID
		runtime.CredentialFingerprint = shortFingerprintFields(
			"managed_account", runtime.AuthMethod, runtime.AccountID,
		)
	}
	if runtime.MaxCallsPerTurn <= 0 {
		runtime.MaxCallsPerTurn = 1
	}
	if runtime.MaxCallsPerSession <= 0 {
		runtime.MaxCallsPerSession = 20
	}
	if managedXAI && !isXAIImageModel(modelID) {
		return ImageRuntime{}, fmt.Errorf("selected image model is not declared by the managed xAI profile")
	}

	endpoint := provider.ImageEndpoint
	switch {
	case managedXAI && isXAIImageModel(modelID):
		runtime.Protocol = imagegen.ProtocolOpenAIImages
		runtime.BaseURL = "https://api.x.ai/v1"
		runtime.AssetHosts = []string{"*.x.ai"}
	case endpoint != nil && configuredImageModel(endpoint.Models, modelID):
		runtime.Protocol = imagegen.Protocol(strings.TrimSpace(endpoint.Protocol))
		if !supportedImageProtocol(string(runtime.Protocol)) {
			return ImageRuntime{}, fmt.Errorf("unsupported image protocol %q", runtime.Protocol)
		}
		runtime.BaseURL = canonicalBaseURL(endpoint.BaseURL)
		runtime.AssetHosts = append([]string(nil), endpoint.AssetHosts...)
	case isBigModelCodingProfile(providerID, provider) && modelID == "cogview-3-flash":
		runtime.Protocol = imagegen.ProtocolOpenAIImages
		runtime.BaseURL, _ = bigModelCodingProfileBaseURL(providerID, provider)
		runtime.AssetHosts = []string{"*.bigmodel.cn", "*.chatglm.cn"}
	case isAlibabaTokenPlanCNProfile(providerID, provider) && isAlibabaWanImageModel(modelID):
		runtime.Protocol = imagegen.ProtocolTokenPlanMultimodal
		runtime.BaseURL = alibabaTokenPlanCNImageURL
		runtime.AssetHosts = []string{
			"*.oss-cn-beijing.aliyuncs.com",
			"*.oss-cn-shanghai.aliyuncs.com",
			"*.oss-cn-hangzhou.aliyuncs.com",
			"*.oss-accelerate.aliyuncs.com",
		}
	default:
		return ImageRuntime{}, fmt.Errorf("selected image model is not declared by the provider endpoint")
	}
	if runtime.BaseURL == "" {
		return ImageRuntime{}, fmt.Errorf("image endpoint base URL is required")
	}
	assetHostsDigest := canonicalStringSetDigest(runtime.AssetHosts)
	runtime.ConfigEpoch = shortFingerprintFields(
		providerID, modelID, string(runtime.Protocol), runtime.BaseURL,
		runtime.CredentialFingerprint, headerDigest, assetHostsDigest,
		strconv.Itoa(runtime.MaxCallsPerTurn), strconv.Itoa(runtime.MaxCallsPerSession),
	)
	return runtime, nil
}

func isManagedXAIProfile(providerID string, provider *config.ProviderConfig) bool {
	return providerID == "xai" && provider != nil && provider.Auth != nil &&
		provider.Auth.Method == "xai_oauth"
}

func isXAIImageModel(modelID string) bool {
	return modelID == "grok-imagine-image" || modelID == "grok-imagine-image-quality"
}

// ImageEndpointProfile is an opaque, Settings-safe identifier for the final
// resolved endpoint. The endpoint URL itself is never returned by capability
// metadata or written to session records.
func ImageEndpointProfile(runtime ImageRuntime) string {
	return "image:" + shortFingerprintFields(runtime.Provider, string(runtime.Protocol), runtime.BaseURL)
}

// EffectiveMCPServers adds provider-managed MCP presets to a detached runtime
// map. The detached Authorization header is never assigned to Config.MCPServers
// and therefore cannot be returned by generic MCP CRUD endpoints.
func EffectiveMCPServers(cfg *config.Config) map[string]*config.MCPServer {
	result := make(map[string]*config.MCPServer)
	if cfg == nil {
		return result
	}
	for name, server := range cfg.MCPServers {
		if IsProviderSearchMCPServer(name) {
			continue
		}
		if server == nil {
			continue
		}
		copy := *server
		copy.Headers = cloneHeaders(server.Headers)
		result[name] = &copy
	}
	runtime, err := ResolveWebSearchTransportRuntime(cfg)
	if err != nil {
		return result
	}
	provider := cfg.GetProviders()[runtime.ProviderProfileID]
	result[bigModelSearchMCPName] = &config.MCPServer{
		Type: "http", URL: bigModelSearchMCPURL,
		Headers:        map[string]string{"Authorization": "Bearer " + provider.APIKey},
		TimeoutSeconds: 180,
	}
	return result
}

func isBigModelCodingProfile(providerID string, provider *config.ProviderConfig) bool {
	_, ok := bigModelCodingProfileBaseURL(providerID, provider)
	return ok
}

func isAlibabaTokenPlanCNProfile(providerID string, provider *config.ProviderConfig) bool {
	if providerID != AlibabaTokenPlanCNProvider || provider == nil {
		return false
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		return true
	}
	return canonicalBaseURL(provider.BaseURL) == alibabaTokenPlanCNBaseURL
}

func isAlibabaWanImageModel(modelID string) bool {
	switch strings.TrimSpace(modelID) {
	case "wan2.7-image", "wan2.7-image-pro":
		return true
	default:
		return false
	}
}

func supportedImageProtocol(protocol string) bool {
	switch imagegen.Protocol(strings.TrimSpace(protocol)) {
	case imagegen.ProtocolOpenAIImages, imagegen.ProtocolTokenPlanMultimodal:
		return true
	default:
		return false
	}
}

// A blank provider BaseURL means "use the registry default" throughout the
// rest of JCode. Treat it as the canonical Coding Plan endpoint here too, while
// still refusing every explicit proxy or alternate profile.
func bigModelCodingProfileBaseURL(
	providerID string,
	provider *config.ProviderConfig,
) (string, bool) {
	if providerID != BigModelCodingProvider || provider == nil {
		return "", false
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		return bigModelCodingBaseURL, true
	}
	canonical := canonicalBaseURL(provider.BaseURL)
	return canonical, canonical == bigModelCodingBaseURL
}

func canonicalBaseURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

func configuredImageModel(models []config.ImageModelConfig, target string) bool {
	for _, model := range models {
		if strings.TrimSpace(model.ID) == target {
			return true
		}
	}
	return false
}

func cloneHeaders(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

// canonicalOutboundHeaders normalizes case-insensitive HTTP field names before
// both dispatch and fingerprinting. Conflicting case variants fail closed;
// otherwise Go map iteration order cannot decide which credential is sent.
func canonicalOutboundHeaders(input map[string]string) (map[string]string, error) {
	if len(input) == 0 {
		return nil, nil
	}
	rawKeys := make([]string, 0, len(input))
	for key := range input {
		rawKeys = append(rawKeys, key)
	}
	sort.Slice(rawKeys, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(rawKeys[i]))
		right := strings.ToLower(strings.TrimSpace(rawKeys[j]))
		if left != right {
			return left < right
		}
		return rawKeys[i] < rawKeys[j]
	})
	result := make(map[string]string, len(input))
	for _, rawKey := range rawKeys {
		key := http.CanonicalHeaderKey(strings.TrimSpace(rawKey))
		if key == "" {
			continue
		}
		value := input[rawKey]
		if existing, duplicate := result[key]; duplicate && existing != value {
			return nil, fmt.Errorf("conflicting image provider header %q", key)
		}
		result[key] = value
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func canonicalHeaderDigest(headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var material strings.Builder
	for _, key := range keys {
		appendDigestField(&material, key)
		appendDigestField(&material, headers[key])
	}
	return shortFingerprint(material.String())
}

func canonicalStringSetDigest(values []string) string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
		if value != "" {
			set[value] = struct{}{}
		}
	}
	normalized := make([]string, 0, len(set))
	for value := range set {
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	var material strings.Builder
	for _, value := range normalized {
		appendDigestField(&material, value)
	}
	return shortFingerprint(material.String())
}

func appendDigestField(material *strings.Builder, value string) {
	material.WriteString(strconv.Itoa(len(value)))
	material.WriteByte(':')
	material.WriteString(value)
}

func dedupeImageModels(input []ImageModel) []ImageModel {
	seen := make(map[string]bool, len(input))
	result := input[:0]
	for _, model := range input {
		key := model.Provider + "\x00" + model.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, model)
	}
	return result
}

func shortFingerprint(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}

func shortFingerprintFields(values ...string) string {
	var material strings.Builder
	for _, value := range values {
		appendDigestField(&material, value)
	}
	return shortFingerprint(material.String())
}
