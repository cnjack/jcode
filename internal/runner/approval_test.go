package runner

import (
	"context"
	"testing"

	"github.com/cnjack/jcode/internal/agent"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/hooks"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/providertools"
	"github.com/cnjack/jcode/internal/toolpolicy"
	internaltools "github.com/cnjack/jcode/internal/tools"
)

// stubHandler is a minimal AgentEventHandler that returns a canned approval
// response, used to exercise the approve-once vs approve-all promotion path.
type stubHandler struct {
	resp     handler.ApprovalResponse
	respond  func(handler.ApprovalRequest) handler.ApprovalResponse
	requests *[]handler.ApprovalRequest
}

type progressStubHandler struct {
	stubHandler
	progressCalls int
	progressName  string
}

func (h *progressStubHandler) NotifyToolInProgress(name, _ string) {
	h.progressCalls++
	h.progressName = name
}

func (stubHandler) OnAgentText(string)                   {}
func (stubHandler) OnToolCall(handler.ToolCallEvent)     {}
func (stubHandler) OnToolResult(handler.ToolResultEvent) {}
func (stubHandler) OnTodoUpdate()                        {}
func (stubHandler) OnAgentStart()                        {}
func (stubHandler) OnAgentDone(error)                    {}
func (stubHandler) OnTokenUpdate(handler.TokenUsage)     {}
func (h stubHandler) RequestApproval(_ context.Context, req handler.ApprovalRequest) (handler.ApprovalResponse, error) {
	if h.requests != nil {
		*h.requests = append(*h.requests, req)
	}
	if h.respond != nil {
		return h.respond(req), nil
	}
	return h.resp, nil
}

func approveBillableOnce(req handler.ApprovalRequest) handler.ApprovalResponse {
	allowOnceID, _, err := handler.BillableApprovalOptionIDs(req.Options)
	if err != nil {
		return handler.ApprovalResponse{Approved: true, Mode: handler.ModeManual}
	}
	return handler.ApprovalResponse{
		Approved: true, Mode: handler.ModeManual, ResolvedOptionID: allowOnceID,
	}
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
		`{"command": "git diff --output=/tmp/result"}`,
		`{"command": "git diff --out=/tmp/result"}`,
		`{"command": "git diff --ext-diff"}`,
		`{"command": "git diff --textconv"}`,
		`{"command": "git -c diff.external=evil diff"}`,
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

func TestBillableApprovalUsesFullAccessButIgnoresHookAllow(t *testing.T) {
	intent := toolpolicy.BillableIntent{
		OperationID: "call-image-1", CapabilityKey: toolpolicy.CapabilityImageGenerate,
		Provider: "bigmodel", Model: "cogview", Count: 1,
		NormalizedArgs: `{"prompt":"desk","size":"1024x1024"}`,
	}
	ctx := toolpolicy.WithBillableIntent(context.Background(), intent)

	var fullAccessRequests []handler.ApprovalRequest
	fullAccess := NewApprovalStateWithMode("/tmp/workdir", mode.FullAccess)
	progressHandler := &progressStubHandler{stubHandler: stubHandler{requests: &fullAccessRequests}}
	fullAccess.SetHandler(progressHandler)
	approved, err := fullAccess.RequestApproval(ctx, "generate_image", intent.NormalizedArgs)
	if err != nil || !approved {
		t.Fatalf("Full access billable approval: approved=%v err=%v", approved, err)
	}
	if len(fullAccessRequests) != 0 {
		t.Fatalf("Full access emitted %d billable approval requests, want 0", len(fullAccessRequests))
	}
	if progressHandler.progressCalls != 1 || progressHandler.progressName != "generate_image" {
		t.Fatalf("Full access progress notifications = %d/%q, want 1/generate_image", progressHandler.progressCalls, progressHandler.progressName)
	}

	var requests []handler.ApprovalRequest
	state := NewApprovalStateWithMode("/tmp/workdir", mode.Approval)
	state.SetHandler(stubHandler{
		respond:  approveBillableOnce,
		requests: &requests,
	})
	hookCtx := hooks.WithPreApproved(context.Background())
	hookCtx = toolpolicy.WithBillableIntent(hookCtx, intent)
	approved, err = state.RequestApproval(hookCtx, "generate_image", intent.NormalizedArgs)
	if err != nil || !approved {
		t.Fatalf("billable approval: approved=%v err=%v", approved, err)
	}
	if len(requests) != 1 {
		t.Fatalf("approval requests = %d, want 1", len(requests))
	}
	request := requests[0]
	if request.ApprovalClass != toolpolicy.ApprovalBillableExternal || request.AllowApproveAll {
		t.Fatalf("billable request = %#v", request)
	}
	if _, _, err := handler.BillableApprovalOptionIDs(request.Options); err != nil {
		t.Fatalf("runner-issued billable options: %v", err)
	}
	if !request.IsExternal || request.BillableSummary == nil ||
		request.BillableSummary.Capability != toolpolicy.CapabilityImageGenerate ||
		request.BillableSummary.Provider != "bigmodel" || request.BillableSummary.Model != "cogview" ||
		request.BillableSummary.Size != "1024x1024" || request.BillableSummary.Count != 1 ||
		!request.BillableSummary.Billable {
		t.Fatalf("billable summary = %#v", request.BillableSummary)
	}
	if state.GetSessionMode() != mode.Approval {
		t.Fatalf("one-time billable approval changed session mode to %v", state.GetSessionMode())
	}
}

func TestBillableApprovalAutoModeStillPrompts(t *testing.T) {
	intent := toolpolicy.BillableIntent{
		OperationID: "call-image-auto", CapabilityKey: toolpolicy.CapabilityImageGenerate,
		Provider: "bigmodel", Model: "cogview", Count: 1,
		NormalizedArgs: `{"prompt":"desk","size":"1024x1024"}`,
	}
	ctx := toolpolicy.WithBillableIntent(context.Background(), intent)
	for _, test := range []struct {
		name  string
		state *ApprovalState
	}{
		{name: "unified Auto", state: NewApprovalStateWithMode("/tmp/workdir", mode.Auto)},
		{name: "legacy low-level auto axis", state: NewApprovalStateWithMode("/tmp/workdir", mode.Approval)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "legacy low-level auto axis" {
				test.state.SetMode(handler.ModeAuto)
			}
			var requests []handler.ApprovalRequest
			test.state.SetHandler(stubHandler{respond: approveBillableOnce, requests: &requests})
			if approved, err := test.state.RequestApproval(ctx, "generate_image", intent.NormalizedArgs); err != nil || !approved {
				t.Fatalf("Auto-mode billable approval: approved=%v err=%v", approved, err)
			}
			if len(requests) != 1 || requests[0].ApprovalClass != toolpolicy.ApprovalBillableExternal {
				t.Fatalf("Auto-mode billable requests = %#v", requests)
			}
		})
	}
}

func TestBillableApprovalRejectsBareBooleanAndReplayedOption(t *testing.T) {
	intent := toolpolicy.BillableIntent{
		OperationID: "host-operation-opaque", ToolCallID: "model-call-opaque",
		CapabilityKey: toolpolicy.CapabilityImageGenerate, Provider: "bigmodel", Model: "cogview",
		NormalizedArgs: `{"prompt":"desk","size":"1024x1024"}`, Count: 1,
	}
	ctx := toolpolicy.WithBillableIntent(context.Background(), intent)

	bare := NewApprovalState("/tmp/workdir", false)
	bare.SetHandler(stubHandler{resp: handler.ApprovalResponse{Approved: true, Mode: handler.ModeManual}})
	if approved, err := bare.RequestApproval(ctx, "generate_image", intent.NormalizedArgs); err == nil || approved {
		t.Fatalf("bare boolean billable response approved=%v err=%v", approved, err)
	}

	var firstAllowID string
	replay := NewApprovalState("/tmp/workdir", false)
	replay.SetHandler(stubHandler{respond: func(req handler.ApprovalRequest) handler.ApprovalResponse {
		allowID, _, err := handler.BillableApprovalOptionIDs(req.Options)
		if err != nil {
			return handler.ApprovalResponse{Approved: true, Mode: handler.ModeManual}
		}
		if firstAllowID == "" {
			firstAllowID = allowID
			return handler.ApprovalResponse{
				Approved: true, Mode: handler.ModeManual, ResolvedOptionID: allowID,
			}
		}
		if allowID == firstAllowID {
			t.Error("runner reused an opaque option id across requests")
		}
		return handler.ApprovalResponse{
			Approved: true, Mode: handler.ModeManual, ResolvedOptionID: firstAllowID,
		}
	}})
	if approved, err := replay.RequestApproval(ctx, "generate_image", intent.NormalizedArgs); err != nil || !approved {
		t.Fatalf("first exact option approved=%v err=%v", approved, err)
	}
	if approved, err := replay.RequestApproval(ctx, "generate_image", intent.NormalizedArgs); err == nil || approved {
		t.Fatalf("replayed option approved=%v err=%v", approved, err)
	}
}

func TestBillableApprovalGateRejectsExpiredMismatchAndSecondResolution(t *testing.T) {
	intent := toolpolicy.BillableIntent{
		OperationID: "host-operation-gate", ToolCallID: "model-call-gate",
		CapabilityKey: toolpolicy.CapabilityImageGenerate, Provider: "bigmodel", Model: "cogview",
		NormalizedArgs: `{"prompt":"desk","size":"1024x1024"}`, Count: 1,
	}

	gate, err := newBillableApprovalGate(intent)
	if err != nil {
		t.Fatal(err)
	}
	allowID, _, err := handler.BillableApprovalOptionIDs(gate.options())
	if err != nil {
		t.Fatal(err)
	}
	allow := handler.ApprovalResponse{
		Approved: true, Mode: handler.ModeManual, ResolvedOptionID: allowID,
	}
	if approved, resolveErr := gate.resolve(context.Background(), intent, allow); resolveErr != nil || !approved {
		t.Fatalf("exact option approved=%v err=%v", approved, resolveErr)
	}
	if approved, resolveErr := gate.resolve(context.Background(), intent, allow); resolveErr == nil || approved {
		t.Fatalf("second resolution approved=%v err=%v", approved, resolveErr)
	}

	mismatchGate, err := newBillableApprovalGate(intent)
	if err != nil {
		t.Fatal(err)
	}
	mismatchAllow, _, _ := handler.BillableApprovalOptionIDs(mismatchGate.options())
	mismatch := intent
	mismatch.OperationID = "other-operation"
	if approved, resolveErr := mismatchGate.resolve(context.Background(), mismatch, handler.ApprovalResponse{
		Approved: true, Mode: handler.ModeManual, ResolvedOptionID: mismatchAllow,
	}); resolveErr == nil || approved {
		t.Fatalf("mismatched intent approved=%v err=%v", approved, resolveErr)
	}

	expiredGate, err := newBillableApprovalGate(intent)
	if err != nil {
		t.Fatal(err)
	}
	expiredAllow, _, _ := handler.BillableApprovalOptionIDs(expiredGate.options())
	expiredCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if approved, resolveErr := expiredGate.resolve(expiredCtx, intent, handler.ApprovalResponse{
		Approved: true, Mode: handler.ModeManual, ResolvedOptionID: expiredAllow,
	}); resolveErr == nil || approved {
		t.Fatalf("expired option approved=%v err=%v", approved, resolveErr)
	}

	for _, test := range []struct {
		name     string
		response func(allowID, denyID string) handler.ApprovalResponse
	}{
		{
			name: "allow once cannot promote to auto",
			response: func(allowID, _ string) handler.ApprovalResponse {
				return handler.ApprovalResponse{
					Approved: true, Mode: handler.ModeAuto, ResolvedOptionID: allowID,
				}
			},
		},
		{
			name: "deny id cannot carry approved boolean",
			response: func(_, denyID string) handler.ApprovalResponse {
				return handler.ApprovalResponse{
					Approved: true, Mode: handler.ModeManual, ResolvedOptionID: denyID,
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			strictGate, gateErr := newBillableApprovalGate(intent)
			if gateErr != nil {
				t.Fatal(gateErr)
			}
			allowOptionID, denyOptionID, gateErr := handler.BillableApprovalOptionIDs(strictGate.options())
			if gateErr != nil {
				t.Fatal(gateErr)
			}
			if approved, resolveErr := strictGate.resolve(
				context.Background(), intent, test.response(allowOptionID, denyOptionID),
			); resolveErr == nil || approved {
				t.Fatalf("forged tuple approved=%v err=%v", approved, resolveErr)
			}
		})
	}
}

func TestBillableApprovalExactDenyIsFailClosedWithoutError(t *testing.T) {
	intent := toolpolicy.BillableIntent{
		OperationID: "host-operation-deny", ToolCallID: "model-call-deny",
		CapabilityKey: toolpolicy.CapabilityImageGenerate, Provider: "bigmodel", Model: "cogview",
		NormalizedArgs: `{"prompt":"desk","size":"1024x1024"}`, Count: 1,
	}
	state := NewApprovalState("/tmp/workdir", false)
	state.SetHandler(stubHandler{respond: func(req handler.ApprovalRequest) handler.ApprovalResponse {
		_, denyID, _ := handler.BillableApprovalOptionIDs(req.Options)
		return handler.ApprovalResponse{
			Approved: false, Mode: handler.ModeManual, ResolvedOptionID: denyID,
		}
	}})
	ctx := toolpolicy.WithBillableIntent(context.Background(), intent)
	if approved, err := state.RequestApproval(ctx, "generate_image", intent.NormalizedArgs); err != nil || approved {
		t.Fatalf("exact deny approved=%v err=%v", approved, err)
	}
}

func TestTeammateBillableApprovalUsesFullAccess(t *testing.T) {
	var requests []handler.ApprovalRequest
	state := NewApprovalState("/tmp/workdir", true)
	state.SetHandler(stubHandler{requests: &requests})
	intent := toolpolicy.BillableIntent{
		OperationID: "host-operation-teammate", ToolCallID: "model-call-teammate",
		CapabilityKey: toolpolicy.CapabilityImageGenerate, Provider: "bigmodel", Model: "cogview",
		NormalizedArgs: `{"prompt":"desk","size":"1024x1024"}`, Count: 1,
	}
	ctx := toolpolicy.WithBillableIntent(context.Background(), intent)
	approve := state.NewTeammateApprovalFunc("worker", "blue")
	if approved, err := approve(ctx, "generate_image", intent.NormalizedArgs); err != nil || !approved {
		t.Fatalf("teammate billable approval=%v err=%v", approved, err)
	}
	if len(requests) != 0 {
		t.Fatalf("Full access teammate emitted billable approval requests = %#v", requests)
	}
	if approved, err := approve(context.Background(), "generate_image", intent.NormalizedArgs); err == nil || approved {
		t.Fatalf("teammate missing intent approved=%v err=%v", approved, err)
	}
}

func TestGenerateImageWithoutPreparedIntentFailsClosed(t *testing.T) {
	state := NewApprovalState("/tmp/workdir", true)
	if approved, err := state.RequestApproval(context.Background(), "generate_image", `{"prompt":"desk"}`); err == nil || approved {
		t.Fatalf("generate_image without intent: approved=%v err=%v", approved, err)
	}
}

func TestBillableWebSearchRequiresExactReservedToolIdentity(t *testing.T) {
	state := NewApprovalState("/tmp/workdir", false)
	state.SetHandler(stubHandler{respond: approveBillableOnce})
	const (
		searchName  = "mcp__approval_provider_search__web_search_prime"
		unknownName = "mcp__approval_provider_search__delete_everything"
	)
	server := providertools.BigModelSearchMCPServerName()
	internaltools.RegisterMCPToolIdentity(searchName, server, providertools.BigModelSearchMCPToolName)
	internaltools.RegisterMCPToolIdentity(unknownName, server, "delete_everything")
	intent := toolpolicy.BillableIntent{
		OperationID: "host-operation-1", ToolCallID: "model-call-1",
		CapabilityKey:  toolpolicy.CapabilityWebSearch,
		Provider:       providertools.BigModelCodingProvider,
		Model:          providertools.BigModelSearchMCPToolName,
		Count:          1,
		NormalizedArgs: `{"query":"jcode"}`,
	}
	ctx := toolpolicy.WithBillableIntent(context.Background(), intent)
	if approved, err := state.RequestApproval(ctx, unknownName, intent.NormalizedArgs); err == nil || approved {
		t.Fatalf("unknown reserved tool approved=%v err=%v", approved, err)
	}
	if approved, err := state.RequestApproval(ctx, searchName, intent.NormalizedArgs); err != nil || !approved {
		t.Fatalf("exact search tool approved=%v err=%v", approved, err)
	}

	var fullAccessRequests []handler.ApprovalRequest
	fullAccess := NewApprovalStateWithMode("/tmp/workdir", mode.FullAccess)
	fullAccess.SetHandler(stubHandler{requests: &fullAccessRequests})
	if approved, err := fullAccess.RequestApproval(ctx, unknownName, intent.NormalizedArgs); err == nil || approved {
		t.Fatalf("Full access unknown reserved tool approved=%v err=%v", approved, err)
	}
	if approved, err := fullAccess.RequestApproval(ctx, searchName, intent.NormalizedArgs); err != nil || !approved {
		t.Fatalf("Full access exact search tool approved=%v err=%v", approved, err)
	}
	if len(fullAccessRequests) != 0 {
		t.Fatalf("Full access search emitted approval requests = %#v", fullAccessRequests)
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

func TestShowArtifactIsAutoApprovedAsMetadataOnlyDelivery(t *testing.T) {
	s := NewApprovalState("/tmp/workdir", false)
	if got := s.decide("show_artifact", `{"path":"report.html"}`); got != decisionAutoApprove {
		t.Fatalf("show_artifact decision=%v want auto approve", got)
	}
}

func TestSubagentDelegatedWriteGrantDecision(t *testing.T) {
	if noApprovalNeeded["subagent"] {
		t.Fatal("subagent must be decided from agent_type, not globally auto-approved")
	}
	s := NewApprovalState("/tmp/workdir", false)
	tests := []struct {
		name string
		args string
		want approvalDecision
	}{
		{name: "missing defaults explore", args: `{}`, want: decisionAutoApprove},
		{name: "empty defaults explore", args: `{"agent_type":""}`, want: decisionAutoApprove},
		{name: "explore", args: `{"agent_type":"explore"}`, want: decisionAutoApprove},
		{name: "explore background", args: `{"agent_type":"explore","run_in_background":true}`, want: decisionAutoApprove},
		{name: "general", args: `{"agent_type":"general"}`, want: decisionPrompt},
		{name: "general background", args: `{"agent_type":"general","run_in_background":true}`, want: decisionPrompt},
		{name: "coordinator", args: `{"agent_type":"coordinator"}`, want: decisionPrompt},
		{name: "invalid", args: `{"agent_type":"writer"}`, want: decisionPrompt},
		{name: "wrong type", args: `{"agent_type":1}`, want: decisionPrompt},
		{name: "null", args: `{"agent_type":null}`, want: decisionPrompt},
		{name: "malformed", args: `{"agent_type":`, want: decisionPrompt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.decide("subagent", tt.args); got != tt.want {
				t.Fatalf("decide(subagent, %s) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestTeamSpawnPermissionDecision(t *testing.T) {
	if noApprovalNeeded["team_spawn"] {
		t.Fatal("team_spawn must be decided from its child profile, not globally auto-approved")
	}
	s := NewApprovalState("/tmp/workdir", false)
	tests := []struct {
		name string
		args string
		want approvalDecision
	}{
		{name: "missing defaults general normal", args: `{}`, want: decisionAutoApprove},
		{name: "empty defaults general normal", args: `{"agent_type":"","mode":""}`, want: decisionAutoApprove},
		{name: "explore normal", args: `{"agent_type":"explore","mode":"normal"}`, want: decisionAutoApprove},
		{name: "explore plan", args: `{"agent_type":"explore","mode":"plan"}`, want: decisionAutoApprove},
		{name: "explore auto", args: `{"agent_type":"explore","mode":"auto"}`, want: decisionAutoApprove},
		{name: "general normal", args: `{"agent_type":"general","mode":"normal"}`, want: decisionAutoApprove},
		{name: "coder normal", args: `{"agent_type":"coder","mode":"normal"}`, want: decisionAutoApprove},
		{name: "general plan", args: `{"agent_type":"general","mode":"plan"}`, want: decisionAutoApprove},
		{name: "coder plan", args: `{"agent_type":"coder","mode":"plan"}`, want: decisionAutoApprove},
		{name: "general auto one-time grant", args: `{"agent_type":"general","mode":"auto"}`, want: decisionPrompt},
		{name: "coder auto one-time grant", args: `{"agent_type":"coder","mode":"auto"}`, want: decisionPrompt},
		{name: "invalid agent type", args: `{"agent_type":"writer","mode":"normal"}`, want: decisionPrompt},
		{name: "invalid mode", args: `{"agent_type":"explore","mode":"unsafe"}`, want: decisionPrompt},
		{name: "agent type wrong JSON type", args: `{"agent_type":1,"mode":"normal"}`, want: decisionPrompt},
		{name: "mode wrong JSON type", args: `{"agent_type":"explore","mode":true}`, want: decisionPrompt},
		{name: "agent type null", args: `{"agent_type":null,"mode":"normal"}`, want: decisionPrompt},
		{name: "mode null", args: `{"agent_type":"explore","mode":null}`, want: decisionPrompt},
		{name: "malformed", args: `{"agent_type":`, want: decisionPrompt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.decide("team_spawn", tt.args); got != tt.want {
				t.Fatalf("decide(team_spawn, %s) = %v, want %v", tt.args, got, tt.want)
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
