package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const MaxReadFileSize = 10 * 1024 * 1024 // 10MB
const defaultReadLimit = 2000

type ReadInput struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

func (e *Env) NewReadTool() tool.InvokableTool {
	info := &schema.ToolInfo{
		Name: "read",
		Desc: `Reads a file with line numbers. Works on both local and remote (SSH) machines.
If the path is a directory, it returns the directory structure instead.
Output format: line numbers followed by │ and content. Default limit is 2000 lines.
Binary files (images, executables, etc.) are detected and rejected.`,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"file_path": {
				Type:     schema.String,
				Desc:     "The absolute path to the file to read.",
				Required: true,
			},
			"offset": {
				Type:     schema.Integer,
				Desc:     "The line number to start reading from (0-indexed).",
				Required: false,
			},
			"limit": {
				Type:     schema.Integer,
				Desc:     "The number of lines to read. Default 2000.",
				Required: false,
			},
		}),
	}

	return &readTool{env: e, info: info}
}

type readTool struct {
	env  *Env
	info *schema.ToolInfo
}

func (r *readTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return r.info, nil
}

func (r *readTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var input ReadInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	if input.FilePath == "" {
		return "", fmt.Errorf("file_path is required")
	}
	input.FilePath = r.env.ResolvePath(input.FilePath)

	// Binary detection by extension (before reading content).
	if detectBinaryByExtension(input.FilePath) {
		return "", fmt.Errorf("cannot read binary file %s (detected by extension %s)",
			input.FilePath, strings.ToLower(getExt(input.FilePath)))
	}

	stat, err := r.env.Exec.Stat(ctx, input.FilePath)
	if err != nil {
		return "", fmt.Errorf("failed to stat file %s: %w", input.FilePath, err)
	}
	if !stat.Exists {
		return "", fmt.Errorf("file %s does not exist", input.FilePath)
	}

	if stat.IsDir {
		out, serr, err := r.env.Exec.Exec(ctx, fmt.Sprintf("ls -la %s", ShellQuote(input.FilePath)), "", 10*time.Second)
		if err != nil {
			return "", fmt.Errorf("failed to list directory %s: %w\n%s", input.FilePath, err, serr)
		}
		return fmt.Sprintf("Path %s is a directory. Here is its structure:\n%s", input.FilePath, out), nil
	}

	content, err := r.env.Exec.ReadFile(ctx, input.FilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", input.FilePath, err)
	}

	// File size check.
	if len(content) > MaxReadFileSize {
		return "", fmt.Errorf("file %s is too large (%d bytes, max %d). Use offset and limit to read a specific range",
			input.FilePath, len(content), MaxReadFileSize)
	}

	// Content-based binary detection.
	if detectBinaryByContent(content) {
		return "", fmt.Errorf("cannot read binary file %s (binary content detected)", input.FilePath)
	}

	// Track the read in FileTracker if available.
	if r.env.FileTracker != nil {
		modTime := time.Now()
		if info, err := os.Stat(input.FilePath); err == nil {
			modTime = info.ModTime()
		}
		r.env.FileTracker.TrackRead(input.FilePath, content, modTime)
	}

	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	start := input.Offset
	if start < 0 {
		start = 0
	}
	if start > totalLines {
		start = totalLines
	}

	// Apply default limit if not specified.
	limit := input.Limit
	if limit <= 0 {
		limit = defaultReadLimit
	}

	end := totalLines
	if start+limit < end {
		end = start + limit
	}

	// Format with line numbers.
	var result strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&result, "%4d │ %s\n", i+1, lines[i])
	}

	// Truncation message.
	if end < totalLines {
		fmt.Fprintf(&result, "\n... (%d more lines, total %d)\n", totalLines-end, totalLines)
	}

	return result.String(), nil
}

// getExt returns the file extension including the dot.
func getExt(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i:]
		}
		if path[i] == '/' || path[i] == '\\' {
			break
		}
	}
	return ""
}
