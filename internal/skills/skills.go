package skills

import (
	"bufio"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/cnjack/jcode/internal/config"
)

// Skill represents a loaded skill with metadata and content.
type Skill struct {
	Name        string // directory name or frontmatter name
	Description string // short description for system prompt (Layer 1)
	Slash       string // optional slash command trigger (e.g. "/review-pr")
	Body        string // full markdown content (Layer 2, on-demand)
	Builtin     bool   // true if embedded in binary
	Path        string // filesystem path (empty for built-in)
}

// Loader discovers and caches skills from built-in embeds and user directories.
type Loader struct {
	mu       sync.RWMutex
	skills   map[string]*Skill
	disabled map[string]bool // skill names hidden from the agent
}

//go:embed builtin
var builtinFS embed.FS

// NewLoader creates a Loader pre-populated with built-in skills, then scans
// the user skills directories (~/.agents/skills/ and ~/.jcode/skills/) for additional skills.
func NewLoader() *Loader {
	return NewLoaderWithDisabled(nil)
}

// NewLoaderWithDisabled creates a Loader with the given skill names disabled.
// Disabled skills are excluded from the system-prompt descriptions, slash
// commands, and the load_skill tool, but remain visible via All() for management UIs.
func NewLoaderWithDisabled(disabled []string) *Loader {
	l := &Loader{
		skills:   make(map[string]*Skill),
		disabled: make(map[string]bool, len(disabled)),
	}
	for _, name := range disabled {
		l.disabled[name] = true
	}
	l.loadBuiltin()
	l.ScanAgentsSkills()
	l.ScanUserSkills()
	return l
}

// SetDisabled replaces the set of disabled skill names.
func (l *Loader) SetDisabled(disabled []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.disabled = make(map[string]bool, len(disabled))
	for _, name := range disabled {
		l.disabled[name] = true
	}
}

// IsEnabled reports whether the named skill is active (not disabled).
func (l *Loader) IsEnabled(name string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return !l.disabled[name]
}

// loadBuiltin reads embedded SKILL.md files from the builtin/ directory.
func (l *Loader) loadBuiltin() {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		config.Logger().Printf("[skills] failed to read builtin dir: %v", err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillName := entry.Name()
		data, err := builtinFS.ReadFile(filepath.Join("builtin", skillName, "SKILL.md"))
		if err != nil {
			config.Logger().Printf("[skills] builtin %s: no SKILL.md: %v", skillName, err)
			continue
		}
		sk := parseSkill(skillName, string(data), true, "")
		l.mu.Lock()
		l.skills[sk.Name] = sk
		l.mu.Unlock()
		config.Logger().Printf("[skills] loaded builtin skill: %s", sk.Name)
	}
}

// ScanUserSkills scans ~/.jcode/skills/ for user-defined skills.
// Each subdirectory (or symlink to a directory) containing a SKILL.md is treated as a skill.
// User skills override built-in skills with the same name.
func (l *Loader) ScanUserSkills() {
	dir := filepath.Join(config.ConfigDir(), "skills")
	l.scanDir(dir, "user")
}

// ScanAgentsSkills scans ~/.agents/skills/ for agent-defined skills.
// Each subdirectory (or symlink to a directory) containing a SKILL.md is treated as a skill.
// Agent skills are loaded before user skills, so user skills can override them.
func (l *Loader) ScanAgentsSkills() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(homeDir, ".agents", "skills")
	l.scanDir(dir, "agents")
}

// ScanProjectSkills scans <projectDir>/.jcode/skills/ for project-local skills.
func (l *Loader) ScanProjectSkills(projectDir string) {
	dir := filepath.Join(projectDir, ".jcode", "skills")
	l.scanDir(dir, "project")
}

// scanDir scans a directory for skill subdirectories (including symlinks to directories).
func (l *Loader) scanDir(dir, source string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		// Resolve symlinks: entry.IsDir() is false for symlinks, so use os.Stat
		// which follows symlinks and reports the target's type.
		fullPath := filepath.Join(dir, entry.Name())
		info, err := os.Stat(fullPath)
		if err != nil || !info.IsDir() {
			continue
		}
		skillName := entry.Name()
		skillPath := filepath.Join(fullPath, "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			continue
		}
		sk := parseSkill(skillName, string(data), false, fullPath)
		l.mu.Lock()
		l.skills[sk.Name] = sk
		l.mu.Unlock()
		config.Logger().Printf("[skills] loaded %s skill: %s from %s", source, sk.Name, sk.Path)
	}
}

// Rescan re-scans all skill sources (preserving built-ins).
func (l *Loader) Rescan(projectDir string) {
	l.mu.Lock()
	// Keep only built-in skills, remove user/project ones.
	for name, sk := range l.skills {
		if !sk.Builtin {
			delete(l.skills, name)
		}
	}
	l.mu.Unlock()
	l.ScanAgentsSkills()
	l.ScanUserSkills()
	if projectDir != "" {
		l.ScanProjectSkills(projectDir)
	}
}

// Get returns a skill by name, or nil if not found.
func (l *Loader) Get(name string) *Skill {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.skills[name]
}

// GetBySlash returns a skill by its slash command, or nil if not found.
// Matches both explicit slash fields and auto-generated "/<name>" triggers.
func (l *Loader) GetBySlash(slash string) *Skill {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, sk := range l.skills {
		if sk.Slash != "" && sk.Slash == slash {
			return sk
		}
		// Auto-generated slash: /name match.
		if sk.Slash == "" && "/"+sk.Name == slash {
			return sk
		}
	}
	return nil
}

// All returns all loaded skills sorted by name.
func (l *Loader) All() []*Skill {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make([]*Skill, 0, len(l.skills))
	for _, sk := range l.skills {
		result = append(result, sk)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Descriptions returns a compact multi-line string listing all skills.
// This is injected into the system prompt (Layer 1 — low token cost).
func (l *Loader) Descriptions() string {
	skills := l.All()
	if len(skills) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, sk := range skills {
		if !l.IsEnabled(sk.Name) {
			continue
		}
		slash := ""
		if sk.Slash != "" {
			slash = fmt.Sprintf(" (%s)", sk.Slash)
		}
		fmt.Fprintf(&sb, "  - %s%s: %s\n", sk.Name, slash, sk.Description)
	}
	return sb.String()
}

// GetContent returns the full skill body wrapped in XML-like tags for injection
// into tool_result (Layer 2 — on-demand, full content).
func (l *Loader) GetContent(name string) string {
	sk := l.Get(name)
	if sk == nil || !l.IsEnabled(name) {
		return fmt.Sprintf("Error: Unknown skill '%s'. Available skills:\n%s", name, l.Descriptions())
	}
	return fmt.Sprintf("<skill name=%q description=%q>\n%s\n</skill>", sk.Name, sk.Description, sk.Body)
}

// SlashCommands returns all skills with slash command triggers.
// Skills with an explicit "slash: false" frontmatter are excluded.
// Skills with no slash field get an auto-generated "/<name>" trigger.
// Skills with a custom slash field (e.g. "/review") use that value.
func (l *Loader) SlashCommands() []*Skill {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var result []*Skill
	for _, sk := range l.skills {
		if l.disabled[sk.Name] {
			// User-disabled via config/UI.
			continue
		}
		switch {
		case sk.Slash == "false":
			// Explicitly disabled.
			continue
		case sk.Slash != "":
			// Has custom slash.
			result = append(result, sk)
		default:
			// Auto-generate: / + skill name.
			autoSk := *sk
			autoSk.Slash = "/" + sk.Name
			result = append(result, &autoSk)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Slash < result[j].Slash
	})
	return result
}

// parseSkill parses a SKILL.md content with optional YAML-like frontmatter.
// Frontmatter fields: name, description, slash
func parseSkill(dirName, content string, builtin bool, path string) *Skill {
	sk := &Skill{
		Name:    dirName,
		Builtin: builtin,
		Path:    path,
	}

	body := content
	if strings.HasPrefix(strings.TrimSpace(content), "---") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) >= 3 {
			frontmatter := parts[1]
			body = strings.TrimSpace(parts[2])
			parseFrontmatter(frontmatter, sk)
		}
	}

	// Use directory name as fallback for Name
	if sk.Name == "" {
		sk.Name = dirName
	}
	// Auto-generate description if missing
	if sk.Description == "" {
		sk.Description = firstLine(body)
	}
	sk.Body = body
	return sk
}

// parseFrontmatter extracts simple key: value pairs from frontmatter text.
func parseFrontmatter(fm string, sk *Skill) {
	scanner := bufio.NewScanner(strings.NewReader(fm))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "---" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "name":
			sk.Name = val
		case "description":
			sk.Description = val
		case "slash":
			sk.Slash = val
		}
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimPrefix(s, "# ")
	if len(s) > 100 {
		s = s[:100] + "..."
	}
	return s
}
