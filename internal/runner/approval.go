package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/hooks"
	"github.com/cnjack/jcode/internal/mode"
)

// ApprovalState manages whether tool calls require interactive user approval.
type ApprovalState struct {
	mu          sync.Mutex
	h           handler.AgentEventHandler
	mode        handler.ApprovalMode // Current approval mode (derived from sessionMode)
	sessionMode mode.SessionMode     // Unified selector mode (Approval/Plan/Full access)
	workpath    string               // Current working directory for path detection

	// browserPerm reports whether a browser action class ("navigate"/"interact")
	// on the given origin is pre-authorized ("always allow" site permission). nil
	// means "always prompt". Set by the frontend from config so approval.go stays
	// decoupled from the config layout.
	browserPerm func(origin, class string) bool

	// browserOrigin reports the origin (scheme://host) of the active browser tab.
	// Interaction actions (browser_act) carry no URL in their args, so the origin
	// for a per-site permission check must come from the live session, not the
	// args. nil means "unknown origin" (→ prompt). Set by the frontend.
	browserOrigin func() string
}

// SetBrowserPermFunc installs the site-permission lookup for browser tools.
func (s *ApprovalState) SetBrowserPermFunc(fn func(origin, class string) bool) {
	s.mu.Lock()
	s.browserPerm = fn
	s.mu.Unlock()
}

// SetBrowserOriginFunc installs the active-tab origin provider used to scope
// per-site permissions for browser_act (whose args carry no URL).
func (s *ApprovalState) SetBrowserOriginFunc(fn func() string) {
	s.mu.Lock()
	s.browserOrigin = fn
	s.mu.Unlock()
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
		return mode.FullAccess
	}
	return mode.Approval
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
		s.sessionMode = mode.FullAccess
		s.mode = handler.ModeAuto
	} else {
		s.sessionMode = mode.Approval
		s.mode = handler.ModeManual
	}
}

// noApprovalNeeded lists tools that never require interactive approval in
// MANUAL mode: read-only inspection, the user-facing question tool, and
// teammate/subagent orchestration (whose own tool calls are gated separately).
var noApprovalNeeded = map[string]bool{
	"glob":              true,
	"grep":              true,
	"todowrite":         true,
	"todoread":          true,
	"ask_user":          true,
	"webfetch":          true,
	"subagent":          true,
	"check_background":  true,
	"team_create":       true,
	"team_spawn":        true,
	"team_send_message": true,
	"team_list":         true,
	"team_delete":       true,
	// Browser read-only tier: inspection never mutates external state.
	"browser_snapshot":   true,
	"browser_screenshot": true,
	"browser_read":       true,
}

// approvalDecision is the outcome of evaluating a tool call in MANUAL mode.
type approvalDecision int

const (
	// decisionAutoApprove: the call is safe and runs without a prompt.
	decisionAutoApprove approvalDecision = iota
	// decisionPrompt: the call needs an interactive user prompt.
	decisionPrompt
	// decisionPromptExternal: like decisionPrompt, but flagged as touching a
	// path outside the workpath (the UI highlights this).
	decisionPromptExternal
)

// safeForegroundCommands are the bare command names that, run in the
// foreground with no shell operators, only read state and never execute a
// caller-supplied program. They are auto-approved in MANUAL mode.
var safeForegroundCommands = map[string]bool{
	"ls":    true,
	"pwd":   true,
	"cat":   true,
	"echo":  true,
	"which": true,
}

// safeGitSubcommands are read-only git subcommands that are auto-approved.
var safeGitSubcommands = map[string]bool{
	"status": true,
	"log":    true,
	"diff":   true,
	"show":   true,
}

// isSafeCommand reports whether a foreground shell command is safe to run
// without approval. It rejects anything containing shell operators that could
// chain, redirect, or substitute additional commands (so a "safe" prefix can
// no longer smuggle a destructive payload), then allows only an explicit set
// of read-only programs matched on the whole command word (not a prefix, so
// "lsof" no longer matches "ls").
func isSafeCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	// Reject command chaining / redirection / substitution. Any of these means
	// the command can run something other than its leading program.
	if strings.ContainsAny(cmd, ";&|<>`\n\r()") {
		return false
	}
	if strings.Contains(cmd, "$(") || strings.Contains(cmd, "${") {
		return false
	}
	fields := strings.Fields(cmd)
	prog := fields[0]
	switch {
	case safeForegroundCommands[prog]:
		return true
	case prog == "env":
		// Bare `env` prints the environment; `env CMD ...` executes CMD, so it
		// is only safe with no arguments.
		return len(fields) == 1
	case prog == "git":
		return len(fields) >= 2 && safeGitSubcommands[fields[1]]
	}
	return false
}

// decide evaluates a single tool call against the MANUAL-mode rules and returns
// how it should be handled. It is the single source of truth shared by the
// primary approval path and the teammate approval path so the two cannot drift.
func (s *ApprovalState) decide(toolName, toolArgs string) approvalDecision {
	if noApprovalNeeded[toolName] {
		return decisionAutoApprove
	}

	if d, ok := s.decideBrowser(toolName, toolArgs); ok {
		return d
	}

	switch toolName {
	case "read":
		var input struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal([]byte(toolArgs), &input); err == nil {
			if s.isWithinWorkpath(input.FilePath) {
				return decisionAutoApprove
			}
			return decisionPromptExternal
		}
		return decisionPrompt
	case "execute":
		var input struct {
			Command    string `json:"command"`
			Background bool   `json:"background"`
		}
		if err := json.Unmarshal([]byte(toolArgs), &input); err == nil {
			// Background commands always require approval: the agent controls the
			// flag, so auto-approving them would let any command (including
			// destructive ones) bypass the gate by setting background=true.
			if input.Background {
				return decisionPrompt
			}
			if isSafeCommand(input.Command) {
				return decisionAutoApprove
			}
		}
		return decisionPrompt
	}

	return decisionPrompt
}

// decideBrowser applies the browser-use approval tiers (see design §3.4). It
// returns (decision, true) when toolName is a browser tool, else (_, false).
// The read-only tier (snapshot/screenshot/read) is handled earlier via
// noApprovalNeeded, so this covers navigate / interact / high-risk.
func (s *ApprovalState) decideBrowser(toolName, toolArgs string) (approvalDecision, bool) {
	switch toolName {
	case "browser_eval":
		// High-risk: always prompt, never pre-authorized by a site permission.
		return decisionPrompt, true
	case "browser_open":
		origin := originFromArgs(toolArgs, "url")
		if s.browserPreapproved(origin, "navigate") {
			return decisionAutoApprove, true
		}
		return decisionPrompt, true
	case "browser_act":
		// Interaction. The origin comes from the live session (the active tab),
		// not the args — a click/fill carries no URL — so per-site interact=allow
		// and the interact class default can actually take effect.
		if s.browserPreapproved(s.browserActiveOrigin(), "interact") {
			return decisionAutoApprove, true
		}
		return decisionPrompt, true
	case "browser_tabs":
		var in struct {
			Op string `json:"op"`
		}
		_ = json.Unmarshal([]byte(toolArgs), &in)
		switch in.Op {
		case "", "list", "select":
			return decisionAutoApprove, true // read-only tab ops
		default: // new/claim/close mutate the controlled set
			return decisionPrompt, true
		}
	}
	return decisionPrompt, false
}

// browserPreapproved consults the site-permission hook (nil → always prompt).
func (s *ApprovalState) browserPreapproved(origin, class string) bool {
	s.mu.Lock()
	fn := s.browserPerm
	s.mu.Unlock()
	if fn == nil {
		return false
	}
	return fn(origin, class)
}

// browserActiveOrigin returns the active browser tab's origin (or "" when no
// provider is set or no tab is open).
func (s *ApprovalState) browserActiveOrigin() string {
	s.mu.Lock()
	fn := s.browserOrigin
	s.mu.Unlock()
	if fn == nil {
		return ""
	}
	return fn()
}

// originFromArgs extracts scheme://host from a URL arg for origin-scoped rules.
func originFromArgs(toolArgs, key string) string {
	var m map[string]any
	if json.Unmarshal([]byte(toolArgs), &m) != nil {
		return ""
	}
	raw, _ := m[key].(string)
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// RequestApproval is the agent.ApprovalFunc implementation.
// It returns true immediately for read-only or obviously safe commands.
// For everything else it sends a TUI prompt and waits for the user's answer.
func (s *ApprovalState) RequestApproval(ctx context.Context, toolName, toolArgs string) (bool, error) {
	// A PreToolUse hook that returned permissionDecision=allow pre-authorizes this
	// specific call, so the user is not prompted. This is scoped to the single
	// invocation whose ctx carries the flag.
	if hooks.IsPreApproved(ctx) {
		s.notifyToolInProgress(toolName, toolArgs)
		return true, nil
	}

	// State machine: AUTO mode passes all operations directly.
	s.mu.Lock()
	currentMode := s.mode
	s.mu.Unlock()
	if currentMode == handler.ModeAuto {
		s.notifyToolInProgress(toolName, toolArgs)
		return true, nil
	}

	switch s.decide(toolName, toolArgs) {
	case decisionAutoApprove:
		s.notifyToolInProgress(toolName, toolArgs)
		return true, nil
	case decisionPromptExternal:
		return s.requestUserApproval(ctx, toolName, toolArgs, true)
	default:
		return s.requestUserApproval(ctx, toolName, toolArgs, false)
	}
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

	// The approval middleware stamps the LLM tool-call id into ctx; forward it
	// so UIs can tie the prompt to the exact pending tool row, and key the
	// wait/denied bookkeeping below by it.
	toolCallID := agent.ToolCallIDFromContext(ctx)

	start := time.Now()
	resp, err := s.h.RequestApproval(ctx, handler.ApprovalRequest{
		ToolName:    toolName,
		ToolArgs:    toolArgs,
		ToolCallID:  toolCallID,
		IsExternal:  isExternal,
		WorkerName:  workerName,
		WorkerColor: workerColor,
	})
	// Record how long this call sat at the prompt (and whether it was denied)
	// so the runner reports pure execution time and a distinct denied state.
	// Recorded even on error: an errored prompt still consumed wall-clock wait.
	if meter := approvalMeterFrom(ctx); meter != nil {
		meter.record(toolCallID, time.Since(start), err == nil && !resp.Approved)
	}
	if err != nil {
		return false, err
	}

	// State transition: "Approve All" promotes the session to Full access (both the
	// unified mode and the derived approval axis). A plain single approve does
	// not change the session mode.
	if resp.Approved && resp.Mode == handler.ModeAuto {
		s.mu.Lock()
		s.sessionMode = mode.FullAccess
		s.mode = handler.ModeAuto
		s.mu.Unlock()
	}
	return resp.Approved, nil
}

// NewTeammateApprovalFunc creates an approval function for a teammate that includes
// the worker identity in the TUI approval prompt. It shares the same decision
// logic as RequestApproval (via decide) so the two paths cannot drift apart.
func (s *ApprovalState) NewTeammateApprovalFunc(workerName, workerColor string) func(ctx context.Context, toolName, toolArgs string) (bool, error) {
	return func(ctx context.Context, toolName, toolArgs string) (bool, error) {
		s.mu.Lock()
		currentMode := s.mode
		s.mu.Unlock()
		if currentMode == handler.ModeAuto {
			s.notifyToolInProgress(toolName, toolArgs)
			return true, nil
		}

		switch s.decide(toolName, toolArgs) {
		case decisionAutoApprove:
			s.notifyToolInProgress(toolName, toolArgs)
			return true, nil
		case decisionPromptExternal:
			return s.requestUserApprovalWithWorker(ctx, toolName, toolArgs, true, workerName, workerColor)
		default:
			return s.requestUserApprovalWithWorker(ctx, toolName, toolArgs, false, workerName, workerColor)
		}
	}
}

// isWithinWorkpath checks if the given path is within the workpath
func (s *ApprovalState) isWithinWorkpath(path string) bool {
	// Read workpath under the lock: decide() reaches here without holding it,
	// and SetWorkpath() can mutate it concurrently on an environment switch.
	s.mu.Lock()
	workpath := s.workpath
	s.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absWorkpath, err := filepath.Abs(workpath)
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
