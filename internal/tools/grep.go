package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GrepInput defines the input for the grep tool.
type GrepInput struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path"`
	Include         string `json:"include,omitempty"`
	CaseInsensitive bool   `json:"case_insensitive,omitempty"`
	MaxResults      int    `json:"max_results,omitempty"`

	OutputMode    string `json:"output_mode,omitempty"`    // "content" (default), "files_with_matches", "count"
	BeforeContext int    `json:"before_context,omitempty"` // -B lines
	AfterContext  int    `json:"after_context,omitempty"`  // -A lines
	Context       int    `json:"context,omitempty"`        // -C lines (overrides before/after)
	Offset        int    `json:"offset,omitempty"`         // skip N results for pagination
	Multiline     bool   `json:"multiline,omitempty"`      // rg --multiline
	FileType      string `json:"file_type,omitempty"`      // rg --type
}

// grepDefaultMax is the default max_results.
const grepDefaultMax = 250

// grepAbsoluteMax is the hard upper limit for max_results.
const grepAbsoluteMax = 1000

// grepVCSExclusions are directories excluded from search.
var grepVCSExclusions = []string{
	".git", "node_modules", "vendor", "__pycache__", ".venv",
	".svn", ".hg", ".bzr", ".jj", ".sl",
}

func (e *Env) NewGrepTool() tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "grep",
		Desc: `Searches for a pattern in files. Returns matching lines with file path and line number.
Uses ripgrep (rg) if available for best performance, otherwise falls back to grep.
By default: skips binary files, respects .gitignore, excludes VCS/dependency directories.
Supports output modes: content (default with line numbers), files_with_matches (filenames only), count (file:count).
Supports context lines, pagination via offset, multiline matching, and file type filtering.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"pattern": {
				Type:     schema.String,
				Desc:     "The search pattern (supports regex).",
				Required: true,
			},
			"path": {
				Type:     schema.String,
				Desc:     "The file or directory path to search in. Use absolute paths.",
				Required: true,
			},
			"include": {
				Type:     schema.String,
				Desc:     "Glob pattern to filter files (e.g. '*.go', '*.py').",
				Required: false,
			},
			"case_insensitive": {
				Type:     schema.Boolean,
				Desc:     "If true, perform case-insensitive matching.",
				Required: false,
			},
			"max_results": {
				Type:     schema.Integer,
				Desc:     "Maximum number of matching lines to return. Default 250, max 1000.",
				Required: false,
			},
			"output_mode": {
				Type:     schema.String,
				Desc:     `Output mode: "content" (default, matching lines), "files_with_matches" (filenames only), "count" (file:count format).`,
				Required: false,
			},
			"before_context": {
				Type:     schema.Integer,
				Desc:     "Number of lines to show before each match.",
				Required: false,
			},
			"after_context": {
				Type:     schema.Integer,
				Desc:     "Number of lines to show after each match.",
				Required: false,
			},
			"context": {
				Type:     schema.Integer,
				Desc:     "Number of lines to show before AND after each match. Overrides before_context/after_context.",
				Required: false,
			},
			"offset": {
				Type:     schema.Integer,
				Desc:     "Skip this many result lines for pagination.",
				Required: false,
			},
			"multiline": {
				Type:     schema.Boolean,
				Desc:     "Enable multiline matching (rg only).",
				Required: false,
			},
			"file_type": {
				Type:     schema.String,
				Desc:     "Filter by file type (e.g. 'go', 'py', 'js'). Uses rg --type.",
				Required: false,
			},
		}),
	}

	return &grepTool{env: e, info: info}
}

type grepTool struct {
	env  *Env
	info *schema.ToolInfo
}

func (g *grepTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return g.info, nil
}

func (g *grepTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var input GrepInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	if input.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	if input.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	maxResults := grepDefaultMax
	if input.MaxResults > 0 {
		maxResults = input.MaxResults
		if maxResults > grepAbsoluteMax {
			maxResults = grepAbsoluteMax
		}
	}

	if input.OutputMode == "" {
		input.OutputMode = "content"
	}

	// On remote (SSH), build the command string and run via Executor.
	if g.env.IsRemote() {
		return g.runRemote(ctx, input, maxResults)
	}

	// On local, use exec.Command directly for better control.
	if rgPath, err := exec.LookPath("rg"); err == nil {
		return g.runLocalCmd(ctx, rgPath, g.buildRgArgs(input, maxResults), input, maxResults)
	}
	return g.runLocalCmd(ctx, "grep", g.buildGrepArgs(input), input, maxResults)
}

func (g *grepTool) buildRgArgs(input GrepInput, maxResults int) []string {
	args := []string{
		"--no-heading", "--line-number", "--color=never",
		"--max-columns=500", "--max-columns-preview",
	}

	// VCS exclusions
	for _, dir := range grepVCSExclusions {
		args = append(args, "--glob", "!"+dir)
	}

	// Output mode
	switch input.OutputMode {
	case "files_with_matches":
		args = append(args, "--files-with-matches")
	case "count":
		args = append(args, "--count")
	default:
		// content mode: use max-count for limiting
		args = append(args, "--max-count", fmt.Sprintf("%d", maxResults+input.Offset+1))
	}

	// Context lines
	if input.Context > 0 {
		args = append(args, fmt.Sprintf("--context=%d", input.Context))
	} else {
		if input.BeforeContext > 0 {
			args = append(args, fmt.Sprintf("--before-context=%d", input.BeforeContext))
		}
		if input.AfterContext > 0 {
			args = append(args, fmt.Sprintf("--after-context=%d", input.AfterContext))
		}
	}

	if input.CaseInsensitive {
		args = append(args, "--ignore-case")
	}
	if input.Include != "" {
		args = append(args, "--glob", input.Include)
	}
	if input.Multiline {
		args = append(args, "--multiline")
	}
	if input.FileType != "" {
		args = append(args, "--type", input.FileType)
	}

	args = append(args, input.Pattern, input.Path)
	return args
}

func (g *grepTool) buildGrepArgs(input GrepInput) []string {
	args := []string{"-rnI", "--color=never"}

	// VCS exclusions
	for _, dir := range grepVCSExclusions {
		args = append(args, "--exclude-dir="+dir)
	}

	// Context lines
	if input.Context > 0 {
		args = append(args, fmt.Sprintf("-C%d", input.Context))
	} else {
		if input.BeforeContext > 0 {
			args = append(args, fmt.Sprintf("-B%d", input.BeforeContext))
		}
		if input.AfterContext > 0 {
			args = append(args, fmt.Sprintf("-A%d", input.AfterContext))
		}
	}

	if input.CaseInsensitive {
		args = append(args, "-i")
	}
	if input.Include != "" {
		args = append(args, "--include="+input.Include)
	}
	if input.FileType != "" {
		args = append(args, "--include=*."+input.FileType)
	}

	// Output mode for grep fallback
	switch input.OutputMode {
	case "files_with_matches":
		args = append(args, "-l")
	case "count":
		args = append(args, "-c")
	}

	args = append(args, input.Pattern, input.Path)
	return args
}

func (g *grepTool) runLocalCmd(ctx context.Context, bin string, args []string, input GrepInput, maxResults int) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "No matches found.", nil
		}
		if stderr.Len() > 0 {
			return "", fmt.Errorf("search error: %s", strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("search failed: %w", err)
	}

	return formatGrepOutput(stdout.String(), maxResults, input.Offset)
}

// runRemote builds a command string and runs it over SSH via the Executor.
func (g *grepTool) runRemote(ctx context.Context, input GrepInput, maxResults int) (string, error) {
	cmd := g.buildRemoteCmd(input, maxResults)
	stdout, stderr, err := g.env.Exec.Exec(ctx, cmd, "", 30*time.Second)

	if err != nil {
		// Exit code 1 = no matches (both rg and grep)
		if strings.Contains(err.Error(), "exit status 1") || strings.Contains(err.Error(), "status 1") {
			return "No matches found.", nil
		}
		if stderr != "" {
			return "", fmt.Errorf("search error: %s", strings.TrimSpace(stderr))
		}
		return "", fmt.Errorf("search failed: %w", err)
	}

	return formatGrepOutput(stdout, maxResults, input.Offset)
}

func (g *grepTool) buildRemoteCmd(input GrepInput, maxResults int) string {
	var parts []string

	// rg command
	rgParts := []string{"rg", "--no-heading", "--line-number", "--color=never",
		"--max-columns=500", "--max-columns-preview"}

	for _, dir := range grepVCSExclusions {
		rgParts = append(rgParts, "--glob", ShellQuote("!"+dir))
	}

	switch input.OutputMode {
	case "files_with_matches":
		rgParts = append(rgParts, "--files-with-matches")
	case "count":
		rgParts = append(rgParts, "--count")
	default:
		rgParts = append(rgParts, "--max-count", fmt.Sprintf("%d", maxResults+input.Offset+1))
	}

	if input.Context > 0 {
		rgParts = append(rgParts, fmt.Sprintf("--context=%d", input.Context))
	} else {
		if input.BeforeContext > 0 {
			rgParts = append(rgParts, fmt.Sprintf("--before-context=%d", input.BeforeContext))
		}
		if input.AfterContext > 0 {
			rgParts = append(rgParts, fmt.Sprintf("--after-context=%d", input.AfterContext))
		}
	}

	if input.CaseInsensitive {
		rgParts = append(rgParts, "--ignore-case")
	}
	if input.Include != "" {
		rgParts = append(rgParts, "--glob", ShellQuote(input.Include))
	}
	if input.Multiline {
		rgParts = append(rgParts, "--multiline")
	}
	if input.FileType != "" {
		rgParts = append(rgParts, "--type", ShellQuote(input.FileType))
	}
	rgParts = append(rgParts, ShellQuote(input.Pattern), ShellQuote(input.Path))

	// grep fallback
	grepParts := []string{"grep", "-rnI", "--color=never"}
	for _, dir := range grepVCSExclusions {
		grepParts = append(grepParts, "--exclude-dir="+dir)
	}

	if input.Context > 0 {
		grepParts = append(grepParts, fmt.Sprintf("-C%d", input.Context))
	} else {
		if input.BeforeContext > 0 {
			grepParts = append(grepParts, fmt.Sprintf("-B%d", input.BeforeContext))
		}
		if input.AfterContext > 0 {
			grepParts = append(grepParts, fmt.Sprintf("-A%d", input.AfterContext))
		}
	}

	if input.CaseInsensitive {
		grepParts = append(grepParts, "-i")
	}
	if input.Include != "" {
		grepParts = append(grepParts, "--include="+ShellQuote(input.Include))
	}
	if input.FileType != "" {
		grepParts = append(grepParts, "--include=*."+ShellQuote(input.FileType))
	}

	switch input.OutputMode {
	case "files_with_matches":
		grepParts = append(grepParts, "-l")
	case "count":
		grepParts = append(grepParts, "-c")
	}

	grepParts = append(grepParts, ShellQuote(input.Pattern), ShellQuote(input.Path))

	// which rg && rg ... || grep ...
	parts = append(parts, "which rg >/dev/null 2>&1 &&")
	parts = append(parts, strings.Join(rgParts, " "))
	parts = append(parts, "||")
	parts = append(parts, strings.Join(grepParts, " "))

	return strings.Join(parts, " ")
}

func formatGrepOutput(output string, maxResults int, offset int) (string, error) {
	if output == "" {
		return "No matches found.", nil
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	totalLines := len(lines)

	// Apply offset (pagination)
	if offset > 0 {
		if offset >= len(lines) {
			return fmt.Sprintf("No more results (offset %d exceeds %d total matches).\n", offset, totalLines), nil
		}
		lines = lines[offset:]
	}

	truncated := len(lines) > maxResults
	if truncated {
		lines = lines[:maxResults]
	}

	var result strings.Builder
	for _, line := range lines {
		result.WriteString(line)
		result.WriteString("\n")
	}

	switch {
	case truncated:
		fmt.Fprintf(&result, "\n(showing %d results, %d total, offset %d — use offset=%d for next page)\n",
			len(lines), totalLines, offset, offset+maxResults)
	case offset > 0:
		fmt.Fprintf(&result, "\n(%d results, offset %d, %d total)\n", len(lines), offset, totalLines)
	default:
		fmt.Fprintf(&result, "\n(%d matches found)\n", len(lines))
	}

	return result.String(), nil
}
