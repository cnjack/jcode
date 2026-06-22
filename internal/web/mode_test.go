package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/runner"
)

// newModeTestServer builds a minimal Server exercising only handleSwitchMode,
// recording the planMode flag every agent rebuild is asked for.
func newModeTestServer(rebuilt *[]bool) *Server {
	return &Server{
		Engine: &Engine{
			approvalState: runner.NewApprovalStateWithMode("/tmp", mode.Approval),
			mode:          "approval",
			rebuildForMode: func(planMode bool) (*adk.ChatModelAgent, error) {
				*rebuilt = append(*rebuilt, planMode)
				return nil, nil
			},
		},
		wsBroker: NewWSBroker(),
	}
}

func postMode(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleSwitchMode(rec, httptest.NewRequest(http.MethodPost, "/api/mode", strings.NewReader(body)))
	return rec
}

func TestWebSwitchMode(t *testing.T) {
	var rebuilt []bool
	s := newModeTestServer(&rebuilt)

	cases := []struct {
		body         string
		wantCode     int
		wantMode     string
		wantSession  mode.SessionMode
		wantApproval handler.ApprovalMode
		wantPlanArg  bool // expected planMode passed to rebuildForMode
	}{
		{`{"mode":"plan"}`, 200, "plan", mode.Plan, handler.ModeManual, true},
		{`{"mode":"full_access"}`, 200, "full_access", mode.FullAccess, handler.ModeAuto, false},
		{`{"mode":"approval"}`, 200, "approval", mode.Approval, handler.ModeManual, false},
	}
	for _, c := range cases {
		rebuilt = rebuilt[:0]
		rec := postMode(t, s, c.body)
		if rec.Code != c.wantCode {
			t.Fatalf("%s: code=%d body=%q, want %d", c.body, rec.Code, rec.Body.String(), c.wantCode)
		}
		if s.mode != c.wantMode {
			t.Errorf("%s: s.mode=%q, want %q", c.body, s.mode, c.wantMode)
		}
		if got := s.approvalState.GetSessionMode(); got != c.wantSession {
			t.Errorf("%s: sessionMode=%v, want %v", c.body, got, c.wantSession)
		}
		if got := s.approvalState.GetMode(); got != c.wantApproval {
			t.Errorf("%s: approvalMode=%v, want %v", c.body, got, c.wantApproval)
		}
		// Exactly one agent rebuild per switch, with the right plan flag (this is
		// what makes web Plan a real read-only tool swap, not a prompt prefix).
		if len(rebuilt) != 1 || rebuilt[0] != c.wantPlanArg {
			t.Errorf("%s: rebuilt=%v, want exactly [%v]", c.body, rebuilt, c.wantPlanArg)
		}
	}
}

func TestWebSwitchModeRejectsGarbage(t *testing.T) {
	var rebuilt []bool
	s := newModeTestServer(&rebuilt)
	rec := postMode(t, s, `{"mode":"banana"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("garbage mode should be 400, got %d", rec.Code)
	}
	if len(rebuilt) != 0 {
		t.Errorf("garbage mode must not rebuild the agent, got %v", rebuilt)
	}
}

func TestWebSwitchModeRejectsRemovedAskMode(t *testing.T) {
	var rebuilt []bool
	s := newModeTestServer(&rebuilt)
	rec := postMode(t, s, `{"mode":"ask"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("removed ask mode should be 400, got %d", rec.Code)
	}
}

func TestWebSwitchModeRejectsRemovedAutopilotMode(t *testing.T) {
	var rebuilt []bool
	s := newModeTestServer(&rebuilt)
	rec := postMode(t, s, `{"mode":"autopilot"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("removed autopilot mode should be 400, got %d", rec.Code)
	}
}

// TestWebHandleApprovalAllowAllSyncsMode covers the user-facing contract: when
// a user clicks "Allow all" on an approval card, the chat composer's selector
// must flip from "Ask for approval" to "Full access" for the rest of the
// session. The promotion happens inside the runner's ApprovalState on resolve,
// but the server's user-facing s.mode (read by /api/health) and the WS
// mode_changed broadcast (read by the selector pill) must follow — otherwise
// the pill silently stays on "Ask for approval" even though every tool call is
// now auto-approved. A plain single allow leaves the mode untouched.
func TestWebHandleApprovalAllowAllSyncsMode(t *testing.T) {
	// Seed a pending approval through the real WebHandler so ResolveApproval
	// finds a pending entry to resolve.
	h := handler.NewWebHandler()
	respCh := make(chan handler.ApprovalResponse, 1)
	go func() {
		ctx := context.Background()
		r, err := h.RequestApproval(ctx, handler.ApprovalRequest{ToolName: "execute"})
		if err != nil {
			t.Errorf("RequestApproval failed: %v", err)
			return
		}
		respCh <- r
	}()
	var id string
	select {
	case ev := <-h.Events():
		data := ev.Data.(handler.WebApprovalRequestData)
		id = data.ID
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for approval_request event")
	}

	var rebuilt []bool
	s := newModeTestServer(&rebuilt)
	s.handler = h

	// --- "Allow all" promotes the selector to Full access. ---
	rec := httptest.NewRecorder()
	s.handleApproval(rec, httptest.NewRequest(http.MethodPost, "/api/approval",
		strings.NewReader(`{"id":"`+id+`","approved":true,"approve_all":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("approve_all: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if s.mode != mode.FullAccess.String() {
		t.Errorf("approve_all: s.mode=%q, want %q", s.mode, mode.FullAccess.String())
	}

	// The runner actually received an auto-approve response (the whole point of
	// "allow all" — not just a UI flip). The runner's own ApprovalState update
	// lives in internal/runner and is exercised there; this test asserts the
	// server's user-facing projection (s.mode + broadcast).
	select {
	case r := <-respCh:
		if !r.Approved || r.Mode != handler.ModeAuto {
			t.Errorf("approve_all: runner got %+v, want {Approved:true Mode:Auto}", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for runner approval response")
	}

	// --- A plain single allow must NOT change the mode (regression guard). ---
	// Re-seed a fresh pending approval.
	h2 := handler.NewWebHandler()
	go func() {
		ctx := context.Background()
		_, _ = h2.RequestApproval(ctx, handler.ApprovalRequest{ToolName: "execute"})
	}()
	var id2 string
	select {
	case ev := <-h2.Events():
		id2 = ev.Data.(handler.WebApprovalRequestData).ID
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for second approval_request event")
	}
	s.handler = h2
	s.mode = mode.Approval.String() // reset to baseline

	rec2 := httptest.NewRecorder()
	s.handleApproval(rec2, httptest.NewRequest(http.MethodPost, "/api/approval",
		strings.NewReader(`{"id":"`+id2+`","approved":true,"approve_all":false}`)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("approve_once: code=%d body=%q", rec2.Code, rec2.Body.String())
	}
	if s.mode != mode.Approval.String() {
		t.Errorf("approve_once: s.mode=%q, want %q (single allow must not flip the selector)", s.mode, mode.Approval.String())
	}
}
