package handler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/tools"
)

// TestWebHandler_ResolveApprovalOnceVsAll covers the P0 fix: the web approval
// endpoint must distinguish "approve once" (no session-mode change) from
// "approve all" (promote to auto-approve). Previously every Allow click mapped
// to ModeAuto, silently flipping the whole session to Autopilot.
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
