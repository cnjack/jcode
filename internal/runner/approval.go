package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cnjack/coding/internal/tui"
)

// ApprovalState manages whether tool calls require interactive user approval.
type ApprovalState struct {
	p        *tea.Program
	mode     tui.ApprovalMode // Current approval mode (replaces approvedSession)
	workpath string           // Current working directory for path detection
}

// NewApprovalState creates a new ApprovalState with the given workpath.
func NewApprovalState(workpath string) *ApprovalState {
	return &ApprovalState{
		mode:     tui.ModeManual, // Default to manual approval
		workpath: workpath,
	}
}

// SetProgram stores the TUI program used to send approval-request messages.
func (s *ApprovalState) SetProgram(p *tea.Program) {
	s.p = p
}

// SetMode sets the approval mode (used for external mode changes).
func (s *ApprovalState) SetMode(mode tui.ApprovalMode) {
	s.mode = mode
}

// SetWorkpath sets the current working directory (called on environment switch).
func (s *ApprovalState) SetWorkpath(path string) {
	s.workpath = path
}

// GetMode returns the current approval mode.
func (s *ApprovalState) GetMode() tui.ApprovalMode {
	return s.mode
}

// SetSessionApproval sets the approval mode based on the boolean value.
// This is kept for backward compatibility with the channel-based mode sync.
func (s *ApprovalState) SetSessionApproval(enabled bool) {
	if enabled {
		s.mode = tui.ModeAuto
	} else {
		s.mode = tui.ModeManual
	}
}

// RequestApproval is the agent.ApprovalFunc implementation.
// It returns true immediately for read-only or obviously safe commands.
// For everything else it sends a TUI prompt and waits for the user's answer.
func (s *ApprovalState) RequestApproval(ctx context.Context, toolName, toolArgs string) (bool, error) {
	// State machine: AUTO mode passes all operations directly
	if s.mode == tui.ModeAuto {
		return true, nil
	}

	// === Below is MANUAL mode handling ===

	// 1. No-approval-needed tools (read is handled separately below)
	noApprovalNeeded := map[string]bool{
		"glob":      true,
		"grep":      true,
		"todowrite": true,
		"todoread":  true,
		"question":  true,
		"webfetch":  true,
	}
	if noApprovalNeeded[toolName] {
		return true, nil
	}

	// 2. read tool: check if path is within workpath
	if toolName == "read" {
		var input struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal([]byte(toolArgs), &input); err == nil {
			if s.isWithinWorkpath(input.FilePath) {
				return true, nil // Within workpath, auto-approve
			}
			// Outside workpath, needs approval, mark as external access
			return s.requestUserApproval(ctx, toolName, toolArgs, true)
		}
	}

	// 3. execute safe command detection
	if toolName == "execute" {
		var input struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(toolArgs), &input); err == nil {
			cmd := strings.TrimSpace(input.Command)
			safePrefix := []string{"ls", "pwd", "env", "ls ", "cat ", "pwd ", "echo ", "which ", "git status", "git log"}
			for _, p := range safePrefix {
				if cmd == p || strings.HasPrefix(cmd, p) {
					return true, nil
				}
			}
		}
	}

	// 4. Other tools: need approval
	return s.requestUserApproval(ctx, toolName, toolArgs, false)
}

// requestUserApproval handles the unified approval request process
func (s *ApprovalState) requestUserApproval(ctx context.Context, toolName, toolArgs string, isExternal bool) (bool, error) {
	if s.p == nil {
		return false, fmt.Errorf("TUI program not initialized")
	}

	respCh := make(chan tui.ToolApprovalResponse, 1)
	s.p.Send(tui.ToolApprovalRequestMsg{
		Name:       toolName,
		Args:       toolArgs,
		Resp:       respCh,
		IsExternal: isExternal,
	})

	select {
	case resp := <-respCh:
		// State transition: update mode based on user choice
		if resp.Approved {
			s.mode = resp.Mode // May stay MANUAL or switch to AUTO
		}
		return resp.Approved, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// isWithinWorkpath checks if the given path is within the workpath
func (s *ApprovalState) isWithinWorkpath(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absWorkpath, err := filepath.Abs(s.workpath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absWorkpath, absPath)
	if err != nil {
		return false
	}
	// Path is within workpath if it doesn't start with ".."
	return !strings.HasPrefix(rel, "..") && rel != ".."
}
