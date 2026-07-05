package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"

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
