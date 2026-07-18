package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/session"
)

// blockingTool simulates a long-running tool: it blocks until its context is
// canceled (user stop) and then returns the context error.
type blockingTool struct {
	info *schema.ToolInfo
}

func newBlockingTool() *blockingTool {
	return &blockingTool{info: &schema.ToolInfo{
		Name: "block",
		Desc: "blocks until the run is cancelled",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"note": {Type: schema.String, Desc: "ignored"},
		}),
	}}
}

func (bt *blockingTool) Info(context.Context) (*schema.ToolInfo, error) { return bt.info, nil }

func (bt *blockingTool) InvokableRun(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

// cancelModel streams one assistant message carrying a call to the blocking
// tool; if the run ever continues past the tool it answers with plain text.
type cancelModel struct{}

func (m *cancelModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func (m *cancelModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return nil, errors.New("Generate is not used: streaming is enabled")
}

func (m *cancelModel) Stream(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	if last := input[len(input)-1]; last.Role == schema.Tool {
		return schema.StreamReaderFromArray([]*schema.Message{
			{Role: schema.Assistant, Content: "done"},
		}), nil
	}
	return schema.StreamReaderFromArray([]*schema.Message{{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{{
			ID:       "call-block-1",
			Function: schema.FunctionCall{Name: "block", Arguments: "{}"},
		}},
	}}), nil
}

// cancelRecordingHandler cancels the run as soon as the tool call is
// announced (the user hitting Stop) and records every tool result.
type cancelRecordingHandler struct {
	stubHandler
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	results []handler.ToolResultEvent
	doneErr error
}

func (h *cancelRecordingHandler) OnToolCall(handler.ToolCallEvent) { h.cancel() }

func (h *cancelRecordingHandler) OnToolResult(ev handler.ToolResultEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.results = append(h.results, ev)
}

func (h *cancelRecordingHandler) OnAgentDone(err error) {
	h.mu.Lock()
	h.doneErr = err
	h.mu.Unlock()
	close(h.done)
}

// TestRunInnerCancellationBackfillsToolResult stops the run while a tool is
// still executing. The announced tool call must not be left dangling: exactly
// one result reaches the handler, and the persisted session reconstructs into
// a history that satisfies the model API's tool-call/tool-message invariant
// (previously a resume of such a session failed with "assistant message with
// 'tool_calls' must be followed by tool messages...").
func TestRunInnerCancellationBackfillsToolResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ag, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "cancel-test",
		Description: "cancel-test",
		Instruction: "test",
		Model:       &cancelModel{},
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{newBlockingTool()},
			},
		},
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	rec, err := session.NewRecorder("cancel-test", "test", "test")
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	h := &cancelRecordingHandler{cancel: cancel, done: make(chan struct{})}

	finished := make(chan bool, 1)
	go func() {
		_, done := runInner(ctx, ag, []adk.Message{schema.UserMessage("go")}, h, rec)
		finished <- done
	}()

	select {
	case done := <-finished:
		if !done {
			t.Errorf("runInner done = false, want true after cancellation")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runInner did not return after cancellation")
	}

	select {
	case <-h.done:
	case <-time.After(time.Second):
		t.Fatal("OnAgentDone was not called")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !errors.Is(h.doneErr, context.Canceled) {
		t.Errorf("OnAgentDone err = %v, want context.Canceled", h.doneErr)
	}

	// The announced call got exactly one result (drain backfill or a folded
	// framework result — either satisfies the invariant).
	if len(h.results) != 1 {
		t.Fatalf("tool results = %d, want exactly 1", len(h.results))
	}
	if h.results[0].ToolCallID != "call-block-1" {
		t.Errorf("result ToolCallID = %q, want call-block-1", h.results[0].ToolCallID)
	}

	// The session on disk pairs the recorded call with a recorded result.
	entries, err := session.LoadSession(rec.UUID())
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	var calls, results int
	for _, e := range entries {
		switch e.Type {
		case session.EntryToolCall:
			calls++
		case session.EntryToolResult:
			results++
			if e.ToolCallID != "call-block-1" {
				t.Errorf("recorded result for %q, want call-block-1", e.ToolCallID)
			}
		}
	}
	if calls != 1 || results != 1 {
		t.Errorf("recorded calls=%d results=%d, want 1/1", calls, results)
	}

	state := session.ReconstructState(entries)
	for i, m := range state.History {
		if m.Role != schema.Assistant || len(m.ToolCalls) == 0 {
			continue
		}
		answered := map[string]bool{}
		for j := i + 1; j < len(state.History) && state.History[j].Role == schema.Tool; j++ {
			answered[state.History[j].ToolCallID] = true
		}
		for _, tc := range m.ToolCalls {
			if !answered[tc.ID] {
				t.Errorf("reconstructed history: tool_call %s has no answering tool message", tc.ID)
			}
		}
	}
}
