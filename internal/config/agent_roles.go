package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const maxAgentRoleFileBytes = 64 << 10

var agentRoleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

var builtinAgentRoleNames = map[string]bool{
	"explore": true, "general": true, "coordinator": true, "coder": true,
}

type agentRoleFile struct {
	Name         string `json:"name,omitempty"`
	Description  string `json:"description"`
	Profile      string `json:"profile,omitempty"`
	Instructions string `json:"instructions"`
	Model        string `json:"model,omitempty"`
}

func normalizeAgentRole(name string, role AgentRoleConfig) (AgentRoleConfig, error) {
	name = strings.TrimSpace(name)
	if !agentRoleNamePattern.MatchString(name) {
		return role, fmt.Errorf("agent role name %q must match %s", name, agentRoleNamePattern)
	}
	if builtinAgentRoleNames[name] {
		return role, fmt.Errorf("agent role name %q is reserved", name)
	}
	role.Description = strings.TrimSpace(role.Description)
	role.Instructions = strings.TrimSpace(role.Instructions)
	role.Profile = strings.TrimSpace(role.Profile)
	role.Model = strings.TrimSpace(role.Model)
	if role.Description == "" || role.Instructions == "" {
		return role, fmt.Errorf("agent role %q requires description and instructions", name)
	}
	if len(role.Description) > 1024 || len(role.Instructions) > 32<<10 || len(role.Model) > 256 {
		return role, fmt.Errorf("agent role %q exceeds metadata limits", name)
	}
	if role.Profile == "" {
		role.Profile = "explore"
	}
	switch role.Profile {
	case "explore", "general", "coordinator":
	default:
		return role, fmt.Errorf("agent role %q has invalid profile %q", name, role.Profile)
	}
	return role, nil
}

func loadAgentRoleDir(dir string, dst map[string]AgentRoleConfig) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			Logger().Printf("[agents] skip %s: %v", dir, err)
		}
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil || info.Size() > maxAgentRoleFileBytes {
			Logger().Printf("[agents] skip %s: file exceeds 64 KiB or cannot be inspected", path)
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			Logger().Printf("[agents] skip %s: %v", path, err)
			continue
		}
		dec := json.NewDecoder(io.LimitReader(file, maxAgentRoleFileBytes+1))
		dec.DisallowUnknownFields()
		var raw agentRoleFile
		err = dec.Decode(&raw)
		if err == nil {
			var trailing any
			if trailingErr := dec.Decode(&trailing); trailingErr != io.EOF {
				err = fmt.Errorf("trailing JSON")
			}
		}
		_ = file.Close()
		if err != nil {
			Logger().Printf("[agents] skip malformed %s: %v", path, err)
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if strings.TrimSpace(raw.Name) != "" {
			name = strings.TrimSpace(raw.Name)
		}
		role, err := normalizeAgentRole(name, AgentRoleConfig{
			Description: raw.Description, Profile: raw.Profile,
			Instructions: raw.Instructions, Model: raw.Model,
		})
		if err != nil {
			Logger().Printf("[agents] skip %s: %v", path, err)
			continue
		}
		if seen[name] {
			Logger().Printf("[agents] skip duplicate role %q in %s", name, dir)
			continue
		}
		seen[name] = true
		dst[name] = role
	}
}

// LoadAgentRoles follows Codex's layered role idea with jcode-native JSON:
// user files < project files < inline config. Malformed files are warning-only
// and cannot override a valid lower layer.
func LoadAgentRoles(pwd string, cfg *Config) map[string]AgentRoleConfig {
	roles := make(map[string]AgentRoleConfig)
	loadAgentRoleDir(filepath.Join(ConfigDir(), "agents"), roles)
	if pwd != "" {
		loadAgentRoleDir(filepath.Join(pwd, ".jcode", "agents"), roles)
	}
	if cfg != nil {
		names := make([]string, 0, len(cfg.Agents))
		for name := range cfg.Agents {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if cfg.Agents[name] == nil {
				continue
			}
			role, err := normalizeAgentRole(name, *cfg.Agents[name])
			if err != nil {
				Logger().Printf("[agents] skip inline role: %v", err)
				continue
			}
			roles[name] = role
		}
	}
	return roles
}

func AgentRoleNames(roles map[string]AgentRoleConfig) []string {
	names := make([]string, 0, len(roles))
	for name := range roles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
