package telemetry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	langfuseacl "github.com/cloudwego/eino-ext/libs/acl/langfuse"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	internalmodel "github.com/cnjack/jcode/internal/model"
)

type captureLangfuse struct {
	trace      *langfuseacl.TraceEventBody
	endedTrace *langfuseacl.TraceEventBody
	generation *langfuseacl.GenerationEventBody
}

func (c *captureLangfuse) CreateTrace(body *langfuseacl.TraceEventBody) (string, error) {
	c.trace = body
	return "trace", nil
}

func (c *captureLangfuse) EndTrace(body *langfuseacl.TraceEventBody) error {
	c.endedTrace = body
	return nil
}

func (c *captureLangfuse) CreateSpan(*langfuseacl.SpanEventBody) (string, error) {
	return "span", nil
}

func (c *captureLangfuse) EndSpan(*langfuseacl.SpanEventBody) error { return nil }

func (c *captureLangfuse) CreateGeneration(body *langfuseacl.GenerationEventBody) (string, error) {
	c.generation = body
	return "generation", nil
}

func (c *captureLangfuse) EndGeneration(*langfuseacl.GenerationEventBody) error { return nil }

func (c *captureLangfuse) CreateEvent(*langfuseacl.EventEventBody) (string, error) {
	return "event", nil
}

func (c *captureLangfuse) Flush() {}

func TestLangfuseUsageMapsCachedAndReasoning(t *testing.T) {
	if got := langfuseUsage(nil); got != nil {
		t.Fatalf("langfuseUsage(nil) = %#v, want nil", got)
	}
	if got := langfuseUsage(&internalmodel.TokenUsageDetail{}); got != nil {
		t.Fatalf("langfuseUsage(zero) = %#v, want nil", got)
	}

	got := langfuseUsage(&internalmodel.TokenUsageDetail{
		PromptTokens:     19,
		CompletionTokens: 10,
		TotalTokens:      29,
		CachedTokens:     4,
		ReasoningTokens:  3,
	})
	if got == nil {
		t.Fatal("langfuseUsage returned nil")
	}
	if got.PromptTokens != 19 || got.CompletionTokens != 10 || got.TotalTokens != 29 {
		t.Fatalf("usage totals = %+v", got)
	}
	if got.PromptTokensDetails == nil || got.PromptTokensDetails.CachedTokens != 4 {
		t.Fatalf("cached details = %#v", got.PromptTokensDetails)
	}
	if got.CompletionTokensDetails == nil || got.CompletionTokensDetails.ReasoningTokens != 3 {
		t.Fatalf("reasoning details = %#v", got.CompletionTokensDetails)
	}
}

func TestWithNewTraceCapturesLatestPlainUserInput(t *testing.T) {
	client := &captureLangfuse{}
	tracer := &LangfuseTracer{client: client}

	tracer.WithNewTrace(context.Background(), "coding_agent", []*schema.Message{
		schema.UserMessage("previous request"),
		{Role: schema.Assistant, Content: "previous response"},
		schema.UserMessage("latest request"),
	})

	if client.trace == nil {
		t.Fatal("trace was not created")
	}
	if got, want := client.trace.Input, "latest request"; got != want {
		t.Fatalf("trace input=%q, want %q", got, want)
	}
}

func TestWithNewTraceCapturesSafeLatestUserInput(t *testing.T) {
	base64Secret := "base64-secret-pixels"
	urlSecret := "https://private.invalid/screenshot.png?token=secret"
	imagePrompt := &schema.Message{
		Role: schema.User,
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "Describe this screenshot"},
			{
				Type: schema.ChatMessagePartTypeImageURL,
				Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
					MIMEType:   "image/png",
					Base64Data: &base64Secret,
				}},
			},
			{
				Type: schema.ChatMessagePartTypeImageURL,
				Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
					MIMEType: "image/png",
					URL:      &urlSecret,
				}},
			},
		},
	}
	client := &captureLangfuse{}
	tracer := &LangfuseTracer{client: client}

	ctx := tracer.WithNewTrace(context.Background(), "coding_agent", []*schema.Message{
		schema.UserMessage("previous request"),
		imagePrompt,
	})
	if client.trace == nil {
		t.Fatal("trace was not created")
	}
	if got, want := client.trace.Input, "Describe this screenshot\n"+telemetryImagePlaceholder+"\n"+telemetryImagePlaceholder; got != want {
		t.Fatalf("trace input=%q, want %q", got, want)
	}
	if strings.Contains(client.trace.Input, "previous request") {
		t.Fatalf("trace input includes an earlier user turn: %q", client.trace.Input)
	}
	for _, secret := range []string{base64Secret, urlSecret} {
		if strings.Contains(client.trace.Input, secret) {
			t.Fatalf("trace input leaked image data %q", secret)
		}
	}

	tracer.EndTrace(ctx, "completed response")
	if client.endedTrace == nil {
		t.Fatal("trace was not ended")
	}
	if client.endedTrace.ID != "trace" || client.endedTrace.Output != "completed response" {
		t.Fatalf("ended trace=%#v", client.endedTrace)
	}
}

func TestBeforeModelRewriteStateRedactsEnhancedToolImagesFromLangfuse(t *testing.T) {
	base64Secret := "base64-secret-pixels"
	urlSecret := "https://private.invalid/screenshot.png?token=secret"
	shot := schema.ToolMessage("", "call-shot", schema.WithToolName("computer_screenshot"))
	shot.UserInputMultiContent = []schema.MessageInputPart{
		{Type: schema.ChatMessagePartTypeText, Text: "image_ref=/api/computer/shots/one.png"},
		{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
				MIMEType:   "image/png",
				Base64Data: &base64Secret,
			}},
		},
		{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
				MIMEType: "image/png",
				URL:      &urlSecret,
			}},
		},
	}
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{shot}}
	client := &captureLangfuse{}
	mw := &langfuseMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		tracer:                       &LangfuseTracer{client: client},
	}
	ctx := context.WithValue(context.Background(), traceIDKey, "trace-id")

	_, returned, err := mw.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if returned != state {
		t.Fatal("telemetry sanitization must not replace the live agent state")
	}
	if client.generation == nil || len(client.generation.InMessages) != 1 {
		t.Fatalf("generation input was not captured: %#v", client.generation)
	}

	traced := client.generation.InMessages[0]
	if traced == shot {
		t.Fatal("Langfuse received the live image-bearing message pointer")
	}
	if len(traced.UserInputMultiContent) != 3 {
		t.Fatalf("trace parts=%d, want original ordering and count", len(traced.UserInputMultiContent))
	}
	if traced.UserInputMultiContent[0].Text != "image_ref=/api/computer/shots/one.png" {
		t.Fatalf("safe tool text was lost: %#v", traced.UserInputMultiContent[0])
	}
	for _, part := range traced.UserInputMultiContent[1:] {
		if part.Type != schema.ChatMessagePartTypeText || part.Text != telemetryImagePlaceholder || part.Image != nil {
			t.Fatalf("image was not replaced with a safe placeholder: %#v", part)
		}
	}
	serialized, err := json.Marshal(client.generation.InMessages)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{base64Secret, urlSecret} {
		if strings.Contains(string(serialized), secret) {
			t.Fatalf("Langfuse payload leaked image data %q", secret)
		}
	}

	// Redaction is trace-only: the model-facing state still owns the original
	// image, and later trace mutations cannot alter it.
	if shot.UserInputMultiContent[1].Image == nil ||
		shot.UserInputMultiContent[1].Image.Base64Data == nil ||
		*shot.UserInputMultiContent[1].Image.Base64Data != base64Secret {
		t.Fatal("sanitization modified the model-facing Base64 image")
	}
	traced.UserInputMultiContent[0].Text = "changed trace text"
	if shot.UserInputMultiContent[0].Text != "image_ref=/api/computer/shots/one.png" {
		t.Fatal("trace and live UserInputMultiContent share backing storage")
	}
}
