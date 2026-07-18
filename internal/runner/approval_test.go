package runner

import (
	"context"
	"testing"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
	internaltools "github.com/cnjack/jcode/internal/tools"
)

// stubHandler is a minimal AgentEventHandler that returns a canned approval
// response, used to exercise the approve-once vs approve-all promotion path.
type stubHandler struct {
	resp handler.ApprovalResponse
}

func (stubHandler) OnAgentText(string)                   {}
func (stubHandler) OnToolCall(handler.ToolCallEvent)     {}
func (stubHandler) OnToolResult(handler.ToolResultEvent) {}
func (stubHandler) OnTodoUpdate()                        {}
func (stubHandler) OnAgentStart()                        {}
func (stubHandler) OnAgentDone(error)                    {}
func (stubHandler) OnTokenUpdate(handler.TokenUsage)     {}
func (h stubHandler) RequestApproval(context.Context, handler.ApprovalRequest) (handler.ApprovalResponse, error) {
	return h.resp, nil
}

func TestNewApprovalState(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	if s.mode != handler.ModeManual {
		t.Errorf("expected default mode to be Manual, got %v", s.mode)
	}
}

func TestNewApprovalState_AutoApprove(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", true)
	if s.mode != handler.ModeAuto {
		t.Errorf("expected mode to be Auto when autoApprove=true, got %v", s.mode)
	}
}

func TestApprovalState_SetWorkpath(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	s.SetWorkpath("/tmp/otherdir")
	if s.workpath != "/tmp/otherdir" {
		t.Errorf("expected workpath to be /tmp/otherdir, got %v", s.workpath)
	}
}

func TestIsWithinWorkpath(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)

	tests := []struct {
		path     string
		expected bool
	}{
		{"/tmp/workdir/file.txt", true},
		{"/tmp/workdir/subdir/file.txt", true},
		{"/tmp/workdir/subdir", true},
		{"/tmp/otherdir/file.txt", false},
		{"/tmp/external_dir/file.txt", false},
		{"/etc/passwd", false},
		{"../file.txt", false},
		{"/tmp/workdir/../external/file.txt", false},
		{"/tmp/workdir/../external_dir/file.txt", false},
	}

	for _, tc := range tests {
		result := s.isWithinWorkpath(tc.path)
		if result != tc.expected {
			t.Errorf("isWithinWorkpath(%q) = %v, expected %v", tc.path, tc.expected, result)
		}
	}
}

func TestRequestApproval_AutoMode(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	s.SetMode(handler.ModeAuto)

	ctx := context.Background()

	// AUTO mode - all tools auto-approve
	approved, err := s.RequestApproval(ctx, "read", `{"file_path": "/etc/passwd"}`)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !approved {
		t.Errorf("expected auto-approve in AUTO mode")
	}
}

func TestRequestApproval_ManualMode(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	ctx := context.Background()

	// Test read within workpath - auto-approve
	approved, err := s.RequestApproval(ctx, "read", `{"file_path": "/tmp/workdir/file.txt"}`)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !approved {
		t.Errorf("expected auto-approve for read within workpath")
	}

	// Test read outside workpath - needs approval (no TUI program, should fail)
	_, err = s.RequestApproval(ctx, "read", `{"file_path": "/etc/passwd"}`)
	if err == nil {
		t.Errorf("expected error for read outside workpath without TUI program")
	}

	// Test safe command - auto-approve
	approved, err = s.RequestApproval(ctx, "execute", `{"command": "ls -la"}`)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !approved {
		t.Errorf("expected auto-approve for safe command")
	}

	// Test dangerous command - needs approval (no TUI program, should fail)
	_, err = s.RequestApproval(ctx, "execute", `{"command": "rm -rf /"}`)
	if err == nil {
		t.Errorf("expected error for dangerous command without TUI program")
	}
}

// TestRequestApproval_BackgroundRequiresApproval covers the P0 fix: in MANUAL
// mode a background command must never be auto-approved, even one that would be
// "safe" in the foreground — otherwise the agent could bypass the gate by
// setting background=true. With no handler attached, the prompt path surfaces
// as an error, which is how we detect "did not auto-approve".
func TestRequestApproval_BackgroundRequiresApproval(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false) // MANUAL, no handler
	ctx := context.Background()

	background := []string{
		`{"command": "ls -la", "background": true}`,
		`{"command": "echo hi", "background": true}`,
		`{"command": "rm -rf /", "background": true}`,
	}
	for _, args := range background {
		if approved, err := s.RequestApproval(ctx, "execute", args); err == nil {
			t.Errorf("background command should require approval, got auto-approve=%v for %s", approved, args)
		}
	}

	// The same command in the foreground still auto-approves when safe.
	if approved, err := s.RequestApproval(ctx, "execute", `{"command": "ls -la"}`); err != nil || !approved {
		t.Errorf("foreground safe command should auto-approve: approved=%v err=%v", approved, err)
	}
}

// TestRequestApproval_ShellOperatorInjection covers the P1 fix: a "safe" prefix
// can no longer smuggle a payload via shell operators, and bare command names
// are matched as whole words (so lsof != ls, env-with-args is not safe).
func TestRequestApproval_ShellOperatorInjection(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	ctx := context.Background()

	mustPrompt := []string{
		`{"command": "git status && rm -rf /"}`,
		`{"command": "ls; whoami"}`,
		`{"command": "cat foo | sh"}`,
		`{"command": "echo pwned > /tmp/x"}`,
		`{"command": "echo $(rm -rf x)"}`,
		"{\"command\": \"echo `whoami`\"}",
		`{"command": "lsof"}`,
		`{"command": "env rm -rf x"}`,
		`{"command": "git difftool"}`,
	}
	for _, args := range mustPrompt {
		if approved, err := s.RequestApproval(ctx, "execute", args); err == nil {
			t.Errorf("command should require approval, got auto-approve=%v for %s", approved, args)
		}
	}

	autoOK := []string{
		`{"command": "git status"}`,
		`{"command": "git log --oneline -5"}`,
		`{"command": "ls -la /tmp"}`,
		`{"command": "cat go.mod"}`,
		`{"command": "env"}`,
		`{"command": "which go"}`,
	}
	for _, args := range autoOK {
		if approved, err := s.RequestApproval(ctx, "execute", args); err != nil || !approved {
			t.Errorf("command should auto-approve: %s (approved=%v err=%v)", args, approved, err)
		}
	}
}

// TestRequestApproval_AskUserAutoApprove covers the P1 fix: the user-facing
// ask_user tool must not itself trigger an approval prompt (the allowlist
// previously held the dead name "question").
func TestRequestApproval_AskUserAutoApprove(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	approved, err := s.RequestApproval(context.Background(), "ask_user", `{"question": "pick one"}`)
	if err != nil || !approved {
		t.Errorf("ask_user should auto-approve without prompting: approved=%v err=%v", approved, err)
	}
}

// TestRequestApproval_ApproveAllPromotes covers the promotion semantics: an
// "approve all" response (Mode=Auto) promotes the session to Full access, while a
// plain "approve once" leaves it in Approval/MANUAL.
func TestRequestApproval_ApproveAllPromotes(t *testing.T) {
	ctx := context.Background()

	all := NewApprovalState("/tmp/workdir", false)
	all.SetHandler(stubHandler{resp: handler.ApprovalResponse{Approved: true, Mode: handler.ModeAuto}})
	if approved, err := all.RequestApproval(ctx, "execute", `{"command": "rm -rf x"}`); err != nil || !approved {
		t.Fatalf("approve-all: approved=%v err=%v", approved, err)
	}
	if all.GetMode() != handler.ModeAuto {
		t.Errorf("approve-all should promote approval axis to Auto, got %v", all.GetMode())
	}
	if all.GetSessionMode() != mode.FullAccess {
		t.Errorf("approve-all should promote session to Full access, got %v", all.GetSessionMode())
	}

	once := NewApprovalState("/tmp/workdir", false)
	once.SetHandler(stubHandler{resp: handler.ApprovalResponse{Approved: true, Mode: handler.ModeManual}})
	if approved, err := once.RequestApproval(ctx, "execute", `{"command": "rm -rf x"}`); err != nil || !approved {
		t.Fatalf("approve-once: approved=%v err=%v", approved, err)
	}
	if once.GetMode() != handler.ModeManual {
		t.Errorf("approve-once should leave approval axis at Manual, got %v", once.GetMode())
	}
	if once.GetSessionMode() != mode.Approval {
		t.Errorf("approve-once should leave session at Approval, got %v", once.GetSessionMode())
	}
}

func TestRequestApproval_NoApprovalTools(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	ctx := context.Background()

	// Test glob - auto-approve
	approved, err := s.RequestApproval(ctx, "glob", `{"pattern": "*.go"}`)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !approved {
		t.Errorf("expected auto-approve for glob")
	}

	// Test grep - auto-approve
	approved, err = s.RequestApproval(ctx, "grep", `{"pattern": "test"}`)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !approved {
		t.Errorf("expected auto-approve for grep")
	}
}

func TestRequestApprovalProgressiveDisclosureReadOnlyTools(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	for _, toolName := range []string{
		agent.ToolSearchReservedName,
		"load_skill",
		"goal_get",
	} {
		t.Run(toolName, func(t *testing.T) {
			approved, err := s.RequestApproval(context.Background(), toolName, `{}`)
			if err != nil || !approved {
				t.Fatalf("%s should auto-approve: approved=%v err=%v", toolName, approved, err)
			}
		})
	}
}

func TestRequestApprovalDeferredMutationStillPrompts(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	for _, toolName := range []string{
		"goal_set",
		"goal_update",
		"automation_create",
		"memory_note",
		"workflow_run",
	} {
		t.Run(toolName, func(t *testing.T) {
			if approved, err := s.RequestApproval(context.Background(), toolName, `{}`); err == nil {
				t.Fatalf("%s should prompt, got approved=%v", toolName, approved)
			}
		})
	}
}

func TestApprovalMCPProvenancePrecedesBuiltinAllowlist(t *testing.T) {
	const canonicalName = "mcp__approval_test__goal_get"
	internaltools.RegisterMCPToolIdentity(canonicalName, "approval-test", "goal_get")

	// Add the canonical name to the internal allowlist as a collision canary.
	// Provenance must still force a prompt before this table is considered.
	noApprovalNeeded[canonicalName] = true
	defer delete(noApprovalNeeded, canonicalName)

	s := NewApprovalState("/tmp/workdir", false)
	if approved, err := s.RequestApproval(context.Background(), canonicalName, `{}`); err == nil {
		t.Fatalf("MCP tool should prompt despite allowlist collision, got approved=%v", approved)
	}
}

func TestApprovalState_AutoModeMapsToManual(t *testing.T) {
	s := NewApprovalStateWithMode("/tmp/workdir", mode.Auto)
	if s.GetMode() != handler.ModeManual {
		t.Errorf("Auto mode should map to Manual approval axis, got %v", s.GetMode())
	}
	if s.GetSessionMode() != mode.Auto {
		t.Errorf("session mode should be Auto, got %v", s.GetSessionMode())
	}
}

func TestApprovalState_ReviewerLifecycle(t *testing.T) {
	s := NewApprovalStateWithMode("/tmp/workdir", mode.Approval)
	s.SetReviewerConfig(nil, "")

	// Reviewer is nil until entering Auto mode.
	if s.reviewer != nil {
		t.Errorf("reviewer should be nil in Approval mode")
	}

	s.SetSessionMode(mode.Auto)
	if s.reviewer == nil {
		t.Errorf("reviewer should be built when entering Auto mode")
	}

	s.SetSessionMode(mode.Approval)
	if s.reviewer != nil {
		t.Errorf("reviewer should be cleared when leaving Auto mode")
	}
}
