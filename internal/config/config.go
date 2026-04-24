package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	configDir  = ".jcode"
	configFile = "config.json"
)

type ProviderConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url,omitempty"`
	// Deprecated: model lists are now sourced from the models.dev registry.
	// Preserved for backward compatibility with existing config files.
	Models []string `json:"models,omitempty"`
}

// SSHAlias represents a saved SSH connection alias
type SSHAlias struct {
	Name string `json:"name"`
	Addr string `json:"addr"`           // user@host
	Path string `json:"path,omitempty"` // remote working directory
}

// MCPServer represents a configured MCP server connection
type MCPServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     []string          `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
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

// CompactionConfig controls automatic context compaction.
type CompactionConfig struct {
	Enabled      bool    `json:"enabled,omitempty"`
	Threshold    float64 `json:"threshold,omitempty"`
	KeepRecent   int     `json:"keep_recent,omitempty"`
	SummaryModel string  `json:"summary_model,omitempty"`
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

// Config represents the application configuration
type Config struct {
	// Provider settings: map of provider name → config (api_key, base_url)
	Providers map[string]*ProviderConfig `json:"providers"`
	// Deprecated: use Providers instead. Kept for backward compatibility.
	Models map[string]*ProviderConfig `json:"models,omitempty"`

	// Active model in "provider/model" format (e.g. "openai/gpt-4o")
	Model string `json:"model"`
	// SmallModel for lightweight tasks (summaries, compaction) in "provider/model" format
	SmallModel string `json:"small_model,omitempty"`

	// Deprecated: use Model field with "provider/model" format instead.
	Provider string `json:"provider,omitempty"`

	MaxIterations int                   `json:"max_iterations,omitempty"`
	SSHAliases    []SSHAlias            `json:"ssh_aliases,omitempty"`
	MCPServers    map[string]*MCPServer `json:"mcp_servers,omitempty"`
	Telemetry     *TelemetryConfig      `json:"telemetry,omitempty"`
	Budget        *BudgetConfig         `json:"budget,omitempty"`
	FallbackModel string                `json:"fallback_model,omitempty"`
	Compaction    *CompactionConfig     `json:"compaction,omitempty"`
	Prompt        *PromptConfig         `json:"prompt,omitempty"`
	Subagent      *SubagentConfig       `json:"subagent,omitempty"`
	Team          *TeamConfig           `json:"team,omitempty"`

	// AutoApprove sets the default approval mode to auto on startup.
	AutoApprove bool `json:"auto_approve,omitempty"`

	// Channel controls external messaging channel behavior.
	Channel *ChannelConfig `json:"channel,omitempty"`

	// DisabledProviders lists provider IDs to exclude from registry
	DisabledProviders []string `json:"disabled_providers,omitempty"`
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

	// Validate Model field is set
	if cfg.Model == "" {
		return nil, fmt.Errorf("model not configured: set 'model' field in 'provider/model' format in %s", cfgPath)
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

	// Migrate FallbackModel field
	for oldID, newID := range migrations {
		if hasPrefix(c.FallbackModel, oldID+"/") {
			c.FallbackModel = newID + c.FallbackModel[len(oldID):]
			Logger().Printf("[config] Migrated fallback_model: %s → %s", oldID, newID)
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
