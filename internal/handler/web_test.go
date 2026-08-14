package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/tools"
)

// TestWebHandler_ResolveApprovalOnceVsAll covers the P0 fix: the web approval
// endpoint must distinguish "approve once" (no session-mode change) from
// "approve all" (promote to auto-approve). Previously every Allow click mapped
// to ModeAuto, silently flipping the whole session to Full access.
func TestWebHandler_ResolveApprovalOnceVsAll(t *testing.T) {
	run := func(approveAll bool) ApprovalResponse {
		t.Helper()
		h := NewWebHandler()
		respCh := make(chan ApprovalResponse, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		go func() {
			r, err := h.RequestApproval(ctx, ApprovalRequest{ToolName: "execute"})
			if err != nil {
				t.Errorf("RequestApproval failed: %v", err)
				return
			}
			respCh <- r
		}()

		// Wait for the emitted approval_request to learn the generated id.
		var ev WebEvent
		select {
		case ev = <-h.Events():
		case <-ctx.Done():
			t.Fatalf("timed out waiting for approval_request event")
		}
		data, ok := ev.Data.(WebApprovalRequestData)
		if !ok {
			t.Fatalf("expected WebApprovalRequestData, got %T", ev.Data)
		}
		if err := h.ResolveApproval(data.ID, true, approveAll); err != nil {
			t.Fatalf("ResolveApproval(%q, true, %v): %v", data.ID, approveAll, err)
		}
		select {
		case r := <-respCh:
			return r
		case <-ctx.Done():
			t.Fatalf("timed out waiting for approval response")
			return ApprovalResponse{}
		}
	}

	if r := run(false); !r.Approved || r.Mode != ModeManual {
		t.Errorf("approve once: expected {Approved:true, Mode:Manual}, got %+v", r)
	}
	if r := run(true); !r.Approved || r.Mode != ModeAuto {
		t.Errorf("approve all: expected {Approved:true, Mode:Auto}, got %+v", r)
	}
}

func TestWebHandler_AgentDoneCarriesRemoteErrorCode(t *testing.T) {
	h := NewWebHandler()
	h.OnAgentDone(tools.Fatal(&tools.RemoteTransportError{
		Kind: "ssh", Code: "ssh_connection_failed",
		Phase:     tools.RemoteTransportOutcomeUnknown,
		Retryable: true, // The transport is retryable; the dispatched operation is not.
		Err:       context.DeadlineExceeded,
	}))

	select {
	case event := <-h.Events():
		if event.Event != "agent_done" {
			t.Fatalf("event = %q", event.Event)
		}
		data, ok := event.Data.(WebDoneData)
		if !ok {
			t.Fatalf("data = %T, want WebDoneData", event.Data)
		}
		if data.Code != "ssh_connection_failed" || data.ErrorKind != "remote_connection" ||
			data.Kind != "ssh" || data.Phase != string(tools.RemoteTransportOutcomeUnknown) {
			t.Fatalf("remote agent_done = %+v", data)
		}
		if data.Retryable == nil || *data.Retryable {
			t.Fatalf("retryable = %v, want explicit false", data.Retryable)
		}
		wire, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("marshal remote agent_done: %v", err)
		}
		if !strings.Contains(string(wire), `"retryable":false`) {
			t.Fatalf("wire data omitted explicit non-retryable state: %s", wire)
		}
		if strings.Contains(strings.ToLower(data.Error), "model") {
			t.Fatalf("remote error mislabelled as model error: %q", data.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for agent_done")
	}
}

func TestWebHandlerModelRetryCarriesBackoffStatus(t *testing.T) {
	h := NewWebHandler()
	h.OnModelRetry(ModelRetryEvent{
		Status: ModelRetryWaiting, Attempt: 2, MaxAttempts: 5, RetryIn: 1500 * time.Millisecond,
	})

	select {
	case event := <-h.Events():
		if event.Event != "model_retry_status" {
			t.Fatalf("event = %q", event.Event)
		}
		data, ok := event.Data.(WebModelRetryData)
		if !ok {
			t.Fatalf("data = %T, want WebModelRetryData", event.Data)
		}
		if data.Status != ModelRetryWaiting || data.Attempt != 2 ||
			data.MaxAttempts != 5 || data.RetryInMS != 1500 {
			t.Fatalf("model retry data = %+v", data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for model_retry_status")
	}
}

func TestWebHandlerBillableApprovalRequiresOpaqueOneTimeOption(t *testing.T) {
	h := NewWebHandler()
	issuedOptions := []ApprovalOption{
		{ID: "runner-allow-once", Label: "Allow once", Kind: "allow_once"},
		{ID: "runner-deny", Label: "Deny", Kind: "deny"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	responseCh := make(chan ApprovalResponse, 1)
	go func() {
		response, err := h.RequestApproval(ctx, ApprovalRequest{
			ToolName: "generate_image", ToolArgs: `{"prompt":"private prompt","aspect_ratio":"9:16","resolution":"2k"}`,
			ToolCallID: "call-image-1", ApprovalClass: "billable_external",
			AllowApproveAll: false,
			Options:         issuedOptions,
			BillableSummary: &BillableApprovalSummary{
				Capability: "image.generate", Provider: "provider", Model: "image-model",
				AspectRatio: "9:16", Resolution: "2k", Count: 1, Billable: true,
			},
		})
		if err != nil {
			t.Errorf("RequestApproval: %v", err)
			return
		}
		responseCh <- response
	}()

	event := <-h.Events()
	request := event.Data.(WebApprovalRequestData)
	if request.ToolArgs != "{}" || strings.Contains(request.ToolArgs, "private prompt") {
		t.Fatalf("billable tool args were exposed: %q", request.ToolArgs)
	}
	if request.AllowApproveAll || len(request.Options) != 2 || request.Options[0].ID == "" ||
		request.Options[0].ID == request.Options[1].ID || request.BillableSummary == nil {
		t.Fatalf("billable request = %#v", request)
	}
	if request.BillableSummary.AspectRatio != "9:16" || request.BillableSummary.Resolution != "2k" {
		t.Fatalf("billable native geometry = %#v", request.BillableSummary)
	}
	if request.Options[0].ID != issuedOptions[0].ID || request.Options[1].ID != issuedOptions[1].ID {
		t.Fatalf("web transport replaced runner option ids: %#v", request.Options)
	}
	if err := h.ResolveApproval(request.ID, true, false); err == nil {
		t.Fatal("boolean approval bypassed the opaque option contract")
	}
	if _, err := h.ResolveApprovalOption(request.ID, "forged-option"); err == nil {
		t.Fatal("forged option id was accepted")
	}
	allowID := request.Options[0].ID
	resolved, err := h.ResolveApprovalOption(request.ID, allowID)
	if err != nil || !resolved.Approved || resolved.Mode != ModeManual || resolved.ResolvedOptionID != allowID {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
	response := <-responseCh
	if !response.Approved || response.ResolvedOptionID != allowID {
		t.Fatalf("request response = %#v", response)
	}
	if _, err := h.ResolveApprovalOption(request.ID, allowID); err == nil {
		t.Fatal("replayed option id was accepted")
	}
}

func TestWebHandlerBillableApprovalRejectsMissingRunnerOptions(t *testing.T) {
	h := NewWebHandler()
	if _, err := h.RequestApproval(context.Background(), ApprovalRequest{
		ToolName: "generate_image", ApprovalClass: "billable_external",
		BillableSummary: &BillableApprovalSummary{Capability: "image.generate", Billable: true},
	}); err == nil {
		t.Fatal("billable web approval accepted missing runner-issued options")
	}
}

// TestWebHandler_ToolResultDeniedAndApprovalToolCallID covers the approval
// semantics WS contract: tool_result carries denied (+ the runner-adjusted
// duration_ms), and approval_request carries the gated call's tool_call_id so
// the UI can paint that row as awaiting approval.
func TestWebHandler_ToolResultDeniedAndApprovalToolCallID(t *testing.T) {
	h := NewWebHandler()

	h.OnToolResult(ToolResultEvent{
		Name:       "execute",
		Output:     "Tool execution was rejected by user.",
		ToolCallID: "call_1",
		Denied:     true,
		Duration:   1500 * time.Millisecond,
	})
	ev := <-h.Events()
	if ev.Event != "tool_result" {
		t.Fatalf("expected tool_result event, got %q", ev.Event)
	}
	data, ok := ev.Data.(WebToolResultData)
	if !ok {
		t.Fatalf("expected WebToolResultData, got %T", ev.Data)
	}
	if !data.Denied {
		t.Error("tool_result should carry denied=true")
	}
	if data.DurationMs != 1500 {
		t.Errorf("duration_ms = %d, want 1500", data.DurationMs)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		_, _ = h.RequestApproval(ctx, ApprovalRequest{ToolName: "execute", ToolCallID: "call_2"})
	}()
	select {
	case ev = <-h.Events():
	case <-ctx.Done():
		t.Fatal("timed out waiting for approval_request event")
	}
	req, ok := ev.Data.(WebApprovalRequestData)
	if !ok {
		t.Fatalf("expected WebApprovalRequestData, got %T", ev.Data)
	}
	if req.ToolCallID != "call_2" {
		t.Errorf("approval_request tool_call_id = %q, want call_2", req.ToolCallID)
	}
	// The pending (reload-reconcile) snapshot must carry it too.
	if pending := h.PendingApprovalRequests(); len(pending) != 1 || pending[0].ToolCallID != "call_2" {
		t.Errorf("pending approvals = %+v, want one entry with tool_call_id call_2", pending)
	}
	if err := h.ResolveApproval(req.ID, false, false); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
}

func TestWebHandlerImageLifecycleCarriesImmutableProviderModelSnapshot(t *testing.T) {
	h := NewWebHandler()
	h.OnToolProgress(ToolProgressEvent{
		Name: "generate_image", ToolCallID: "call-image", OperationID: "operation-image",
		Phase: ToolPhaseGenerating, Provider: "provider-old", Model: "model-old",
	})
	progress := (<-h.Events()).Data.(WebToolProgressData)
	if progress.Provider != "provider-old" || progress.Model != "model-old" {
		t.Fatalf("progress snapshot = %q/%q", progress.Provider, progress.Model)
	}

	h.OnToolResult(ToolResultEvent{
		Name: "generate_image", ToolCallID: "call-image", OperationID: "operation-image",
		Phase: ToolPhaseFailed, Outcome: ToolOutcomeFailed, ErrorCode: "authentication_failed",
		Provider: "provider-old", Model: "model-old",
	})
	result := (<-h.Events()).Data.(WebToolResultData)
	if result.Provider != "provider-old" || result.Model != "model-old" ||
		result.ErrorCode != "authentication_failed" {
		t.Fatalf("result snapshot = %#v", result)
	}
}

// TestWebHandler_AskUserRoundTrip exercises the full web ask_user loop through
// the real tool: the tool's BatchRequestFn blocks on RequestAskUser, the
// handler emits ask_user_request with a generated id, and ResolveAskUser
// (what POST /api/ask calls) unblocks the tool with the user's answer. This is
// the path that previously hung — the web tool was wired with a dead channel.
func TestWebHandler_AskUserRoundTrip(t *testing.T) {
	h := NewWebHandler()
	tool := tools.NewAskUserTool(&tools.AskUserDeps{
		BatchRequestFn: h.RequestAskUser,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resultCh := make(chan string, 1)
	go func() {
		out, err := tool.InvokableRun(ctx,
			`{"questions":[{"question":"Which feature?","header":"feature","options":[{"label":"Single","description":"one"},{"label":"Multi","description":"many"}]}]}`)
		if err != nil {
			t.Errorf("InvokableRun failed: %v", err)
		}
		resultCh <- out
	}()

	// Capture the emitted request to learn the generated id + normalized questions.
	var ev WebEvent
	select {
	case ev = <-h.Events():
	case <-ctx.Done():
		t.Fatalf("timed out waiting for ask_user_request event")
	}
	if ev.Event != "ask_user_request" {
		t.Fatalf("expected ask_user_request event, got %q", ev.Event)
	}
	data, ok := ev.Data.(WebAskUserRequestData)
	if !ok {
		t.Fatalf("expected WebAskUserRequestData, got %T", ev.Data)
	}
	if len(data.Questions) != 1 || data.Questions[0].Question != "Which feature?" {
		t.Fatalf("unexpected questions payload: %+v", data.Questions)
	}

	// While in flight, the question must be re-surfaceable (page-reload recovery).
	pending := h.PendingAskUserRequests()
	if len(pending) != 1 || pending[0].ID != data.ID {
		t.Fatalf("expected 1 pending ask_user with id %q, got %+v", data.ID, pending)
	}

	// Resolve as the API handler would.
	if err := h.ResolveAskUser(data.ID, tools.AskUserBatchResponse{
		Answers: []tools.AskUserAnswer{{QuestionHeader: "feature", Answer: "Single"}},
	}); err != nil {
		t.Fatalf("ResolveAskUser(%q): %v", data.ID, err)
	}

	select {
	case out := <-resultCh:
		if !strings.Contains(out, "Single") {
			t.Errorf("expected tool result to contain the answer, got %q", out)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for tool result (the hang this fix addresses)")
	}

	// Once answered, the request is cleared from the pending set.
	if pending := h.PendingAskUserRequests(); len(pending) != 0 {
		t.Errorf("expected no pending ask_user after answer, got %+v", pending)
	}

	// A second resolve for the now-cleared id must report no pending request.
	if err := h.ResolveAskUser(data.ID, tools.AskUserBatchResponse{}); err == nil {
		t.Errorf("expected error resolving an already-answered ask_user id")
	}
}
