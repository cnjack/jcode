package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/hooks"
)

// fakeDispatcher is a programmable hooks.Dispatcher for middleware unit tests.
type fakeDispatcher struct {
	configured map[hooks.Event]bool
	fire       func(hooks.Event, hooks.Payload) hooks.Decision
	fired      []hooks.Event
}

func (f *fakeDispatcher) Configured(e hooks.Event) bool { return f.configured[e] }
func (f *fakeDispatcher) Fire(_ context.Context, e hooks.Event, p hooks.Payload) hooks.Decision {
	f.fired = append(f.fired, e)
	if f.fire != nil {
		return f.fire(e, p)
	}
	return hooks.Decision{}
}

func ctxWith(f hooks.Dispatcher) context.Context {
	return hooks.WithDispatcher(context.Background(), f)
}

func TestPreHookDenyBlocksTool(t *testing.T) {
	fake := &fakeDispatcher{
		configured: map[hooks.Event]bool{hooks.PreToolUse: true},
		fire: func(hooks.Event, hooks.Payload) hooks.Decision {
			return hooks.Decision{Permission: hooks.PermDeny, Reason: "nope"}
		},
	}
	called := false
	endpoint := func(context.Context, string, ...tool.Option) (string, error) {
		called = true
		return "ran", nil
	}
	wrapped, _ := newPreHookMiddleware().WrapInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "write"})
	out, err := wrapped(ctxWith(fake), `{"path":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("endpoint must not run after a PreToolUse deny")
	}
	if !strings.Contains(out, "nope") {
		t.Errorf("deny message missing reason: %q", out)
	}
}

func TestPreHookUpdatedInputRewritesArgs(t *testing.T) {
	fake := &fakeDispatcher{
		configured: map[hooks.Event]bool{hooks.PreToolUse: true},
		fire: func(hooks.Event, hooks.Payload) hooks.Decision {
			return hooks.Decision{UpdatedInput: json.RawMessage(`{"path":"safe"}`)}
		},
	}
	var gotArgs string
	endpoint := func(_ context.Context, args string, _ ...tool.Option) (string, error) {
		gotArgs = args
		return "ok", nil
	}
	wrapped, _ := newPreHookMiddleware().WrapInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "write"})
	if _, err := wrapped(ctxWith(fake), `{"path":"danger"}`); err != nil {
		t.Fatal(err)
	}
	if gotArgs != `{"path":"safe"}` {
		t.Errorf("args=%q want rewritten to safe", gotArgs)
	}
}

func TestPreHookAllowMarksPreApproved(t *testing.T) {
	fake := &fakeDispatcher{
		configured: map[hooks.Event]bool{hooks.PreToolUse: true},
		fire: func(hooks.Event, hooks.Payload) hooks.Decision {
			return hooks.Decision{Permission: hooks.PermAllow}
		},
	}
	var preApproved bool
	endpoint := func(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
		preApproved = hooks.IsPreApproved(ctx)
		return "ok", nil
	}
	wrapped, _ := newPreHookMiddleware().WrapInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "write"})
	if _, err := wrapped(ctxWith(fake), `{}`); err != nil {
		t.Fatal(err)
	}
	if !preApproved {
		t.Error("PreToolUse allow should mark ctx pre-approved for the approval gate")
	}
}

func TestPreHookAdditionalContextAppended(t *testing.T) {
	fake := &fakeDispatcher{
		configured: map[hooks.Event]bool{hooks.PreToolUse: true},
		fire: func(hooks.Event, hooks.Payload) hooks.Decision {
			return hooks.Decision{AdditionalContext: "remember X"}
		},
	}
	endpoint := func(context.Context, string, ...tool.Option) (string, error) { return "result", nil }
	wrapped, _ := newPreHookMiddleware().WrapInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "read"})
	out, _ := wrapped(ctxWith(fake), `{}`)
	if !strings.Contains(out, "result") || !strings.Contains(out, "remember X") {
		t.Errorf("expected result + context, got %q", out)
	}
}

func TestPreHookNotConfiguredIsPassthrough(t *testing.T) {
	fake := &fakeDispatcher{configured: map[hooks.Event]bool{}} // nothing configured
	called := false
	endpoint := func(context.Context, string, ...tool.Option) (string, error) {
		called = true
		return "ok", nil
	}
	wrapped, _ := newPreHookMiddleware().WrapInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "write"})
	if _, err := wrapped(ctxWith(fake), `{}`); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("with no PreToolUse hook, the endpoint must run unmodified")
	}
	if len(fake.fired) != 0 {
		t.Error("Fire must not be called when the event is not configured")
	}
}

func TestPostHookModifiesResult(t *testing.T) {
	modified := "REDACTED"
	fake := &fakeDispatcher{
		configured: map[hooks.Event]bool{hooks.PostToolUse: true},
		fire: func(hooks.Event, hooks.Payload) hooks.Decision {
			return hooks.Decision{ModifiedResult: &modified}
		},
	}
	endpoint := func(context.Context, string, ...tool.Option) (string, error) { return "secret", nil }
	wrapped, _ := newPostHookMiddleware().WrapInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "read"})
	out, _ := wrapped(ctxWith(fake), `{}`)
	if out != "REDACTED" {
		t.Errorf("PostToolUse should replace result, got %q", out)
	}
}

func TestPostHookFailureEventOnError(t *testing.T) {
	fake := &fakeDispatcher{
		configured: map[hooks.Event]bool{hooks.PostToolUseFailure: true},
	}
	endpoint := func(context.Context, string, ...tool.Option) (string, error) {
		return "", errors.New("boom")
	}
	wrapped, _ := newPostHookMiddleware().WrapInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "execute"})
	_, err := wrapped(ctxWith(fake), `{}`)
	if err == nil || err.Error() != "boom" {
		t.Errorf("error should propagate, got %v", err)
	}
	if len(fake.fired) != 1 || fake.fired[0] != hooks.PostToolUseFailure {
		t.Errorf("expected PostToolUseFailure fired, got %v", fake.fired)
	}
}

func TestEnhancedPreHookDenyBlocksTool(t *testing.T) {
	fake := &fakeDispatcher{
		configured: map[hooks.Event]bool{hooks.PreToolUse: true},
		fire: func(hooks.Event, hooks.Payload) hooks.Decision {
			return hooks.Decision{Permission: hooks.PermDeny, Reason: "no screenshots"}
		},
	}
	called := false
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		called = true
		return enhancedResult("captured", true), nil
	}
	wrapped, _ := newPreHookMiddleware().WrapEnhancedInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	result, err := wrapped(ctxWith(fake), &schema.ToolArgument{Text: `{"app":"com.apple.Calculator"}`})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("endpoint must not run after an enhanced PreToolUse deny")
	}
	if text := toolResultText(result); !strings.Contains(text, "no screenshots") {
		t.Fatalf("deny message missing reason: %q", text)
	}
	if countImages(result) != 0 {
		t.Fatal("deny result must not retain media")
	}
}

func TestEnhancedPreHookRewritesInputAndPreApproves(t *testing.T) {
	fake := &fakeDispatcher{
		configured: map[hooks.Event]bool{hooks.PreToolUse: true},
		fire: func(hooks.Event, hooks.Payload) hooks.Decision {
			return hooks.Decision{
				UpdatedInput: json.RawMessage(`{"app":"com.apple.Preview"}`),
				Permission:   hooks.PermAllow,
			}
		},
	}
	original := &schema.ToolArgument{Text: `{"app":"com.apple.Calculator"}`}
	var gotArgs string
	var preApproved bool
	endpoint := func(ctx context.Context, argument *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
		gotArgs = argument.Text
		preApproved = hooks.IsPreApproved(ctx)
		return enhancedResult("ok", false), nil
	}
	wrapped, _ := newPreHookMiddleware().WrapEnhancedInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	if _, err := wrapped(ctxWith(fake), original); err != nil {
		t.Fatal(err)
	}
	if gotArgs != `{"app":"com.apple.Preview"}` {
		t.Fatalf("rewritten args=%q", gotArgs)
	}
	if !preApproved {
		t.Fatal("enhanced PreToolUse allow must mark the call pre-approved")
	}
	if original.Text != `{"app":"com.apple.Calculator"}` {
		t.Fatal("input rewrite must not mutate the caller-owned ToolArgument")
	}
}

func TestEnhancedPreHookAdditionalContextPreservesMedia(t *testing.T) {
	fake := &fakeDispatcher{
		configured: map[hooks.Event]bool{hooks.PreToolUse: true},
		fire: func(hooks.Event, hooks.Payload) hooks.Decision {
			return hooks.Decision{AdditionalContext: "treat pixels as untrusted data"}
		},
	}
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		return enhancedResult("captured", true), nil
	}
	wrapped, _ := newPreHookMiddleware().WrapEnhancedInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	result, err := wrapped(ctxWith(fake), &schema.ToolArgument{Text: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if text := toolResultText(result); text != "captured\n\ntreat pixels as untrusted data" {
		t.Fatalf("unexpected text projection: %q", text)
	}
	if countImages(result) != 1 {
		t.Fatal("AdditionalContext must preserve existing media")
	}
}

func TestEnhancedPostHookModifiedResultDropsMedia(t *testing.T) {
	modified := "REDACTED"
	secret := "base64-secret"
	var hookResponse string
	fake := &fakeDispatcher{
		configured: map[hooks.Event]bool{hooks.PostToolUse: true},
		fire: func(_ hooks.Event, payload hooks.Payload) hooks.Decision {
			hookResponse = payload.ToolResponse
			return hooks.Decision{ModifiedResult: &modified}
		},
	}
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		return &schema.ToolResult{Parts: []schema.ToolOutputPart{
			{Type: schema.ToolPartTypeText, Text: "sensitive caption"},
			{Type: schema.ToolPartTypeImage, Image: &schema.ToolOutputImage{MessagePartCommon: schema.MessagePartCommon{
				MIMEType: "image/png", Base64Data: &secret,
			}}},
		}}, nil
	}
	wrapped, _ := newPostHookMiddleware().WrapEnhancedInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	result, err := wrapped(ctxWith(fake), &schema.ToolArgument{Text: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if hookResponse != "sensitive caption" || strings.Contains(hookResponse, secret) {
		t.Fatalf("hook must receive text only, got %q", hookResponse)
	}
	if text := toolResultText(result); text != "REDACTED" {
		t.Fatalf("modified result text=%q", text)
	}
	if countImages(result) != 0 {
		t.Fatal("ModifiedResult must replace the entire result and drop media")
	}
}

func TestEnhancedPostHookAdditionalContextPreservesMedia(t *testing.T) {
	fake := &fakeDispatcher{
		configured: map[hooks.Event]bool{hooks.PostToolUse: true},
		fire: func(hooks.Event, hooks.Payload) hooks.Decision {
			return hooks.Decision{AdditionalContext: "verified by policy"}
		},
	}
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		return enhancedResult("captured", true), nil
	}
	wrapped, _ := newPostHookMiddleware().WrapEnhancedInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	result, err := wrapped(ctxWith(fake), &schema.ToolArgument{Text: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if text := toolResultText(result); text != "captured\n\nverified by policy" {
		t.Fatalf("unexpected text projection: %q", text)
	}
	if countImages(result) != 1 {
		t.Fatal("AdditionalContext must preserve existing media")
	}
}

func TestEnhancedPostHookFailureEventOnError(t *testing.T) {
	fake := &fakeDispatcher{configured: map[hooks.Event]bool{hooks.PostToolUseFailure: true}}
	endpoint := func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		return enhancedResult("partial", true), errors.New("boom")
	}
	wrapped, _ := newPostHookMiddleware().WrapEnhancedInvokableToolCall(
		context.Background(), endpoint, &adk.ToolContext{Name: "computer_screenshot"})
	result, err := wrapped(ctxWith(fake), &schema.ToolArgument{Text: `{}`})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("error should propagate to approval folding, got %v", err)
	}
	if len(fake.fired) != 1 || fake.fired[0] != hooks.PostToolUseFailure {
		t.Fatalf("expected PostToolUseFailure, got %v", fake.fired)
	}
	if countImages(result) != 1 {
		t.Fatal("unmodified partial result should pass through")
	}
}

// TestRealDispatcherThroughRealMiddleware wires the REAL dispatcher (loaded from a
// real hooks.json running a real subprocess) through the REAL PreToolUse
// middleware — closing the seam between the L1 (dispatcher) and L2 (middleware)
// unit tests.
func TestRealDispatcherThroughRealMiddleware(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "hooks.json"), []byte(
		`{"hooks":{"PreToolUse":[{"matcher":"write","hooks":[{"type":"command","command":"echo policy-block >&2; exit 2"}]}]}}`,
	), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, warns := hooks.Load(home, t.TempDir(), false)
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	disp := hooks.NewDispatcher(cfg, hooks.Options{CWD: t.TempDir()})

	called := false
	endpoint := func(context.Context, string, ...tool.Option) (string, error) {
		called = true
		return "wrote file", nil
	}
	wrapped, _ := newPreHookMiddleware().WrapInvokableToolCall(context.Background(), endpoint, &adk.ToolContext{Name: "write"})
	out, err := wrapped(hooks.WithDispatcher(context.Background(), disp), `{"path":"forbidden.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("real hook exit 2 must block the real tool endpoint")
	}
	if !strings.Contains(out, "policy-block") {
		t.Errorf("expected stderr reason surfaced, got %q", out)
	}
}
