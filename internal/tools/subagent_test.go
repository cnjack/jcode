package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// #10: A panic inside a subagent tool is recovered and folded into a
// model-visible string instead of killing the subagent (or the process).
func TestSafeToolMiddleware_PanicRecovered(t *testing.T) {
	endpoint := func(context.Context, string, ...tool.Option) (string, error) {
		panic("boom")
	}
	wrapped, _ := newSafeToolMiddleware().WrapInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "read"})
	out, err := wrapped(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("panic must fold to a string, got err=%v", err)
	}
	if !strings.Contains(out, "Tool execution panicked: boom") {
		t.Fatalf("expected panic message in output, got %q", out)
	}
}

// #10: Non-fatal tool errors are folded with the exact same format as
// internal/agent/middleware.go — the "Tool execution failed:" prefix is
// load-bearing for internal/handler/acp.go and internal/agent/reminder.go.
func TestSafeToolMiddleware_ErrorFolded(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   string
	}{
		{"error without partial output", "", "Tool execution failed: disk err"},
		{"error with partial output", "partial", "partial\n\nTool execution failed: disk err"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := func(context.Context, string, ...tool.Option) (string, error) {
				return tt.result, errors.New("disk err")
			}
			wrapped, _ := newSafeToolMiddleware().WrapInvokableToolCall(
				context.Background(), endpoint, &adk.ToolContext{Name: "read"})
			out, err := wrapped(context.Background(), `{}`)
			if err != nil {
				t.Fatalf("non-fatal error must be folded, got err=%v", err)
			}
			if out != tt.want {
				t.Fatalf("folded output = %q, want %q", out, tt.want)
			}
		})
	}
}

// #10 + #16: Fatal errors pass through unfolded so the abort signal reaches
// the parent run.
func TestSafeToolMiddleware_FatalPassthrough(t *testing.T) {
	endpoint := func(context.Context, string, ...tool.Option) (string, error) {
		return "", Fatal(errors.New("container gone"))
	}
	wrapped, _ := newSafeToolMiddleware().WrapInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "execute"})
	out, err := wrapped(context.Background(), `{}`)
	if err == nil {
		t.Fatal("fatal error must propagate, got nil")
	}
	if !IsFatal(err) {
		t.Fatalf("propagated error must stay fatal, got %v", err)
	}
	if out != "" {
		t.Fatalf("no folded result expected for fatal errors, got %q", out)
	}
}

func TestSafeToolMiddlewareEnhancedErrorPreservesMedia(t *testing.T) {
	encoded := "image-bytes"
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		return &schema.ToolResult{Parts: []schema.ToolOutputPart{
			{Type: schema.ToolPartTypeText, Text: "partial"},
			{Type: schema.ToolPartTypeImage, Image: &schema.ToolOutputImage{MessagePartCommon: schema.MessagePartCommon{
				MIMEType: "image/png", Base64Data: &encoded,
			}}},
		}}, errors.New("capture err")
	}
	wrapped, _ := newSafeToolMiddleware().WrapEnhancedInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	out, err := wrapped(context.Background(), &schema.ToolArgument{Text: `{}`})
	if err != nil {
		t.Fatalf("non-fatal enhanced error must be folded, got %v", err)
	}
	if got := enhancedText(out); got != "partial\n\nTool execution failed: capture err" {
		t.Fatalf("folded output=%q", got)
	}
	if len(out.Parts) != 3 || out.Parts[1].Type != schema.ToolPartTypeImage {
		t.Fatalf("partial media was lost: %#v", out.Parts)
	}
}

func TestSafeToolMiddlewareEnhancedPanicDropsUntrustedPartial(t *testing.T) {
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		panic("enhanced boom")
	}
	wrapped, _ := newSafeToolMiddleware().WrapEnhancedInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	out, err := wrapped(context.Background(), &schema.ToolArgument{Text: `{}`})
	if err != nil {
		t.Fatalf("panic must fold to a result, got %v", err)
	}
	if got := enhancedText(out); got != "Tool execution panicked: enhanced boom" {
		t.Fatalf("panic output=%q", got)
	}
	if len(out.Parts) != 1 || out.Parts[0].Type != schema.ToolPartTypeText {
		t.Fatalf("panic result must be text-only: %#v", out.Parts)
	}
}
