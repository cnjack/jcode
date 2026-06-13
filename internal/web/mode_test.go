package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/runner"
)

// newModeTestServer builds a minimal Server exercising only handleSwitchMode,
// recording the planMode flag every agent rebuild is asked for.
func newModeTestServer(rebuilt *[]bool) *Server {
	return &Server{
		broker:        NewSSEBroker(),
		wsBroker:      NewWSBroker(),
		approvalState: runner.NewApprovalStateWithMode("/tmp", mode.Ask),
		mode:          "ask",
		rebuildForMode: func(planMode bool) (*adk.ChatModelAgent, error) {
			*rebuilt = append(*rebuilt, planMode)
			return nil, nil
		},
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
		{`{"mode":"autopilot"}`, 200, "autopilot", mode.Autopilot, handler.ModeAuto, false},
		{`{"mode":"ask"}`, 200, "ask", mode.Ask, handler.ModeManual, false},
		{`{"mode":"build"}`, 200, "ask", mode.Ask, handler.ModeManual, false}, // legacy alias
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
