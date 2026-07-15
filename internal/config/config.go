package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	configDir  = ".jcode"
	configFile = "config.json"
)

type ProviderConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url,omitempty"`
	// Name is an optional display name for custom providers not in the registry.
	Name string `json:"name,omitempty"`
	// Headers are extra HTTP headers injected into every request to this
	// provider's endpoint (e.g. a gateway's "X-Api-Key" or "X-Org-Id"). Values
	// may be secrets — they are masked by the API and never logged.
	Headers map[string]string `json:"headers,omitempty"`
	// Vision, when non-nil, overrides registry detection of image-input support
	// for this provider. nil ⇒ defer to registry metadata (default: allow images).
	Vision *bool `json:"vision,omitempty"`
	// Thinking, when non-nil, explicitly toggles extended reasoning for this
	// provider. It is sent as the OpenAI-compatible chat_template_kwargs
	// {"enable_thinking": <bool>} extension (e.g. qwen3 gateways). nil ⇒ omit.
	Thinking *bool `json:"thinking,omitempty"`
	// ReasoningEffort controls thinking depth via the OpenAI-compatible
	// "reasoning_effort" parameter. One of "", "low", "medium", "high".
	// Empty ⇒ omit the parameter.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// Deprecated: model lists are now sourced from the models.dev registry.
	// Preserved for backward compatibility with existing config files.
	Models []string `json:"models,omitempty"`
	// CustomModels defines additional models for this provider.
	// These are merged into the registry and treated identically to built-in models.
	CustomModels []CustomModelConfig `json:"custom_models,omitempty"`
}

// CustomModelConfig defines a model that can be added via config.
type CustomModelConfig struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	ToolCall  bool   `json:"tool_call,omitempty"`
	Reasoning bool   `json:"reasoning,omitempty"`
	Context   int    `json:"context,omitempty"`
	// Attachment marks the model as accepting image inputs. When false (the
	// default) the model inherits the provider-level Vision override (if set) or
	// the registry default (allow images).
	Attachment bool `json:"attachment,omitempty"`
	// EffortTiers are the selectable reasoning-effort levels for a reasoning
	// model, e.g. ["minimal","low","medium","high","max"]. When Reasoning is true
	// and this is non-empty, it overrides the default standard effort options;
	// when empty the standard set (minimal/low/medium/high) is used.
	EffortTiers []string `json:"effort_tiers,omitempty"`
}

// SSHAlias represents a saved SSH connection alias
type SSHAlias struct {
	Name string `json:"name"`
	Addr string `json:"addr"`           // user@host
	Path string `json:"path,omitempty"` // remote working directory
}

// DockerAlias represents a saved Docker container alias
type DockerAlias struct {
	Name      string `json:"name"`
	Container string `json:"container"`      // container name or id
	Path      string `json:"path,omitempty"` // working directory inside the container
}

// MCPServer represents a configured MCP server connection
type MCPServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     []string          `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// TimeoutSeconds is the request timeout for HTTP/SSE transports. 0 → default (180s).
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// Disabled, when true, excludes this server from tool loading without deleting it.
	Disabled bool `json:"disabled,omitempty"`
	// OAuth configures OAuth 2.0 authorization for HTTP/SSE transports (MCP
	// authorization spec). When nil, only static Headers are used.
	OAuth *MCPOAuthConfig `json:"oauth,omitempty"`
}

// MCPOAuthConfig holds OAuth 2.0 settings for an MCP server. Tokens are NOT
// stored here — they live in ~/.jcode/oauth/<server>.json. ClientID/ClientSecret
// are the manual fallback used when the authorization server does not support
// dynamic client registration (RFC 7591).
type MCPOAuthConfig struct {
	// Enabled turns on the OAuth bearer-token flow for this server.
	Enabled bool `json:"enabled,omitempty"`
	// ClientID is the OAuth client id. Empty → attempt dynamic registration.
	ClientID string `json:"client_id,omitempty"`
	// ClientSecret is set for confidential clients (manual fallback).
	ClientSecret string `json:"client_secret,omitempty"`
	// Scopes is the list of OAuth scopes to request.
	Scopes []string `json:"scopes,omitempty"`
	// AuthServerMetadataURL optionally overrides automatic metadata discovery.
	AuthServerMetadataURL string `json:"auth_server_metadata_url,omitempty"`
}

// LangfuseConfig holds Langfuse telemetry credentials.
type LangfuseConfig struct {
	Host      string `json:"LANGFUSE_BASE_URL,omitempty"`
	PublicKey string `json:"LANGFUSE_PUBLIC_KEY,omitempty"`
	SecretKey string `json:"LANGFUSE_SECRET_KEY,omitempty"`
}

// TelemetryConfig holds optional observability integrations.
type TelemetryConfig struct {
	Langfuse *LangfuseConfig `json:"langfuse,omitempty"`
}

// BudgetConfig controls token and cost budget limits.
type BudgetConfig struct {
	MaxTokensPerTurn  int64   `json:"max_tokens_per_turn,omitempty"`
	MaxCostPerSession float64 `json:"max_cost_per_session,omitempty"`
	WarningThreshold  float64 `json:"warning_threshold,omitempty"`
}

// ChannelConfig controls external messaging channel behavior.
type ChannelConfig struct {
	// WebEnabled enables WeChat channel in web mode (default false).
	WebEnabled bool `json:"web_enabled,omitempty"`
	// BLEEnabled enables BLE device notifications (default false).
	BLEEnabled bool `json:"ble_enabled,omitempty"`
}

// CompactionConfig controls automatic context compaction. Compaction always
// runs on the session's main model: summary quality directly bounds the
// agent's post-compaction performance, so it is deliberately not routed to
// SmallModel (a former "summary_model" key was parsed but never honored, and
// has been removed).
type CompactionConfig struct {
	Enabled    bool    `json:"enabled,omitempty"`
	Threshold  float64 `json:"threshold,omitempty"`
	KeepRecent int     `json:"keep_recent,omitempty"`
}

// PromptConfig controls prompt system behavior.
type PromptConfig struct {
	Compaction      *CompactionConfig `json:"compaction,omitempty"`
	MemoryMaxChars  int               `json:"memory_max_chars,omitempty"`
	MemoryMaxDepth  int               `json:"memory_max_depth,omitempty"`
	CacheEnabled    bool              `json:"cache_enabled,omitempty"`
	AsyncEnvTimeout string            `json:"async_env_timeout,omitempty"`
}

// SubagentConfig controls subagent behavior.
type SubagentConfig struct {
	MaxParallel  int `json:"max_parallel,omitempty"`
	MaxCompleted int `json:"max_completed,omitempty"`
	MaxDepth     int `json:"max_depth,omitempty"`
}

// MemoryConfig controls cross-session learned memory (the file-based store
// under ~/.jcode/memory). See internal-doc/agent-memory-design.md. All fields
// have defaults so zero config works; Enabled/Generate are pointers because
// their default is true.
type MemoryConfig struct {
	Enabled *bool `json:"enabled,omitempty"` // default true; false disables read+write
	// Generate gates the offline distillation pipeline (M2+); false keeps the
	// system a read-only/manual notebook.
	Generate *bool `json:"generate,omitempty"` // default true
	// Model for pipeline extraction, "provider/model". Empty → main Model.
	// Deliberately not routed through SmallModel: distilled memories persist
	// across sessions, so extraction quality matters more than the token cost.
	Model string `json:"model,omitempty"`
	// DailyTokenBudget caps pipeline token spend per day (BYOM guard).
	DailyTokenBudget int `json:"daily_token_budget,omitempty"` // default 300000
	CooldownHours    int `json:"cooldown_hours,omitempty"`     // default 6
	MaxAgeDays       int `json:"max_age_days,omitempty"`       // default 30
	MaxUnusedDays    int `json:"max_unused_days,omitempty"`    // default 45
	Phase2TopN       int `json:"phase2_top_n,omitempty"`       // default 40
	// SummaryInjectTokens caps the memory summary injected into the system prompt.
	SummaryInjectTokens int `json:"summary_inject_tokens,omitempty"` // default 1200
}

// MemoryEnabled reports whether the memory system is on (default true).
func MemoryEnabled(c *Config) bool {
	if c == nil || c.Memory == nil || c.Memory.Enabled == nil {
		return true
	}
	return *c.Memory.Enabled
}

// MemoryGenerate reports whether the distillation pipeline may run (default true).
func MemoryGenerate(c *Config) bool {
	if !MemoryEnabled(c) {
		return false
	}
	if c == nil || c.Memory == nil || c.Memory.Generate == nil {
		return true
	}
	return *c.Memory.Generate
}

// MemorySummaryInjectTokens returns the summary injection cap (default 1200).
func MemorySummaryInjectTokens(c *Config) int {
	if c != nil && c.Memory != nil && c.Memory.SummaryInjectTokens > 0 {
		return c.Memory.SummaryInjectTokens
	}
	return 1200
}

// MemoryDailyTokenBudget returns the pipeline daily token budget (default 300k).
func MemoryDailyTokenBudget(c *Config) int {
	if c != nil && c.Memory != nil && c.Memory.DailyTokenBudget > 0 {
		return c.Memory.DailyTokenBudget
	}
	return 300000
}

// MemoryCooldownHours returns the pipeline cooldown (default 6).
func MemoryCooldownHours(c *Config) int {
	if c != nil && c.Memory != nil && c.Memory.CooldownHours > 0 {
		return c.Memory.CooldownHours
	}
	return 6
}

// MemoryMaxAgeDays returns the extraction window (default 30).
func MemoryMaxAgeDays(c *Config) int {
	if c != nil && c.Memory != nil && c.Memory.MaxAgeDays > 0 {
		return c.Memory.MaxAgeDays
	}
	return 30
}

// MemoryMaxUnusedDays returns the unused-expiry window (default 45).
func MemoryMaxUnusedDays(c *Config) int {
	if c != nil && c.Memory != nil && c.Memory.MaxUnusedDays > 0 {
		return c.Memory.MaxUnusedDays
	}
	return 45
}

// MemoryPhase2TopN returns the consolidation input cap (default 40).
func MemoryPhase2TopN(c *Config) int {
	if c != nil && c.Memory != nil && c.Memory.Phase2TopN > 0 {
		return c.Memory.Phase2TopN
	}
	return 40
}

// Config represents the application configuration
type Config struct {
	// Provider settings: map of provider name → config (api_key, base_url)
	Providers map[string]*ProviderConfig `json:"providers"`
	// Deprecated: use Providers instead. Kept for backward compatibility.
	Models map[string]*ProviderConfig `json:"models,omitempty"`

	// Active model in "provider/model" format (e.g. "openai/gpt-4o")
	Model string `json:"model"`
	// SmallModel is an optional lightweight model in "provider/model" format.
	// It backs the "small" model alias (subagent/flow model params) and LLM
	// session-title generation. Unset → those paths use the main model /
	// truncated titles; behavior is unchanged.
	SmallModel string `json:"small_model,omitempty"`

	// ContextLimits overrides the resolved context window (in tokens) for a model.
	// Keys may be "provider/model" (preferred) or a bare model id. Use this to teach
	// jcode the window of a brand-new or custom model the registry doesn't know yet.
	ContextLimits map[string]int `json:"context_limits,omitempty"`
	// DefaultContextLimit is the fallback context window (in tokens) assumed when a
	// model's limit is unknown from the registry and built-in tables. Defaults to 200000.
	DefaultContextLimit int `json:"default_context_limit,omitempty"`

	// Deprecated: use Model field with "provider/model" format instead.
	Provider string `json:"provider,omitempty"`

	MaxIterations int                   `json:"max_iterations,omitempty"`
	SSHAliases    []SSHAlias            `json:"ssh_aliases,omitempty"`
	DockerAliases []DockerAlias         `json:"docker_aliases,omitempty"`
	MCPServers    map[string]*MCPServer `json:"mcp_servers,omitempty"`
	Telemetry     *TelemetryConfig      `json:"telemetry,omitempty"`
	Budget        *BudgetConfig         `json:"budget,omitempty"`
	Compaction    *CompactionConfig     `json:"compaction,omitempty"`
	Prompt        *PromptConfig         `json:"prompt,omitempty"`
	Subagent      *SubagentConfig       `json:"subagent,omitempty"`
	Team          *TeamConfig           `json:"team,omitempty"`
	Memory        *MemoryConfig         `json:"memory,omitempty"`

	// AutoApprove sets the default approval mode to auto on startup.
	//
	// Deprecated: superseded by DefaultMode; still honored as a fallback when
	// DefaultMode is empty (true → "full_access").
	AutoApprove bool `json:"auto_approve,omitempty"`

	// DefaultMode is the unified session mode to start in: "approval" (default),
	// "plan", or "full_access". When empty, AutoApprove is used as a fallback.
	DefaultMode string `json:"default_mode,omitempty"`

	// Theme is the built-in color theme name for the terminal UI (e.g.
	// "jcode-dark", "nord-dark", "github-light"). Empty auto-selects a default
	// from the detected terminal background. See internal/theme for the catalog.
	Theme string `json:"theme,omitempty"`

	// Channel controls external messaging channel behavior.
	Channel *ChannelConfig `json:"channel,omitempty"`

	// DisabledProviders lists provider IDs to exclude from registry
	DisabledProviders []string `json:"disabled_providers,omitempty"`

	// DisabledSkills lists skill names to exclude from the agent (slash commands,
	// system-prompt descriptions, and the load_skill tool).
	DisabledSkills []string `json:"disabled_skills,omitempty"`

	// Browser controls the browser-use capability (CDP-driven page control).
	Browser *BrowserConfig `json:"browser,omitempty"`

	// ApprovalReview holds tuning knobs for the LLM approval reviewer used in
	// Auto session mode. It does not contain an on/off switch — the reviewer is
	// active whenever the session is in Auto mode.
	ApprovalReview *ApprovalReviewConfig `json:"approval_review,omitempty"`
}

// ApprovalReviewConfig holds tuning knobs for jcode's LLM approval reviewer.
// The reviewer is active only in Auto session mode; these settings control its
// model, policy, timeout, investigation behavior, prompt-cache reuse, and audit
// log location. See internal-doc/approval-review-design.md.
type ApprovalReviewConfig struct {
	// Model is the "provider/model" (or "small" alias) the reviewer runs on.
	// Empty resolves to small_model, then to the main model — so the reviewer
	// always has a working model even if small_model is unset.
	Model string `json:"model,omitempty"`
	// Policy is extra workspace-specific policy text appended to the built-in
	// risk policy (e.g. trusted internal hosts, stricter deny rules).
	Policy string `json:"policy,omitempty"`
	// TimeoutSeconds bounds a single review. 0 uses the built-in default.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// Investigate lets the reviewer run read-only tools (read/grep/glob) to
	// gather evidence before deciding (V2). Off by default (single-shot).
	Investigate bool `json:"investigate,omitempty"`
	// ReuseSession keeps a cached reviewer conversation so the large policy
	// prefix is served from the provider's prompt cache across reviews (V3).
	ReuseSession bool `json:"reuse_session,omitempty"`
	// AuditPath overrides the verdict log location. Empty →
	// <config dir>/approval-review.jsonl.
	AuditPath string `json:"audit_path,omitempty"`
}

// approvalReviewMu guards the Config.ApprovalReview pointer against concurrent
// publish/read. The web settings handler swaps the block in on the live shared
// Config from an HTTP goroutine, while a task goroutine reads it to build its
// reviewer on entering Auto mode; those two hold no lock in common, so the
// pointer needs its own. It is package-level rather than a Config field because
// Config is copied by value in a few places and an embedded mutex would trip
// go vet's copylocks check.
var approvalReviewMu sync.RWMutex

// ApprovalReviewSettings returns a snapshot of the reviewer tuning knobs, or
// zero values when unset. The copy means callers never hold a pointer into the
// live config, so a later SetApprovalReview cannot mutate what they read.
func (c *Config) ApprovalReviewSettings() ApprovalReviewConfig {
	approvalReviewMu.RLock()
	defer approvalReviewMu.RUnlock()
	if c == nil || c.ApprovalReview == nil {
		return ApprovalReviewConfig{}
	}
	return *c.ApprovalReview
}

// SetApprovalReview publishes a new reviewer tuning block onto the live config
// so reviewers built after this call pick it up. rc must not be mutated after
// being handed over.
func (c *Config) SetApprovalReview(rc *ApprovalReviewConfig) {
	approvalReviewMu.Lock()
	defer approvalReviewMu.Unlock()
	c.ApprovalReview = rc
}

// BrowserConfig controls the browser-use capability. See
// internal-doc/browser-use-design.md.
type BrowserConfig struct {
	Enabled    bool   `json:"enabled,omitempty"`
	Backend    string `json:"backend,omitempty"`     // auto | managed | extension (default auto)
	ChromePath string `json:"chrome_path,omitempty"` // empty → auto-discover
	Headless   bool   `json:"headless,omitempty"`    // managed backend
	Viewport   string `json:"viewport,omitempty"`    // e.g. "1280x720"
	// Approval holds per-class defaults: "navigate" and "interact" map to
	// "ask" (default) or "always_allow".
	Approval map[string]string `json:"approval,omitempty"`
	// SitePermissions overrides Approval defaults per origin.
	SitePermissions []BrowserSitePermission `json:"site_permissions,omitempty"`
	// DevMode unlocks browser_eval / raw CDP (high-risk). Off by default.
	DevMode bool `json:"dev_mode,omitempty"`
}

// BrowserSitePermission is a per-origin approval override.
type BrowserSitePermission struct {
	Origin   string `json:"origin"`
	Navigate string `json:"navigate,omitempty"` // ask | allow
	Interact string `json:"interact,omitempty"` // ask | allow
}

// TeamConfig controls agent team behavior.
type TeamConfig struct {
	MaxTeammates  int `json:"max_teammates,omitempty"`   // max teammates per team (default 5)
	MailboxPollMs int `json:"mailbox_poll_ms,omitempty"` // mailbox poll interval in ms (default 500)
	MessageCap    int `json:"message_cap,omitempty"`     // max messages in UI per teammate (default 50)
}

// GetProviders returns the effective provider map, merging legacy Models field into Providers.
func (c *Config) GetProviders() map[string]*ProviderConfig {
	if len(c.Providers) > 0 {
		return c.Providers
	}
	return c.Models
}

// GetProviderModel returns the provider name and model name from the active
// Model field. If Model is in "provider/model" format, it splits them.
// Otherwise it falls back to the legacy Provider + Model fields.
func (c *Config) GetProviderModel() (provider, model string) {
	if parts := splitProviderModel(c.Model); len(parts) == 2 {
		return parts[0], parts[1]
	}
	// Legacy fallback
	return c.Provider, c.Model
}

// CompactionThreshold returns the configured fraction (0-1) of the context window
// at which automatic compaction/summarization triggers, or 0.75 when unset/invalid.
func (c *Config) CompactionThreshold() float64 {
	if c != nil && c.Compaction != nil && c.Compaction.Threshold > 0 && c.Compaction.Threshold <= 1 {
		return c.Compaction.Threshold
	}
	return 0.75
}

func splitProviderModel(s string) []string {
	idx := -1
	for i, ch := range s {
		if ch == '/' {
			idx = i
			break
		}
	}
	if idx <= 0 || idx >= len(s)-1 {
		return nil
	}
	return []string{s[:idx], s[idx+1:]}
}

// ConfigDir returns the full path to the config directory (~/.jcode).
func ConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback: use /tmp so callers never get a literal "~" path
		// that Go's filesystem APIs cannot resolve.
		return filepath.Join(os.TempDir(), configDir)
	}
	return filepath.Join(home, configDir)
}

// configFilePath returns the full path to the config file
func configFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, configDir, configFile), nil
}

// HistoryFilePath returns the full path to the history file
func HistoryFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, configDir, "history"), nil
}

// NeedsSetup returns true if the config file does not exist or is incomplete.
func NeedsSetup() bool {
	cfgPath, err := configFilePath()
	if err != nil {
		return true
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return true
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return true
	}
	return len(cfg.GetProviders()) == 0
}

// LoadConfig loads configuration from $HOME/.jcode/config.json.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		MaxIterations: 1000, // default
	}

	cfgPath, err := configFilePath()
	if err != nil {
		return nil, fmt.Errorf("config file path error: %w", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("config file not found at %s, please run setup first", cfgPath)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", cfgPath, err)
	}

	// Migrate legacy "models" field to "providers"
	if len(cfg.Providers) == 0 && len(cfg.Models) > 0 {
		cfg.Providers = cfg.Models
	}

	// Migrate legacy provider IDs to models.dev canonical IDs
	cfg.migrateProviderIDs()

	// Validation
	if len(cfg.GetProviders()) == 0 {
		return nil, fmt.Errorf("no providers configured: set 'providers' in %s", cfgPath)
	}

	// Resolve legacy Provider field format
	if cfg.Provider != "" && !containsSlash(cfg.Model) {
		cfg.Model = cfg.Provider + "/" + cfg.Model
	}

	// Validate Model field is set. This is no longer a hard error: setup no
	// longer forces a model selection, and some agent-construction paths can
	// pick a default at runtime. We only warn so a stale/legacy config with
	// providers but no active model doesn't prevent the app from booting — the
	// caller resolves a concrete model (or surfaces a clearer error) when it
	// actually builds an agent.
	if cfg.Model == "" {
		Logger().Printf("[config] warning: no active model set in %s; it will be resolved on first use", cfgPath)
	}

	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 1000
	}

	return cfg, nil
}

// migrateProviderIDs converts legacy provider IDs to models.dev canonical IDs.
// This ensures backward compatibility with configs using old IDs like "bailian" or "bailian-plan".
func (c *Config) migrateProviderIDs() {
	// Map of legacy provider ID → models.dev canonical ID
	migrations := map[string]string{
		"bailian":      "alibaba-cn",
		"bailian-plan": "alibaba-coding-plan-cn",
	}

	// Migrate provider keys in Providers map
	if c.Providers != nil {
		for oldID, newID := range migrations {
			if provCfg, exists := c.Providers[oldID]; exists {
				c.Providers[newID] = provCfg
				delete(c.Providers, oldID)
				Logger().Printf("[config] Migrated provider ID: %s → %s", oldID, newID)
			}
		}
	}

	// Migrate Model field (active model in "provider/model" format)
	for oldID, newID := range migrations {
		if hasPrefix(c.Model, oldID+"/") {
			c.Model = newID + c.Model[len(oldID):]
			Logger().Printf("[config] Migrated active model: %s → %s", oldID, newID)
		}
	}

	// Migrate SmallModel field
	for oldID, newID := range migrations {
		if hasPrefix(c.SmallModel, oldID+"/") {
			c.SmallModel = newID + c.SmallModel[len(oldID):]
			Logger().Printf("[config] Migrated small_model: %s → %s", oldID, newID)
		}
	}

}

func hasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if s[i] != prefix[i] {
			return false
		}
	}
	return true
}

func containsSlash(s string) bool {
	for _, ch := range s {
		if ch == '/' {
			return true
		}
	}
	return false
}

// SaveConfig writes the config to $HOME/.jcode/config.json.
func SaveConfig(cfg *Config) error {
	cfgPath, err := configFilePath()
	if err != nil {
		return fmt.Errorf("config file path error: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", cfgPath, err)
	}

	return nil
}

// ConfigPath returns the expected path of the config file (for display purposes)
func ConfigPath() string {
	p, err := configFilePath()
	if err != nil {
		return filepath.Join("~", configDir, configFile)
	}
	return p
}

// SessionsDir returns the path to the sessions directory (~/.jcode/sessions).
func SessionsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, configDir, "sessions"), nil
}

// SessionsIndexPath returns the path to the sessions index file
// (~/.jcode/sessions/session.json).
func SessionsIndexPath() (string, error) {
	dir, err := SessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

// UsageDir returns the path to the usage-statistics directory (~/.jcode/usage).
func UsageDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, configDir, "usage"), nil
}

// UsageEventsPath returns the path to the append-only usage event log
// (~/.jcode/usage/events.jsonl), one JSON line per recorded agent turn.
func UsageEventsPath() (string, error) {
	dir, err := UsageDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "events.jsonl"), nil
}
