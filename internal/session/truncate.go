package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

const (
	// MaxOutputBytes is the maximum byte size of a tool output before truncation.
	MaxOutputBytes = 50 * 1024 // 50 KB
	// MaxOutputLines is the maximum line count of a tool output before truncation.
	MaxOutputLines = 2000
	// HeadLines is the number of lines to preserve from the beginning.
	HeadLines = 200
	// TailLines is the number of lines to preserve from the end.
	TailLines = 500
)

// TruncateToolOutput truncates a tool output string if it exceeds size limits.
// It preserves head + tail lines and writes the full content to disk.
// Returns the (possibly truncated) output. If no truncation is needed, the
// original string is returned unchanged.
//
// sessionUUID is used to namespace the overflow files. toolCallID identifies
// the specific tool call.
func TruncateToolOutput(output, sessionUUID, toolCallID string) string {
	if len(output) <= MaxOutputBytes && countLines(output) <= MaxOutputLines {
		return output
	}

	lines := strings.Split(output, "\n")
	totalLines := len(lines)
	totalBytes := len(output)

	// Save full output to disk.
	overflowPath := saveOverflow(output, sessionUUID, toolCallID)

	// Build truncated version: head + notice + tail.
	headEnd := HeadLines
	if headEnd > totalLines {
		headEnd = totalLines
	}
	tailStart := totalLines - TailLines
	if tailStart < headEnd {
		tailStart = headEnd
	}

	var sb strings.Builder
	// Head section.
	for i := 0; i < headEnd; i++ {
		sb.WriteString(lines[i])
		sb.WriteByte('\n')
	}

	// Truncation notice.
	omitted := tailStart - headEnd
	if omitted > 0 {
		notice := fmt.Sprintf("\n... [%d lines / %d bytes truncated] ...\n", omitted, totalBytes)
		if overflowPath != "" {
			notice += fmt.Sprintf("Full output saved to: %s\nUse the read tool to view the complete output.\n", overflowPath)
		}
		sb.WriteString(notice)
	}

	// Tail section.
	for i := tailStart; i < totalLines; i++ {
		sb.WriteString(lines[i])
		if i < totalLines-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// saveOverflow writes the full tool output to a file under the session's
// overflow directory. Returns the file path, or "" on error.
func saveOverflow(content, sessionUUID, toolCallID string) string {
	dir, err := config.SessionsDir()
	if err != nil {
		return ""
	}

	overflowDir := filepath.Join(dir, "overflow", sessionUUID)
	if err := os.MkdirAll(overflowDir, 0o700); err != nil {
		config.Logger().Printf("[session] overflow mkdir error: %v", err)
		return ""
	}

	// Sanitize toolCallID for filename safety.
	safeID := toolCallID
	if safeID == "" {
		safeID = "unknown"
	}
	safeID = strings.ReplaceAll(safeID, "/", "_")
	safeID = strings.ReplaceAll(safeID, "..", "_")

	fp := filepath.Join(overflowDir, safeID+".txt")
	if err := os.WriteFile(fp, []byte(content), 0o600); err != nil {
		config.Logger().Printf("[session] overflow write error: %v", err)
		return ""
	}
	return fp
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := 1
	for i := range s {
		if s[i] == '\n' {
			n++
		}
	}
	return n
}
