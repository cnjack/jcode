package runner

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	openai "github.com/sashabaranov/go-openai"

	internalhandler "github.com/cnjack/jcode/internal/handler"
	internalmodel "github.com/cnjack/jcode/internal/model"
)

type rateLimitThenSuccessModel struct {
	calls atomic.Int32
}

func (m *rateLimitThenSuccessModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func (*rateLimitThenSuccessModel) Generate(
	context.Context,
	[]*schema.Message,
	...einomodel.Option,
) (*schema.Message, error) {
	return nil, errors.New("Generate is not used: streaming is enabled")
}

func (m *rateLimitThenSuccessModel) Stream(
	context.Context,
	[]*schema.Message,
	...einomodel.Option,
) (*schema.StreamReader[*schema.Message], error) {
	if m.calls.Add(1) == 1 {
		return nil, &openai.APIError{HTTPStatusCode: 429, Message: "rate limited; retry-after-ms: 1"}
	}
	return schema.StreamReaderFromArray([]*schema.Message{{
		Role: schema.Assistant, Content: "recovered",
	}}), nil
}

type modelRetryCaptureHandler struct {
	stubHandler
	mu      sync.Mutex
	events  []internalhandler.ModelRetryEvent
	doneErr error
}

func (h *modelRetryCaptureHandler) OnModelRetry(event internalhandler.ModelRetryEvent) {
	h.mu.Lock()
	h.events = append(h.events, event)
	h.mu.Unlock()
}

func (h *modelRetryCaptureHandler) OnAgentDone(err error) {
	h.mu.Lock()
	h.doneErr = err
	h.mu.Unlock()
}

func TestRunReportsRateLimitBackoffAndRecovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	model := &rateLimitThenSuccessModel{}
	ag, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "model-retry-test", Description: "model-retry-test", Instruction: "test",
		Model: model, MaxIterations: 2,
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries:  internalmodel.DefaultMaxRetries,
			IsRetryAble: internalmodel.IsRetryable,
			BackoffFunc: internalmodel.SmartBackoff,
		},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	h := &modelRetryCaptureHandler{}

	result := Run(
		ctx, ag, []adk.Message{schema.UserMessage("hello")}, h,
		nil, nil, nil, nil, nil,
	)

	if result.Err != nil || result.Response != "recovered" {
		t.Fatalf("result = %#v", result)
	}
	if got := model.calls.Load(); got != 2 {
		t.Fatalf("model calls = %d, want 2", got)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.doneErr != nil {
		t.Fatalf("OnAgentDone error = %v", h.doneErr)
	}
	if len(h.events) != 2 {
		t.Fatalf("retry events = %+v, want waiting then ready", h.events)
	}
	waiting, ready := h.events[0], h.events[1]
	if waiting.Status != internalhandler.ModelRetryWaiting || waiting.Attempt != 1 ||
		waiting.MaxAttempts != internalmodel.DefaultMaxRetries || waiting.RetryIn <= 0 {
		t.Fatalf("waiting event = %+v", waiting)
	}
	if ready.Status != internalhandler.ModelRetryReady {
		t.Fatalf("ready event = %+v", ready)
	}
}
