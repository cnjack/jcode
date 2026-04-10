package prompts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

// MemoryConfig controls how AGENTS.md files are loaded and merged.
type MemoryConfig struct {
	MaxTotalChars int
	MaxIncDepth   int
}

// MemoryLoader loads and merges multi-level AGENTS.md files with @include support.
type MemoryLoader struct {
	cfg MemoryConfig
}

var includeRe = regexp.MustCompile(`(?m)^@include\s+(.+)$`)

// NewMemoryLoader creates a MemoryLoader with the given config.
func NewMemoryLoader(cfg MemoryConfig) *MemoryLoader {
	if cfg.MaxTotalChars <= 0 {
		cfg.MaxTotalChars = 40000
	}
	if cfg.MaxIncDepth <= 0 {
		cfg.MaxIncDepth = 5
	}
	return &MemoryLoader{cfg: cfg}
}

// Load loads and merges multi-level AGENTS.md files:
//  1. ~/.jcode/AGENTS.md (global)
//  2. {pwd}/AGENTS.md (project-level)
//  3. {pwd}/AGENTS.local.md (local, expected gitignored)
//
// Each file's @include directives are resolved recursively.
// The merged result is truncated to MaxTotalChars.
func (m *MemoryLoader) Load(pwd string) (string, error) {
	var sections []string

	// 1. Global AGENTS.md
	globalPath := filepath.Join(config.ConfigDir(), "AGENTS.md")
	if content, err := m.loadFile(globalPath); err == nil && content != "" {
		sections = append(sections, "<!-- global agents.md -->\n"+content)
	}

	// 2. Project AGENTS.md (case-insensitive lookup)
	if projectPath := HasAgentsMd(pwd); projectPath != "" {
		if content, err := m.loadFile(projectPath); err == nil && content != "" {
			sections = append(sections, "<!-- project agents.md -->\n"+content)
		}
	}

	// 3. Local AGENTS.local.md
	localPath := filepath.Join(pwd, "AGENTS.local.md")
	if content, err := m.loadFile(localPath); err == nil && content != "" {
		sections = append(sections, "<!-- local agents.md -->\n"+content)
	}

	if len(sections) == 0 {
		return "", nil
	}

	merged := strings.Join(sections, "\n\n---\n\n")

	// Enforce total character limit.
	if len(merged) > m.cfg.MaxTotalChars {
		merged = merged[:m.cfg.MaxTotalChars] + "\n... (agents.md content truncated)"
	}

	return merged, nil
}

// loadFile reads a file and resolves @include directives.
func (m *MemoryLoader) loadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	visited := map[string]bool{path: true}
	resolved := m.resolveIncludes(content, filepath.Dir(path), visited, 0)
	return strings.TrimSpace(resolved), nil
}

// resolveIncludes recursively resolves @include directives in content.
// basePath is the directory of the file containing the directives.
// visited tracks already-included files to prevent circular references.
// depth is the current recursion depth.
func (m *MemoryLoader) resolveIncludes(content, basePath string, visited map[string]bool, depth int) string {
	if depth >= m.cfg.MaxIncDepth {
		return content
	}

	return includeRe.ReplaceAllStringFunc(content, func(match string) string {
		subs := includeRe.FindStringSubmatch(match)
		if len(subs) < 2 {
			return match
		}
		relPath := strings.TrimSpace(subs[1])
		absPath := relPath
		if !filepath.IsAbs(relPath) {
			absPath = filepath.Join(basePath, relPath)
		}
		absPath = filepath.Clean(absPath)

		// Security: prevent path traversal — included files must stay within
		// the base directory tree.
		rel, err := filepath.Rel(basePath, absPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			config.Logger().Printf("[memory] include path escapes base dir: %s (base: %s)", absPath, basePath)
			return "<!-- include blocked (path traversal): " + relPath + " -->"
		}

		if visited[absPath] {
			config.Logger().Printf("[memory] circular include detected: %s", absPath)
			return "<!-- circular include: " + relPath + " -->"
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			config.Logger().Printf("[memory] include file not found: %s", absPath)
			return "<!-- include not found: " + relPath + " -->"
		}

		visited[absPath] = true
		included := m.resolveIncludes(string(data), filepath.Dir(absPath), visited, depth+1)
		return strings.TrimSpace(included)
	})
}
