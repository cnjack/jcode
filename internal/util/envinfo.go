package utils

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// EnvInfo is collected once at session start and injected into the system prompt.
type EnvInfo struct {
	GitBranch   string // empty if not a git repo
	GitDirty    bool
	LastCommit  string // one-line: hash + subject
	ProjectType string // comma-joined list of detected project markers
	DirTree     string // shallow directory tree
}

// CollectEnvInfo gathers environment facts for pwd.
// All errors are suppressed — missing data is represented as empty strings/false.
func CollectEnvInfo(pwd string) *EnvInfo {
	info := &EnvInfo{}
	info.GitBranch = gitCommand(pwd, "rev-parse", "--abbrev-ref", "HEAD")
	info.GitDirty = gitCommand(pwd, "status", "--porcelain") != ""
	info.LastCommit = gitCommand(pwd, "log", "-1", "--format=%h %s")
	info.ProjectType = detectProjectType(pwd)
	info.DirTree = buildDirTree(pwd, 2, 200)
	return info
}

func gitCommand(pwd string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	fullArgs := append([]string{"-C", pwd}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}

var projectMarkers = []struct {
	file  string
	label string
}{
	{"go.mod", "Go module"},
	{"package.json", "Node.js"},
	{"Cargo.toml", "Rust"},
	{"pyproject.toml", "Python"},
	{"setup.py", "Python"},
	{"pom.xml", "Java (Maven)"},
	{"build.gradle", "Java (Gradle)"},
	{"Makefile", "Make"},
	{"Dockerfile", "Docker"},
}

func detectProjectType(pwd string) string {
	var labels []string
	seen := map[string]bool{}
	for _, m := range projectMarkers {
		if _, err := os.Stat(filepath.Join(pwd, m.file)); err == nil {
			if !seen[m.label] {
				labels = append(labels, m.label)
				seen[m.label] = true
			}
		}
	}
	return strings.Join(labels, ", ")
}

var ignoreDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	".cache": true, "dist": true, "build": true, "__pycache__": true,
	".idea": true, ".vscode": true, "target": true, ".next": true,
}

func buildDirTree(root string, maxDepth, maxLines int) string {
	var lines []string
	lines = append(lines, filepath.Base(root)+"/")
	walkDir(root, "", 1, maxDepth, &lines, maxLines)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "  ... (truncated)")
	}
	return strings.Join(lines, "\n")
}

func walkDir(dir, prefix string, depth, maxDepth int, lines *[]string, maxLines int) {
	if depth > maxDepth || len(*lines) >= maxLines {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	// Sort: dirs first, then files, both alphabetically.
	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di
		}
		return entries[i].Name() < entries[j].Name()
	})
	for _, e := range entries {
		if len(*lines) >= maxLines {
			return
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") && name != ".github" {
			continue
		}
		if e.IsDir() {
			if ignoreDirs[name] {
				continue
			}
			*lines = append(*lines, fmt.Sprintf("%s%s/", prefix+"  ", name))
			walkDir(filepath.Join(dir, name), prefix+"  ", depth+1, maxDepth, lines, maxLines)
		} else {
			*lines = append(*lines, fmt.Sprintf("%s%s", prefix+"  ", name))
		}
	}
}
