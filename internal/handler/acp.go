package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	acp "github.com/coder/acp-go-sdk"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/mode"
)

// logACPError logs a failed ACP SessionUpdate to the debug log.
func logACPError(op string, err error) {
	config.Logger().Printf("[acp-handler] %s error: %v", op, err)
}

// ACPHandler implements AgentEventHandler by sending ACP SessionUpdate
// notifications through an AgentSideConnection to the connected client.
type ACPHandler struct {
	conn      *acp.AgentSideConnection
	sessionID acp.SessionId
	workDir   string

	toolCallCounter atomic.Int64
	mu              sync.Mutex
	// einoToACP maps Eino tool call IDs to ACP tool call IDs so that
	// OnToolResult can find the correct ACP ID even when multiple tool calls
	// are active concurrently.
	einoToACP map[string]acp.ToolCallId
	// toolArgs caches the raw args JSON by ACP tool call ID so that
	// OnToolResult can build diff content.
	toolArgs map[acp.ToolCallId]string
	// toolTerminated tracks tool calls that already reached a terminal ACP
	// status before the Eino tool-result message arrives (for example a
	// permission rejection converted into an agent-visible tool string).
	toolTerminated map[acp.ToolCallId]bool
	// turnErr is the error the current turn died on, recorded by OnAgentDone and
	// consumed by Prompt via TakeTurnError. Guarded by mu.
	turnErr error
	// pendingApprovals is a FIFO queue of ACP tool call IDs that have been
	// started but not yet matched to a RequestApproval call. The approval
	// middleware does not pass the Eino tool call ID, so we match by
	// (toolName, toolArgs) in arrival order.
	pendingApprovals []pendingApproval
	// subagentCalls maps a running subagent's name to the ACP tool call ID of
	// its "subagent" tool call, so lifecycle/progress callbacks (which only
	// carry the subagent name) can be forwarded as tool_call_update
	// notifications on the right call.
	subagentCalls map[string]acp.ToolCallId

	// onModeChange, when set, is invoked after the handler promotes the session
	// mode (e.g. "Allow All" → Full access) so the owning session can reconcile its
	// own advertised mode field with the approval state's source of truth.
	onModeChange func(mode.SessionMode) error

	// artifactPathResolver revalidates a transport-safe artifact reference and
	// returns the JCode engine's absolute path for same-machine ACP clients. The
	// path is emitted only as bounded ACP rawOutput metadata; it is never added
	// to model-visible output or the durable session journal.
	artifactPathResolver func(context.Context, ArtifactRef) (string, error)
}

// SetModeChangeCallback registers a callback invoked whenever the handler
// changes the session mode (currently the "Allow All" → Full access promotion).
func (h *ACPHandler) SetModeChangeCallback(fn func(mode.SessionMode) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onModeChange = fn
}

// SetArtifactPathResolver installs the command-owned artifact resolver. The
// handler deliberately does not know how managed/workspace artifacts are
// stored; command wires the session-bound artifact.Service instance.
func (h *ACPHandler) SetArtifactPathResolver(fn func(context.Context, ArtifactRef) (string, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.artifactPathResolver = fn
}

type pendingApproval struct {
	acpID    acp.ToolCallId
	toolName string
	toolArgs string
	claimed  bool
}

// NewACPHandler creates a handler bound to an ACP connection and session.
func NewACPHandler(conn *acp.AgentSideConnection, sessionID acp.SessionId, workDir string) *ACPHandler {
	return &ACPHandler{
		conn:           conn,
		sessionID:      sessionID,
		workDir:        workDir,
		einoToACP:      make(map[string]acp.ToolCallId),
		toolArgs:       make(map[acp.ToolCallId]string),
		toolTerminated: make(map[acp.ToolCallId]bool),
		subagentCalls:  make(map[string]acp.ToolCallId),
	}
}

func (h *ACPHandler) nextToolCallID() acp.ToolCallId {
	n := h.toolCallCounter.Add(1)
	return acp.ToolCallId(fmt.Sprintf("tc_%d", n))
}

// --- Output events ---

func (h *ACPHandler) OnAgentText(text string) {
	if err := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateAgentMessageText(text),
	}); err != nil {
		logACPError("AgentText", err)
	}
}

// toolKindForName maps a jcode tool name to an ACP ToolKind.
func toolKindForName(name string) acp.ToolKind {
	switch name {
	case "read", "todoread", "check_background", "team_list":
		return acp.ToolKindRead
	case "glob", "grep":
		return acp.ToolKindSearch
	case "edit", "multi_edit", "write", "todowrite":
		return acp.ToolKindEdit
	case "execute", "background", "team_send_message", "team_create", "team_spawn", "team_delete":
		return acp.ToolKindExecute
	case "webfetch":
		return acp.ToolKindFetch
	case "subagent", "ask_user", "load_skill":
		return acp.ToolKindThink
	case "switch_env":
		return acp.ToolKindSwitchMode
	default:
		return acp.ToolKindOther
	}
}

type acpToolPresentation struct {
	Title     string
	Kind      acp.ToolKind
	Locations []acp.ToolCallLocation
	Content   []acp.ToolCallContent
	RawInput  any
}

func billableACPPresentation(summary *BillableApprovalSummary) acpToolPresentation {
	if summary == nil {
		return acpToolPresentation{Title: billableApprovalTitle(nil), Kind: acp.ToolKindExecute}
	}
	return acpToolPresentation{
		Title: truncateTitle(billableApprovalTitle(summary)),
		Kind:  acp.ToolKindExecute,
		RawInput: map[string]any{
			"capability":    summary.Capability,
			"provider":      summary.Provider,
			"model":         summary.Model,
			"size":          summary.Size,
			"aspect_ratio":  summary.AspectRatio,
			"resolution":    summary.Resolution,
			"count":         summary.Count,
			"billable":      summary.Billable,
			"has_reference": summary.HasReference,
		},
	}
}

func (h *ACPHandler) presentationForTool(name, argsJSON string) acpToolPresentation {
	args := parseRawInput(argsJSON)
	obj, _ := args.(map[string]any)
	getString := func(key string) string {
		if v, ok := obj[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	getInt := func(key string) int {
		if v, ok := obj[key]; ok {
			switch n := v.(type) {
			case float64:
				return int(n)
			case int:
				return n
			}
		}
		return 0
	}

	p := acpToolPresentation{
		Title:    name,
		Kind:     toolKindForName(name),
		RawInput: args,
	}
	path := firstNonEmpty(getString("file_path"), getString("path"), getString("file"))
	if path != "" {
		line := firstPositive(getInt("start_line"), getInt("line"), getInt("offset"))
		p.Locations = []acp.ToolCallLocation{h.location(path, line)}
	}

	switch name {
	case "read":
		p.Title = "Read " + h.displayPath(path)
		if offset, limit := getInt("offset"), getInt("limit"); limit > 0 {
			start := offset
			if start <= 0 {
				start = 1
			}
			p.Title = fmt.Sprintf("%s (%d-%d)", p.Title, start, start+limit-1)
		} else if offset > 0 {
			p.Title = fmt.Sprintf("%s (from line %d)", p.Title, offset)
		}
	case "write":
		p.Title = "Write " + h.displayPath(path)
		p.Content = buildWriteDiffContent(argsJSON, "")
	case "edit":
		p.Title = "Edit " + h.displayPath(path)
		p.Content = buildEditDiffContent(argsJSON, "")
	case "multi_edit":
		p.Title = "Edit " + h.displayPath(path)
		p.Content = buildEditDiffContent(argsJSON, "")
	case "glob":
		pattern := getString("pattern")
		p.Title = "Find " + quoteIfPresent(pattern)
		if path != "" {
			p.Title += " in " + h.displayPath(path)
		}
	case "grep":
		pattern := getString("pattern")
		p.Title = "Search " + quoteIfPresent(pattern)
		if path != "" {
			p.Title += " in " + h.displayPath(path)
		}
	case "execute":
		p.Title = firstNonEmpty(getString("description"), getString("command"), "Run command")
	case "background":
		p.Title = firstNonEmpty(getString("description"), getString("command"), "Run background command")
	case "todowrite":
		p.Title = "Update todos"
		p.Kind = acp.ToolKindThink
	case "todoread":
		p.Title = "Read todos"
	case "check_background":
		p.Title = "Check background tasks"
	case "subagent":
		p.Title = firstNonEmpty(getString("description"), getString("name"), "Run subagent")
	case "ask_user":
		p.Title = firstNonEmpty(getString("question"), "Ask user")
	case "load_skill":
		p.Title = "Load skill " + getString("name")
	case "team_send_message":
		to := getString("to")
		switch {
		case to == "*":
			p.Title = "Message team"
		case to != "":
			p.Title = "Message @" + to
		default:
			p.Title = "Send team message"
		}
	case "team_create":
		p.Title = "Create team " + getString("team_name")
	case "team_spawn":
		p.Title = "Spawn " + firstNonEmpty(getString("name"), "teammate")
	case "team_delete":
		p.Title = "Delete team"
	case "switch_env":
		p.Title = "Switch environment to " + getString("target")
	case "webfetch":
		p.Title = "Fetch " + getString("url")
	default:
		if strings.Contains(name, "__") {
			p.Title = "Call " + strings.ReplaceAll(name, "__", "/")
		}
	}
	p.Title = truncateTitle(strings.TrimSpace(p.Title))
	if p.Title == "" {
		p.Title = name
	}
	return p
}

func (h *ACPHandler) location(path string, line int) acp.ToolCallLocation {
	loc := acp.ToolCallLocation{Path: h.absolutePath(path)}
	if line > 0 {
		loc.Line = &line
	}
	return loc
}

func (h *ACPHandler) absolutePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if h.workDir == "" {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(h.workDir, path))
}

func (h *ACPHandler) displayPath(path string) string {
	if path == "" {
		return "file"
	}
	abs := h.absolutePath(path)
	if h.workDir != "" {
		if rel, err := filepath.Rel(h.workDir, abs); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return abs
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func quoteIfPresent(s string) string {
	if s == "" {
		return "pattern"
	}
	return fmt.Sprintf("%q", s)
}

func truncateTitle(s string) string {
	const max = 160
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// parseRawInput converts a JSON args string into a map so that it serializes
// as a JSON object (not a double-escaped string) when assigned to an `any` field.
func parseRawInput(argsJSON string) any {
	if argsJSON == "" {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &obj); err != nil {
		config.Logger().Printf("[acp-handler] parseRawInput: invalid JSON args: %v", err)
		return nil
	}
	return obj
}

func (h *ACPHandler) OnToolCall(ev ToolCallEvent) {
	name, args, einoToolCallID := ev.Name, ev.Args, ev.ToolCallID
	id := h.nextToolCallID()
	h.mu.Lock()
	if einoToolCallID != "" {
		h.einoToACP[einoToolCallID] = id
	}
	h.toolArgs[id] = args
	h.pendingApprovals = append(h.pendingApprovals, pendingApproval{
		acpID: id, toolName: name, toolArgs: args,
	})
	if name == "subagent" {
		if sub := subagentNameFromArgs(args); sub != "" {
			h.subagentCalls[sub] = id
		}
	}
	h.mu.Unlock()

	presentation := h.presentationForTool(name, args)
	opts := []acp.ToolCallStartOpt{
		acp.WithStartStatus(acp.ToolCallStatusPending),
		acp.WithStartRawInput(presentation.RawInput),
		acp.WithStartKind(presentation.Kind),
	}
	if len(presentation.Locations) > 0 {
		opts = append(opts, acp.WithStartLocations(presentation.Locations))
	}
	if len(presentation.Content) > 0 {
		opts = append(opts, acp.WithStartContent(presentation.Content))
	}

	if err := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.StartToolCall(id, presentation.Title, opts...),
	}); err != nil {
		logACPError("StartToolCall", err)
	}
}

// NotifyToolInProgress is used by the approval state when a tool is about to
// execute without a visible permission prompt (auto-approval or safe tools).
func (h *ACPHandler) NotifyToolInProgress(name, args string) {
	h.updateMatchedToolStatus(name, args, acp.ToolCallStatusInProgress, false)
}

func (h *ACPHandler) updateMatchedToolStatus(name, args string, status acp.ToolCallStatus, terminal bool) {
	h.mu.Lock()
	var id acp.ToolCallId
	for _, p := range h.pendingApprovals {
		if p.toolName == name && p.toolArgs == args {
			id = p.acpID
			break
		}
	}
	if id != "" && terminal {
		h.toolTerminated[id] = true
	}
	h.mu.Unlock()
	if id == "" {
		return
	}
	if err := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateToolCall(id, acp.WithUpdateStatus(status)),
	}); err != nil {
		logACPError("UpdateToolStatus", err)
	}
}

func (h *ACPHandler) OnToolResult(ev ToolResultEvent) {
	name, output, einoToolCallID, err := ev.Name, ev.Output, ev.ToolCallID, ev.Err
	h.mu.Lock()
	id := h.einoToACP[einoToolCallID]
	cachedArgs := h.toolArgs[id]
	delete(h.einoToACP, einoToolCallID)
	delete(h.toolArgs, id)
	terminated := h.toolTerminated[id]
	artifactResolver := h.artifactPathResolver
	delete(h.toolTerminated, id)
	// Drop any still-queued approval entry for this ACP id (e.g. auto-approved
	// tools never go through RequestApproval and would otherwise leak and
	// poison the FIFO fallback on the next approval request).
	if id != "" {
		for i, p := range h.pendingApprovals {
			if p.acpID == id {
				h.pendingApprovals = append(h.pendingApprovals[:i], h.pendingApprovals[i+1:]...)
				break
			}
		}
		// Drop the subagent progress mapping for this call in case the done
		// lifecycle event never fired (rejected permission, tool error).
		for sub, acpID := range h.subagentCalls {
			if acpID == id {
				delete(h.subagentCalls, sub)
			}
		}
	}
	h.mu.Unlock()

	if id == "" {
		return
	}
	if terminated {
		return
	}

	status := acp.ToolCallStatusCompleted
	if err != nil || isToolFailureOutput(output) ||
		ev.Outcome == ToolOutcomeFailed || ev.Outcome == ToolOutcomeUncertain {
		status = acp.ToolCallStatusFailed
	}

	var content []acp.ToolCallContent
	switch name {
	case "edit":
		content = buildEditDiffContent(cachedArgs, output)
	case "write":
		content = buildWriteDiffContent(cachedArgs, output)
	}
	// Always include the text output as well.
	if output != "" {
		content = append(content, acp.ToolContent(acp.TextBlock(output)))
	}
	artifactMetadata, artifactsTruncated := resolvedACPArtifactMetadata(
		context.Background(), ev.Artifacts, artifactResolver,
	)
	content = append(content, acpArtifactReceiptContent(artifactMetadata, artifactsTruncated)...)

	opts := []acp.ToolCallUpdateOpt{
		acp.WithUpdateStatus(status),
	}
	if len(content) > 0 {
		opts = append(opts, acp.WithUpdateContent(content))
	}
	// Include output plus bounded, revalidated artifact metadata for structured
	// access. Engine paths are intentionally ACP-only: neither the model-facing
	// output above nor the session journal is modified.
	if output != "" || len(ev.Artifacts) > 0 {
		opts = append(opts, acp.WithUpdateRawOutput(acpToolResultRawOutputFromMetadata(
			output, artifactMetadata, artifactsTruncated,
		)))
	}

	if updateErr := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateToolCall(id, opts...),
	}); updateErr != nil {
		logACPError("UpdateToolCall", updateErr)
	}
}

const (
	maxACPResultArtifacts = 8
	maxACPMetadataString  = 512
	maxACPEnginePath      = 4096
)

// acpToolResultRawOutput preserves the legacy string shape when no artifact is
// present. Artifact-bearing results use an object with a text field plus a
// strictly bounded metadata array. Resolver failures omit engine_path rather
// than leaking storage errors or publishing an unverified path.
func acpToolResultRawOutput(
	ctx context.Context,
	output string,
	refs []ArtifactRef,
	resolver func(context.Context, ArtifactRef) (string, error),
) any {
	if len(refs) == 0 {
		return output
	}
	artifacts, truncated := resolvedACPArtifactMetadata(ctx, refs, resolver)
	return acpToolResultRawOutputFromMetadata(output, artifacts, truncated)
}

func acpToolResultRawOutputFromMetadata(
	output string,
	artifacts []map[string]any,
	truncated bool,
) any {
	if len(artifacts) == 0 && !truncated {
		return output
	}
	return map[string]any{
		"text":                output,
		"artifacts":           artifacts,
		"artifacts_truncated": truncated,
	}
}

func resolvedACPArtifactMetadata(
	ctx context.Context,
	refs []ArtifactRef,
	resolver func(context.Context, ArtifactRef) (string, error),
) ([]map[string]any, bool) {
	limit := len(refs)
	if limit > maxACPResultArtifacts {
		limit = maxACPResultArtifacts
	}
	artifacts := make([]map[string]any, 0, limit)
	for _, ref := range refs[:limit] {
		metadata := map[string]any{
			"id":         boundedACPMetadata(ref.ID, maxACPMetadataString),
			"storage":    boundedACPMetadata(ref.Storage, maxACPMetadataString),
			"key":        boundedACPMetadata(ref.Key, maxACPMetadataString),
			"title":      boundedACPMetadata(ref.Title, maxACPMetadataString),
			"kind":       boundedACPMetadata(ref.Kind, maxACPMetadataString),
			"media_type": boundedACPMetadata(ref.MediaType, maxACPMetadataString),
			"size":       ref.Size,
			"width":      ref.Width,
			"height":     ref.Height,
			"shareable":  ref.Shareable,
		}
		if ref.Provider != "" {
			metadata["provider"] = boundedACPMetadata(ref.Provider, maxACPMetadataString)
		}
		if ref.Model != "" {
			metadata["model"] = boundedACPMetadata(ref.Model, maxACPMetadataString)
		}
		if ref.OperationID != "" {
			metadata["operation_id"] = boundedACPMetadata(ref.OperationID, maxACPMetadataString)
		}
		if ref.ToolCallID != "" {
			metadata["tool_call_id"] = boundedACPMetadata(ref.ToolCallID, maxACPMetadataString)
		}
		if resolver != nil {
			if resolved, err := resolver(ctx, ref); err == nil && filepath.IsAbs(resolved) {
				metadata["engine_path"] = boundedACPMetadata(resolved, maxACPEnginePath)
			}
		}
		artifacts = append(artifacts, metadata)
	}
	return artifacts, len(refs) > limit
}

func acpArtifactReceipt(artifacts []map[string]any, truncated bool) string {
	lines := make([]string, 0, len(artifacts)+1)
	for _, metadata := range artifacts {
		parts := []string{"Artifact: " + stringMetadata(metadata, "title")}
		provider, model := stringMetadata(metadata, "provider"), stringMetadata(metadata, "model")
		if provider != "" || model != "" {
			parts = append(parts, strings.Trim(provider+" / "+model, " /"))
		}
		width, _ := metadata["width"].(int)
		height, _ := metadata["height"].(int)
		if width > 0 && height > 0 {
			parts = append(parts, fmt.Sprintf("%dx%d", width, height))
		}
		if size, ok := metadata["size"].(int64); ok && size >= 0 {
			parts = append(parts, fmt.Sprintf("%d bytes", size))
		}
		if enginePath := stringMetadata(metadata, "engine_path"); enginePath != "" {
			parts = append(parts, "JCode engine path: "+enginePath)
		}
		lines = append(lines, strings.Join(parts, " | "))
	}
	if truncated {
		lines = append(lines, fmt.Sprintf("Additional artifacts omitted (showing first %d).", maxACPResultArtifacts))
	}
	return strings.Join(lines, "\n")
}

func acpArtifactReceiptContent(artifacts []map[string]any, truncated bool) []acp.ToolCallContent {
	if len(artifacts) == 0 {
		return nil
	}
	return []acp.ToolCallContent{
		acp.ToolContent(acp.TextBlock(acpArtifactReceipt(artifacts, truncated))),
	}
}

func stringMetadata(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return value
}

func boundedACPMetadata(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (h *ACPHandler) OnToolProgress(ev ToolProgressEvent) {
	h.mu.Lock()
	id := h.einoToACP[ev.ToolCallID]
	h.mu.Unlock()
	if id == "" {
		return
	}
	label := "Working…"
	switch ev.Phase {
	case ToolPhaseGenerating:
		label = "Generating image…"
	case ToolPhaseSaving:
		label = "Saving generated image…"
	case ToolPhaseUncertain:
		label = "Provider outcome is uncertain"
	}
	if err := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update: acp.UpdateToolCall(id,
			acp.WithUpdateStatus(acp.ToolCallStatusInProgress),
			acp.WithUpdateContent([]acp.ToolCallContent{
				acp.ToolContent(acp.TextBlock(label)),
			}),
		),
	}); err != nil {
		logACPError("ImageToolProgress", err)
	}
}

func isToolFailureOutput(output string) bool {
	output = strings.TrimSpace(output)
	return strings.HasPrefix(output, "Tool execution failed:") ||
		strings.Contains(output, "\n\nTool execution failed:") ||
		strings.HasPrefix(output, "Tool execution panicked:")
}

func (h *ACPHandler) OnTodoUpdate() {
	// No ACP equivalent; todo state is internal.
}

// --- Lifecycle ---

func (h *ACPHandler) OnAgentStart() {
	// ACP does not have a standard "agent started" notification.
}

// OnAgentDone records how the turn ended so Prompt can report it truthfully.
//
// This used to be a no-op, on the reasoning that "the Prompt response is
// returned by the Prompt method, nothing to send here" — but Prompt had no other
// way to learn an error had happened, so every failure became StopReasonEndTurn:
// a clean, successful-looking turn with no text. A 402 from the provider was
// indistinguishable from an agent that had thought about it and decided to say
// nothing. In one eval campaign that scored 310 runs as passing on a model that
// never ran (agent-eval finding F2), and for a real user it is worse: the agent
// silently does nothing and looks content about it.
//
// The error is recorded, not sent — Prompt still owns the response. But it can
// no longer claim success it did not have.
func (h *ACPHandler) OnAgentDone(err error) {
	h.mu.Lock()
	h.turnErr = err
	h.mu.Unlock()
}

// TakeTurnError returns and clears the error recorded for this turn.
// Prompt calls it to decide the StopReason.
func (h *ACPHandler) TakeTurnError() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	err := h.turnErr
	h.turnErr = nil
	return err
}

func (h *ACPHandler) OnTokenUpdate(info TokenUsage) {
	// ACP does not have a standard token update notification.
}

// --- Subagent events ---

// OnSubagentEvent bridges subagent lifecycle events (tools.SubagentNotifier)
// onto the "subagent" tool call as tool_call_update notifications. The final
// status and result ride the regular OnToolResult update; the done event only
// clears the progress mapping.
func (h *ACPHandler) OnSubagentEvent(name, agentType string, done bool, result string, err error) {
	h.mu.Lock()
	id, ok := h.subagentCalls[name]
	if done {
		delete(h.subagentCalls, name)
	}
	h.mu.Unlock()
	if !ok || done {
		return
	}

	if updateErr := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update: acp.UpdateToolCall(id,
			acp.WithUpdateStatus(acp.ToolCallStatusInProgress),
			acp.WithUpdateContent([]acp.ToolCallContent{
				acp.ToolContent(acp.TextBlock(fmt.Sprintf("%s subagent started", agentType))),
			}),
		),
	}); updateErr != nil {
		logACPError("SubagentEvent", updateErr)
	}
}

// OnSubagentProgress bridges intermediate subagent tool activity
// (tools.SubagentProgressFn) onto the "subagent" tool call as a rolling
// content update — ACP replaces the content collection on each update, so the
// client shows the latest activity line while the subagent runs.
func (h *ACPHandler) OnSubagentProgress(agentName, event, toolName, detail string) {
	h.mu.Lock()
	id, ok := h.subagentCalls[agentName]
	h.mu.Unlock()
	if !ok {
		return
	}

	if err := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update: acp.UpdateToolCall(id,
			acp.WithUpdateContent([]acp.ToolCallContent{
				acp.ToolContent(acp.TextBlock(subagentProgressLine(event, toolName, detail))),
			}),
		),
	}); err != nil {
		logACPError("SubagentProgress", err)
	}
}

// subagentNameFromArgs extracts the "name" field from a subagent tool call's
// raw args JSON ("" when absent or unparsable).
func subagentNameFromArgs(argsJSON string) string {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	return args.Name
}

// subagentProgressLine renders one progress event as a short single-line
// summary ("tool_call" / "tool_result" are the events subagents emit today).
func subagentProgressLine(event, toolName, detail string) string {
	prefix := event
	switch event {
	case "tool_call":
		prefix = "→"
	case "tool_result":
		prefix = "←"
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s %s", prefix, toolName, compactProgressDetail(detail)))
}

// compactProgressDetail flattens tool args/output to one truncated line.
func compactProgressDetail(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 160
	if len(s) > max {
		s = strings.ToValidUTF8(s[:max-3], "") + "..."
	}
	return s
}

// --- Approval flow ---

func (h *ACPHandler) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	allowApproveAll := req.ApprovalClass == ""
	h.mu.Lock()
	// Claim, but do not consume, the matching pending tool call. Allow All can
	// fail its durable mode commit after the ACP client selects it; retaining the
	// claim lets this same request stay pending and re-prompt without another
	// concurrent approval stealing its tool-call id.
	var matchedID acp.ToolCallId
	for i := range h.pendingApprovals {
		p := &h.pendingApprovals[i]
		if !p.claimed && p.toolName == req.ToolName && p.toolArgs == req.ToolArgs {
			matchedID = p.acpID
			p.claimed = true
			break
		}
	}
	// Fallback: use first pending if no exact match (e.g. args were modified).
	if matchedID == "" {
		for i := range h.pendingApprovals {
			if !h.pendingApprovals[i].claimed {
				matchedID = h.pendingApprovals[i].acpID
				h.pendingApprovals[i].claimed = true
				break
			}
		}
	}
	h.mu.Unlock()
	if matchedID == "" {
		matchedID = h.nextToolCallID()
	}
	presentation := h.presentationForTool(req.ToolName, req.ToolArgs)
	if req.BillableSummary != nil {
		presentation = billableACPPresentation(req.BillableSummary)
	}
	optionSet, optionErr := buildACPPermissionOptions(req, allowApproveAll)
	if optionErr != nil {
		h.consumePendingApproval(matchedID)
		return ApprovalResponse{}, optionErr
	}

	for {
		permResp, err := h.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
			SessionId: h.sessionID,
			ToolCall: acp.ToolCallUpdate{
				ToolCallId: matchedID,
				Title:      acp.Ptr(presentation.Title),
				Kind:       acp.Ptr(presentation.Kind),
				Status:     acp.Ptr(acp.ToolCallStatusPending),
				Locations:  presentation.Locations,
				Content:    presentation.Content,
				RawInput:   presentation.RawInput,
			},
			Options: optionSet.options,
		})
		if err != nil {
			h.consumePendingApproval(matchedID)
			return ApprovalResponse{}, err
		}

		if permResp.Outcome.Cancelled != nil {
			h.consumePendingApproval(matchedID)
			h.markPermissionRejected(matchedID)
			return ApprovalResponse{Approved: false, Mode: ModeManual}, nil
		}

		if permResp.Outcome.Selected != nil {
			response, resolveErr := resolveACPPermissionOption(
				string(permResp.Outcome.Selected.OptionId),
				optionSet.allowOnceID,
				optionSet.rejectOnceID,
				optionSet.allowAlwaysID,
				allowApproveAll,
			)
			if resolveErr != nil {
				h.consumePendingApproval(matchedID)
				h.markPermissionRejected(matchedID)
				return ApprovalResponse{}, resolveErr
			}
			if response.Approved && response.Mode == ModeAuto {
				// Keep the request pending when Full access cannot be durably
				// committed. Re-prompting lets the user retry after fixing storage,
				// or choose a one-time grant/deny; the middleware never receives an
				// error that OnToolResult could fold into a terminal update.
				if modeErr := h.notifyModeChanged(mode.FullAccess); modeErr != nil {
					h.markModePromotionRetry(matchedID)
					continue
				}
			}
			h.consumePendingApproval(matchedID)
			if response.Approved {
				h.markPermissionApproved(matchedID)
			} else {
				h.markPermissionRejected(matchedID)
			}
			return response, nil
		}

		h.consumePendingApproval(matchedID)
		h.markPermissionRejected(matchedID)
		return ApprovalResponse{}, fmt.Errorf("permission response did not return a matching opaque option id")
	}
}

func (h *ACPHandler) consumePendingApproval(id acp.ToolCallId) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.pendingApprovals {
		if h.pendingApprovals[i].acpID == id {
			h.pendingApprovals = append(h.pendingApprovals[:i], h.pendingApprovals[i+1:]...)
			return
		}
	}
}

func (h *ACPHandler) markModePromotionRetry(id acp.ToolCallId) {
	content := []acp.ToolCallContent{acp.ToolContent(acp.TextBlock(
		"Full access could not be saved. Fix session storage, then retry Allow All, or choose Allow/Deny.",
	))}
	if err := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update: acp.UpdateToolCall(
			id,
			acp.WithUpdateStatus(acp.ToolCallStatusPending),
			acp.WithUpdateContent(content),
		),
	}); err != nil {
		logACPError("ModePromotionRetry", err)
	}
}

type acpPermissionOptionSet struct {
	allowOnceID   string
	rejectOnceID  string
	allowAlwaysID string
	options       []acp.PermissionOption
}

func buildACPPermissionOptions(req ApprovalRequest, allowApproveAll bool) (acpPermissionOptionSet, error) {
	var result acpPermissionOptionSet
	if req.ApprovalClass != "" {
		allowApproveAll = false
		allowID, denyID, err := BillableApprovalOptionIDs(req.Options)
		if err != nil {
			return result, err
		}
		result.allowOnceID = allowID
		result.rejectOnceID = denyID
	} else {
		result.allowOnceID = newApprovalOptionID()
		result.rejectOnceID = newApprovalOptionID()
		result.allowAlwaysID = newApprovalOptionID()
	}
	result.options = []acp.PermissionOption{
		{
			OptionId: acp.PermissionOptionId(result.allowOnceID),
			Name:     "Allow",
			Kind:     acp.PermissionOptionKindAllowOnce,
		},
		{
			OptionId: acp.PermissionOptionId(result.rejectOnceID),
			Name:     "Deny",
			Kind:     acp.PermissionOptionKindRejectOnce,
		},
	}
	if allowApproveAll {
		result.options = append(result.options, acp.PermissionOption{
			OptionId: acp.PermissionOptionId(result.allowAlwaysID),
			Name:     "Allow All (auto-approve this session)",
			Kind:     acp.PermissionOptionKindAllowAlways,
		})
	}
	return result, nil
}

func resolveACPPermissionOption(
	selectedID, allowOnceID, rejectOnceID, allowAlwaysID string,
	allowApproveAll bool,
) (ApprovalResponse, error) {
	if selectedID == "" {
		return ApprovalResponse{}, fmt.Errorf("permission response returned an empty opaque option id")
	}
	switch selectedID {
	case allowOnceID:
		return ApprovalResponse{
			Approved: true, Mode: ModeManual, ResolvedOptionID: allowOnceID,
		}, nil
	case rejectOnceID:
		return ApprovalResponse{
			Approved: false, Mode: ModeManual, ResolvedOptionID: rejectOnceID,
		}, nil
	case allowAlwaysID:
		if allowAlwaysID == "" || !allowApproveAll {
			return ApprovalResponse{}, fmt.Errorf("permission response selected a forbidden blanket grant")
		}
		return ApprovalResponse{
			Approved: true, Mode: ModeAuto, ResolvedOptionID: allowAlwaysID,
		}, nil
	default:
		return ApprovalResponse{}, fmt.Errorf("permission response returned an unknown opaque option id")
	}
}

// notifyModeChanged tells the connected client the session's unified mode
// changed (e.g. after "Allow All" promotes to Full access), so its selector syncs.
func (h *ACPHandler) notifyModeChanged(m mode.SessionMode) error {
	h.mu.Lock()
	onModeChange := h.onModeChange
	h.mu.Unlock()
	if onModeChange != nil {
		if err := onModeChange(m); err != nil {
			logACPError("ModePromotion", err)
			return ErrApprovalModePromotion
		}
	}
	if err := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update: acp.SessionUpdate{
			CurrentModeUpdate: &acp.SessionCurrentModeUpdate{
				CurrentModeId: acp.SessionModeId(m.String()),
			},
		},
	}); err != nil {
		logACPError("CurrentModeUpdate", err)
	}
	return nil
}

func (h *ACPHandler) markPermissionApproved(id acp.ToolCallId) {
	if err := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateToolCall(id, acp.WithUpdateStatus(acp.ToolCallStatusInProgress)),
	}); err != nil {
		logACPError("PermissionApprovedStatus", err)
	}
}

func (h *ACPHandler) markPermissionRejected(id acp.ToolCallId) {
	h.mu.Lock()
	h.toolTerminated[id] = true
	h.mu.Unlock()
	if err := h.conn.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: h.sessionID,
		Update:    acp.UpdateToolCall(id, acp.WithUpdateStatus(acp.ToolCallStatusFailed)),
	}); err != nil {
		logACPError("PermissionRejectedStatus", err)
	}
}

// buildEditDiffContent extracts diff information from an edit tool call and
// returns ToolCallContentDiff entries.
func buildEditDiffContent(argsJSON, _ string) []acp.ToolCallContent {
	var args struct {
		FilePath  string `json:"file_path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
		Edits     []struct {
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.FilePath == "" {
		return nil
	}

	var content []acp.ToolCallContent
	if args.OldString != "" || args.NewString != "" {
		content = append(content, acp.ToolDiffContent(args.FilePath, args.NewString, args.OldString))
	}
	for _, e := range args.Edits {
		content = append(content, acp.ToolDiffContent(args.FilePath, e.NewString, e.OldString))
	}
	return content
}

// buildWriteDiffContent creates a diff entry for a write tool call (new file
// creation or full file overwrite).
func buildWriteDiffContent(argsJSON, _ string) []acp.ToolCallContent {
	var args struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.FilePath == "" {
		return nil
	}
	return []acp.ToolCallContent{
		acp.ToolDiffContent(args.FilePath, args.Content),
	}
}
