package runner

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	internalhandler "github.com/cnjack/jcode/internal/handler"
	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/tools"
)

type oneRemoteToolModel struct {
	modelCalls atomic.Int32
	toolName   string
}

func (m *oneRemoteToolModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func (*oneRemoteToolModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return nil, errors.New("Generate is not used: streaming is enabled")
}

func (m *oneRemoteToolModel) Stream(
	_ context.Context,
	input []*schema.Message,
	_ ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	m.modelCalls.Add(1)
	if len(input) > 0 && input[len(input)-1].Role == schema.Tool {
		return schema.StreamReaderFromArray([]*schema.Message{{
			Role: schema.Assistant, Content: "continued after reconnect",
		}}), nil
	}
	toolName := m.toolName
	if toolName == "" {
		toolName = "remote_operation"
	}
	arguments := "{}"
	if toolName == "execute" {
		arguments = `{"command":"mutate-once"}`
	}
	return schema.StreamReaderFromArray([]*schema.Message{{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{{
			ID: "remote-call-1",
			Function: schema.FunctionCall{
				Name: toolName, Arguments: arguments,
			},
		}},
	}}), nil
}

type outcomeUnknownTool struct {
	invocations atomic.Int32
}

func (*outcomeUnknownTool) Info(context.Context) (*schema.ToolInfo, error) {
	return remoteOperationInfo(), nil
}

func (t *outcomeUnknownTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	t.invocations.Add(1)
	return "", tools.Fatal(&tools.RemoteTransportError{
		Kind: "ssh", Code: "ssh_transport_lost",
		Phase:     tools.RemoteTransportOutcomeUnknown,
		Retryable: true,
		Err:       io.EOF,
	})
}

type internallyRetriedReadTool struct {
	invocations       atomic.Int32
	transportAttempts atomic.Int32
}

type fatalExecuteExecutor struct {
	tools.Executor
	calls atomic.Int32
}

func (e *fatalExecuteExecutor) Exec(
	context.Context,
	string,
	string,
	time.Duration,
) (string, string, error) {
	e.calls.Add(1)
	return "possibly applied", "connection dropped", tools.Fatal(&tools.RemoteTransportError{
		Kind: "ssh", Code: "ssh_connection_failed",
		Phase:     tools.RemoteTransportOutcomeUnknown,
		Retryable: true,
		Err:       io.EOF,
	})
}

func (*internallyRetriedReadTool) Info(context.Context) (*schema.ToolInfo, error) {
	return remoteOperationInfo(), nil
}

func (t *internallyRetriedReadTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	// This models the executor's bounded singleflight repair: the model-issued
	// tool invocation remains one while the transport implementation makes a
	// second safe read attempt after reconnecting.
	t.invocations.Add(1)
	t.transportAttempts.Add(2)
	return "remote read succeeded", nil
}

func remoteOperationInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name:        "remote_operation",
		Desc:        "test remote operation",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}
}

func remoteRecoveryAgent(
	t *testing.T,
	model einomodel.ToolCallingChatModel,
	remoteTool tool.BaseTool,
) *adk.ChatModelAgent {
	t.Helper()
	ag, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name: "remote-recovery-test", Description: "remote-recovery-test",
		Instruction: "test", Model: model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{remoteTool},
		}},
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return ag
}

func TestRunRemoteOutcomeUnknownIsNotModelErrorOrReplayed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	model := &oneRemoteToolModel{}
	remoteTool := &outcomeUnknownTool{}
	h := &resultCaptureHandler{}

	result := Run(
		context.Background(), remoteRecoveryAgent(t, model, remoteTool),
		[]adk.Message{schema.UserMessage("run once")}, h,
		nil, nil, nil, nil, nil,
	)
	if result.Err == nil {
		t.Fatal("RunResult.Err = nil, want remote transport failure")
	}
	var remoteErr *tools.RemoteTransportError
	if !errors.As(result.Err, &remoteErr) {
		t.Fatalf("RunResult.Err = %T %v, want RemoteTransportError in chain", result.Err, result.Err)
	}
	if remoteErr.Phase != tools.RemoteTransportOutcomeUnknown {
		t.Fatalf("remote phase = %q", remoteErr.Phase)
	}
	if got := remoteTool.invocations.Load(); got != 1 {
		t.Fatalf("tool invocations = %d, want exactly one", got)
	}
	if got := model.modelCalls.Load(); got != 1 {
		t.Fatalf("model calls = %d, want no graph replay after uncertain mutation", got)
	}
	if strings.Contains(strings.ToLower(result.Err.Error()), "model") ||
		strings.Contains(result.Err.Error(), "Could not reach") {
		t.Fatalf("remote failure was mislabelled as model failure: %v", result.Err)
	}
	if !strings.Contains(result.Err.Error(), "did not replay") {
		t.Fatalf("remote failure lacks no-replay guidance: %v", result.Err)
	}
	if len(result.Messages) != 2 || result.Messages[1].Role != schema.Tool ||
		result.Messages[1].Content != session.InterruptedToolOutput {
		t.Fatalf("result messages = %#v, want paired interrupted tool result", result.Messages)
	}
}

func TestRunContinuesAfterExecutorInternalSafeRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	model := &oneRemoteToolModel{}
	remoteTool := &internallyRetriedReadTool{}
	h := &resultCaptureHandler{}

	result := Run(
		context.Background(), remoteRecoveryAgent(t, model, remoteTool),
		[]adk.Message{schema.UserMessage("read")}, h,
		nil, nil, nil, nil, nil,
	)
	if result.Err != nil {
		t.Fatalf("RunResult.Err = %v", result.Err)
	}
	if got := remoteTool.invocations.Load(); got != 1 {
		t.Fatalf("model-issued tool invocations = %d, want one", got)
	}
	if got := remoteTool.transportAttempts.Load(); got != 2 {
		t.Fatalf("transport attempts = %d, want executor-local retry", got)
	}
	if got := model.modelCalls.Load(); got != 2 {
		t.Fatalf("model calls = %d, want initial tool call then continuation", got)
	}
	if !strings.Contains(result.Response, "continued after reconnect") {
		t.Fatalf("response = %q", result.Response)
	}
}

func TestRunRemoteOutcomeUnknownEmitsStructuredWebDone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	model := &oneRemoteToolModel{}
	remoteTool := &outcomeUnknownTool{}
	h := internalhandler.NewWebHandler()

	result := Run(
		context.Background(), remoteRecoveryAgent(t, model, remoteTool),
		[]adk.Message{schema.UserMessage("run once")}, h,
		nil, nil, nil, nil, nil,
	)
	if result.Err == nil {
		t.Fatal("RunResult.Err = nil, want remote transport failure")
	}

	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	for {
		select {
		case event := <-h.Events():
			if event.Event != "agent_done" {
				continue
			}
			data, ok := event.Data.(internalhandler.WebDoneData)
			if !ok {
				t.Fatalf("agent_done data = %T", event.Data)
			}
			if data.ErrorKind != "remote_connection" || data.Code != "ssh_transport_lost" ||
				data.Kind != "ssh" || data.Phase != string(tools.RemoteTransportOutcomeUnknown) {
				t.Fatalf("agent_done = %+v", data)
			}
			if data.Retryable == nil || *data.Retryable {
				t.Fatalf("agent_done retryable = %v, want false for dispatched operation", data.Retryable)
			}
			if strings.Contains(strings.ToLower(data.Error), "model") {
				t.Fatalf("structured remote error was relabelled as a model failure: %q", data.Error)
			}
			return
		case <-timeout.C:
			t.Fatal("timed out waiting for structured agent_done")
		}
	}
}

func TestRunExecuteToolPropagatesRemoteFatalWithoutReplay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	model := &oneRemoteToolModel{toolName: "execute"}
	env := tools.NewEnv(t.TempDir(), "linux/amd64")
	executor := &fatalExecuteExecutor{Executor: env.Exec}
	env.Exec = executor
	h := &resultCaptureHandler{}

	result := Run(
		context.Background(), remoteRecoveryAgent(t, model, env.NewExecuteTool(nil)),
		[]adk.Message{schema.UserMessage("run once")}, h,
		nil, nil, nil, nil, nil,
	)
	var remoteErr *tools.RemoteTransportError
	if !errors.As(result.Err, &remoteErr) {
		t.Fatalf("RunResult.Err = %T %v, want RemoteTransportError", result.Err, result.Err)
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("execute calls = %d, want exactly one", got)
	}
	if got := model.modelCalls.Load(); got != 1 {
		t.Fatalf("model calls = %d, want no model retry after uncertain execute", got)
	}
	if len(result.Messages) != 2 || result.Messages[1].Content != session.InterruptedToolOutput {
		t.Fatalf("result messages = %#v, want interrupted result instead of partial execute output", result.Messages)
	}
}
