package runner

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/session"
)

type historyModel struct{}

func (m *historyModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func (m *historyModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return nil, errors.New("Generate is not used: streaming is enabled")
}

func (m *historyModel) Stream(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	if last := input[len(input)-1]; last.Role == schema.Tool {
		return schema.StreamReaderFromArray([]*schema.Message{{
			Role:    schema.Assistant,
			Content: "installed",
		}}), nil
	}
	return schema.StreamReaderFromArray([]*schema.Message{{
		Role:    schema.Assistant,
		Content: "checking",
		ToolCalls: []schema.ToolCall{{
			ID:       "call-check-1",
			Function: schema.FunctionCall{Name: "check", Arguments: `{}`},
		}},
	}}), nil
}

type historyTool struct{}

func (historyTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        "check",
		Desc:        "returns an installation result",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

func (historyTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "uv 0.12.1", nil
}

type parallelHistoryModel struct{}

func (m *parallelHistoryModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func (m *parallelHistoryModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return nil, errors.New("Generate is not used: streaming is enabled")
}

func (m *parallelHistoryModel) Stream(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	if last := input[len(input)-1]; last.Role == schema.Tool {
		return schema.StreamReaderFromArray([]*schema.Message{{Role: schema.Assistant, Content: "done"}}), nil
	}
	first, second := 0, 1
	return schema.StreamReaderFromArray([]*schema.Message{{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{
			{Index: &first, ID: "call-a", Function: schema.FunctionCall{Name: "check_a", Arguments: `{}`}},
			{Index: &second, ID: "call-b", Function: schema.FunctionCall{Name: "check_b", Arguments: `{}`}},
		},
	}}), nil
}

type namedHistoryTool struct {
	name   string
	output string
}

func (t namedHistoryTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        t.name,
		Desc:        "returns a fixed result",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

func (t namedHistoryTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return t.output, nil
}

func TestRunReturnsStructuredMessagesMatchingSessionReplay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	ag := newHistoryAgent(ctx, t)

	rec, err := session.NewRecorder("history-test", "test", "test")
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	rec.RecordUser("install uv")

	result := Run(
		ctx,
		ag,
		[]adk.Message{schema.UserMessage("install uv")},
		stubHandler{},
		rec,
		nil,
		nil,
		nil,
		nil,
	)

	if result.Response != "checkinginstalled" {
		t.Fatalf("Response = %q, want %q", result.Response, "checkinginstalled")
	}
	assertStructuredTurn(t, result.Messages)

	entries, err := session.LoadSession(rec.UUID())
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	replayed := session.ReconstructState(entries).History
	if len(replayed) != len(result.Messages)+1 {
		t.Fatalf("replayed messages = %d, want user + %d turn messages", len(replayed), len(result.Messages))
	}
	assertMessagesEqual(t, replayed[1:], result.Messages)
}

func TestRunStructuredMessagesDoNotDependOnRecorder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	result := Run(
		ctx,
		newHistoryAgent(ctx, t),
		[]adk.Message{schema.UserMessage("install uv")},
		stubHandler{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	if result.Response != "checkinginstalled" {
		t.Fatalf("Response = %q, want %q", result.Response, "checkinginstalled")
	}
	assertStructuredTurn(t, result.Messages)
}

func TestRunPreservesParallelToolCallBatchInLiveAndReplayHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	ag, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "parallel-history-test",
		Description: "parallel-history-test",
		Instruction: "test",
		Model:       &parallelHistoryModel{},
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{
				namedHistoryTool{name: "check_a", output: "a"},
				namedHistoryTool{name: "check_b", output: strings.Repeat("line\n", 3000)},
			},
		}},
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	rec, err := session.NewRecorder("parallel-history-test", "test", "test")
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	rec.RecordUser("check both")

	result := Run(ctx, ag, []adk.Message{schema.UserMessage("check both")}, stubHandler{}, rec, nil, nil, nil, nil)
	if len(result.Messages) != 4 {
		t.Fatalf("turn messages = %d, want assistant + 2 tools + assistant", len(result.Messages))
	}
	if result.Messages[0].Role != schema.Assistant || len(result.Messages[0].ToolCalls) != 2 {
		t.Fatalf("first message = %#v, want assistant with 2 tool calls", result.Messages[0])
	}
	wantIDs := map[string]bool{"call-a": true, "call-b": true}
	for _, message := range result.Messages[1:3] {
		if message.Role != schema.Tool || !wantIDs[message.ToolCallID] {
			t.Fatalf("parallel tool message = %#v, want call-a or call-b result", message)
		}
		delete(wantIDs, message.ToolCallID)
	}
	if len(wantIDs) != 0 {
		t.Fatalf("missing tool results for %v", wantIDs)
	}
	var sawTruncated bool
	for _, message := range result.Messages[1:3] {
		if strings.Contains(message.Content, "truncated") {
			sawTruncated = true
		}
	}
	if !sawTruncated {
		t.Fatal("large tool result was not normalized to the persisted truncated form")
	}
	if result.Messages[3].Role != schema.Assistant || result.Messages[3].Content != "done" {
		t.Fatalf("last message = %#v, want final assistant", result.Messages[3])
	}

	entries, err := session.LoadSession(rec.UUID())
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	replayed := session.ReconstructState(entries).History
	assertMessagesEqual(t, replayed[1:], result.Messages)
}

func newHistoryAgent(ctx context.Context, t *testing.T) *adk.ChatModelAgent {
	t.Helper()
	ag, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "history-test",
		Description: "history-test",
		Instruction: "test",
		Model:       &historyModel{},
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{historyTool{}},
		}},
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return ag
}

func assertStructuredTurn(t *testing.T, messages []adk.Message) {
	t.Helper()
	if len(messages) != 3 {
		t.Fatalf("turn messages = %d, want assistant + tool + assistant", len(messages))
	}
	if messages[0].Role != schema.Assistant || messages[0].Content != "checking" || len(messages[0].ToolCalls) != 1 {
		t.Fatalf("first message = %#v, want assistant text with tool call", messages[0])
	}
	if messages[0].ToolCalls[0].ID != "call-check-1" {
		t.Errorf("tool call ID = %q, want call-check-1", messages[0].ToolCalls[0].ID)
	}
	if messages[1].Role != schema.Tool || messages[1].ToolCallID != "call-check-1" || messages[1].Content != "uv 0.12.1" {
		t.Fatalf("tool message = %#v, want matching tool result", messages[1])
	}
	if messages[2].Role != schema.Assistant || messages[2].Content != "installed" {
		t.Fatalf("last message = %#v, want final assistant response", messages[2])
	}
}

func assertMessagesEqual(t *testing.T, got, want []adk.Message) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("message lengths differ: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("message[%d] mismatch:\n got  %#v\n want %#v", i, got[i], want[i])
		}
	}
}
