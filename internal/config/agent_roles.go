package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxAgentRoleFileBytes = 64 << 10

var agentRoleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

var builtinAgentRoleNames = map[string]bool{
	"default": true, "explore": true, "general": true, "coordinator": true, "coder": true,
}

type agentRoleFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Model       string `yaml:"model,omitempty"`
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
	role.Model = strings.TrimSpace(role.Model)
	if role.Description == "" || role.Instructions == "" {
		return role, fmt.Errorf("agent role %q requires description and Markdown instructions", name)
	}
	if len(role.Description) > 1024 || len(role.Instructions) > 32<<10 || len(role.Model) > 256 {
		return role, fmt.Errorf("agent role %q exceeds metadata limits", name)
	}
	if role.Model != "" && role.Model != "small" {
		provider, model, ok := strings.Cut(role.Model, "/")
		if !ok || strings.TrimSpace(provider) == "" || strings.TrimSpace(model) == "" {
			return role, fmt.Errorf(
				"agent role %q model must be \"small\" or use provider/model format", name,
			)
		}
	}
	return role, nil
}

func parseAgentRoleMarkdown(content []byte) (agentRoleFrontmatter, string, error) {
	text := strings.TrimPrefix(string(content), "\uFEFF")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return agentRoleFrontmatter{}, "", fmt.Errorf("missing YAML frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return agentRoleFrontmatter{}, "", fmt.Errorf("unterminated YAML frontmatter")
	}

	var meta agentRoleFrontmatter
	dec := yaml.NewDecoder(strings.NewReader(strings.Join(lines[1:end], "\n")))
	dec.KnownFields(true)
	if err := dec.Decode(&meta); err != nil {
		return agentRoleFrontmatter{}, "", fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple YAML documents")
		}
		return agentRoleFrontmatter{}, "", fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	return meta, strings.TrimSpace(strings.Join(lines[end+1:], "\n")), nil
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
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 ||
			!strings.HasSuffix(entry.Name(), ".agent.md") {
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
		openedInfo, statErr := file.Stat()
		if statErr != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
			_ = file.Close()
			Logger().Printf("[agents] skip %s: file changed while opening", path)
			continue
		}
		content, err := io.ReadAll(io.LimitReader(file, maxAgentRoleFileBytes+1))
		_ = file.Close()
		if err == nil && len(content) > maxAgentRoleFileBytes {
			err = fmt.Errorf("file exceeds 64 KiB")
		}
		if err != nil {
			Logger().Printf("[agents] skip %s: %v", path, err)
			continue
		}
		raw, instructions, err := parseAgentRoleMarkdown(content)
		if err != nil {
			Logger().Printf("[agents] skip malformed %s: %v", path, err)
			continue
		}
		name := strings.TrimSpace(raw.Name)
		role, err := normalizeAgentRole(name, AgentRoleConfig{
			Description: raw.Description, Instructions: instructions, Model: raw.Model,
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

// LoadAgentRoles discovers Markdown role definitions with project files
// overriding user files. Malformed files are warning-only and cannot override
// a valid lower layer.
func LoadAgentRoles(pwd string) map[string]AgentRoleConfig {
	roles := make(map[string]AgentRoleConfig)
	loadAgentRoleDir(filepath.Join(ConfigDir(), "agents"), roles)
	if pwd != "" {
		loadAgentRoleDir(filepath.Join(pwd, ".jcode", "agents"), roles)
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
