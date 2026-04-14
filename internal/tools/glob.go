package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type GlobInput struct {
	Pattern  string `json:"pattern"`
	Path     string `json:"path,omitempty"`
	MaxDepth int    `json:"max_depth,omitempty"`
	Limit    int    `json:"limit,omitempty"` // default 100, max 500
}

const globDefaultLimit = 100
const globMaxLimit = 500

// globExclusions are directories excluded from glob search.
var globExclusions = []string{
	".git", "node_modules", "vendor", "__pycache__", ".venv",
	".svn", ".hg", ".bzr", ".jj", ".sl",
}

func (e *Env) NewGlobTool() tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "glob",
		Desc: "Search for files by glob pattern. Returns relative file paths. " +
			"Excludes VCS and dependency directories (.git, node_modules, vendor, etc.).",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"pattern": {
				Type:     schema.String,
				Desc:     "Glob pattern to match file names (e.g. '*.go', '**/*.test.ts', 'Makefile').",
				Required: true,
			},
			"path": {
				Type:     schema.String,
				Desc:     "Directory to search in. Defaults to current working directory.",
				Required: false,
			},
			"max_depth": {
				Type:     schema.Integer,
				Desc:     "Maximum directory depth to search.",
				Required: false,
			},
			"limit": {
				Type:     schema.Integer,
				Desc:     "Maximum number of files to return. Default 100, max 500.",
				Required: false,
			},
		}),
	}

	return &globTool{env: e, info: info}
}

type globTool struct {
	env  *Env
	info *schema.ToolInfo
}

func (g *globTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return g.info, nil
}

func (g *globTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var input GlobInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	if input.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}

	searchPath := input.Path
	if searchPath == "" {
		searchPath = g.env.Pwd()
	}

	limit := globDefaultLimit
	if input.Limit > 0 {
		limit = input.Limit
		if limit > globMaxLimit {
			limit = globMaxLimit
		}
	}

	cmd := buildFindCmd(input.Pattern, searchPath, input.MaxDepth, limit)

	start := time.Now()
	var stdout, stderr string
	var err error

	if g.env.IsRemote() {
		stdout, stderr, err = g.env.Exec.Exec(ctx, cmd, "", 30*time.Second)
	} else {
		stdout, stderr, err = execLocal(ctx, cmd, 30*time.Second)
	}
	elapsed := time.Since(start)

	if err != nil {
		// find returns exit code 0 normally; non-zero might mean partial failure
		if stdout == "" {
			if stderr != "" {
				return "", fmt.Errorf("glob error: %s", strings.TrimSpace(stderr))
			}
			return "No files found.", nil
		}
		// partial results — continue with what we have
	}

	return formatGlobOutput(stdout, limit, elapsed)
}

// buildFindCmd constructs a find command for the given glob parameters.
func buildFindCmd(pattern, searchPath string, maxDepth, limit int) string {
	var parts []string
	parts = append(parts, "find", ShellQuote(searchPath))

	// Exclude VCS directories with pruning for efficiency
	var pruneExprs []string
	for _, dir := range globExclusions {
		pruneExprs = append(pruneExprs, "-name", ShellQuote(dir))
		pruneExprs = append(pruneExprs, "-o")
	}
	// Remove trailing -o and wrap in parentheses with -prune
	if len(pruneExprs) > 0 {
		pruneExprs = pruneExprs[:len(pruneExprs)-1] // remove last -o
		parts = append(parts, "\\(")
		parts = append(parts, pruneExprs...)
		parts = append(parts, "\\)", "-prune", "-o")
	}

	// Max depth
	if maxDepth > 0 {
		parts = append(parts, "-maxdepth", fmt.Sprintf("%d", maxDepth))
	}

	// File type and name pattern
	parts = append(parts, "-type", "f", "-name", ShellQuote(pattern), "-print")

	// Limit results (take limit+1 to detect truncation)
	parts = append(parts, "|", "head", "-n", fmt.Sprintf("%d", limit+1))

	return strings.Join(parts, " ")
}

// execLocal runs a shell command locally and returns stdout, stderr, error.
func execLocal(ctx context.Context, cmd string, timeout time.Duration) (string, string, error) {
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c := exec.CommandContext(execCtx, "sh", "-c", cmd)
	var stdout, stderr strings.Builder
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	return stdout.String(), stderr.String(), err
}

func formatGlobOutput(output string, limit int, elapsed time.Duration) (string, error) {
	if strings.TrimSpace(output) == "" {
		return "No files found.", nil
	}

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	truncated := len(lines) > limit
	if truncated {
		lines = lines[:limit]
	}

	var result strings.Builder
	for _, line := range lines {
		result.WriteString(line)
		result.WriteString("\n")
	}

	if truncated {
		fmt.Fprintf(&result, "\n(%d files shown, more available — increase limit. %.0fms)\n", limit, float64(elapsed.Milliseconds()))
	} else {
		fmt.Fprintf(&result, "\n(%d files found. %.0fms)\n", len(lines), float64(elapsed.Milliseconds()))
	}

	return result.String(), nil
}
