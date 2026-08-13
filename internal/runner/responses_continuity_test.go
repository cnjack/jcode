package runner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/cnjack/jcode/internal/model/responsemeta"
	"github.com/cnjack/jcode/internal/session"
)

type responsesContinuityModel struct{}

type responsesFailedStreamModel struct {
	content string
	cancel  context.CancelFunc
}

func (m *responsesContinuityModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func (m *responsesContinuityModel) Generate(
	context.Context,
	[]*schema.Message,
	...einomodel.Option,
) (*schema.Message, error) {
	return nil, errors.New("Generate is not used: streaming is enabled")
}

func (m *responsesContinuityModel) Stream(
	context.Context,
	[]*schema.Message,
	...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	opaque := json.RawMessage(`{"type":"reasoning","id":"rs-runner","summary":[{"type":"summary_text","text":"clear-summary"}],"encrypted_content":"cipher-runner"}`)
	return schema.StreamReaderFromArray([]*schema.Message{
		{
			Role: schema.Assistant, ReasoningContent: "runtime-thought",
			Extra: map[string]any{
				responsemeta.OpaqueItemsExtraKey: []json.RawMessage{opaque},
				"provider_meta":                  "kept-live",
			},
		},
		{Role: schema.Assistant, Content: "answer"},
	}), nil
}

func (m *responsesFailedStreamModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func (m *responsesFailedStreamModel) Generate(
	context.Context,
	[]*schema.Message,
	...einomodel.Option,
) (*schema.Message, error) {
	return nil, errors.New("Generate is not used: streaming is enabled")
}

func (m *responsesFailedStreamModel) Stream(
	ctx context.Context,
	_ []*schema.Message,
	_ ...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	reader, writer := schema.Pipe[*schema.Message](2)
	go func() {
		defer writer.Close()
		opaque := json.RawMessage(`{"type":"reasoning","id":"rs-partial","summary":[{"type":"summary_text","text":"clear-partial"}],"encrypted_content":"cipher-partial"}`)
		if writer.Send(&schema.Message{
			Role: schema.Assistant, Content: m.content, ReasoningContent: "runtime-partial",
			Extra: responsemeta.Extra([]json.RawMessage{opaque}),
		}, nil) {
			return
		}
		if m.cancel != nil {
			m.cancel()
			writer.Send(nil, ctx.Err())
			return
		}
		writer.Send(nil, errors.New("provider stream failed"))
	}()
	return reader, nil
}

func TestRunPreservesResponsesReasoningAndExtraWithoutPersistingCleartext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "responses-continuity", Description: "test", Instruction: "test",
		Model: &responsesContinuityModel{}, MaxIterations: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := session.NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	recorder.RecordUser("hello")
	result := Run(
		ctx, agent, []adk.Message{schema.UserMessage("hello")}, stubHandler{}, recorder,
		nil, nil, nil, nil,
	)
	recorder.Close()
	if result.Err != nil || result.Response != "answer" || len(result.Messages) != 1 {
		t.Fatalf("result = %#v", result)
	}
	live := result.Messages[0]
	if live.ReasoningContent != "runtime-thought" || live.Extra["provider_meta"] != "kept-live" {
		t.Fatalf("live message lost Responses metadata: %#v", live)
	}
	if items := responsemeta.FromExtra(live.Extra); len(items) != 1 ||
		!strings.Contains(string(items[0]), "cipher-runner") || strings.Contains(string(items[0]), "clear-summary") {
		t.Fatalf("live opaque items = %s", items)
	}

	entries, err := session.LoadSession(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	replayed := session.ReconstructState(entries).History
	if len(replayed) != 2 {
		t.Fatalf("replayed history length = %d", len(replayed))
	}
	assistant := replayed[1]
	if assistant.Content != "answer" || assistant.ReasoningContent != "" {
		t.Fatalf("replayed assistant = %#v", assistant)
	}
	if _, ok := assistant.Extra["provider_meta"]; ok {
		t.Fatal("arbitrary provider metadata was persisted")
	}
	if items := responsemeta.FromExtra(assistant.Extra); len(items) != 1 ||
		!strings.Contains(string(items[0]), "cipher-runner") {
		t.Fatalf("replayed opaque items = %s", items)
	}
}

func TestRunFailedResponsesStreamDoesNotPersistOrphanOpaqueReasoning(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		cancel         bool
		wantAssistant  bool
		wantOpaqueItem bool
	}{
		{name: "error without visible content"},
		{name: "cancel without visible content", cancel: true},
		{name: "error with visible content", content: "visible partial", wantAssistant: true, wantOpaqueItem: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			ctx := context.Background()
			var cancel context.CancelFunc
			if test.cancel {
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
			}
			model := &responsesFailedStreamModel{content: test.content, cancel: cancel}
			agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
				Name: "responses-failed-stream", Description: "test", Instruction: "test",
				Model: model, MaxIterations: 2,
			})
			if err != nil {
				t.Fatal(err)
			}
			recorder, err := session.NewRecorder(t.TempDir(), "provider", "model")
			if err != nil {
				t.Fatal(err)
			}
			recorder.RecordUser("hello")
			result := Run(
				ctx, agent, []adk.Message{schema.UserMessage("hello")}, stubHandler{}, recorder,
				nil, nil, nil, nil,
			)
			id := recorder.UUID()
			recorder.Close()
			if result.Err == nil {
				t.Fatal("Run unexpectedly succeeded")
			}
			if got := len(result.Messages); got != btoi(test.wantAssistant) {
				t.Fatalf("live assistant messages = %d, want %d", got, btoi(test.wantAssistant))
			}
			if test.wantAssistant && result.Messages[0].Content != test.content {
				t.Fatalf("live assistant = %#v, want content %q", result.Messages[0], test.content)
			}

			entries, err := session.LoadSession(id)
			if err != nil {
				t.Fatal(err)
			}
			history := session.ReconstructState(entries).History
			var assistant *schema.Message
			for _, message := range history {
				if message.Role == schema.Assistant {
					assistant = message
				}
			}
			if !test.wantAssistant {
				if assistant != nil {
					t.Fatalf("restart retained failed assistant turn: %#v", assistant)
				}
				return
			}
			if assistant == nil || assistant.Content != test.content {
				t.Fatalf("restart assistant = %#v, want content %q", assistant, test.content)
			}
			items := responsemeta.FromExtra(assistant.Extra)
			if test.wantOpaqueItem && (len(items) != 1 || !strings.Contains(string(items[0]), "cipher-partial")) {
				t.Fatalf("restart opaque items = %s", items)
			}
		})
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}
