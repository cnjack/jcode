package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/session"
)

// newModeTestServer builds a minimal Server exercising only handleSwitchMode,
// recording the planMode flag every agent rebuild is asked for.
func newModeTestServer(t *testing.T, rebuilt *[]bool) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	recorder, err := session.NewRecorder(t.TempDir(), "provider-a", "model-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recorder.Close)
	eng := &Engine{
		taskID:        recorder.UUID(),
		recorder:      recorder,
		handler:       handler.NewWebHandler(),
		approvalState: runner.NewApprovalStateWithMode("/tmp", mode.Approval),
		mode:          "approval",
		rebuildForMode: func(planMode bool) (*adk.ChatModelAgent, error) {
			*rebuilt = append(*rebuilt, planMode)
			return nil, nil
		},
	}
	s := &Server{Engine: eng, wsBroker: NewWSBroker()}
	eng.handler.SetModePromotionCallback(func() error {
		return s.syncModeAfterApproval(eng, true, true)
	})
	return s
}

func postMode(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleSwitchMode(rec, httptest.NewRequest(http.MethodPost, "/api/mode", strings.NewReader(body)))
	return rec
}

func TestWebSwitchMode(t *testing.T) {
	var rebuilt []bool
	s := newModeTestServer(t, &rebuilt)

	cases := []struct {
		body         string
		wantCode     int
		wantMode     string
		wantSession  mode.SessionMode
		wantApproval handler.ApprovalMode
		wantPlanArg  bool // expected planMode passed to rebuildForMode
	}{
		{`{"mode":"plan"}`, 200, "plan", mode.Plan, handler.ModeManual, true},
		{`{"mode":"auto"}`, 200, "auto", mode.Auto, handler.ModeManual, false},
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
	entries, err := session.LoadSession(s.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got := session.ReconstructState(entries).Mode; got != mode.Approval.String() {
		t.Fatalf("durable mode = %q, want %q", got, mode.Approval.String())
	}
}

func TestWebSwitchModeRejectsGarbage(t *testing.T) {
	var rebuilt []bool
	s := newModeTestServer(t, &rebuilt)
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
	s := newModeTestServer(t, &rebuilt)
	rec := postMode(t, s, `{"mode":"ask"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("removed ask mode should be 400, got %d", rec.Code)
	}
}

func TestWebSwitchModeRejectsRemovedAutopilotMode(t *testing.T) {
	var rebuilt []bool
	s := newModeTestServer(t, &rebuilt)
	rec := postMode(t, s, `{"mode":"autopilot"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("removed autopilot mode should be 400, got %d", rec.Code)
	}
}

func TestWebSwitchModeSameValueIsNoOp(t *testing.T) {
	var rebuilt []bool
	s := newModeTestServer(t, &rebuilt)
	rec := postMode(t, s, `{"mode":"approval"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("same mode: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if len(rebuilt) != 0 {
		t.Fatalf("same mode must not rebuild the agent, got %v", rebuilt)
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
	s := newModeTestServer(t, &rebuilt)
	s.handler = h
	h.SetModePromotionCallback(func() error {
		return s.syncModeAfterApproval(s.Engine, true, true)
	})

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

func TestApproveAllWaitsForAgentRebuildAndPreservesRefreshedAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := session.NewRecorder(t.TempDir(), "provider-a", "model-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recorder.Close)
	oldAgent := new(adk.ChatModelAgent)
	refreshedAgent := new(adk.ChatModelAgent)
	rebuildStarted := make(chan struct{})
	releaseRebuild := make(chan struct{})
	eng := &Engine{
		taskID:       "active",
		recorder:     recorder,
		agent:        oldAgent,
		mode:         mode.Approval.String(),
		providerName: "provider-a",
		modelName:    "model-a",
		createAgent: func(_, _ string) (*adk.ChatModelAgent, error) {
			close(rebuildStarted)
			<-releaseRebuild
			return refreshedAgent, nil
		},
	}
	s := &Server{Engine: eng, wsBroker: NewWSBroker()}

	rebuildDone := make(chan error, 1)
	go func() { rebuildDone <- s.rebuildToolAgents() }()
	<-rebuildStarted

	approveStarted := make(chan struct{})
	approveDone := make(chan error, 1)
	go func() {
		close(approveStarted)
		approveDone <- s.syncModeAfterApproval(eng, true, true)
	}()
	<-approveStarted

	select {
	case <-approveDone:
		t.Fatal("approve-all completed while an agent rebuild held rebuildMu")
	case <-time.After(100 * time.Millisecond):
	}
	eng.emu.Lock()
	agentBefore, modeBefore, revisionBefore := eng.agent, eng.mode, eng.agentRevision
	eng.emu.Unlock()
	if agentBefore != oldAgent || modeBefore != mode.Approval.String() || revisionBefore != 0 {
		t.Fatalf("approve-all changed engine before rebuild completed: agent=%p mode=%q revision=%d",
			agentBefore, modeBefore, revisionBefore)
	}

	close(releaseRebuild)
	if err := <-rebuildDone; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-approveDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approve-all did not resume after the agent rebuild completed")
	}

	eng.emu.Lock()
	defer eng.emu.Unlock()
	if eng.agent != refreshedAgent || eng.mode != mode.FullAccess.String() || eng.agentRevision != 2 {
		t.Fatalf("final engine agent=%p mode=%q revision=%d, want refreshed agent=%p mode=%q revision=2",
			eng.agent, eng.mode, eng.agentRevision, refreshedAgent, mode.FullAccess.String())
	}
}

func TestWebSwitchModeJournalFailureDoesNotPublishElevation(t *testing.T) {
	var rebuilt []bool
	s := newModeTestServer(t, &rebuilt)
	badHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(badHome, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", badHome)

	rec := postMode(t, s, `{"mode":"full_access"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d body=%q, want 500", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), badHome) || !strings.Contains(rec.Body.String(), "failed to persist mode change") {
		t.Fatalf("response leaked storage detail or lost safe error: %q", rec.Body.String())
	}
	if got := s.curMode(); got != mode.Approval.String() {
		t.Fatalf("engine mode=%q after failed commit, want approval", got)
	}
	if got := s.approvalState.GetSessionMode(); got != mode.Approval {
		t.Fatalf("approval state=%v after failed commit, want approval", got)
	}
	if len(rebuilt) != 1 || rebuilt[0] {
		t.Fatalf("candidate rebuild=%v, want one unpublished non-plan agent", rebuilt)
	}
}

func TestWebAllowAllJournalFailureDoesNotReachRunnerOrPromote(t *testing.T) {
	var rebuilt []bool
	s := newModeTestServer(t, &rebuilt)
	h := handler.NewWebHandler()
	s.handler = h
	h.SetModePromotionCallback(func() error {
		return s.syncModeAfterApproval(s.Engine, true, true)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	response := make(chan handler.ApprovalResponse, 1)
	requestErr := make(chan error, 1)
	go func() {
		got, err := h.RequestApproval(ctx, handler.ApprovalRequest{ToolName: "execute"})
		if err != nil {
			requestErr <- err
			return
		}
		response <- got
	}()
	var approvalID string
	select {
	case event := <-h.Events():
		approvalID = event.Data.(handler.WebApprovalRequestData).ID
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval request")
	}

	badHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(badHome, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", badHome)
	rec := httptest.NewRecorder()
	s.handleApproval(rec, httptest.NewRequest(http.MethodPost, "/api/approval", strings.NewReader(
		`{"id":"`+approvalID+`","approved":true,"approve_all":true}`,
	)))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "failed to persist mode change") {
		t.Fatalf("code=%d body=%q, want opaque persistence error", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), badHome) {
		t.Fatalf("response leaked storage path: %q", rec.Body.String())
	}
	select {
	case got := <-response:
		t.Fatalf("runner received an unpersisted allow-all response: %+v", got)
	case err := <-requestErr:
		t.Fatalf("approval request unexpectedly ended before cancellation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if got := s.curMode(); got != mode.Approval.String() {
		t.Fatalf("engine mode=%q after failed allow-all commit", got)
	}
	if got := s.approvalState.GetMode(); got != handler.ModeManual {
		t.Fatalf("approval axis=%v after failed allow-all commit", got)
	}
}

func TestWebResumeUsesTaskModeInsteadOfActiveFullAccess(t *testing.T) {
	s := stubFactoryServer(t)
	project := t.TempDir()
	approvalID := recordModeTestSession(t, project, mode.Approval)
	fullAccessID := recordModeTestSession(t, project, mode.FullAccess)
	planID := recordModeTestSession(t, project, mode.Plan)
	corruptID := recordModeTestSession(t, project, mode.FullAccess)
	sessionsDir, err := config.SessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	corruptFile, err := os.OpenFile(filepath.Join(sessionsDir, corruptID+".json"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := corruptFile.WriteString(`{"type":"mode_change"` + "\n"); err != nil {
		_ = corruptFile.Close()
		t.Fatal(err)
	}
	if err := corruptFile.Close(); err != nil {
		t.Fatal(err)
	}

	originalFactory := s.newEngine
	builtModes := make(map[string]string)
	s.newEngine = func(taskID, pwd, modeStr string) (*EngineConfig, error) {
		builtModes[taskID] = modeStr
		cfg, err := originalFactory(taskID, pwd, modeStr)
		if err != nil {
			return nil, err
		}
		cfg.ApprovalState = runner.NewApprovalStateWithMode(pwd, mode.Parse(modeStr))
		return cfg, nil
	}

	active, err := s.buildLocalEngine(fullAccessID, project, mode.FullAccess.String())
	if err != nil {
		t.Fatal(err)
	}
	s.setActiveEngine(active)

	resume := func(sessionID string) *Engine {
		t.Helper()
		rec := httptest.NewRecorder()
		body := `{"session_id":"` + sessionID + `","pwd":"` + project + `"}`
		s.handleNewSession(rec, httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("resume %s: code=%d body=%q", sessionID, rec.Code, rec.Body.String())
		}
		eng := s.activeEngine()
		if eng == nil || eng.taskID != sessionID {
			t.Fatalf("resume %s focused engine %#v", sessionID, eng)
		}
		return eng
	}

	approvalEngine := resume(approvalID)
	if got := builtModes[approvalID]; got != mode.Approval.String() {
		t.Fatalf("approval task factory mode=%q, inherited active Full access", got)
	}
	if got := approvalEngine.curMode(); got != mode.Approval.String() {
		t.Fatalf("approval task engine mode=%q", got)
	}
	if got := approvalEngine.approvalState.GetMode(); got != handler.ModeManual {
		t.Fatalf("approval task approval axis=%v, want manual", got)
	}
	if got := active.curMode(); got != mode.FullAccess.String() {
		t.Fatalf("restoring task A mutated task B mode to %q", got)
	}

	planEngine := resume(planID)
	if got := builtModes[planID]; got != mode.Approval.String() {
		t.Fatalf("saved Plan factory mode=%q, want normalized approval", got)
	}
	if got := planEngine.curMode(); got != mode.Approval.String() {
		t.Fatalf("saved Plan engine mode=%q, want approval", got)
	}

	corruptEngine := resume(corruptID)
	if got := builtModes[corruptID]; got != mode.Approval.String() {
		t.Fatalf("corrupt mode journal factory mode=%q, want fail-closed approval", got)
	}
	if got := corruptEngine.approvalState.GetMode(); got != handler.ModeManual {
		t.Fatalf("corrupt mode journal approval axis=%v, want manual", got)
	}
}

func recordModeTestSession(t *testing.T, project string, sessionMode mode.SessionMode) string {
	t.Helper()
	recorder, err := session.NewRecorder(project, "provider-a", "model-a")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	if err := recorder.RecordModeChangeStrict(sessionMode.String()); err != nil {
		t.Fatal(err)
	}
	return recorder.UUID()
}
