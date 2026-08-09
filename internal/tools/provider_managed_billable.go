package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/cnjack/jcode/internal/session"
	"github.com/cnjack/jcode/internal/toolpolicy"
)

// ProviderToolOperationRecorder is the narrow durable boundary required by a
// billable provider-managed tool. *session.Recorder implements it.
type ProviderToolOperationRecorder interface {
	UUID() string
	RecordProviderToolOperation(session.ProviderToolOperation) error
	RecordProviderToolDispatch(session.ProviderToolOperation, session.DispatchPolicy) error
}

// ProviderManagedToolDeps binds a provider-owned endpoint to the exact
// credential/config snapshot authorized by the approval middleware and session
// mode. Only the
// fingerprint is held in memory; the durable journal stores a further intent
// hash and never stores arguments, credentials, URLs, bodies, or results.
type ProviderManagedToolDeps struct {
	Recorder              ProviderToolOperationRecorder
	Ledger                *toolpolicy.UsageLedger
	CapabilityKey         string
	ProviderProfileID     string
	ModelLabel            string
	CredentialFingerprint string
	ConfigEpoch           string
	DispatchPolicy        session.DispatchPolicy
	VerifyRuntime         func(context.Context) error
}

type providerManagedToolCore struct {
	info *schema.ToolInfo
	deps ProviderManagedToolDeps
}

type providerManagedInvokableTool struct {
	*providerManagedToolCore
	endpoint tool.InvokableTool
}

type providerManagedEnhancedInvokableTool struct {
	*providerManagedToolCore
	endpoint tool.EnhancedInvokableTool
}

// WrapProviderManagedBillableTool preserves the endpoint's ToolInfo and its
// invokable interface while adding immutable authorization binding, atomic limits,
// and a synchronous provider_tool_operation journal. It never retries the
// endpoint. The endpoint should already have its final canonical MCP name.
func WrapProviderManagedBillableTool(
	ctx context.Context,
	endpoint tool.BaseTool,
	deps ProviderManagedToolDeps,
) (tool.BaseTool, error) {
	if endpoint == nil {
		return nil, fmt.Errorf("provider-managed endpoint is required")
	}
	if deps.Recorder == nil || deps.Ledger == nil ||
		strings.TrimSpace(deps.CapabilityKey) == "" ||
		strings.TrimSpace(deps.ProviderProfileID) == "" ||
		strings.TrimSpace(deps.CredentialFingerprint) == "" ||
		strings.TrimSpace(deps.ConfigEpoch) == "" ||
		deps.DispatchPolicy.Tool != session.SessionToolWebSearch ||
		deps.DispatchPolicy.MaxPerSession <= 0 || deps.VerifyRuntime == nil {
		return nil, fmt.Errorf("provider-managed tool dependencies are incomplete")
	}
	info, err := endpoint.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("read provider-managed tool info: %w", err)
	}
	if info == nil || strings.TrimSpace(info.Name) == "" {
		return nil, fmt.Errorf("provider-managed tool name is required")
	}
	if strings.TrimSpace(deps.ModelLabel) == "" {
		deps.ModelLabel = info.Name
	}
	core := &providerManagedToolCore{info: info, deps: deps}
	if invokable, ok := endpoint.(tool.InvokableTool); ok {
		return &providerManagedInvokableTool{providerManagedToolCore: core, endpoint: invokable}, nil
	}
	if enhanced, ok := endpoint.(tool.EnhancedInvokableTool); ok {
		return &providerManagedEnhancedInvokableTool{providerManagedToolCore: core, endpoint: enhanced}, nil
	}
	return nil, fmt.Errorf("provider-managed tool %q is not invokable", info.Name)
}

func (t *providerManagedToolCore) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *providerManagedToolCore) PrepareBillableIntent(
	_ context.Context,
	argsJSON, toolCallID string,
) (toolpolicy.BillableIntent, error) {
	operationID, err := toolpolicy.NewOperationID()
	if err != nil {
		return toolpolicy.BillableIntent{}, err
	}
	return t.prepareBillableIntent(argsJSON, toolCallID, operationID)
}

func (t *providerManagedToolCore) prepareBillableIntent(
	argsJSON, toolCallID, operationID string,
) (toolpolicy.BillableIntent, error) {
	normalized, err := normalizeProviderToolArgs(argsJSON)
	if err != nil {
		return toolpolicy.BillableIntent{}, err
	}
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return toolpolicy.BillableIntent{}, fmt.Errorf("provider tool call ID is required")
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return toolpolicy.BillableIntent{}, fmt.Errorf("provider operation ID is required")
	}
	sessionID := strings.TrimSpace(t.deps.Recorder.UUID())
	if sessionID == "" {
		return toolpolicy.BillableIntent{}, fmt.Errorf("provider tool session ID is required")
	}
	return toolpolicy.BillableIntent{
		OperationID: operationID, ToolCallID: toolCallID,
		CapabilityKey: t.deps.CapabilityKey,
		Provider:      t.deps.ProviderProfileID, Model: t.deps.ModelLabel,
		CredentialFingerprint: t.deps.CredentialFingerprint,
		ConfigEpoch:           t.deps.ConfigEpoch, NormalizedArgs: normalized, Count: 1,
		IdempotencyKey: toolpolicy.StableID(
			sessionID, operationID, toolCallID, t.deps.CapabilityKey, t.deps.ConfigEpoch, normalized,
		),
	}, nil
}

func (t *providerManagedInvokableTool) InvokableRun(
	ctx context.Context,
	argumentsInJSON string,
	opts ...tool.Option,
) (string, error) {
	operation, err := t.authorizeAndJournal(ctx, argumentsInJSON)
	if err != nil {
		return "", err
	}
	result, callErr := t.endpoint.InvokableRun(ctx, argumentsInJSON, opts...)
	if callErr == nil && isMCPErrorResult(result) {
		callErr = errProviderMCPRejected
	}
	journalErr := t.recordTerminal(operation, callErr)
	if callErr != nil {
		// Some MCP adapters return a response body alongside their error. Never
		// let that body cross the billable wrapper boundary: it can echo bearer
		// tokens, signed URLs, request arguments, or arbitrary upstream text.
		result = ""
	}
	return result, providerToolCallError(t.info.Name, callErr, journalErr)
}

func (t *providerManagedEnhancedInvokableTool) InvokableRun(
	ctx context.Context,
	argument *schema.ToolArgument,
	opts ...tool.Option,
) (*schema.ToolResult, error) {
	argsJSON := ""
	if argument != nil {
		argsJSON = argument.Text
	}
	operation, err := t.authorizeAndJournal(ctx, argsJSON)
	if err != nil {
		return nil, err
	}
	result, callErr := t.endpoint.InvokableRun(ctx, argument, opts...)
	journalErr := t.recordTerminal(operation, callErr)
	if callErr != nil {
		result = nil
	}
	return result, providerToolCallError(t.info.Name, callErr, journalErr)
}

func (t *providerManagedToolCore) authorizeAndJournal(
	ctx context.Context,
	argsJSON string,
) (session.ProviderToolOperation, error) {
	if err := ctx.Err(); err != nil {
		return session.ProviderToolOperation{}, err
	}
	intent, ok := toolpolicy.BillableIntentFromContext(ctx)
	if !ok {
		return session.ProviderToolOperation{}, fmt.Errorf("provider tool call is missing billable authorization")
	}
	expected, err := t.prepareBillableIntent(argsJSON, intent.ToolCallID, intent.OperationID)
	if err != nil {
		return session.ProviderToolOperation{}, fmt.Errorf("invalid provider tool arguments: %w", err)
	}
	if intent != expected {
		return session.ProviderToolOperation{}, fmt.Errorf("approved provider tool request no longer matches the current configuration")
	}
	if err := t.deps.VerifyRuntime(ctx); err != nil {
		return session.ProviderToolOperation{}, fmt.Errorf(
			"provider tool was not dispatched because its runtime configuration changed or is unavailable: %w", err,
		)
	}
	runID := strings.TrimSpace(toolpolicy.RunIDFromContext(ctx))
	if runID == "" {
		runID = "direct"
	}
	reservation, err := t.deps.Ledger.ReserveRun(runID, intent.OperationID)
	if err != nil {
		return session.ProviderToolOperation{}, err
	}
	operation := session.ProviderToolOperation{
		OperationID: intent.OperationID, ToolCallID: intent.ToolCallID,
		RunID:         runID,
		State:         session.ProviderToolDispatchAttempted,
		CapabilityKey: intent.CapabilityKey, ProviderProfileID: intent.Provider,
		ToolName: t.info.Name, IntentHash: providerToolIntentHash(intent),
		ConfigEpoch: intent.ConfigEpoch, IdempotencyKey: intent.IdempotencyKey,
		UpdatedAt: time.Now().UTC(),
	}
	if err := session.ValidateProviderToolStart(operation); err != nil {
		reservation.Release()
		return session.ProviderToolOperation{}, err
	}
	if err := t.deps.Recorder.RecordProviderToolDispatch(operation, t.deps.DispatchPolicy); err != nil {
		reservation.Release()
		if errors.Is(err, session.ErrDispatchSessionLimit) {
			return session.ProviderToolOperation{}, err
		}
		return session.ProviderToolOperation{}, fmt.Errorf(
			"provider tool was not dispatched because its operation journal could not be saved: %w", err,
		)
	}
	reservation.Commit()
	return operation, nil
}

func (t *providerManagedToolCore) recordTerminal(
	operation session.ProviderToolOperation,
	callErr error,
) error {
	state, code := classifyProviderToolResult(callErr)
	terminal := operation
	terminal.State = state
	terminal.ErrorCode = code
	terminal.UpdatedAt = time.Now().UTC()
	if err := session.ValidateProviderToolTransition(operation, terminal); err != nil {
		return err
	}
	return t.deps.Recorder.RecordProviderToolOperation(terminal)
}

func providerToolCallError(toolName string, callErr, journalErr error) error {
	var safeCallErr error
	if callErr != nil {
		_, code := classifyProviderToolResult(callErr)
		if code == "" {
			code = "provider_outcome_uncertain"
		}
		safeCallErr = fmt.Errorf(
			"provider-managed tool %q failed (%s); do not retry automatically",
			toolName, code,
		)
		// Preserve only standard cancellation identity. The original provider
		// error is deliberately not wrapped because its text may contain a
		// credential or response body that the runner persists into the session.
		if errors.Is(callErr, context.Canceled) {
			safeCallErr = fmt.Errorf("%v: %w", safeCallErr, context.Canceled)
		} else if errors.Is(callErr, context.DeadlineExceeded) {
			safeCallErr = fmt.Errorf("%v: %w", safeCallErr, context.DeadlineExceeded)
		}
	}
	if journalErr == nil {
		return safeCallErr
	}
	journalFailure := fmt.Errorf(
		"provider-managed tool %q was dispatched but its terminal outcome could not be durably recorded; do not retry automatically",
		toolName,
	)
	if safeCallErr == nil {
		return journalFailure
	}
	return errors.Join(safeCallErr, journalFailure)
}

var errProviderMCPRejected = errors.New("provider rejected the MCP request")

func isMCPErrorResult(raw string) bool {
	var envelope struct {
		IsError bool `json:"isError"`
	}
	return json.Unmarshal([]byte(raw), &envelope) == nil && envelope.IsError
}

func classifyProviderToolResult(err error) (session.ProviderToolOperationState, string) {
	if err == nil {
		return session.ProviderToolSucceeded, ""
	}
	if errors.Is(err, errProviderMCPRejected) {
		return session.ProviderToolFailed, "provider_rejected"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "mcp server return error") {
		return session.ProviderToolFailed, "provider_rejected"
	}
	if strings.Contains(message, "request failed with status ") ||
		strings.Contains(message, "unexpected status code:") ||
		strings.Contains(message, "authorization required") {
		return session.ProviderToolFailed, "provider_http_error"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return session.ProviderToolUncertain, "context_ended"
	}
	var netErr net.Error
	var urlErr *url.Error
	if errors.As(err, &netErr) || errors.As(err, &urlErr) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		isProviderTransportMessage(message) {
		return session.ProviderToolUncertain, "transport_uncertain"
	}
	// The endpoint was already invoked and an arbitrary adapter error does not
	// prove the provider rejected the request. Conservatively classify it as
	// uncertain so callers never issue a blind retry.
	return session.ProviderToolUncertain, "provider_outcome_uncertain"
}

func isProviderTransportMessage(message string) bool {
	for _, marker := range []string{
		"connection reset", "connection refused", "broken pipe", "unexpected eof",
		"transport is closing", "stream closed", "network is unreachable",
		"no such host", "tls handshake timeout", "i/o timeout",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func providerToolIntentHash(intent toolpolicy.BillableIntent) string {
	return toolpolicy.StableID(
		intent.OperationID, intent.ToolCallID, intent.CapabilityKey, intent.Provider, intent.Model,
		intent.CredentialFingerprint, intent.ConfigEpoch, intent.NormalizedArgs,
		fmt.Sprintf("%d", intent.Count), intent.IdempotencyKey,
	)
}

func normalizeProviderToolArgs(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", fmt.Errorf("unexpected trailing JSON value")
	}
	if _, ok := value.(map[string]any); !ok {
		return "", fmt.Errorf("provider tool arguments must be a JSON object")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// NewProviderToolUsageLedger rebuilds the session count from durable
// dispatch_attempted entries. A brand-new lazy session has no file yet and is
// treated as zero usage. All wrapped tools for the same provider capability
// must share the returned ledger instance.
func NewProviderToolUsageLedger(
	sessionID, capabilityKey, providerProfileID string,
	maxPerRun, maxPerSession int,
) (*toolpolicy.UsageLedger, error) {
	if maxPerRun <= 0 || maxPerSession <= 0 {
		return nil, fmt.Errorf("provider tool usage limits must be positive")
	}
	snapshots, err := session.LoadProviderToolOperations(sessionID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("load provider tool usage ledger: %w", err)
	}
	dispatched := session.CountDispatchedProviderToolOperations(
		snapshots, capabilityKey, providerProfileID,
	)
	return toolpolicy.NewUsageLedger(maxPerRun, maxPerSession, dispatched), nil
}

var (
	_ tool.InvokableTool                = (*providerManagedInvokableTool)(nil)
	_ tool.EnhancedInvokableTool        = (*providerManagedEnhancedInvokableTool)(nil)
	_ toolpolicy.BillableIntentPreparer = (*providerManagedInvokableTool)(nil)
	_ toolpolicy.BillableIntentPreparer = (*providerManagedEnhancedInvokableTool)(nil)
)
