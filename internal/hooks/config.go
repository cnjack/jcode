package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Layer file names, lowest → highest precedence. All matching groups from every
// layer run (append semantics), so a project hook never silently disables a user
// hook; it only adds to it.
const (
	userHooksFile    = "hooks.json"       // ~/.jcode/hooks.json
	projectHooksFile = "hooks.json"       // <work>/.jcode/hooks.json
	localHooksFile   = "hooks.local.json" // <work>/.jcode/hooks.local.json (gitignored)
)

// Load reads and merges the hooks.json layers.
//
//	configDir    — the ~/.jcode directory (config.ConfigDir()).
//	workDir      — the project working directory; its .jcode/ holds project layers.
//	trustProject — whether to load the project layers (.jcode/hooks.json and
//	               .jcode/hooks.local.json).
//
// SECURITY: only the user layer (~/.jcode/hooks.json) is trusted by default. A
// hook runs arbitrary commands the moment its event fires (SessionStart fires on
// startup) and can auto-approve tools, so honoring project-provided hooks would
// be arbitrary code execution from an untrusted clone. Project layers therefore
// load only when trustProject is true — a stand-in until trust-on-first-use
// (per-hook hash confirmation, see internal-doc/hooks-design.md §7) lands.
//
// Missing files are skipped silently. Malformed files are skipped with a warning
// (returned, not fatal) so one broken layer cannot brick the agent.
func Load(configDir, workDir string, trustProject bool) (Config, []string) {
	paths := []string{filepath.Join(configDir, userHooksFile)}
	if trustProject {
		paths = append(paths,
			filepath.Join(workDir, ".jcode", projectHooksFile),
			filepath.Join(workDir, ".jcode", localHooksFile),
		)
	}
	merged := Config{Hooks: map[string][]HookGroup{}}
	var warnings []string
	for _, p := range paths {
		c, err := loadFile(p)
		if err != nil {
			if !os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("hooks: skipping %s: %v", p, err))
			}
			continue
		}
		for ev, groups := range c.Hooks {
			merged.Hooks[ev] = append(merged.Hooks[ev], groups...)
		}
	}
	return merged, warnings
}

// trustProjectEnv is the opt-in that loads untrusted project-layer hooks.
const trustProjectEnv = "JCODE_HOOKS_TRUST_PROJECT"

// NewSessionDispatcher loads the hook config for a session and builds a
// Dispatcher, honoring the project-trust env gate. Shared by all command
// surfaces (TUI/Web/ACP) so hooks behave identically everywhere.
//
//	configDir — the ~/.jcode directory (config.ConfigDir()).
//	cwd       — the session working directory.
//	sessionID — the session UUID (for the hook payload).
func NewSessionDispatcher(configDir, cwd, sessionID string, logf func(string, ...any)) Dispatcher {
	trustProject := os.Getenv(trustProjectEnv) == "1"
	cfg, warns := Load(configDir, cwd, trustProject)
	for _, w := range warns {
		if logf != nil {
			logf("%s", w)
		}
	}
	return NewDispatcher(cfg, Options{CWD: cwd, SessionID: sessionID, Logf: logf})
}

func loadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("invalid JSON: %w", err)
	}
	return c, nil
}

// Empty reports whether the config defines no hooks at all.
func (c Config) Empty() bool {
	for _, groups := range c.Hooks {
		if len(groups) > 0 {
			return false
		}
	}
	return true
}
