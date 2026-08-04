package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/session"
)

var errFirstToolChunk = errors.New("tool stream failed before first chunk")

type firstRecvErrorTool struct{}

func (firstRecvErrorTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        "check",
		Desc:        "fails before emitting its first result chunk",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

func (firstRecvErrorTool) StreamableRun(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
	reader, writer := schema.Pipe[string](1)
	writer.Send("", errFirstToolChunk)
	writer.Close()
	return reader, nil
}

type resultCaptureHandler struct {
	stubHandler
	results []handler.ToolResultEvent
	doneErr error
}

func (h *resultCaptureHandler) OnToolResult(event handler.ToolResultEvent) {
	h.results = append(h.results, event)
}

func (h *resultCaptureHandler) OnAgentDone(err error) { h.doneErr = err }

type closeRecorderOnResultHandler struct {
	resultCaptureHandler
	recorder *session.Recorder
}

func (h *closeRecorderOnResultHandler) OnToolResult(event handler.ToolResultEvent) {
	h.resultCaptureHandler.OnToolResult(event)
	h.recorder.Close()
}

func TestRunPairsFirstReceiveToolStreamFailureWithAnnouncedCall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	ag, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "stream-error-test",
		Description: "stream-error-test",
		Instruction: "test",
		Model:       &historyModel{},
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{firstRecvErrorTool{}},
		}},
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	rec, err := session.NewRecorder("stream-error-test", "test", "test")
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	rec.RecordUser("check")
	h := &resultCaptureHandler{}

	result := Run(ctx, ag, []adk.Message{schema.UserMessage("check")}, h, rec, nil, nil, nil, nil)
	if result.Err == nil {
		t.Fatal("RunResult.Err = nil, want tool stream failure")
	}
	if len(h.results) != 1 || h.results[0].ToolCallID != "call-check-1" {
		t.Fatalf("tool results = %#v, want one result for call-check-1", h.results)
	}
	if len(result.Messages) != 2 || result.Messages[1].Role != schema.Tool || result.Messages[1].ToolCallID != "call-check-1" {
		t.Fatalf("result messages = %#v, want paired assistant call and tool failure", result.Messages)
	}

	entries, err := session.LoadSession(rec.UUID())
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	assertMessagesEqual(t, session.ReconstructState(entries).History[1:], result.Messages)
}

func TestRunSurfacesToolResultPersistenceFailureWithoutUsingUnstoredOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	rec, err := session.NewRecorder("persist-error-test", "test", "test")
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	rec.RecordUser("check")
	h := &closeRecorderOnResultHandler{recorder: rec}

	result := Run(ctx, newHistoryAgent(ctx, t), []adk.Message{schema.UserMessage("check")}, h, rec, nil, nil, nil, nil)
	if result.Err == nil {
		t.Fatal("RunResult.Err = nil, want recorder persistence failure")
	}
	if h.doneErr == nil {
		t.Fatal("OnAgentDone err = nil, want recorder persistence failure")
	}
	for _, want := range []string{rec.UUID() + ".json", "call-check-1"} {
		if !strings.Contains(result.Err.Error(), want) {
			t.Errorf("RunResult.Err = %q, want it to contain %q", result.Err, want)
		}
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %d, want assistant call + interrupted result", len(result.Messages))
	}
	toolMessage := result.Messages[1]
	if toolMessage.Role != schema.Tool || toolMessage.ToolCallID != "call-check-1" || toolMessage.Content != session.InterruptedToolOutput {
		t.Fatalf("tool message = %#v, want replay-equivalent interrupted result", toolMessage)
	}

	entries, err := session.LoadSession(rec.UUID())
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	assertMessagesEqual(t, session.ReconstructState(entries).History[1:], result.Messages)
}
