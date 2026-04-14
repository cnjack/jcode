package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cnjack/jcode/internal/handler"
)

// ApprovalState manages whether tool calls require interactive user approval.
type ApprovalState struct {
	h        handler.AgentEventHandler
	mode     handler.ApprovalMode // Current approval mode
	workpath string               // Current working directory for path detection
}

// NewApprovalState creates a new ApprovalState with the given workpath.
func NewApprovalState(workpath string, autoApprove bool) *ApprovalState {
	mode := handler.ModeManual
	if autoApprove {
		mode = handler.ModeAuto
	}
	return &ApprovalState{
		mode:     mode,
		workpath: workpath,
	}
}

// SetHandler stores the handler used to send approval-request messages.
func (s *ApprovalState) SetHandler(h handler.AgentEventHandler) {
	s.h = h
}

// SetMode sets the approval mode (used for external mode changes).
func (s *ApprovalState) SetMode(mode handler.ApprovalMode) {
	s.mode = mode
}

// SetWorkpath sets the current working directory (called on environment switch).
func (s *ApprovalState) SetWorkpath(path string) {
	s.workpath = path
}

// GetMode returns the current approval mode.
func (s *ApprovalState) GetMode() handler.ApprovalMode {
	return s.mode
}

// SetSessionApproval sets the approval mode based on the boolean value.
// This is kept for backward compatibility with the channel-based mode sync.
func (s *ApprovalState) SetSessionApproval(enabled bool) {
	if enabled {
		s.mode = handler.ModeAuto
	} else {
		s.mode = handler.ModeManual
	}
}

// RequestApproval is the agent.ApprovalFunc implementation.
// It returns true immediately for read-only or obviously safe commands.
// For everything else it sends a TUI prompt and waits for the user's answer.
func (s *ApprovalState) RequestApproval(ctx context.Context, toolName, toolArgs string) (bool, error) {
	// State machine: AUTO mode passes all operations directly
	if s.mode == handler.ModeAuto {
		return true, nil
	}

	// === Below is MANUAL mode handling ===

	// 1. No-approval-needed tools (read is handled separately below)
	noApprovalNeeded := map[string]bool{
		"glob":              true,
		"grep":              true,
		"todowrite":         true,
		"todoread":          true,
		"question":          true,
		"webfetch":          true,
		"subagent":          true,
		"check_background":  true,
		"team_create":       true,
		"team_spawn":        true,
		"team_send_message": true,
		"team_list":         true,
		"team_delete":       true,
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

	// 3. execute: auto-approve safe commands and background tasks
	if toolName == "execute" {
		var input struct {
			Command    string `json:"command"`
			Background bool   `json:"background"`
		}
		if err := json.Unmarshal([]byte(toolArgs), &input); err == nil {
			// Background tasks are auto-approved (long-running, agent can check later).
			if input.Background {
				return true, nil
			}
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
	return s.requestUserApprovalWithWorker(ctx, toolName, toolArgs, isExternal, "", "")
}

// requestUserApprovalWithWorker handles approval with optional worker identity
func (s *ApprovalState) requestUserApprovalWithWorker(ctx context.Context, toolName, toolArgs string, isExternal bool, workerName, workerColor string) (bool, error) {
	if s.h == nil {
		return false, fmt.Errorf("event handler not initialized")
	}

	resp, err := s.h.RequestApproval(ctx, handler.ApprovalRequest{
		ToolName:    toolName,
		ToolArgs:    toolArgs,
		IsExternal:  isExternal,
		WorkerName:  workerName,
		WorkerColor: workerColor,
	})
	if err != nil {
		return false, err
	}

	// State transition: update mode based on user choice
	if resp.Approved {
		s.mode = resp.Mode
	}
	return resp.Approved, nil
}

// NewTeammateApprovalFunc creates an approval function for a teammate that includes
// the worker identity in the TUI approval prompt.
func (s *ApprovalState) NewTeammateApprovalFunc(workerName, workerColor string) func(ctx context.Context, toolName, toolArgs string) (bool, error) {
	return func(ctx context.Context, toolName, toolArgs string) (bool, error) {
		// Same logic as RequestApproval, but with worker badge.
		if s.mode == handler.ModeAuto {
			return true, nil
		}

		noApprovalNeeded := map[string]bool{
			"glob":              true,
			"grep":              true,
			"todowrite":         true,
			"todoread":          true,
			"question":          true,
			"webfetch":          true,
			"subagent":          true,
			"check_background":  true,
			"team_create":       true,
			"team_spawn":        true,
			"team_send_message": true,
			"team_list":         true,
			"team_delete":       true,
		}
		if noApprovalNeeded[toolName] {
			return true, nil
		}

		if toolName == "read" {
			var input struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal([]byte(toolArgs), &input); err == nil {
				if s.isWithinWorkpath(input.FilePath) {
					return true, nil
				}
				return s.requestUserApprovalWithWorker(ctx, toolName, toolArgs, true, workerName, workerColor)
			}
		}

		if toolName == "execute" {
			var input struct {
				Command    string `json:"command"`
				Background bool   `json:"background"`
			}
			if err := json.Unmarshal([]byte(toolArgs), &input); err == nil {
				if input.Background {
					return true, nil
				}
				cmd := strings.TrimSpace(input.Command)
				safePrefix := []string{"ls", "pwd", "env", "ls ", "cat ", "pwd ", "echo ", "which ", "git status", "git log"}
				for _, p := range safePrefix {
					if cmd == p || strings.HasPrefix(cmd, p) {
						return true, nil
					}
				}
			}
		}

		return s.requestUserApprovalWithWorker(ctx, toolName, toolArgs, false, workerName, workerColor)
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
