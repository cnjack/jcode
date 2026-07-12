package handler

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockHandler is a test double for AgentEventHandler.
type mockHandler struct {
	mu           sync.Mutex
	texts        []string
	toolCalls    []string
	doneErr      error
	doneCalled   bool
	approvalResp ApprovalResponse
	approvalErr  error
	approvalCh   chan struct{} // signal when RequestApproval is called

	// block approval until released
	approvalGate chan struct{}
}

func newMockHandler() *mockHandler {
	return &mockHandler{
		approvalResp: ApprovalResponse{Approved: true, Mode: ModeManual},
		approvalCh:   make(chan struct{}, 1),
		approvalGate: make(chan struct{}),
	}
}

func (m *mockHandler) OnAgentText(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.texts = append(m.texts, text)
}

func (m *mockHandler) OnToolCall(ev ToolCallEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolCalls = append(m.toolCalls, ev.Name)
}

func (m *mockHandler) OnToolResult(ev ToolResultEvent) {}
func (m *mockHandler) OnTodoUpdate()                   {}

func (m *mockHandler) OnAgentStart() {}

func (m *mockHandler) OnAgentDone(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.doneCalled = true
	m.doneErr = err
}

func (m *mockHandler) OnTokenUpdate(info TokenUsage) {}

func (m *mockHandler) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	// Signal that approval was requested
	select {
	case m.approvalCh <- struct{}{}:
	default:
	}
	// Wait for the gate to open (simulates user thinking)
	select {
	case <-m.approvalGate:
	case <-ctx.Done():
		return ApprovalResponse{}, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.approvalResp, m.approvalErr
}

func TestNotifyingHandler_DoneNotification(t *testing.T) {
	inner := newMockHandler()
	close(inner.approvalGate) // don't block
	h := NewNotifyingHandler(inner, 10*time.Second)

	var gotSummary string
	var gotErr error
	h.SetDoneNotifier(func(summary string, err error) {
		gotSummary = summary
		gotErr = err
	})

	h.OnAgentText("hello ")
	h.OnAgentText("world")
	h.OnAgentDone(nil)

	if !inner.doneCalled {
		t.Fatal("inner.OnAgentDone not called")
	}
	if gotSummary != "hello world" {
		t.Fatalf("expected summary 'hello world', got %q", gotSummary)
	}
	if gotErr != nil {
		t.Fatalf("expected nil error, got %v", gotErr)
	}
}

func TestNotifyingHandler_DoneWithError(t *testing.T) {
	inner := newMockHandler()
	close(inner.approvalGate)
	h := NewNotifyingHandler(inner, 10*time.Second)

	var gotErr error
	h.SetDoneNotifier(func(summary string, err error) {
		gotErr = err
	})

	testErr := context.DeadlineExceeded
	h.OnAgentDone(testErr)

	if gotErr != testErr {
		t.Fatalf("expected %v, got %v", testErr, gotErr)
	}
}

func TestNotifyingHandler_ApprovalNotificationFires(t *testing.T) {
	inner := newMockHandler()
	h := NewNotifyingHandler(inner, 50*time.Millisecond)

	var notifiedTool string
	var notifyMu sync.Mutex
	h.SetApprovalNotifier(func(toolName, toolArgs string) {
		notifyMu.Lock()
		notifiedTool = toolName
		notifyMu.Unlock()
	})

	// Start approval in background (will block on inner.approvalGate)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = h.RequestApproval(context.Background(), ApprovalRequest{
			ToolName: "execute",
			ToolArgs: `{"command":"rm -rf /"}`,
		})
	}()

	// Wait for the notification to fire (50ms delay + buffer)
	time.Sleep(150 * time.Millisecond)

	notifyMu.Lock()
	if notifiedTool != "execute" {
		t.Fatalf("expected notification for 'execute', got %q", notifiedTool)
	}
	notifyMu.Unlock()

	// Release the approval
	close(inner.approvalGate)
	<-done
}

func TestNotifyingHandler_ApprovalResolvedBeforeNotification(t *testing.T) {
	inner := newMockHandler()
	h := NewNotifyingHandler(inner, 5*time.Second) // long delay

	var notified bool
	var notifyMu sync.Mutex
	h.SetApprovalNotifier(func(toolName, toolArgs string) {
		notifyMu.Lock()
		notified = true
		notifyMu.Unlock()
	})

	// Immediately release approval
	close(inner.approvalGate)

	resp, err := h.RequestApproval(context.Background(), ApprovalRequest{
		ToolName: "edit",
		ToolArgs: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Approved {
		t.Fatal("expected approved")
	}

	// Wait a bit to make sure notification didn't fire
	time.Sleep(100 * time.Millisecond)

	notifyMu.Lock()
	if notified {
		t.Fatal("notification should NOT have fired since approval resolved quickly")
	}
	notifyMu.Unlock()
}

func TestNotifyingHandler_TextSummaryCapping(t *testing.T) {
	inner := newMockHandler()
	close(inner.approvalGate)
	h := NewNotifyingHandler(inner, 10*time.Second)

	var gotLen int
	h.SetDoneNotifier(func(summary string, err error) {
		gotLen = len(summary)
	})

	// Send more than 600 chars
	bigText := string(make([]byte, 1000))
	for i := range bigText {
		bigText = bigText[:i] + "x" + bigText[i+1:]
	}
	h.OnAgentText(bigText)
	h.OnAgentDone(nil)

	if gotLen != 600 {
		t.Fatalf("expected summary capped to 600 chars, got %d", gotLen)
	}
}

func TestNotifyingHandler_Passthrough(t *testing.T) {
	inner := newMockHandler()
	close(inner.approvalGate)
	h := NewNotifyingHandler(inner, 10*time.Second)

	h.OnAgentText("test")
	h.OnToolCall(ToolCallEvent{Name: "edit", Args: "{}", ToolCallID: "call_1"})
	h.OnAgentDone(nil)

	inner.mu.Lock()
	defer inner.mu.Unlock()

	if len(inner.texts) != 1 || inner.texts[0] != "test" {
		t.Fatalf("text not passed through: %v", inner.texts)
	}
	if len(inner.toolCalls) != 1 || inner.toolCalls[0] != "edit" {
		t.Fatalf("tool call not passed through: %v", inner.toolCalls)
	}
	if !inner.doneCalled {
		t.Fatal("done not passed through")
	}
}
