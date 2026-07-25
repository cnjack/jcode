package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// projectConfigFile is the project-level config filename inside <project>/.jcode/.
const projectConfigFile = "config.json"

// LoadProjectConfig reads <projectDir>/.jcode/config.json. It returns nil
// (without error) when the file does not exist — a missing project config is
// the common case and not an error condition. Parse errors are reported.
func LoadProjectConfig(projectDir string) (*Config, error) {
	if projectDir == "" {
		return nil, nil
	}
	path := filepath.Join(projectDir, configDir, projectConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("project config read %s: %w", path, err)
	}
	var pc Config
	if err := json.Unmarshal(data, &pc); err != nil {
		return nil, fmt.Errorf("project config parse %s: %w", path, err)
	}
	return &pc, nil
}

// MergeProjectConfig merges a project-level config overlay into the base
// (global) config. The merge is field-by-field: project values override global
// values when set. Security-sensitive fields (providers, telemetry, cloud,
// SSH/Docker aliases) are NEVER taken from project config — a malicious repo
// must not be able to redirect API keys or exfiltrate credentials.
//
// MCP servers are merged by name: the project can add new servers or override
// individual fields of an existing server (e.g. change args), but cannot
// delete a globally-configured server.
//
// The base config is mutated in place and returned for convenience.
func MergeProjectConfig(base, overlay *Config) *Config {
	if base == nil || overlay == nil {
		return base
	}

	// --- Model selection ---
	if overlay.Model != "" {
		base.Model = overlay.Model
	}
	if overlay.SmallModel != "" {
		base.SmallModel = overlay.SmallModel
	}

	// --- Iteration cap ---
	if overlay.MaxIterations > 0 {
		base.MaxIterations = overlay.MaxIterations
	}

	// --- Session mode ---
	// DefaultMode is intentionally NOT merged: a project config must not
	// escalate to "full_access" and bypass the user's approval policy.

	// --- Theme / Language ---
	if overlay.Theme != "" {
		base.Theme = overlay.Theme
	}
	if overlay.Language != "" {
		base.Language = overlay.Language
	}

	// --- Context limits ---
	if overlay.DefaultContextLimit > 0 {
		base.DefaultContextLimit = overlay.DefaultContextLimit
	}
	if len(overlay.ContextLimits) > 0 {
		if base.ContextLimits == nil {
			base.ContextLimits = make(map[string]int, len(overlay.ContextLimits))
		}
		for k, v := range overlay.ContextLimits {
			base.ContextLimits[k] = v
		}
	}

	// --- MCP servers: merge by name ---
	if len(overlay.MCPServers) > 0 {
		if base.MCPServers == nil {
			base.MCPServers = make(map[string]*MCPServer, len(overlay.MCPServers))
		}
		for name, srv := range overlay.MCPServers {
			if existing := base.MCPServers[name]; existing != nil {
				mergeMCPServer(existing, srv)
			} else {
				base.MCPServers[name] = srv
			}
		}
	}

	// --- Disabled skills: union ---
	if len(overlay.DisabledSkills) > 0 {
		seen := make(map[string]bool, len(base.DisabledSkills))
		for _, s := range base.DisabledSkills {
			seen[s] = true
		}
		for _, s := range overlay.DisabledSkills {
			if !seen[s] {
				base.DisabledSkills = append(base.DisabledSkills, s)
			}
		}
	}

	// --- Disabled providers: union ---
	if len(overlay.DisabledProviders) > 0 {
		seen := make(map[string]bool, len(base.DisabledProviders))
		for _, p := range base.DisabledProviders {
			seen[p] = true
		}
		for _, p := range overlay.DisabledProviders {
			if !seen[p] {
				base.DisabledProviders = append(base.DisabledProviders, p)
			}
		}
	}

	// --- Pointer-block overrides (project replaces the whole block if set) ---
	if overlay.Budget != nil {
		base.Budget = overlay.Budget
	}
	if overlay.Compaction != nil {
		base.Compaction = overlay.Compaction
	}
	if overlay.Prompt != nil {
		base.Prompt = overlay.Prompt
	}
	if overlay.Subagent != nil {
		base.Subagent = overlay.Subagent
	}
	if overlay.Team != nil {
		base.Team = overlay.Team
	}
	if overlay.Browser != nil {
		base.Browser = overlay.Browser
	}
	if overlay.Computer != nil {
		base.Computer = overlay.Computer
	}
	if overlay.ToolSearch != nil {
		base.ToolSearch = overlay.ToolSearch
	}
	if overlay.Channel != nil {
		base.Channel = overlay.Channel
	}
	if overlay.ApprovalReview != nil {
		base.ApprovalReview = overlay.ApprovalReview
	}

	// --- SECURITY DENYLIST ---
	// The following fields are intentionally NOT merged from project config:
	//   - Providers / Models (API keys, base URLs, headers)
	//   - Telemetry (Langfuse secrets)
	//   - Cloud (relay credentials, E2EE keys)
	//   - SSHAliases / DockerAliases (remote access credentials)
	//   - Memory (pipeline model/budget — could redirect to attacker endpoint)
	//   - Developer (debug/tracing toggles)
	//   - AutoApprove (privilege escalation)
	// A project config that sets these fields has them silently ignored.

	return base
}

// mergeMCPServer merges overlay fields into an existing MCP server config.
// Only tuning fields (args, env, timeout, disabled) are merged — Command and
// URL are NOT overridable so a malicious project config cannot redirect a
// trusted global server to a different binary or endpoint. New servers (added
// via the map-merge in MergeProjectConfig) get their full definition from the
// project config, which is the user's explicit choice.
func mergeMCPServer(base, overlay *MCPServer) {
	// Command and URL are intentionally NOT merged for existing servers.
	if len(overlay.Args) > 0 {
		base.Args = overlay.Args
	}
	if len(overlay.Env) > 0 {
		base.Env = overlay.Env
	}
	if len(overlay.Headers) > 0 {
		if base.Headers == nil {
			base.Headers = make(map[string]string, len(overlay.Headers))
		}
		for k, v := range overlay.Headers {
			base.Headers[k] = v
		}
	}
	if overlay.TimeoutSeconds > 0 {
		base.TimeoutSeconds = overlay.TimeoutSeconds
	}
	// Disabled can be set to true by project config (to suppress a global
	// server in this project) but not back to false (project cannot
	// re-enable a server the user globally disabled).
	if overlay.Disabled {
		base.Disabled = true
	}
}
