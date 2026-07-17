package model

import (
	"encoding/base64"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/cloudwego/eino/schema"
)

func multimodalToolMessage(rawImage string) *schema.Message {
	encoded := base64.StdEncoding.EncodeToString([]byte(rawImage))
	msg := schema.ToolMessage("", "call-shot", schema.WithToolName("computer_screenshot"))
	msg.UserInputMultiContent = []schema.MessageInputPart{
		{Type: schema.ChatMessagePartTypeText, Text: "shot-ref"},
		{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
				MIMEType:   "image/png",
				Base64Data: &encoded,
			}},
		},
	}
	return msg
}

func TestToOpenAIMessagesAppendsImagesAfterParallelToolBatch(t *testing.T) {
	input := []*schema.Message{
		{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{
				{ID: "call-shot", Function: schema.FunctionCall{Name: "computer_screenshot", Arguments: `{}`}},
				{ID: "call-read", Function: schema.FunctionCall{Name: "computer_apps", Arguments: `{}`}},
			},
		},
		multimodalToolMessage("PNG"),
		schema.ToolMessage("apps", "call-read", schema.WithToolName("computer_apps")),
	}

	got := toOpenAIMessages(input, true)
	if len(got) != 4 {
		t.Fatalf("messages=%d, want assistant + 2 tool + synthetic user", len(got))
	}
	if got[1].Role != string(schema.Tool) || got[1].ToolCallID != "call-shot" || got[1].Content != "shot-ref" {
		t.Fatalf("screenshot tool response malformed: %#v", got[1])
	}
	if len(got[1].MultiContent) != 0 {
		t.Fatalf("tool response must be text-only for gateway compatibility: %#v", got[1].MultiContent)
	}
	if got[2].Role != string(schema.Tool) || got[2].ToolCallID != "call-read" || got[2].Content != "apps" {
		t.Fatalf("second tool response malformed: %#v", got[2])
	}
	visual := got[3]
	if visual.Role != string(schema.User) || len(visual.MultiContent) != 2 {
		t.Fatalf("synthetic visual message malformed: %#v", visual)
	}
	if visual.MultiContent[0].Type != openai.ChatMessagePartTypeText ||
		!strings.Contains(visual.MultiContent[0].Text, "computer_screenshot") ||
		!strings.Contains(visual.MultiContent[0].Text, "untrusted app content") {
		t.Fatalf("visual provenance label missing: %#v", visual.MultiContent[0])
	}
	image := visual.MultiContent[1]
	if image.Type != openai.ChatMessagePartTypeImageURL || image.ImageURL == nil ||
		image.ImageURL.URL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("PNG")) {
		t.Fatalf("unexpected visual image part: %#v", image)
	}
}

func TestToOpenAIMessagesVisionDisabledKeepsTextOnly(t *testing.T) {
	got := toOpenAIMessages([]*schema.Message{
		multimodalToolMessage("PNG"),
	}, false)
	if len(got) != 1 {
		t.Fatalf("messages=%d, want only tool response", len(got))
	}
	if got[0].Role != string(schema.Tool) ||
		!strings.Contains(got[0].Content, "shot-ref") ||
		!strings.Contains(got[0].Content, "image omitted") ||
		len(got[0].MultiContent) != 0 {
		t.Fatalf("vision=false result=%#v, want text-only tool response", got[0])
	}
}

func TestToOpenAIMessagesKeepsCurrentImageAfterSystemReminder(t *testing.T) {
	got := toOpenAIMessages([]*schema.Message{
		multimodalToolMessage("PNG"),
		schema.SystemMessage("fresh tool-loop reminder"),
	}, true)
	if len(got) != 3 {
		t.Fatalf("messages=%d, want tool + reminder + synthetic visual", len(got))
	}
	if got[0].Role != string(schema.Tool) || got[1].Role != string(schema.System) || got[2].Role != string(schema.User) {
		t.Fatalf("unexpected message order: %q, %q, %q", got[0].Role, got[1].Role, got[2].Role)
	}
	if len(got[2].MultiContent) != 2 || got[2].MultiContent[1].ImageURL == nil {
		t.Fatalf("current screenshot lost after reminder: %#v", got[2])
	}
}

func TestToOpenAIMessagesDoesNotResendHistoricalImage(t *testing.T) {
	got := toOpenAIMessages([]*schema.Message{
		multimodalToolMessage("SECRET-PIXELS"),
		schema.AssistantMessage("I inspected the screenshot", nil),
	}, true)
	if len(got) != 2 {
		t.Fatalf("messages=%d, historical image should not add a synthetic message", len(got))
	}
	if got[0].Content != "shot-ref" || len(got[0].MultiContent) != 0 {
		t.Fatalf("historical tool result was not reduced to text: %#v", got[0])
	}
	for _, msg := range got {
		for _, part := range msg.MultiContent {
			if part.ImageURL != nil && strings.Contains(part.ImageURL.URL, "SECRET-PIXELS") {
				t.Fatal("historical image was resent")
			}
		}
	}
}

func TestToOpenAIMessagesCapsParallelVisualPayload(t *testing.T) {
	msg := schema.ToolMessage("", "call-many", schema.WithToolName("computer_screenshot"))
	msg.UserInputMultiContent = []schema.MessageInputPart{{
		Type: schema.ChatMessagePartTypeText, Text: "many shots",
	}}
	for i := 0; i < MaxModelImagesPerRequest+2; i++ {
		encoded := base64.StdEncoding.EncodeToString([]byte{byte(i)})
		msg.UserInputMultiContent = append(msg.UserInputMultiContent, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
				MIMEType: "image/png", Base64Data: &encoded,
			}},
		})
	}

	got := toOpenAIMessages([]*schema.Message{msg}, true)
	if len(got) != 2 {
		t.Fatalf("messages=%d, want tool text + bounded synthetic visual", len(got))
	}
	if !strings.Contains(got[0].Content, "2 image(s) omitted") {
		t.Fatalf("tool result did not disclose visual budget omission: %q", got[0].Content)
	}
	if parts := got[1].MultiContent; len(parts) != 1+MaxModelImagesPerRequest {
		t.Fatalf("synthetic visual parts=%d, want provenance + %d images", len(parts), MaxModelImagesPerRequest)
	}
}

func TestOpenAIImagePartsUsesSharedDecodedByteBudget(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("abc")) // three decoded bytes
	parts := make([]schema.MessageInputPart, 0, 3)
	for range 3 {
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
				MIMEType: "image/png", Base64Data: &encoded,
			}},
		})
	}

	images, omitted := openAIImageParts(parts, NewModelImageBudgetWithLimits(10, 5))
	if len(images) != 1 || omitted != 2 {
		t.Fatalf("images=%d omitted=%d, want one admitted and two omitted", len(images), omitted)
	}
}
