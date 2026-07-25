package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// projectConfigFile is the project-level config filename inside <dir>/.jcode/.
const projectConfigFile = "config.json"

// GitRoot returns the top-level directory of the git repository containing dir,
// or "" if dir is not inside a git work tree. It shells out to git with a 2s
// timeout so a hung network mount cannot block startup indefinitely. The
// returned path has symlinks resolved (macOS /var → /private/var).
func GitRoot(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	cmd.Env = scrubbedGitEnv()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return ""
	}
	root := strings.TrimSpace(out.String())
	// Resolve symlinks so the result is comparable to caller-provided paths
	// (macOS: /var/folders/... → /private/var/folders/...).
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		return resolved
	}
	return root
}

// scrubbedGitEnv returns the process environment with repo-targeting GIT_*
// variables removed so git subprocesses resolve against -C/cwd rather than an
// inherited GIT_DIR (e.g. from a parent repo's hook).
func scrubbedGitEnv() []string {
	drop := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true,
		"GIT_OBJECT_DIRECTORY": true, "GIT_COMMON_DIR": true, "GIT_PREFIX": true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_NAMESPACE": true,
	}
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			out = append(out, kv)
		}
	}
	return out
}

// ConfigWalkDirs returns the list of directories from gitRoot down to pwd
// (inclusive), ordered root-first. When gitRoot is empty or pwd is not a
// descendant of gitRoot, it returns just [pwd]. Exported for use by the
// prompts package (walk-up AGENTS.md).
func ConfigWalkDirs(gitRoot, pwd string) []string {
	if gitRoot == "" {
		return []string{pwd}
	}
	// Resolve symlinks on pwd so it is comparable to the already-resolved
	// gitRoot (macOS: /var → /private/var).
	resolvedPwd := pwd
	if r, err := filepath.EvalSymlinks(pwd); err == nil {
		resolvedPwd = r
	}
	// Ensure pwd is under gitRoot.
	rel, err := filepath.Rel(gitRoot, resolvedPwd)
	if err != nil || strings.HasPrefix(rel, "..") {
		return []string{pwd}
	}
	if rel == "." {
		return []string{pwd}
	}

	parts := strings.Split(rel, string(filepath.Separator))
	dirs := make([]string, 0, len(parts)+1)
	dirs = append(dirs, gitRoot)
	cur := gitRoot
	for _, p := range parts {
		cur = filepath.Join(cur, p)
		dirs = append(dirs, cur)
	}
	return dirs
}

// LoadProjectConfig discovers and merges project-level config files. It walks
// from the git repository root down to pwd, loading <dir>/.jcode/config.json at
// each level. Closer-to-pwd files have higher precedence (merged last).
//
// Returns nil (without error) when no project config exists anywhere in the
// chain — a missing project config is the common case and not an error.
func LoadProjectConfig(pwd string) (*Config, error) {
	if pwd == "" {
		return nil, nil
	}
	return loadProjectConfigWithRoot(pwd, GitRoot(pwd))
}

// loadProjectConfigWithRoot is the internal implementation that accepts a
// pre-resolved gitRoot to avoid redundant git subprocess calls.
func loadProjectConfigWithRoot(pwd, gitRoot string) (*Config, error) {
	if pwd == "" {
		return nil, nil
	}

	dirs := ConfigWalkDirs(gitRoot, pwd)

	var merged *Config
	for _, dir := range dirs {
		path := filepath.Join(dir, configDir, projectConfigFile)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("project config read %s: %w", path, err)
		}
		var pc Config
		if err := json.Unmarshal(data, &pc); err != nil {
			return nil, fmt.Errorf("project config parse %s: %w", path, err)
		}
		if merged == nil {
			merged = &pc
		} else {
			mergeProjectFields(merged, &pc)
		}
	}
	return merged, nil
}

// MergeProjectConfig merges a project-level config overlay into the base
// (global) config. The merge is field-by-field: project values override global
// values when set. Security-sensitive fields (providers, telemetry, cloud,
// SSH/Docker aliases) are NEVER taken from project config — a malicious repo
// must not be able to redirect API keys or exfiltrate credentials.
//
// MCP servers are merged by name: the project can add new servers or override
// individual fields of an existing server (e.g. change args), but cannot
// change command/url of an existing server (security).
//
// The base config is mutated in place and returned for convenience.
func MergeProjectConfig(base, overlay *Config) *Config {
	if base == nil || overlay == nil {
		return base
	}
	mergeProjectFields(base, overlay)
	return base
}

// mergeProjectFields performs the actual field-by-field merge. It is used both
// for merging the project chain internally and for the final global→project
// merge.
func mergeProjectFields(base, overlay *Config) {
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
	//   - AutoApprove / DefaultMode (privilege escalation)
	// A project config that sets these fields has them silently ignored.
}

// ApplyProjectOverlay loads and merges all project-level configuration sources
// into cfg: walk-up .jcode/config.json files and standalone mcp.json files.
// This is the single entry point the command layer calls after LoadConfig().
// Errors from individual files are logged but do not abort startup — a broken
// project config should not prevent the agent from running.
func ApplyProjectOverlay(cfg *Config, pwd string) {
	if cfg == nil || pwd == "" {
		return
	}

	// Resolve git root once and share across both loaders to avoid spawning
	// two git subprocesses per startup.
	gitRoot := GitRoot(pwd)

	// Walk-up project config.json
	if projCfg, err := loadProjectConfigWithRoot(pwd, gitRoot); err != nil {
		Logger().Printf("[config] project config error (ignored): %v", err)
	} else if projCfg != nil {
		MergeProjectConfig(cfg, projCfg)
	}

	// Standalone mcp.json files
	if mcpServers, err := loadMCPFilesWithRoot(pwd, gitRoot); err != nil {
		Logger().Printf("[config] mcp file error (ignored): %v", err)
	} else {
		MergeMCPServers(cfg, mcpServers)
	}
}

// mergeMCPServer merges overlay fields into an existing MCP server config.
// Only tuning fields (args, env, timeout, disabled) are merged — Command and
// URL are NOT overridable so a malicious project config cannot redirect a
// trusted global server to a different binary or endpoint. New servers (added
// via the map-merge in mergeProjectFields) get their full definition from the
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
