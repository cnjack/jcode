package handler

import (
	"context"
	"testing"
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
		go func() {
			r, _ := h.RequestApproval(context.Background(), ApprovalRequest{ToolName: "execute"})
			respCh <- r
		}()

		// Wait for the emitted approval_request to learn the generated id.
		ev := <-h.Events()
		data, ok := ev.Data.(WebApprovalRequestData)
		if !ok {
			t.Fatalf("expected WebApprovalRequestData, got %T", ev.Data)
		}
		if err := h.ResolveApproval(data.ID, true, approveAll); err != nil {
			t.Fatalf("ResolveApproval(%q, true, %v): %v", data.ID, approveAll, err)
		}
		return <-respCh
	}

	if r := run(false); !r.Approved || r.Mode != ModeManual {
		t.Errorf("approve once: expected {Approved:true, Mode:Manual}, got %+v", r)
	}
	if r := run(true); !r.Approved || r.Mode != ModeAuto {
		t.Errorf("approve all: expected {Approved:true, Mode:Auto}, got %+v", r)
	}
}
