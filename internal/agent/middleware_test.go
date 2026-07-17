package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/tools"
)

// #16: A tools.Fatal error must abort the run (propagate as a real error)
// instead of being folded into a model-visible string.
func TestApprovalMiddleware_FatalErrorAborts(t *testing.T) {
	endpoint := func(context.Context, string, ...tool.Option) (string, error) {
		return "", tools.Fatal(errors.New("container removed"))
	}
	wrapped, _ := newApprovalMiddleware(nil).WrapInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "execute"})
	out, err := wrapped(context.Background(), `{}`)
	if err == nil {
		t.Fatal("fatal error must propagate, got nil")
	}
	if !tools.IsFatal(err) {
		t.Fatalf("propagated error must stay fatal, got %v", err)
	}
	if out != "" {
		t.Fatalf("no folded result expected for fatal errors, got %q", out)
	}
}

// Baseline: non-fatal errors keep the existing folding behavior. The
// "Tool execution failed:" prefix is load-bearing — internal/handler/acp.go
// (isToolFailureOutput) and internal/agent/reminder.go (updateErrorStreak)
// classify failures by it.
func TestApprovalMiddleware_NonFatalFolded(t *testing.T) {
	tests := []struct {
		name       string
		result     string
		wantPrefix string
	}{
		{"error without partial output", "", "Tool execution failed: disk err"},
		{"error with partial output", "partial", "partial\n\nTool execution failed: disk err"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := func(context.Context, string, ...tool.Option) (string, error) {
				return tt.result, errors.New("disk err")
			}
			wrapped, _ := newApprovalMiddleware(nil).WrapInvokableToolCall(
				context.Background(), endpoint, &adk.ToolContext{Name: "read"})
			out, err := wrapped(context.Background(), `{}`)
			if err != nil {
				t.Fatalf("non-fatal error must be folded, got err=%v", err)
			}
			if out != tt.wantPrefix {
				t.Fatalf("folded output = %q, want %q", out, tt.wantPrefix)
			}
		})
	}
}

// Panics keep folding to a string (never the fatal channel): the panic source
// is unknown, so aborting the whole run would be an over-reaction.
func TestApprovalMiddleware_PanicNotFatal(t *testing.T) {
	endpoint := func(context.Context, string, ...tool.Option) (string, error) {
		panic("boom")
	}
	wrapped, _ := newApprovalMiddleware(nil).WrapInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "read"})
	out, err := wrapped(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("panic must fold to a string, got err=%v", err)
	}
	if !strings.Contains(out, "Tool execution panicked: boom") {
		t.Fatalf("expected panic message in output, got %q", out)
	}
}

func TestApprovalMiddleware_EnhancedApprovalAndToolCallID(t *testing.T) {
	var gotName, gotArgs, gotCallID string
	approval := func(ctx context.Context, name, args string) (bool, error) {
		gotName = name
		gotArgs = args
		gotCallID = ToolCallIDFromContext(ctx)
		return true, nil
	}
	want := enhancedResult("captured", true)
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		return want, nil
	}
	wrapped, _ := newApprovalMiddleware(approval).WrapEnhancedInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot", CallID: "call-shot"})
	got, err := wrapped(context.Background(), &schema.ToolArgument{Text: `{"app":"com.apple.Calculator"}`})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("successful enhanced result should pass through unchanged")
	}
	if gotName != "computer_screenshot" || gotArgs != `{"app":"com.apple.Calculator"}` || gotCallID != "call-shot" {
		t.Fatalf("approval saw name=%q args=%q callID=%q", gotName, gotArgs, gotCallID)
	}
}

func TestApprovalMiddleware_EnhancedRejectionBlocksTool(t *testing.T) {
	called := false
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		called = true
		return enhancedResult("secret", true), nil
	}
	wrapped, _ := newApprovalMiddleware(func(context.Context, string, string) (bool, error) {
		return false, nil
	}).WrapEnhancedInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	result, err := wrapped(context.Background(), &schema.ToolArgument{Text: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("rejected enhanced tool must not execute")
	}
	if text := toolResultText(result); !strings.Contains(text, "rejected by user") {
		t.Fatalf("unexpected rejection result: %q", text)
	}
}

func TestApprovalMiddleware_EnhancedAutoReviewDenial(t *testing.T) {
	called := false
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		called = true
		return nil, nil
	}
	wrapped, _ := newApprovalMiddleware(func(context.Context, string, string) (bool, error) {
		return false, &ReviewDeniedError{Reason: "screen policy"}
	}).WrapEnhancedInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	result, err := wrapped(context.Background(), &schema.ToolArgument{Text: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("review-denied enhanced tool must not execute")
	}
	text := toolResultText(result)
	if !strings.Contains(text, "automatic safety reviewer") || !strings.Contains(text, "screen policy") {
		t.Fatalf("unexpected review denial: %q", text)
	}
}

func TestApprovalMiddleware_EnhancedNonFatalErrorFolded(t *testing.T) {
	tests := []struct {
		name       string
		partial    *schema.ToolResult
		wantText   string
		wantImages int
	}{
		{name: "without partial", wantText: "Tool execution failed: capture err"},
		{name: "with multimodal partial", partial: enhancedResult("partial", true), wantText: "partial\n\nTool execution failed: capture err", wantImages: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
				return tt.partial, errors.New("capture err")
			}
			wrapped, _ := newApprovalMiddleware(nil).WrapEnhancedInvokableToolCall(
				context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
			result, err := wrapped(context.Background(), &schema.ToolArgument{Text: `{}`})
			if err != nil {
				t.Fatalf("non-fatal error must be folded: %v", err)
			}
			if text := toolResultText(result); text != tt.wantText {
				t.Fatalf("folded text=%q want=%q", text, tt.wantText)
			}
			if got := countImages(result); got != tt.wantImages {
				t.Fatalf("image parts=%d want=%d", got, tt.wantImages)
			}
		})
	}
}

func TestApprovalMiddleware_EnhancedFatalErrorAborts(t *testing.T) {
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		return enhancedResult("partial", true), tools.Fatal(errors.New("daemon gone"))
	}
	wrapped, _ := newApprovalMiddleware(nil).WrapEnhancedInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	result, err := wrapped(context.Background(), &schema.ToolArgument{Text: `{}`})
	if !tools.IsFatal(err) {
		t.Fatalf("fatal error must propagate, got %v", err)
	}
	if result != nil {
		t.Fatal("fatal error must discard partial enhanced result")
	}
}

func TestApprovalMiddleware_EnhancedPanicNotFatal(t *testing.T) {
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		panic("boom")
	}
	wrapped, _ := newApprovalMiddleware(nil).WrapEnhancedInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	result, err := wrapped(context.Background(), &schema.ToolArgument{Text: `{}`})
	if err != nil {
		t.Fatalf("panic must be folded, got %v", err)
	}
	if text := toolResultText(result); !strings.Contains(text, "Tool execution panicked: boom") {
		t.Fatalf("unexpected panic result: %q", text)
	}
}

func enhancedResult(text string, withImage bool) *schema.ToolResult {
	parts := []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: text}}
	if withImage {
		parts = append(parts, schema.ToolOutputPart{
			Type:  schema.ToolPartTypeImage,
			Image: &schema.ToolOutputImage{MessagePartCommon: schema.MessagePartCommon{MIMEType: "image/png"}},
		})
	}
	return &schema.ToolResult{Parts: parts}
}

func countImages(result *schema.ToolResult) int {
	if result == nil {
		return 0
	}
	n := 0
	for _, part := range result.Parts {
		if part.Type == schema.ToolPartTypeImage {
			n++
		}
	}
	return n
}
