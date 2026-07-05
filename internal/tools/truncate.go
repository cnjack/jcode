package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cnjack/jcode/internal/config"
)

// Per-stream caps for synchronous execute results. stdout and stderr get
// separate budgets so a huge stdout can never squeeze the error report out of
// stderr. Both are tail-biased because failures usually surface at the end.
// The combined worst case (30k + 15k + markers) stays below the 50 000-char
// eino reduction threshold, so the middleware never re-truncates a result that
// already carries these markers.
const (
	execStdoutHeadBytes = 12_000
	execStdoutTailBytes = 18_000 // stdout cap: 30k
	execStderrHeadBytes = 6_000
	execStderrTailBytes = 9_000 // stderr cap: 15k
)

// truncateHeadTail caps s at roughly headMax+tailMax bytes. When s is over the
// limit it keeps the head and the tail — each aligned to the nearest line
// boundary — and replaces the middle with a marker line carrying the dropped
// byte/line counts. Inputs at or under the limit are returned untouched.
//
// The marker text is a stable contract: models and tests key off it.
func truncateHeadTail(s string, headMax, tailMax int) (out string, droppedBytes, droppedLines int) {
	if len(s) <= headMax+tailMax {
		return s, 0, 0
	}

	head := s[:headMax]
	// Align the head back to the last complete line; the marker supplies the
	// separating newline. Keep the raw cut when there is no interior newline.
	if idx := strings.LastIndexByte(head, '\n'); idx > 0 {
		head = head[:idx]
	}

	tail := s[len(s)-tailMax:]
	// Align the tail forward to the next line start so its first line is whole.
	if idx := strings.IndexByte(tail, '\n'); idx >= 0 && idx < len(tail)-1 {
		tail = tail[idx+1:]
	}

	droppedBytes = len(s) - len(head) - len(tail)
	droppedLines = strings.Count(s[len(head):len(s)-len(tail)], "\n")
	out = head +
		fmt.Sprintf("\n[... output truncated: %d bytes (~%d lines) dropped ...]\n", droppedBytes, droppedLines) +
		tail
	return out, droppedBytes, droppedLines
}

// spillExecOutput writes the untruncated stdout/stderr of an execute call to
// ~/.jcode/tasks/exec_<nano>.log and returns the path. On any write failure it
// returns "" — the caller then simply omits the pointer, never errors.
func spillExecOutput(stdout, stderr string) string {
	dir := filepath.Join(config.ConfigDir(), "tasks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		config.Logger().Printf("[execute] spill dir failed: %v", err)
		return ""
	}
	path := filepath.Join(dir, fmt.Sprintf("exec_%d.log", time.Now().UnixNano()))

	var sb strings.Builder
	if stdout != "" {
		sb.WriteString("STDOUT:\n")
		sb.WriteString(stdout)
	}
	if stderr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("STDERR:\n")
		sb.WriteString(stderr)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		config.Logger().Printf("[execute] spill write failed: %v", err)
		return ""
	}
	return path
}
