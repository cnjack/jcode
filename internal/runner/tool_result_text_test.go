package runner

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestToolMessageTextExtractsEnhancedTextWithoutImage(t *testing.T) {
	encoded := "do-not-record-this-base64"
	msg := schema.ToolMessage("", "call-shot", schema.WithToolName("computer_screenshot"))
	msg.UserInputMultiContent = []schema.MessageInputPart{
		{Type: schema.ChatMessagePartTypeText, Text: "image_ref=/api/computer/shots/one.png"},
		{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{
				MIMEType:   "image/png",
				Base64Data: &encoded,
			}},
		},
		{Type: schema.ChatMessagePartTypeText, Text: "visual confirmation"},
	}

	got := toolMessageText(msg)
	want := "image_ref=/api/computer/shots/one.png\nvisual confirmation"
	if got != want {
		t.Fatalf("toolMessageText=%q, want %q", got, want)
	}
}

func TestToolMessageTextPrefersOrdinaryContent(t *testing.T) {
	msg := schema.ToolMessage("ordinary", "call", schema.WithToolName("read"))
	msg.UserInputMultiContent = []schema.MessageInputPart{
		{Type: schema.ChatMessagePartTypeText, Text: "duplicate"},
	}
	if got := toolMessageText(msg); got != "ordinary" {
		t.Fatalf("toolMessageText=%q, want ordinary Content", got)
	}
}
