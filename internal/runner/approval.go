package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
)

// ApprovalState manages whether tool calls require interactive user approval.
type ApprovalState struct {
	mu          sync.Mutex
	h           handler.AgentEventHandler
	mode        handler.ApprovalMode // Current approval mode (derived from sessionMode)
	sessionMode mode.SessionMode     // Unified selector mode (Ask/Plan/Autopilot)
	workpath    string               // Current working directory for path detection
}

type toolProgressNotifier interface {
	NotifyToolInProgress(name, args string)
}

// NewApprovalState creates a new ApprovalState with the given workpath.
func NewApprovalState(workpath string, autoApprove bool) *ApprovalState {
	return NewApprovalStateWithMode(workpath, sessionModeFor(autoApprove))
}

// NewApprovalStateWithMode creates a new ApprovalState seeded with a unified
// session mode (the preferred constructor; NewApprovalState wraps it for the
// legacy autoApprove-bool callers).
func NewApprovalStateWithMode(workpath string, m mode.SessionMode) *ApprovalState {
	return &ApprovalState{
		mode:        approvalModeFor(m),
		sessionMode: m,
		workpath:    workpath,
	}
}

// sessionModeFor maps the legacy autoApprove bool to a unified mode.
func sessionModeFor(autoApprove bool) mode.SessionMode {
	if autoApprove {
		return mode.Autopilot
	}
	return mode.Ask
}

// approvalModeFor derives the low-level approval axis from the unified mode.
func approvalModeFor(m mode.SessionMode) handler.ApprovalMode {
	if m.AutoApprove() {
		return handler.ModeAuto
	}
	return handler.ModeManual
}

// SetHandler stores the handler used to send approval-request messages.
func (s *ApprovalState) SetHandler(h handler.AgentEventHandler) {
	s.h = h
}

// SetMode sets the approval mode (used for external mode changes).
func (s *ApprovalState) SetMode(m handler.ApprovalMode) {
	s.mu.Lock()
	s.mode = m
	s.mu.Unlock()
}

// SetSessionMode sets the unified session mode and derives the approval axis
// from it under the same lock. This is the single entry point a frontend uses
// to change the approval behavior; the tool/prompt axis is applied separately
// by each frontend's agent-rebuild path.
func (s *ApprovalState) SetSessionMode(m mode.SessionMode) {
	s.mu.Lock()
	s.sessionMode = m
	s.mode = approvalModeFor(m)
	s.mu.Unlock()
}

// GetSessionMode returns the current unified session mode.
func (s *ApprovalState) GetSessionMode() mode.SessionMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionMode
}

// SetWorkpath sets the current working directory (called on environment switch).
func (s *ApprovalState) SetWorkpath(path string) {
	s.mu.Lock()
	s.workpath = path
	s.mu.Unlock()
}

// GetMode returns the current approval mode.
func (s *ApprovalState) GetMode() handler.ApprovalMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

// SetSessionApproval sets the approval mode based on the boolean value.
// This is kept for backward compatibility with the channel-based mode sync.
func (s *ApprovalState) SetSessionApproval(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled {
		s.sessionMode = mode.Autopilot
		s.mode = handler.ModeAuto
	} else {
		s.sessionMode = mode.Ask
		s.mode = handler.ModeManual
	}
}

// RequestApproval is the agent.ApprovalFunc implementation.
// It returns true immediately for read-only or obviously safe commands.
// For everything else it sends a TUI prompt and waits for the user's answer.
func (s *ApprovalState) RequestApproval(ctx context.Context, toolName, toolArgs string) (bool, error) {
	// State machine: AUTO mode passes all operations directly
	s.mu.Lock()
	currentMode := s.mode
	s.mu.Unlock()
	if currentMode == handler.ModeAuto {
		s.notifyToolInProgress(toolName, toolArgs)
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
		s.notifyToolInProgress(toolName, toolArgs)
		return true, nil
	}

	// 2. read tool: check if path is within workpath
	if toolName == "read" {
		var input struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal([]byte(toolArgs), &input); err == nil {
			if s.isWithinWorkpath(input.FilePath) {
				s.notifyToolInProgress(toolName, toolArgs)
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
				s.notifyToolInProgress(toolName, toolArgs)
				return true, nil
			}
			cmd := strings.TrimSpace(input.Command)
			safePrefix := []string{"ls", "pwd", "env", "ls ", "cat ", "pwd ", "echo ", "which ", "git status", "git log", "git diff", "git show"}
			for _, p := range safePrefix {
				if cmd == p || strings.HasPrefix(cmd, p) {
					s.notifyToolInProgress(toolName, toolArgs)
					return true, nil
				}
			}
		}
	}

	// 4. Other tools: need approval
	return s.requestUserApproval(ctx, toolName, toolArgs, false)
}

func (s *ApprovalState) notifyToolInProgress(toolName, toolArgs string) {
	if notifier, ok := s.h.(toolProgressNotifier); ok {
		notifier.NotifyToolInProgress(toolName, toolArgs)
	}
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

	// State transition: "Approve All" promotes the session to Autopilot (both the
	// unified mode and the derived approval axis). A plain single approve does
	// not change the session mode.
	if resp.Approved && resp.Mode == handler.ModeAuto {
		s.mu.Lock()
		s.sessionMode = mode.Autopilot
		s.mode = handler.ModeAuto
		s.mu.Unlock()
	}
	return resp.Approved, nil
}

// NewTeammateApprovalFunc creates an approval function for a teammate that includes
// the worker identity in the TUI approval prompt.
func (s *ApprovalState) NewTeammateApprovalFunc(workerName, workerColor string) func(ctx context.Context, toolName, toolArgs string) (bool, error) {
	return func(ctx context.Context, toolName, toolArgs string) (bool, error) {
		// Same logic as RequestApproval, but with worker badge.
		s.mu.Lock()
		currentMode := s.mode
		s.mu.Unlock()
		if currentMode == handler.ModeAuto {
			s.notifyToolInProgress(toolName, toolArgs)
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
			s.notifyToolInProgress(toolName, toolArgs)
			return true, nil
		}

		if toolName == "read" {
			var input struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal([]byte(toolArgs), &input); err == nil {
				if s.isWithinWorkpath(input.FilePath) {
					s.notifyToolInProgress(toolName, toolArgs)
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
					s.notifyToolInProgress(toolName, toolArgs)
					return true, nil
				}
				cmd := strings.TrimSpace(input.Command)
				safePrefix := []string{"ls", "pwd", "env", "ls ", "cat ", "pwd ", "echo ", "which ", "git status", "git log", "git diff", "git show"}
				for _, p := range safePrefix {
					if cmd == p || strings.HasPrefix(cmd, p) {
						s.notifyToolInProgress(toolName, toolArgs)
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
