package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	acp "github.com/coder/acp-go-sdk"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/mode"
	"github.com/cnjack/jcode/internal/runner"
	"github.com/cnjack/jcode/internal/session"
)

type modeLifecycleACPClient struct {
	mu                  sync.Mutex
	goodHome            string
	badHome             string
	permissionCalls     int
	terminalBeforeRetry bool
	pendingRetrySeen    bool
	updates             []acp.SessionNotification
	terminalCh          chan struct{}
	pendingRetryCh      chan struct{}
}

func (c *modeLifecycleACPClient) ReadTextFile(
	context.Context, acp.ReadTextFileRequest,
) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, nil
}

func (c *modeLifecycleACPClient) WriteTextFile(
	context.Context, acp.WriteTextFileRequest,
) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, nil
}

func (c *modeLifecycleACPClient) RequestPermission(
	ctx context.Context,
	params acp.RequestPermissionRequest,
) (acp.RequestPermissionResponse, error) {
	c.mu.Lock()
	c.permissionCalls++
	call := c.permissionCalls
	c.mu.Unlock()
	if call == 2 {
		// ACP dispatches requests and notifications on separate goroutines. Wait
		// for the retry notification explicitly instead of assuming its handler
		// completed before this second RequestPermission handler started.
		select {
		case <-c.pendingRetryCh:
		case <-ctx.Done():
			return acp.RequestPermissionResponse{}, ctx.Err()
		}
	}

	if call == 1 {
		if err := os.Setenv("HOME", c.badHome); err != nil {
			return acp.RequestPermissionResponse{}, err
		}
	} else {
		if err := os.Setenv("HOME", c.goodHome); err != nil {
			return acp.RequestPermissionResponse{}, err
		}
	}
	for _, option := range params.Options {
		if option.Kind == acp.PermissionOptionKindAllowAlways {
			return acp.RequestPermissionResponse{
				Outcome: acp.RequestPermissionOutcome{
					Selected: &acp.RequestPermissionOutcomeSelected{OptionId: option.OptionId},
				},
			}, nil
		}
	}
	return acp.RequestPermissionResponse{}, fmt.Errorf("Allow All option was not offered")
}

func (c *modeLifecycleACPClient) SessionUpdate(
	_ context.Context,
	params acp.SessionNotification,
) error {
	c.mu.Lock()
	c.updates = append(c.updates, params)
	terminal := false
	retryPending := false
	if update := params.Update.ToolCallUpdate; update != nil && update.Status != nil {
		terminal = *update.Status == acp.ToolCallStatusCompleted ||
			*update.Status == acp.ToolCallStatusFailed
		if terminal && c.permissionCalls < 2 {
			c.terminalBeforeRetry = true
		}
		if *update.Status == acp.ToolCallStatusPending && containsModePromotionRetry(update.Content) {
			c.pendingRetrySeen = true
			retryPending = true
		}
	}
	c.mu.Unlock()
	if retryPending {
		select {
		case c.pendingRetryCh <- struct{}{}:
		default:
		}
	}
	if terminal {
		select {
		case c.terminalCh <- struct{}{}:
		default:
		}
	}
	return nil
}

func containsModePromotionRetry(content []acp.ToolCallContent) bool {
	for _, item := range content {
		if item.Content != nil && item.Content.Content.Text != nil &&
			strings.Contains(item.Content.Content.Text.Text, "Full access could not be saved") {
			return true
		}
	}
	return false
}

func (c *modeLifecycleACPClient) CreateTerminal(
	context.Context, acp.CreateTerminalRequest,
) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, nil
}

func (c *modeLifecycleACPClient) KillTerminal(
	context.Context, acp.KillTerminalRequest,
) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, nil
}

func (c *modeLifecycleACPClient) TerminalOutput(
	context.Context, acp.TerminalOutputRequest,
) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, nil
}

func (c *modeLifecycleACPClient) ReleaseTerminal(
	context.Context, acp.ReleaseTerminalRequest,
) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, nil
}

func (c *modeLifecycleACPClient) WaitForTerminalExit(
	context.Context, acp.WaitForTerminalExitRequest,
) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, nil
}

func newACPModeFixture(t *testing.T) (*acpAgent, *acpSession, acp.SessionId, *adk.ChatModelAgent) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorder, err := session.NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(recorder.Close)
	oldAgent := &adk.ChatModelAgent{}
	candidate := &adk.ChatModelAgent{}
	sess := &acpSession{
		ag: oldAgent, rec: recorder,
		approvalState: runner.NewApprovalStateWithMode(t.TempDir(), mode.Approval),
		mode:          acpModeApproval,
		createAgent: func(string, []tool.BaseTool, bool) (*adk.ChatModelAgent, error) {
			return candidate, nil
		},
	}
	sessionID := acp.SessionId("sess_" + recorder.UUID())
	agent := &acpAgent{sessions: map[acp.SessionId]*acpSession{sessionID: sess}}
	return agent, sess, sessionID, candidate
}

func TestACPModeSwitchJournalFailureDoesNotPublish(t *testing.T) {
	agent, sess, sessionID, candidate := newACPModeFixture(t)
	oldAgent := sess.ag
	originalHome := os.Getenv("HOME")
	badHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(badHome, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("HOME", badHome); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("HOME", originalHome) })

	_, err := agent.SetSessionMode(context.Background(), acp.SetSessionModeRequest{
		SessionId: sessionID,
		ModeId:    acpModeFullAccess,
	})
	if err == nil || !strings.Contains(err.Error(), "safely") {
		t.Fatalf("mode switch error=%v", err)
	}
	if sess.ag != oldAgent || sess.ag == candidate {
		t.Fatal("candidate agent was published after journal failure")
	}
	if sess.mode != acpModeApproval || sess.approvalState.GetSessionMode() != mode.Approval ||
		sess.approvalState.GetMode() != handler.ModeManual {
		t.Fatalf("failed switch published mode=%s session=%v approval=%v",
			sess.mode, sess.approvalState.GetSessionMode(), sess.approvalState.GetMode())
	}
}

func TestACPAllowAllTransactionWritesModeBeforePublish(t *testing.T) {
	_, sess, _, candidate := newACPModeFixture(t)
	sess.mu.Lock()
	err := sess.commitModeLocked(mode.FullAccess)
	sess.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := session.LoadSessionModeStrict(sess.rec.UUID()); err != nil || got != mode.FullAccess.String() {
		t.Fatalf("durable mode=%q err=%v", got, err)
	}
	if sess.ag != candidate || sess.mode != acpModeFullAccess ||
		sess.approvalState.GetSessionMode() != mode.FullAccess ||
		sess.approvalState.GetMode() != handler.ModeAuto {
		t.Fatalf("published agent=%p mode=%s session=%v approval=%v",
			sess.ag, sess.mode, sess.approvalState.GetSessionMode(), sess.approvalState.GetMode())
	}
}

func TestACPAllowAllJournalFailureStaysPendingAndRetriesLifecycle(t *testing.T) {
	_, sess, sessionID, _ := newACPModeFixture(t)
	goodHome := os.Getenv("HOME")
	badHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(badHome, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	c2aR, c2aW := io.Pipe()
	a2cR, a2cW := io.Pipe()
	t.Cleanup(func() {
		_ = c2aR.Close()
		_ = c2aW.Close()
		_ = a2cR.Close()
		_ = a2cW.Close()
	})
	client := &modeLifecycleACPClient{
		goodHome: goodHome, badHome: badHome,
		terminalCh: make(chan struct{}, 4), pendingRetryCh: make(chan struct{}, 1),
	}
	_ = acp.NewClientSideConnection(client, c2aW, a2cR)
	wireAgent := &acpAgent{sessions: make(map[acp.SessionId]*acpSession)}
	agentConn := acp.NewAgentSideConnection(wireAgent, a2cW, c2aR)
	wireAgent.conn = agentConn

	h := handler.NewACPHandler(agentConn, sessionID, t.TempDir())
	h.SetModeChangeCallback(func(target mode.SessionMode) error {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return sess.commitModeLocked(target)
	})
	const toolCallID = "eino-mode-lifecycle"
	const toolArgs = `{"command":"true"}`
	h.OnToolCall(handler.ToolCallEvent{
		Name: "execute", Args: toolArgs, ToolCallID: toolCallID,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := h.RequestApproval(ctx, handler.ApprovalRequest{
		ToolName: "execute", ToolArgs: toolArgs, ToolCallID: toolCallID,
		AllowApproveAll: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Approved || response.Mode != handler.ModeAuto {
		t.Fatalf("approval response=%+v", response)
	}
	client.mu.Lock()
	permissionCalls := client.permissionCalls
	terminalBeforeRetry := client.terminalBeforeRetry
	pendingRetrySeen := client.pendingRetrySeen
	client.mu.Unlock()
	if permissionCalls != 2 {
		t.Fatalf("permission prompts=%d, want failed Allow All plus retry", permissionCalls)
	}
	if terminalBeforeRetry {
		t.Fatal("journal failure emitted a terminal ACP tool status before retry")
	}
	if !pendingRetrySeen {
		t.Fatal("journal failure did not retain and explain the pending tool call")
	}
	if got, loadErr := session.LoadSessionModeStrict(sess.rec.UUID()); loadErr != nil ||
		got != mode.FullAccess.String() {
		t.Fatalf("durable mode after retry=%q err=%v", got, loadErr)
	}

	h.OnToolResult(handler.ToolResultEvent{
		Name: "execute", Output: "ok", ToolCallID: toolCallID,
	})
	select {
	case <-client.terminalCh:
	case <-time.After(2 * time.Second):
		t.Fatal("successful retried tool did not emit its final terminal status")
	}
}
