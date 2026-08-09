package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/toolpolicy"
)

type providerBillableCallOptions struct {
	marker string
}

type providerBillableInvokableStub struct {
	info         *schema.ToolInfo
	result       string
	err          error
	calls        atomic.Int32
	mu           sync.Mutex
	args         string
	optionMarker string
}

func (s *providerBillableInvokableStub) Info(context.Context) (*schema.ToolInfo, error) {
	return s.info, nil
}

func (s *providerBillableInvokableStub) InvokableRun(
	_ context.Context,
	args string,
	opts ...tool.Option,
) (string, error) {
	s.calls.Add(1)
	options := tool.GetImplSpecificOptions(&providerBillableCallOptions{}, opts...)
	s.mu.Lock()
	s.args = args
	s.optionMarker = options.marker
	s.mu.Unlock()
	return s.result, s.err
}

type providerBillableEnhancedStub struct {
	info     *schema.ToolInfo
	result   *schema.ToolResult
	err      error
	calls    atomic.Int32
	argument *schema.ToolArgument
}

func (s *providerBillableEnhancedStub) Info(context.Context) (*schema.ToolInfo, error) {
	return s.info, nil
}

func (s *providerBillableEnhancedStub) InvokableRun(
	_ context.Context,
	argument *schema.ToolArgument,
	_ ...tool.Option,
) (*schema.ToolResult, error) {
	s.calls.Add(1)
	s.argument = argument
	return s.result, s.err
}

type providerBillableRecorderStub struct {
	mu       sync.Mutex
	id       string
	entries  []session.ProviderToolOperation
	calls    int
	failCall int
}

func (r *providerBillableRecorderStub) UUID() string { return r.id }

func (r *providerBillableRecorderStub) RecordProviderToolOperation(
	operation session.ProviderToolOperation,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.failCall > 0 && r.calls == r.failCall {
		return errors.New("journal unavailable")
	}
	r.entries = append(r.entries, operation)
	return nil
}

func (r *providerBillableRecorderStub) RecordProviderToolDispatch(
	operation session.ProviderToolOperation,
	_ session.DispatchPolicy,
) error {
	return r.RecordProviderToolOperation(operation)
}

func (r *providerBillableRecorderStub) setFailCall(call int) {
	r.mu.Lock()
	r.failCall = call
	r.mu.Unlock()
}

func (r *providerBillableRecorderStub) snapshot() []session.ProviderToolOperation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]session.ProviderToolOperation(nil), r.entries...)
}

func TestProviderManagedBillableToolPreservesInfoOptionsAndJournals(t *testing.T) {
	endpoint := &providerBillableInvokableStub{
		info:   &schema.ToolInfo{Name: "mcp__provider__web_search_prime", Desc: "search"},
		result: `{"ok":true}`,
	}
	recorder := &providerBillableRecorderStub{id: "session-search-1"}
	base, err := WrapProviderManagedBillableTool(
		context.Background(), endpoint, providerBillableDeps(recorder, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	info, err := base.Info(context.Background())
	if err != nil || info != endpoint.info {
		t.Fatalf("Info = %p, %v; want original %p", info, err, endpoint.info)
	}
	wrapped, ok := base.(tool.InvokableTool)
	if !ok {
		t.Fatalf("wrapped type %T is not InvokableTool", base)
	}
	preparer, ok := base.(toolpolicy.BillableIntentPreparer)
	if !ok {
		t.Fatalf("wrapped type %T is not BillableIntentPreparer", base)
	}
	intent, err := preparer.PrepareBillableIntent(
		context.Background(), `{"query":"jcode","page":1}`, "call-search-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := toolpolicy.WithRunID(context.Background(), "turn-1")
	ctx = toolpolicy.WithBillableIntent(ctx, intent)
	option := tool.WrapImplSpecificOptFn(func(options *providerBillableCallOptions) {
		options.marker = "kept"
	})
	result, err := wrapped.InvokableRun(ctx, `{"page":1,"query":"jcode"}`, option)
	if err != nil || result != endpoint.result {
		t.Fatalf("result=%q err=%v", result, err)
	}
	endpoint.mu.Lock()
	gotArgs, gotMarker := endpoint.args, endpoint.optionMarker
	endpoint.mu.Unlock()
	if gotArgs != `{"page":1,"query":"jcode"}` || gotMarker != "kept" {
		t.Fatalf("delegation args=%q option=%q", gotArgs, gotMarker)
	}
	entries := recorder.snapshot()
	if len(entries) != 2 || entries[0].State != session.ProviderToolDispatchAttempted ||
		entries[1].State != session.ProviderToolSucceeded || entries[0].IntentHash == "" ||
		entries[0].IntentHash != entries[1].IntentHash {
		t.Fatalf("journal = %#v", entries)
	}
}

func TestProviderManagedBillableToolFailsClosedBeforeDispatchAndReleasesReservation(t *testing.T) {
	endpoint := &providerBillableInvokableStub{
		info: &schema.ToolInfo{Name: "web_search_prime"}, result: "ok",
	}
	recorder := &providerBillableRecorderStub{id: "session-search-1", failCall: 1}
	base, err := WrapProviderManagedBillableTool(
		context.Background(), endpoint, providerBillableDeps(recorder, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := base.(tool.InvokableTool)
	preparer := base.(toolpolicy.BillableIntentPreparer)
	intent, err := preparer.PrepareBillableIntent(context.Background(), `{"query":"first"}`, "call-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := toolpolicy.WithRunID(context.Background(), "turn-1")
	ctx = toolpolicy.WithBillableIntent(ctx, intent)
	if _, err := wrapped.InvokableRun(ctx, `{"query":"first"}`); err == nil ||
		!strings.Contains(err.Error(), "was not dispatched") {
		t.Fatalf("journal failure err = %v", err)
	}
	if endpoint.calls.Load() != 0 {
		t.Fatalf("endpoint calls after journal failure = %d", endpoint.calls.Load())
	}

	recorder.setFailCall(0)
	intent, err = preparer.PrepareBillableIntent(context.Background(), `{"query":"second"}`, "call-2")
	if err != nil {
		t.Fatal(err)
	}
	ctx = toolpolicy.WithRunID(context.Background(), "turn-1")
	ctx = toolpolicy.WithBillableIntent(ctx, intent)
	if _, err := wrapped.InvokableRun(ctx, `{"query":"second"}`); err != nil {
		t.Fatalf("reservation was not released: %v", err)
	}
	if endpoint.calls.Load() != 1 {
		t.Fatalf("endpoint calls = %d", endpoint.calls.Load())
	}
}

func TestProviderManagedBillableToolHardLimitIsAtomic(t *testing.T) {
	endpoint := &providerBillableInvokableStub{
		info: &schema.ToolInfo{Name: "web_search_prime"}, result: "ok",
	}
	recorder := &providerBillableRecorderStub{id: "session-search-1"}
	base, err := WrapProviderManagedBillableTool(
		context.Background(), endpoint, providerBillableDeps(recorder, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := base.(tool.InvokableTool)
	preparer := base.(toolpolicy.BillableIntentPreparer)
	intents := make([]toolpolicy.BillableIntent, 2)
	for index := range intents {
		intents[index], err = preparer.PrepareBillableIntent(
			context.Background(), `{"query":"same"}`, "call-"+string(rune('a'+index)),
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	errorsOut := make(chan error, 2)
	for _, intent := range intents {
		intent := intent
		go func() {
			<-start
			ctx := toolpolicy.WithRunID(context.Background(), "same-turn")
			ctx = toolpolicy.WithBillableIntent(ctx, intent)
			_, invokeErr := wrapped.InvokableRun(ctx, `{"query":"same"}`)
			errorsOut <- invokeErr
		}()
	}
	close(start)
	var success, limited int
	for range 2 {
		invokeErr := <-errorsOut
		switch {
		case invokeErr == nil:
			success++
		case strings.Contains(invokeErr.Error(), "limit reached"):
			limited++
		default:
			t.Fatalf("unexpected invoke error: %v", invokeErr)
		}
	}
	if success != 1 || limited != 1 || endpoint.calls.Load() != 1 {
		t.Fatalf("success=%d limited=%d calls=%d", success, limited, endpoint.calls.Load())
	}
	entries := recorder.snapshot()
	if len(entries) != 2 || entries[0].State != session.ProviderToolDispatchAttempted ||
		entries[1].State != session.ProviderToolSucceeded {
		t.Fatalf("journal = %#v", entries)
	}
}

func TestProviderManagedRepeatedModelToolCallIDGetsUniqueHostOperations(t *testing.T) {
	endpoint := &providerBillableInvokableStub{info: &schema.ToolInfo{Name: "web_search_prime"}}
	recorder := &providerBillableRecorderStub{id: "session-search-1"}
	base, err := WrapProviderManagedBillableTool(
		context.Background(), endpoint, providerBillableDeps(recorder, 2),
	)
	if err != nil {
		t.Fatal(err)
	}
	preparer := base.(toolpolicy.BillableIntentPreparer)
	first, err := preparer.PrepareBillableIntent(context.Background(), `{"query":"same"}`, "reused-call")
	if err != nil {
		t.Fatal(err)
	}
	second, err := preparer.PrepareBillableIntent(context.Background(), `{"query":"same"}`, "reused-call")
	if err != nil {
		t.Fatal(err)
	}
	if first.OperationID == second.OperationID || first.ToolCallID != "reused-call" || second.ToolCallID != "reused-call" {
		t.Fatalf("operation identities first=%#v second=%#v", first, second)
	}
	for index, intent := range []toolpolicy.BillableIntent{first, second} {
		ctx := toolpolicy.WithRunID(context.Background(), "turn-"+string(rune('a'+index)))
		ctx = toolpolicy.WithBillableIntent(ctx, intent)
		if _, err := base.(tool.InvokableTool).InvokableRun(ctx, `{"query":"same"}`); err != nil {
			t.Fatal(err)
		}
	}
	entries := recorder.snapshot()
	if len(entries) != 4 || entries[0].OperationID == entries[2].OperationID ||
		entries[0].ToolCallID != "reused-call" || entries[2].ToolCallID != "reused-call" {
		t.Fatalf("journal identities = %#v", entries)
	}
}

func TestProviderManagedBillableToolClassifiesKnownAndUncertainFailures(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantState session.ProviderToolOperationState
		wantCode  string
	}{
		{
			name:      "known provider rejection",
			err:       errors.New("failed to call mcp tool, mcp server return error: redacted"),
			wantState: session.ProviderToolFailed, wantCode: "provider_rejected",
		},
		{
			name:      "known provider http rejection",
			err:       errors.New("failed to call mcp tool: request failed with status 401: redacted"),
			wantState: session.ProviderToolFailed, wantCode: "provider_http_error",
		},
		{
			name:      "network outcome uncertain",
			err:       &url.Error{Op: "Post", URL: "https://redacted.invalid", Err: errors.New("connection reset")},
			wantState: session.ProviderToolUncertain, wantCode: "transport_uncertain",
		},
		{
			name:      "context ended after dispatch",
			err:       context.Canceled,
			wantState: session.ProviderToolUncertain, wantCode: "context_ended",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := &providerBillableInvokableStub{
				info: &schema.ToolInfo{Name: "web_search_prime"}, err: tt.err,
			}
			recorder := &providerBillableRecorderStub{id: "session-search-1"}
			base, err := WrapProviderManagedBillableTool(
				context.Background(), endpoint, providerBillableDeps(recorder, 1),
			)
			if err != nil {
				t.Fatal(err)
			}
			intent, err := base.(toolpolicy.BillableIntentPreparer).PrepareBillableIntent(
				context.Background(), `{"query":"jcode"}`, "call-1",
			)
			if err != nil {
				t.Fatal(err)
			}
			ctx := toolpolicy.WithBillableIntent(context.Background(), intent)
			_, gotErr := base.(tool.InvokableTool).InvokableRun(ctx, `{"query":"jcode"}`)
			if gotErr == nil || (tt.err != context.Canceled &&
				strings.Contains(gotErr.Error(), tt.err.Error())) {
				t.Fatalf("call error was not sanitized: %v", gotErr)
			}
			if tt.err == context.Canceled && !errors.Is(gotErr, context.Canceled) {
				t.Fatalf("sanitized cancellation lost identity: %v", gotErr)
			}
			entries := recorder.snapshot()
			if len(entries) != 2 || entries[1].State != tt.wantState ||
				entries[1].ErrorCode != tt.wantCode {
				t.Fatalf("journal = %#v", entries)
			}
		})
	}
}

func TestProviderManagedBillableToolDoesNotExposeUpstreamErrorBody(t *testing.T) {
	const canary = "Authorization: Bearer credential-canary"
	endpoint := &providerBillableInvokableStub{
		info:   &schema.ToolInfo{Name: "web_search_prime"},
		result: `{"isError":true,"content":[{"type":"text","text":"` + canary + `"}]}`,
	}
	recorder := &providerBillableRecorderStub{id: "session-search-redaction"}
	base, err := WrapProviderManagedBillableTool(
		context.Background(), endpoint, providerBillableDeps(recorder, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := base.(toolpolicy.BillableIntentPreparer).PrepareBillableIntent(
		context.Background(), `{"query":"jcode"}`, "call-redaction",
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := toolpolicy.WithBillableIntent(context.Background(), intent)
	result, callErr := base.(tool.InvokableTool).InvokableRun(ctx, `{"query":"jcode"}`)
	if callErr == nil || result != "" {
		t.Fatalf("result=%q error=%v", result, callErr)
	}
	encoded, err := json.Marshal(struct {
		Error   string
		Entries []session.ProviderToolOperation
	}{Error: callErr.Error(), Entries: recorder.snapshot()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), canary) || strings.Contains(string(encoded), "Bearer") {
		t.Fatalf("upstream secret escaped wrapper: %s", encoded)
	}
	entries := recorder.snapshot()
	if len(entries) != 2 || entries[1].State != session.ProviderToolFailed ||
		entries[1].ErrorCode != "provider_rejected" {
		t.Fatalf("journal = %#v", entries)
	}
}

func TestProviderManagedBillableToolIntentMismatchDoesNotDispatch(t *testing.T) {
	endpoint := &providerBillableInvokableStub{
		info: &schema.ToolInfo{Name: "web_search_prime"}, result: "ok",
	}
	recorder := &providerBillableRecorderStub{id: "session-search-1"}
	base, err := WrapProviderManagedBillableTool(
		context.Background(), endpoint, providerBillableDeps(recorder, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := base.(toolpolicy.BillableIntentPreparer).PrepareBillableIntent(
		context.Background(), `{"query":"approved"}`, "call-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := toolpolicy.WithBillableIntent(context.Background(), intent)
	if _, err := base.(tool.InvokableTool).InvokableRun(ctx, `{"query":"changed"}`); err == nil ||
		!strings.Contains(err.Error(), "no longer matches") {
		t.Fatalf("intent mismatch err = %v", err)
	}
	if endpoint.calls.Load() != 0 || len(recorder.snapshot()) != 0 {
		t.Fatalf("calls=%d journal=%#v", endpoint.calls.Load(), recorder.snapshot())
	}
}

func TestProviderManagedRuntimeVerificationFailsBeforeReservationJournalAndDispatch(t *testing.T) {
	endpoint := &providerBillableInvokableStub{
		info: &schema.ToolInfo{Name: "web_search_prime"}, result: "ok",
	}
	recorder := &providerBillableRecorderStub{id: "session-search-runtime"}
	deps := providerBillableDeps(recorder, 1)
	var allowed atomic.Bool
	deps.VerifyRuntime = func(context.Context) error {
		if !allowed.Load() {
			return errors.New("runtime changed")
		}
		return nil
	}
	base, err := WrapProviderManagedBillableTool(context.Background(), endpoint, deps)
	if err != nil {
		t.Fatal(err)
	}
	preparer := base.(toolpolicy.BillableIntentPreparer)
	first, err := preparer.PrepareBillableIntent(
		context.Background(), `{"query":"first"}`, "call-runtime-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	firstCtx := toolpolicy.WithRunID(context.Background(), "same-turn")
	firstCtx = toolpolicy.WithBillableIntent(firstCtx, first)
	if _, err := base.(tool.InvokableTool).InvokableRun(
		firstCtx, `{"query":"first"}`,
	); err == nil || !strings.Contains(err.Error(), "runtime configuration") {
		t.Fatalf("runtime verification error = %v", err)
	}
	if endpoint.calls.Load() != 0 || len(recorder.snapshot()) != 0 {
		t.Fatalf("pre-verification calls=%d journal=%#v", endpoint.calls.Load(), recorder.snapshot())
	}

	// The failed verifier ran before ReserveRun: after the runtime is valid, a
	// separate operation in the same run still has the full per-run allowance.
	allowed.Store(true)
	second, err := preparer.PrepareBillableIntent(
		context.Background(), `{"query":"second"}`, "call-runtime-2",
	)
	if err != nil {
		t.Fatal(err)
	}
	secondCtx := toolpolicy.WithRunID(context.Background(), "same-turn")
	secondCtx = toolpolicy.WithBillableIntent(secondCtx, second)
	if _, err := base.(tool.InvokableTool).InvokableRun(
		secondCtx, `{"query":"second"}`,
	); err != nil {
		t.Fatalf("runtime verifier consumed reservation: %v", err)
	}
	if endpoint.calls.Load() != 1 || len(recorder.snapshot()) != 2 {
		t.Fatalf("post-verification calls=%d journal=%#v", endpoint.calls.Load(), recorder.snapshot())
	}
}

func TestProviderManagedBillableToolPreservesEnhancedInterface(t *testing.T) {
	result := &schema.ToolResult{}
	endpoint := &providerBillableEnhancedStub{
		info: &schema.ToolInfo{Name: "web_search_prime"}, result: result,
	}
	recorder := &providerBillableRecorderStub{id: "session-search-1"}
	base, err := WrapProviderManagedBillableTool(
		context.Background(), endpoint, providerBillableDeps(recorder, 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, ok := base.(tool.EnhancedInvokableTool)
	if !ok {
		t.Fatalf("wrapped type %T is not EnhancedInvokableTool", base)
	}
	argument := &schema.ToolArgument{Text: `{"query":"jcode"}`}
	intent, err := base.(toolpolicy.BillableIntentPreparer).PrepareBillableIntent(
		context.Background(), argument.Text, "call-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := toolpolicy.WithBillableIntent(context.Background(), intent)
	got, err := wrapped.InvokableRun(ctx, argument)
	if err != nil || got != result || endpoint.argument != argument || endpoint.calls.Load() != 1 {
		t.Fatalf("result=%p err=%v argument=%p calls=%d", got, err, endpoint.argument, endpoint.calls.Load())
	}
}

func TestNewProviderToolUsageLedgerReplaysDispatchedCount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := session.NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	operation := session.ProviderToolOperation{
		OperationID: "operation-1", ToolCallID: "call-1",
		RunID: "turn-1",
		State: session.ProviderToolDispatchAttempted, CapabilityKey: toolpolicy.CapabilityWebSearch,
		ProviderProfileID: "zhipuai-coding-plan", ToolName: "web_search_prime",
		IntentHash: "intent-hash", ConfigEpoch: "epoch", IdempotencyKey: "idempotency",
	}
	if err := recorder.RecordProviderToolOperation(operation); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewProviderToolUsageLedger(
		recorder.UUID(), toolpolicy.CapabilityWebSearch, "zhipuai-coding-plan", 1, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Reserve("new-turn", "operation-2"); err == nil ||
		!strings.Contains(err.Error(), "session") {
		t.Fatalf("replayed session limit err = %v", err)
	}

	brandNew, err := NewProviderToolUsageLedger(
		"brand-new-session", toolpolicy.CapabilityWebSearch,
		"zhipuai-coding-plan", 1, 1,
	)
	if err != nil {
		t.Fatalf("brand new lazy session: %v", err)
	}
	if _, err := brandNew.Reserve("turn", "operation"); err != nil {
		t.Fatalf("brand new reserve: %v", err)
	}
	if _, err := NewProviderToolUsageLedger(
		"brand-new-session", toolpolicy.CapabilityWebSearch,
		"zhipuai-coding-plan", 0, 10,
	); err == nil {
		t.Fatal("zero per-run limit did not fail closed")
	}
}

func providerBillableDeps(
	recorder ProviderToolOperationRecorder,
	maxPerRun int,
) ProviderManagedToolDeps {
	return ProviderManagedToolDeps{
		Recorder: recorder, Ledger: toolpolicy.NewUsageLedger(maxPerRun, 20, 0),
		CapabilityKey:     toolpolicy.CapabilityWebSearch,
		ProviderProfileID: "zhipuai-coding-plan", ModelLabel: "web_search_prime",
		CredentialFingerprint: "credential-fingerprint", ConfigEpoch: "config-epoch",
		DispatchPolicy: session.DispatchPolicy{
			Tool: session.SessionToolWebSearch, MaxPerSession: 20,
		},
		VerifyRuntime: func(context.Context) error { return nil },
	}
}
