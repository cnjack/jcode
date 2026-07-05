package flow

import (
	"embed"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/cnjack/jcode/internal/config"
)

//go:embed builtin/*.js
var builtinFS embed.FS

// Loader discovers workflows from builtin (embedded), the user dir
// (~/.jcode/workflows), and the project dir (<project>/.jcode/workflows). On name
// collision, project wins over user wins over builtin (mirrors skills.Loader).
type Loader struct {
	mu        sync.RWMutex
	workflows map[string]Workflow
}

// NewLoader creates a Loader pre-populated with builtins and the user dir.
func NewLoader() *Loader {
	l := &Loader{workflows: make(map[string]Workflow)}
	l.loadBuiltin()
	l.scanDir(userWorkflowsDir(), ScopeUser)
	return l
}

// LoadProject scans a project's .jcode/workflows dir (project overrides user).
func (l *Loader) LoadProject(projectDir string) {
	if projectDir == "" {
		return
	}
	l.scanDir(filepath.Join(projectDir, ".jcode", "workflows"), ScopeProject)
}

func userWorkflowsDir() string {
	return filepath.Join(config.ConfigDir(), "workflows")
}

func (l *Loader) loadBuiltin() {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		config.Logger().Printf("[flow] read builtin workflows dir: %v", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		// embed.FS always uses forward slashes — path.Join, not filepath.Join
		// (which would emit backslashes on Windows and miss the file).
		data, err := builtinFS.ReadFile(path.Join("builtin", e.Name()))
		if err != nil {
			config.Logger().Printf("[flow] read builtin workflow %s: %v", e.Name(), err)
			continue
		}
		l.add(string(data), "", ScopeBuiltin, e.Name())
	}
}

func (l *Loader) scanDir(dir string, scope Scope) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			config.Logger().Printf("[flow] read %s workflow %s: %v", scope, full, err)
			continue
		}
		l.add(string(data), full, scope, e.Name())
	}
}

// add parses a workflow source and registers it by meta name (falling back to the
// filename stem). Later scopes override earlier ones.
func (l *Loader) add(src, path string, scope Scope, filename string) {
	meta, err := ParseMeta(src)
	if err != nil {
		config.Logger().Printf("[flow] skip %s: %v", filename, err)
		return
	}
	name := meta.Name
	if name == "" {
		name = strings.TrimSuffix(strings.TrimSuffix(filename, ".js"), ".flow")
	}
	l.mu.Lock()
	l.workflows[name] = Workflow{Meta: meta, Source: src, Path: path, Scope: scope}
	l.mu.Unlock()
}

// Get returns a workflow by name.
func (l *Loader) Get(name string) (Workflow, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	w, ok := l.workflows[name]
	return w, ok
}

// All returns all workflows sorted by name.
func (l *Loader) All() []Workflow {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Workflow, 0, len(l.workflows))
	for _, w := range l.workflows {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Meta.Name < out[j].Meta.Name })
	return out
}

// Resolve is a convenience matching Engine's WithResolver signature.
func (l *Loader) Resolve(name string) (Workflow, bool) { return l.Get(name) }

// SlashCommand is a "/name" trigger derived from a workflow.
type SlashCommand struct {
	Slash       string
	Name        string
	Description string
	WhenToUse   string
}

// SlashCommands returns a "/name" trigger for each workflow, sorted.
func (l *Loader) SlashCommands() []SlashCommand {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]SlashCommand, 0, len(l.workflows))
	for _, w := range l.workflows {
		out = append(out, SlashCommand{
			Slash:       "/" + w.Meta.Name,
			Name:        w.Meta.Name,
			Description: w.Meta.Description,
			WhenToUse:   w.Meta.WhenToUse,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slash < out[j].Slash })
	return out
}

// SaveUser writes a workflow source to the user dir as <name>.js and registers it.
// Used by "save this run as a command".
func (l *Loader) SaveUser(name, src string) (string, error) {
	dir := userWorkflowsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, safeName(name)+".js")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		return "", err
	}
	l.add(src, path, ScopeUser, filepath.Base(path))
	return path, nil
}
