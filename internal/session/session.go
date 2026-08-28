package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/cnjack/jcode/internal/artifact"
	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/model/responsemeta"
)

// EntryType identifies the kind of JSONL record.
type EntryType string

const (
	privateSessionDirMode  os.FileMode = 0o700
	privateSessionFileMode os.FileMode = 0o600
)

const (
	EntrySessionStart EntryType = "session_start"
	EntryUser         EntryType = "user"
	EntryAssistant    EntryType = "assistant"
	EntryToolCall     EntryType = "tool_call"
	EntryToolResult   EntryType = "tool_result"

	// Extended entry types for structured state tracking.
	EntryPlanUpdate            EntryType = "plan_update"
	EntryTodoSnapshot          EntryType = "todo_snapshot"
	EntrySubagentStart         EntryType = "subagent_start"
	EntrySubagentResult        EntryType = "subagent_result"
	EntrySubagentAsync         EntryType = "subagent_async"
	EntryModeChange            EntryType = "mode_change"
	EntryAgentChange           EntryType = "agent_change"
	EntryCompact               EntryType = "compact"
	EntryBudgetWarning         EntryType = "budget_warning"
	EntrySystemPrompt          EntryType = "system_prompt"
	EntryGoalUpdate            EntryType = "goal_update"
	EntryToolObservation       EntryType = "tool_observation"
	EntryArtifact              EntryType = "artifact"
	EntryGenerationOperation   EntryType = "generation_operation"
	EntryProviderToolOperation EntryType = "provider_tool_operation"
	EntrySessionToolOverride   EntryType = "session_tool_override"
)

// GenerationOperationState is a durable provider-dispatch state. Progress UI
// phases are ephemeral; these states exist to answer the crash-recovery
// question "could the provider already have accepted a billable request?".
type GenerationOperationState string

const (
	GenerationDispatchAttempted GenerationOperationState = "dispatch_attempted"
	GenerationAccepted          GenerationOperationState = "accepted"
	GenerationSaving            GenerationOperationState = "saving"
	GenerationSucceeded         GenerationOperationState = "succeeded"
	GenerationFailed            GenerationOperationState = "failed"
	GenerationUncertain         GenerationOperationState = "uncertain"
)

func (state GenerationOperationState) IsTerminal() bool {
	switch state {
	case GenerationSucceeded, GenerationFailed, GenerationUncertain:
		return true
	default:
		return false
	}
}

func (state GenerationOperationState) valid() bool {
	switch state {
	case GenerationDispatchAttempted, GenerationAccepted, GenerationSaving,
		GenerationSucceeded, GenerationFailed, GenerationUncertain:
		return true
	default:
		return false
	}
}

func generationStateRank(state GenerationOperationState) int {
	if state.IsTerminal() {
		return 4
	}
	switch state {
	case GenerationDispatchAttempted:
		return 1
	case GenerationAccepted:
		return 2
	case GenerationSaving:
		return 3
	default:
		return 0
	}
}

// OperationCapabilityKey is the metadata-only snapshot used by the durable
// journal. Runtime capability implementations map their key into this neutral
// representation; session replay never imports provider/config packages.
type OperationCapabilityKey struct {
	ProviderProfileID string `json:"provider_profile_id"`
	CredentialKind    string `json:"credential_kind,omitempty"`
	EndpointProfile   string `json:"endpoint_profile"`
	ModelID           string `json:"model_id"`
}

// GenerationOperation is one immutable-intent state transition. Secret
// credentials, prompts, signed URLs, and provider response bodies are never
// persisted here.
type GenerationOperation struct {
	OperationID           string                   `json:"operation_id"`
	ToolCallID            string                   `json:"tool_call_id"`
	State                 GenerationOperationState `json:"state"`
	CapabilityKey         OperationCapabilityKey   `json:"capability_key"`
	CredentialFingerprint string                   `json:"credential_fingerprint"`
	ConfigEpoch           uint64                   `json:"config_epoch"`
	IdempotencyKey        string                   `json:"idempotency_key"`
	ProviderRequestIDHash string                   `json:"provider_request_id_hash,omitempty"`
	ArtifactIDs           []string                 `json:"artifact_ids,omitempty"`
	ErrorCode             string                   `json:"error_code,omitempty"`
	UpdatedAt             time.Time                `json:"updated_at"`
}

func (operation GenerationOperation) validate() error {
	if strings.TrimSpace(operation.OperationID) == "" {
		return fmt.Errorf("generation operation ID is required")
	}
	if strings.TrimSpace(operation.ToolCallID) == "" {
		return fmt.Errorf("generation operation tool call ID is required")
	}
	if !operation.State.valid() {
		return fmt.Errorf("invalid generation operation state %q", operation.State)
	}
	if operation.CapabilityKey.ProviderProfileID == "" ||
		operation.CapabilityKey.EndpointProfile == "" || operation.CapabilityKey.ModelID == "" {
		return fmt.Errorf("generation operation capability key is incomplete")
	}
	if operation.CredentialFingerprint == "" || operation.IdempotencyKey == "" {
		return fmt.Errorf("generation operation immutable intent is incomplete")
	}
	return nil
}

// ValidateGenerationStart is the pre-dispatch guard used before the first
// journal append. Recovery code may append a terminal repair entry to a legacy
// transcript, so RecordGenerationOperation validates shape but deliberately
// leaves first-state policy to this explicit helper.
func ValidateGenerationStart(operation GenerationOperation) error {
	if err := operation.validate(); err != nil {
		return err
	}
	if operation.State != GenerationDispatchAttempted {
		return fmt.Errorf("generation operation must start at %q", GenerationDispatchAttempted)
	}
	return nil
}

// ValidateGenerationTransition enforces monotonic state and immutable intent.
// Duplicate transitions are allowed so crash recovery can append idempotently.
func ValidateGenerationTransition(previous, next GenerationOperation) error {
	if err := previous.validate(); err != nil {
		return fmt.Errorf("invalid previous generation operation: %w", err)
	}
	if err := next.validate(); err != nil {
		return err
	}
	if previous.OperationID != next.OperationID || previous.ToolCallID != next.ToolCallID ||
		previous.CapabilityKey != next.CapabilityKey ||
		previous.CredentialFingerprint != next.CredentialFingerprint ||
		previous.ConfigEpoch != next.ConfigEpoch || previous.IdempotencyKey != next.IdempotencyKey {
		return fmt.Errorf("generation operation immutable intent changed")
	}
	if previous.State.IsTerminal() && previous.State != next.State {
		return fmt.Errorf("generation operation cannot leave terminal state %q", previous.State)
	}
	if previous.State == next.State {
		return nil
	}
	if next.State.IsTerminal() {
		return nil
	}
	validProgression := (previous.State == GenerationDispatchAttempted && next.State == GenerationAccepted) ||
		(previous.State == GenerationAccepted && next.State == GenerationSaving)
	if !validProgression {
		return fmt.Errorf("invalid generation operation transition from %q to %q", previous.State, next.State)
	}
	return nil
}

// GenerationOperationSnapshot is the replay projection used to rebuild the
// atomic session ledger. Dispatched remains true once a dispatch_attempted
// entry was observed, independent of later success or failure.
type GenerationOperationSnapshot struct {
	Latest     GenerationOperation `json:"latest"`
	Dispatched bool                `json:"dispatched"`
}

// GenerationRecoveryPriority formalizes the release ordering. Higher evidence
// wins: terminal operation > artifact > terminal tool result > nonterminal op.
type GenerationRecoveryPriority int

const (
	GenerationRecoveryNone GenerationRecoveryPriority = iota
	GenerationRecoveryNonTerminalOperation
	GenerationRecoveryTerminalToolResult
	GenerationRecoveryArtifact
	GenerationRecoveryTerminalOperation
)

// ToolObservation stores metadata-only evidence about progressive tool
// disclosure. It intentionally contains no raw query, arguments, schema,
// output, model content, or error text.
type ToolObservation struct {
	Kind string `json:"kind"`

	ModelRequestSeq      int      `json:"model_request_seq,omitempty"`
	VisibleNames         []string `json:"visible_names,omitempty"`
	VisibleCount         int      `json:"visible_count,omitempty"`
	SchemaBytes          int      `json:"schema_bytes,omitempty"`
	SchemaTokensEstimate int64    `json:"schema_tokens_estimate,omitempty"`
	NewlyVisibleDeferred []string `json:"newly_visible_deferred,omitempty"`

	ToolCallID           string   `json:"tool_call_id,omitempty"`
	QueryMode            string   `json:"query_mode,omitempty"`
	QueryBytes           int      `json:"query_bytes,omitempty"`
	TermCount            int      `json:"term_count,omitempty"`
	RequiredTermCount    int      `json:"required_term_count,omitempty"`
	MaxResults           int      `json:"max_results,omitempty"`
	ValidatedSelectNames []string `json:"validated_select_names,omitempty"`
	UnknownSelectCount   int      `json:"unknown_select_count,omitempty"`
	MatchNames           []string `json:"match_names,omitempty"`
	NewMatchNames        []string `json:"new_match_names,omitempty"`
	RepeatedQuery        bool     `json:"repeated_query,omitempty"`
	Redundant            bool     `json:"redundant,omitempty"`
	Success              bool     `json:"success,omitempty"`

	ToolName string `json:"tool_name,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// TodoSnapshotItem is a single todo entry stored in a todo_snapshot event.
type TodoSnapshotItem struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// EntryImage stores a single image attached to a user message.
type EntryImage struct {
	MimeType string `json:"media_type"`
	Data     string `json:"data"` // base64-encoded
}

// Entry is one line of the JSONL session file.
type Entry struct {
	Type          EntryType     `json:"type"`
	UUID          string        `json:"uuid,omitempty"`
	Project       string        `json:"project,omitempty"`
	WorkspaceKind WorkspaceKind `json:"workspace_kind,omitempty"`
	Provider      string        `json:"provider,omitempty"`
	Model         string        `json:"model,omitempty"`
	Agent         string        `json:"agent,omitempty"`
	Content       string        `json:"content,omitempty"`
	Name          string        `json:"name,omitempty"`         // tool name
	Args          string        `json:"args,omitempty"`         // tool args JSON
	Output        string        `json:"output,omitempty"`       // tool output
	Error         string        `json:"error,omitempty"`        // tool error
	ToolCallID    string        `json:"tool_call_id,omitempty"` // links tool_call ↔ tool_result
	Timestamp     string        `json:"timestamp"`
	// Typed tool/generation correlation. These additive fields are shared by
	// operation journal and enhanced tool-result entries.
	OperationID string   `json:"operation_id,omitempty"`
	Outcome     string   `json:"outcome,omitempty"`
	ErrorCode   string   `json:"error_code,omitempty"`
	ArtifactIDs []string `json:"artifact_ids,omitempty"`

	// Tool-call batch fields. Tool calls issued by one assistant message share
	// a BatchID; legacy files simply lack these keys (unmarshal to zero values,
	// i.e. "no batch info").
	BatchID    string `json:"batch_id,omitempty"`
	BatchIndex int    `json:"batch_index,omitempty"`
	BatchSize  int    `json:"batch_size,omitempty"`

	// tool_result semantics. Denied marks a user-rejected approval (replay
	// renders it struck-through, not as an error); DurationMs is the runner's
	// approval-wait-adjusted execution latency. Legacy files lack both keys.
	Denied     bool  `json:"denied,omitempty"`
	DurationMs int64 `json:"duration_ms,omitempty"`

	// Images attached to a user message.
	Images []EntryImage `json:"images,omitempty"`

	// OpaqueResponseItems carries only bounded encrypted Responses API
	// reasoning items. Cleartext ReasoningContent is intentionally never
	// persisted. Legacy JSONL files simply omit this additive field.
	OpaqueResponseItems []json.RawMessage `json:"opaque_response_items,omitempty"`

	// plan_update fields
	PlanStatus  string `json:"plan_status,omitempty"`
	PlanTitle   string `json:"plan_title,omitempty"`
	PlanContent string `json:"plan_content,omitempty"`
	Feedback    string `json:"feedback,omitempty"`

	// todo_snapshot fields
	Todos []TodoSnapshotItem `json:"todos,omitempty"`

	// subagent_start / subagent_result fields
	SubagentName string `json:"subagent_name,omitempty"`
	SubagentType string `json:"subagent_type,omitempty"`

	// mode_change field
	Mode string `json:"mode,omitempty"`

	// compact fields. KeptN is the number of trailing messages the live agent
	// kept verbatim after the summary; replay re-attaches that many entries
	// from before the compact event. Legacy files lack kept_n (unmarshals to
	// 0), which replays as "summary only" — the pre-KeptN behaviour.
	Summary    string `json:"summary,omitempty"`
	CompactedN int    `json:"compacted_n,omitempty"`
	KeptN      int    `json:"kept_n,omitempty"`

	// system_prompt fields
	EnvInfo string `json:"env_info,omitempty"` // serialized environment snapshot

	// goal_update fields. GoalStatus == "cleared" marks goal removal.
	GoalObjective  string `json:"goal_objective,omitempty"`
	GoalStatus     string `json:"goal_status,omitempty"`
	GoalTokensUsed int64  `json:"goal_tokens_used,omitempty"`
	GoalCreatedAt  int64  `json:"goal_created_at,omitempty"`
	GoalUpdatedAt  int64  `json:"goal_updated_at,omitempty"`

	// tool_observation fields
	ToolObservation *ToolObservation `json:"tool_observation,omitempty"`

	// Artifact fields. Content and absolute paths are never persisted. A missing
	// storage kind is a legacy workspace record.
	ArtifactID          string `json:"artifact_id,omitempty"`
	ArtifactPath        string `json:"artifact_path,omitempty"`
	ArtifactStorageKind string `json:"artifact_storage_kind,omitempty"`
	ArtifactKey         string `json:"artifact_key,omitempty"`
	ArtifactTitle       string `json:"artifact_title,omitempty"`
	ArtifactKind        string `json:"artifact_kind,omitempty"`
	ArtifactMediaType   string `json:"artifact_media_type,omitempty"`
	ArtifactSize        int64  `json:"artifact_size,omitempty"`
	ArtifactWidth       int    `json:"artifact_width,omitempty"`
	ArtifactHeight      int    `json:"artifact_height,omitempty"`
	ArtifactSHA256      string `json:"artifact_sha256,omitempty"`
	ArtifactProviderID  string `json:"artifact_provider_id,omitempty"`
	ArtifactModelID     string `json:"artifact_model_id,omitempty"`
	ArtifactParentID    string `json:"artifact_parent_id,omitempty"`
	ArtifactRevision    int    `json:"artifact_revision,omitempty"`
	ArtifactFocus       bool   `json:"artifact_focus,omitempty"`
	ArtifactShareable   bool   `json:"artifact_shareable,omitempty"`

	// generation_operation fields.
	OperationState                 string                  `json:"operation_state,omitempty"`
	OperationCapabilityKey         *OperationCapabilityKey `json:"operation_capability_key,omitempty"`
	OperationCredentialFingerprint string                  `json:"operation_credential_fingerprint,omitempty"`
	OperationConfigEpoch           uint64                  `json:"operation_config_epoch,omitempty"`
	OperationIdempotencyKey        string                  `json:"operation_idempotency_key,omitempty"`
	OperationProviderRequestIDHash string                  `json:"operation_provider_request_id_hash,omitempty"`
	OperationUpdatedAt             string                  `json:"operation_updated_at,omitempty"`

	// provider_tool_operation fields. These entries deliberately persist only
	// metadata hashes and stable identifiers: never tool arguments, raw
	// credentials, endpoint URLs, response bodies, or result content.
	ProviderToolState             string `json:"provider_tool_state,omitempty"`
	ProviderToolRunID             string `json:"provider_tool_run_id,omitempty"`
	ProviderToolCapabilityKey     string `json:"provider_tool_capability_key,omitempty"`
	ProviderToolProviderProfileID string `json:"provider_tool_provider_profile_id,omitempty"`
	ProviderToolName              string `json:"provider_tool_name,omitempty"`
	ProviderToolIntentHash        string `json:"provider_tool_intent_hash,omitempty"`
	ProviderToolConfigEpoch       string `json:"provider_tool_config_epoch,omitempty"`
	ProviderToolIdempotencyKey    string `json:"provider_tool_idempotency_key,omitempty"`
	ProviderToolUpdatedAt         string `json:"provider_tool_updated_at,omitempty"`

	// session_tool_override fields. Persisted is deliberately separate from
	// runtime availability/effectiveness: those are evaluated from the current
	// engine configuration and must never be replayed as durable truth.
	SessionToolOverrideTool      string `json:"session_tool_override_tool,omitempty"`
	SessionToolOverridePersisted bool   `json:"session_tool_override_persisted,omitempty"`
	SessionToolOverrideRevision  uint64 `json:"session_tool_override_revision,omitempty"`
}

// SessionMeta is stored in the index for fast listing.
type SessionMeta struct {
	UUID          string        `json:"uuid"`
	Project       string        `json:"project"`
	WorkspaceKind WorkspaceKind `json:"workspace_kind,omitempty"`
	Provider      string        `json:"provider"`
	Model         string        `json:"model"`
	Agent         string        `json:"agent,omitempty"`
	StartTime     string        `json:"start_time"` // RFC3339
	Title         string        `json:"title,omitempty"`
	// Task metadata. Additive — legacy index files simply lack these keys, which
	// unmarshal to zero values (not pinned / not archived / read).
	Pinned    bool   `json:"pinned,omitempty"`
	Archived  bool   `json:"archived,omitempty"`
	Unread    bool   `json:"unread,omitempty"`
	Status    string `json:"status,omitempty"`     // idle/running/done/error (set by the web layer)
	UpdatedAt string `json:"updated_at,omitempty"` // RFC3339
	// TitleUserSet marks a title the user chose explicitly (TUI /rename, web
	// rename). Once set, automatic title writers (the async LLM refiner) must
	// not overwrite it — an in-flight suggestion can never clobber what the
	// user just saved, in this or any other transport's process.
	TitleUserSet bool `json:"title_user_set,omitempty"`
	// Automation metadata. A run launched by an automation is a normal session
	// tagged here: AutomationID is the correlation key for the "Recent runs"
	// list, and the main task list excludes any session with AutomationID set so
	// nightly runs don't pollute the sidebar. TerminalStatus/EndTime/ErrorReason
	// are the run-outcome audit fields (success|error|interrupted) that back the
	// Status filter — Status alone is only idle/running.
	AutomationID   string `json:"automation_id,omitempty"`
	TriggerKind    string `json:"trigger_kind,omitempty"` // scheduled|manual
	TerminalStatus string `json:"terminal_status,omitempty"`
	EndTime        string `json:"end_time,omitempty"`
	ErrorReason    string `json:"error_reason,omitempty"`
	// Artifact summary is a repairable materialized view over artifact entries.
	ArtifactCount     int    `json:"artifact_count,omitempty"`
	ArtifactUnseen    bool   `json:"artifact_unseen,omitempty"`
	ArtifactUpdatedAt string `json:"artifact_updated_at,omitempty"`
	ArtifactViewedAt  string `json:"artifact_viewed_at,omitempty"`
	// ArtifactViewedRevisions is the durable per-artifact read cursor. The
	// legacy ArtifactViewedAt timestamp remains a read-only fallback for index
	// files written by older versions; new views never advance that global
	// timestamp because doing so would mark unrelated artifacts as read.
	ArtifactViewedRevisions map[string]int `json:"artifact_viewed_revisions,omitempty"`
}

// ProjectMeta is project-level metadata kept in its own file (projects.json,
// alongside the session index). UpdatedAt is the project's "last activity"
// timestamp: it is bumped when a session is created or when a session's own
// UpdatedAt moves (a real turn), and — deliberately — is NEVER rolled back
// when a session is deleted. The sidebar sorts projects by this timestamp, so
// deleting a conversation must not reorder the project list.
//
// Stored separately from session.json on purpose: older binaries rewriting
// the index don't know this data exists, and would silently drop an embedded
// "projects" section on their next read-modify-write (the index is shared
// across processes — desktop sidecar + CLI may run different versions). A
// sidecar file they never touch is immune; losing it only degrades the
// sidebar to session-derived recency, and the next turn re-stamps it.
type ProjectMeta struct {
	UpdatedAt     string        `json:"updated_at,omitempty"` // RFC3339
	WorkspaceKind WorkspaceKind `json:"workspace_kind,omitempty"`
}

// sessionIndex is the on-disk structure of session.json.
type sessionIndex struct {
	Sessions map[string][]SessionMeta `json:"sessions"` // project path → metas
}

// isNewerTimestamp reports whether candidate is a strictly newer instant than
// current, comparing parsed RFC3339 instants — NOT raw strings. String
// comparison breaks across UTC offsets ("05:00Z" is the same instant as
// "13:00+08:00" yet compares smaller), and the index mixes local-offset
// writes (time.Now().Format(time.RFC3339)) with UTC ones. Unparseable
// candidates never win; any candidate beats an unparseable current value.
func isNewerTimestamp(candidate, current string) bool {
	ca, err := time.Parse(time.RFC3339, candidate)
	if err != nil {
		return false
	}
	if current == "" {
		return true
	}
	cur, err := time.Parse(time.RFC3339, current)
	if err != nil {
		return true
	}
	return ca.After(cur)
}

// touchProjectLocked bumps the project's persisted last-activity timestamp to
// ts when ts is a newer instant than the stored value (monotonic max — an
// out-of-order write never moves the timestamp backwards). Callers must hold
// indexMu, which serializes every read-modify-rename writer of BOTH the
// session index and the projects file.
func touchProjectLocked(project, ts string, kind WorkspaceKind) error {
	if project == "" || ts == "" {
		return nil
	}
	projects, err := loadProjectsLocked()
	if err != nil {
		return err
	}
	if projects == nil {
		projects = make(map[string]ProjectMeta)
	}
	current := projects[project]
	changed := false
	if isNewerTimestamp(ts, current.UpdatedAt) {
		current.UpdatedAt = ts
		changed = true
	}
	if NormalizeWorkspaceKind(kind) == WorkspaceScratch && current.WorkspaceKind != WorkspaceScratch {
		current.WorkspaceKind = WorkspaceScratch
		changed = true
	}
	if !changed {
		return nil
	}
	projects[project] = current
	return saveProjectsLocked(projects)
}

// projectsFilePath returns the path of the per-project metadata file that
// lives next to the session index.
func projectsFilePath() (string, error) {
	dir, err := config.SessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "projects.json"), nil
}

// loadProjectsLocked reads the projects file. A missing file (legacy install,
// no activity since upgrade) yields a nil map, not an error.
func loadProjectsLocked() (map[string]ProjectMeta, error) {
	path, err := projectsFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var projects map[string]ProjectMeta
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

// saveProjectsLocked atomically persists the projects map (temp + rename,
// private mode — same discipline as the session index writers).
func saveProjectsLocked(projects map[string]ProjectMeta) error {
	path, err := projectsFilePath()
	if err != nil {
		return err
	}
	if err := ensurePrivateSessionDir(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := writePrivateSessionFile(tmpPath, data); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func ensurePrivateSessionDir(path string) error {
	if err := os.MkdirAll(path, privateSessionDirMode); err != nil {
		return err
	}
	return os.Chmod(path, privateSessionDirMode)
}

func writePrivateSessionFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, privateSessionFileMode); err != nil {
		return err
	}
	return os.Chmod(path, privateSessionFileMode)
}

func durableReplacePrivateSessionFile(path string, data []byte) (resultErr error) {
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, "."+filepath.Base(path)+".rewrite-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if temporaryPath != "" {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(privateSessionFileMode); err != nil {
		return err
	}
	remaining := data
	for len(remaining) > 0 {
		written, writeErr := temporary.Write(remaining)
		if writeErr != nil {
			return writeErr
		}
		if written <= 0 {
			return fmt.Errorf("write durable session replacement: no progress")
		}
		remaining = remaining[written:]
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	temporaryPath = ""
	if err := syncSessionDirectory(dir); err != nil {
		return fmt.Errorf("sync session directory: %w", err)
	}
	return nil
}

func openPrivateSessionAppend(path string) (*os.File, error) {
	if err := os.Chmod(path, privateSessionFileMode); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_APPEND|os.O_WRONLY, privateSessionFileMode)
}

// Recorder appends events to a JSONL session file synchronously.
// The file and index entry are created lazily on the first real message so that
// sessions with no conversation are never persisted.
// Call Close() (or defer it) to finalize.
type Recorder struct {
	uuid          string
	project       string
	workspaceKind WorkspaceKind
	provider      string
	model         string
	agent         string
	startTime     time.Time
	file          *os.File
	mu            sync.Mutex
	// Per-teammate fields (empty for leader recorder).
	customDir string // leader UUID for subagent path
	agentID   string // teammate agent ID
	// resuming is true when loading an existing session via --resume.
	// In this mode, ensureFile opens the existing file for append instead of creating new.
	resuming bool
	title    string
	hasTitle bool // true after title has been generated or when resuming
	// userTitle is true once the user renamed this session explicitly (e.g.
	// TUI /rename). Later automatic SetTitleFor calls are then dropped so an
	// in-flight LLM suggestion cannot overwrite the user's choice.
	userTitle bool
	// titleRefiner, when set, is invoked once with the first user message after
	// the truncated fallback title is persisted. Implementations spawn their own
	// goroutine for slow work (e.g. LLM title generation) and call SetTitle.
	titleRefiner func(firstUserMsg string)
	// pendingSystemPrompt / pendingEnvInfo buffer the system prompt until the
	// first real message is recorded so that opening and immediately closing
	// jcode does not leave an empty session on disk.
	pendingSystemPrompt string
	pendingEnvInfo      string
	// toolOverrides is a lazy replay cache. CompareAndSwapSessionToolOverride
	// updates it only after the JSONL append has been fsynced successfully.
	toolOverrides       map[SessionTool]SessionToolOverride
	toolOverridesLoaded bool
}

// NewRecorder returns a Recorder that will create the session file only when
// the first message is recorded.  Never returns an error — recording is
// best-effort and must not break normal operation.
func NewRecorder(project, provider, model string) (*Recorder, error) {
	return &Recorder{
		uuid:          uuid.New().String(),
		project:       project,
		workspaceKind: WorkspaceProject,
		provider:      provider,
		model:         model,
		startTime:     time.Now(),
	}, nil
}

// UUID returns the session identifier. Locked because SetUUID can update it
// concurrently (a resumed web task swaps the recorder's UUID under r.mu).
func (r *Recorder) UUID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.uuid
}

// Project returns the workspace path this recorder is scoped to.
func (r *Recorder) Project() string { return r.project }

// WorkspaceKind returns the conversation's persisted workspace classification.
func (r *Recorder) WorkspaceKind() WorkspaceKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	return NormalizeWorkspaceKind(r.workspaceKind)
}

// SetWorkspaceKind classifies a recorder before its first durable entry. It is
// also safe on resumed recorders: the index is already authoritative there.
func (r *Recorder) SetWorkspaceKind(kind WorkspaceKind) {
	r.mu.Lock()
	r.workspaceKind = NormalizeWorkspaceKind(kind)
	r.mu.Unlock()
}

// Provider returns the provider currently attributed to recorded usage.
func (r *Recorder) Provider() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.provider
}

// Model returns the model currently attributed to recorded usage.
func (r *Recorder) Model() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.model
}

// SetProviderModel updates the inseparable provider/model pair attributed to
// subsequently recorded usage. Before the first entry it also determines the
// session_start pair; afterwards the immutable opening header is retained while
// the session index follows the current pair.
func (r *Recorder) SetProviderModel(provider, model string) {
	r.mu.Lock()
	r.provider = provider
	r.model = model
	hasRecording := r.file != nil
	id := r.uuid
	r.mu.Unlock()
	if hasRecording {
		_, _ = UpdateSessionMeta(id, func(m *SessionMeta) {
			m.Provider = provider
			m.Model = model
		})
	}
}

// SetAgent records the selected top-level custom agent. Empty means the default
// agent. Before the first message it is buffered into session_start; after a
// session exists it also appends an agent_change event for replay.
func (r *Recorder) SetAgent(agent string) {
	agent = strings.TrimSpace(agent)
	r.mu.Lock()
	if r.agent == agent {
		r.mu.Unlock()
		return
	}
	r.agent = agent
	hasRecording := r.file != nil
	id := r.uuid
	r.mu.Unlock()
	if !hasRecording {
		return
	}
	_, _ = UpdateSessionMeta(id, func(m *SessionMeta) {
		m.Agent = agent
	})
	_ = r.writeEntry(Entry{Type: EntryAgentChange, Agent: agent})
}

// ValidateSessionID checks that a session ID is safe for use as a filename.
// It rejects empty IDs, path traversal sequences, and path separators.
func ValidateSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("session ID must not be empty")
	}
	if id == "." || id == ".." {
		return fmt.Errorf("invalid session ID: %q", id)
	}
	if strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("session ID must not contain path separators: %q", id)
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("session ID must not contain path traversal: %q", id)
	}
	return nil
}

// SetUUID overrides the session identifier. Used when resuming an existing session
// so that new messages are appended to the same session file.
// If the session file does not yet exist on disk, the recorder is NOT marked as
// resuming, so the first user message will still generate a title.
func (r *Recorder) SetUUID(id string) {
	if err := ValidateSessionID(id); err != nil {
		config.Logger().Printf("[session] SetUUID rejected: %v", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.uuid = id
	r.toolOverrides = nil
	r.toolOverridesLoaded = false
	// Only mark as resuming if the session file already exists on disk.
	// For brand-new sessions with a client-provided UUID, we still want
	// title generation on the first user message.
	dir, err := config.SessionsDir()
	if err == nil {
		filePath := filepath.Join(dir, id+".json")
		if _, statErr := os.Stat(filePath); statErr == nil {
			r.resuming = true
		}
	}
	// If a file was already opened with the old UUID, close it so the next
	// write opens the correct file for append.
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
}

// HasRecording reports whether any message has been recorded (i.e. the
// session file has been created).  Returns false for sessions where the
// user quit without any conversation.
func (r *Recorder) HasRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file != nil
}

// RecordUser appends a user message entry.
// On the first user message, the title is auto-generated from the content.
// Optional images are persisted alongside the text so they survive session restore.
func (r *Recorder) RecordUser(content string, images ...EntryImage) {
	r.mu.Lock()
	needsTitle := !r.hasTitle && !r.resuming
	var refine func(string)
	if needsTitle {
		// Claim atomically: concurrent first messages must generate exactly one
		// title and fire the refiner exactly once. The refiner is one-shot —
		// clearing it here also releases its closure (config/factory) for GC.
		r.hasTitle = true
		refine = r.titleRefiner
		r.titleRefiner = nil
	}
	r.mu.Unlock()
	_ = r.writeEntry(Entry{Type: EntryUser, Content: content, Images: images})
	if needsTitle {
		r.mu.Lock()
		id := r.uuid
		r.mu.Unlock()
		r.SetTitleFor(id, generateTitle(content))
		if refine != nil {
			refine(content)
		}
	}
}

// SetTitleRefiner installs a hook invoked once with the first user message,
// right after the truncated fallback title is persisted. The hook must not
// block; it upgrades the title asynchronously via SetTitleFor.
func (r *Recorder) SetTitleRefiner(fn func(firstUserMsg string)) {
	r.mu.Lock()
	r.titleRefiner = fn
	r.mu.Unlock()
}

// Title returns the session's current title. It is "" for a recorder that has
// not titled a session yet (before the first message) and may also be "" for a
// resumed recorder whose title lives only in the index — callers that need the
// persisted title should fall back to FindSessionMeta.
func (r *Recorder) Title() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.title
}

// SetTitleFor overrides the session title and persists it to the shared index
// — but only while the recorder still records session id. A title computed for
// one session (e.g. by the async LLM refiner) must never clobber another
// session's index entry after SetUUID re-points this recorder (the TUI /resume
// path reuses the live recorder). Empty titles are ignored, and so is any
// automatic write after the user renamed the session explicitly (see
// SetUserTitle).
func (r *Recorder) SetTitleFor(id, title string) {
	title = strings.TrimSpace(title)
	if title == "" || id == "" {
		return
	}
	r.mu.Lock()
	if r.uuid != id {
		r.mu.Unlock()
		config.Logger().Printf("[session] dropping stale title for %s (recorder now on %s)", id, r.uuid)
		return
	}
	if r.userTitle {
		r.mu.Unlock()
		config.Logger().Printf("[session] dropping automatic title for %s (user set the title)", id)
		return
	}
	r.title = title
	r.hasTitle = true
	project := r.project
	r.mu.Unlock()
	_ = updateIndexTitle(project, id, title, false)
}

// SetUserTitle persists an explicit, user-issued rename (TUI /rename) for
// session id and marks it user-owned: every later automatic SetTitleFor —
// including one from a refiner goroutine still in flight — is dropped, and the
// index write refuses to clobber the user title even across processes.
// Returns a descriptive error when the id is stale (the recorder was re-pointed
// by /resume), the session has no index entry yet (nothing recorded), or the
// index write fails.
func (r *Recorder) SetUserTitle(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("title must not be empty")
	}
	r.mu.Lock()
	if r.uuid != id {
		current := r.uuid
		r.mu.Unlock()
		return fmt.Errorf("session changed (was %s, recorder now on %s): rename cancelled", id, current)
	}
	if !r.hasTitle && !r.resuming && r.file == nil {
		r.mu.Unlock()
		return fmt.Errorf("session has no messages yet: send a message before renaming")
	}
	r.title = title
	r.hasTitle = true
	r.userTitle = true
	project := r.project
	r.mu.Unlock()
	if err := updateIndexTitle(project, id, title, true); err != nil {
		return fmt.Errorf("saving title: %w", err)
	}
	return nil
}

// RecordAssistant appends an assistant message entry.
func (r *Recorder) RecordAssistant(content string) {
	r.RecordAssistantMessage(&schema.Message{Role: schema.Assistant, Content: content})
}

// RecordAssistantMessage appends the persistable subset of an assistant
// message. Cleartext reasoning and arbitrary Extra fields are never written;
// only canonical encrypted Responses continuation items are retained.
func (r *Recorder) RecordAssistantMessage(message *schema.Message) {
	if message == nil {
		return
	}
	items := responsemeta.FromExtra(message.Extra)
	// Opaque continuation data is meaningful only when the same assistant turn
	// has visible content or function calls. In particular, never leave an
	// encrypted-reasoning-only entry behind after a failed/cancelled stream.
	if message.Content == "" && len(message.ToolCalls) == 0 {
		items = nil
	}
	if message.Content == "" && len(items) == 0 {
		return
	}
	_ = r.writeEntry(Entry{
		Type:                EntryAssistant,
		Content:             message.Content,
		OpaqueResponseItems: items,
	})
}

// RecordToolCall appends a tool-call entry. The batch fields group tool calls
// issued by the same assistant message so replay can rebuild batch boundaries
// (batchSize > 1 means a concurrent batch).
func (r *Recorder) RecordToolCall(name, args, toolCallID, batchID string, batchIndex, batchSize int) {
	_ = r.writeEntry(Entry{
		Type: EntryToolCall, Name: name, Args: args, ToolCallID: toolCallID,
		BatchID: batchID, BatchIndex: batchIndex, BatchSize: batchSize,
	})
}

// RecordToolResult appends a tool-result entry and returns the exact output
// stored in the transcript. denied marks a user-rejected approval; duration is
// the approval-wait-adjusted execution latency (0 when unknown). Large outputs
// are automatically truncated (head+tail preserved) and the full content is
// saved to an overflow file on disk. Callers should use the returned output in
// live model history so live and replayed sessions have the same context. A
// persistence failure is returned so callers do not treat an unstored result as
// durable conversation history.
func (r *Recorder) RecordToolResult(name, output, toolCallID string, err error, denied bool, duration time.Duration) (string, error) {
	return r.RecordToolResultWithDetails(name, output, toolCallID, err, denied, duration, ToolResultDetails{})
}

// ToolResultDetails persists the transport-neutral terminal evidence needed to
// reconstruct a provider-backed operation without parsing its human-readable
// output. It deliberately contains identifiers and classifications only.
type ToolResultDetails struct {
	OperationID string
	Outcome     string
	ErrorCode   string
	Provider    string
	Model       string
	ArtifactIDs []string
}

func (r *Recorder) RecordToolResultWithDetails(
	name, output, toolCallID string,
	err error,
	denied bool,
	duration time.Duration,
	details ToolResultDetails,
) (string, error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	output = TruncateToolOutput(output, r.uuid, toolCallID)
	if writeErr := r.writeEntry(Entry{
		Type: EntryToolResult, Name: name, Output: output, ToolCallID: toolCallID, Error: errStr,
		Denied: denied, DurationMs: duration.Milliseconds(),
		OperationID: details.OperationID, Outcome: details.Outcome, ErrorCode: details.ErrorCode,
		Provider: details.Provider, Model: details.Model,
		ArtifactIDs: append([]string(nil), details.ArtifactIDs...),
	}); writeErr != nil {
		return output, fmt.Errorf("record tool result in session %s for tool call %q: %w",
			r.sessionFilePathForError(), toolCallID, writeErr)
	}
	return output, nil
}

// sessionFilePathForError returns the recorder's intended transcript path for
// diagnostics. It is best-effort because resolving the config directory may be
// the same operation that caused the write to fail.
func (r *Recorder) sessionFilePathForError() string {
	dir, err := config.SessionsDir()
	if err != nil {
		return r.UUID()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agentID != "" {
		return filepath.Join(dir, r.customDir, "subagents", "agent-"+r.agentID+".jsonl")
	}
	return filepath.Join(dir, r.uuid+".json")
}

// RecordToolObservation appends metadata-only progressive-disclosure evidence.
func (r *Recorder) RecordToolObservation(observation ToolObservation) {
	_ = r.writeEntry(Entry{Type: EntryToolObservation, ToolObservation: &observation})
}

// RecordArtifact durably appends one metadata-only Artifact revision. Unlike
// the historical best-effort recorder helpers, it returns append failures so
// the show_artifact tool cannot report a revision that was never persisted.
func (r *Recorder) RecordArtifact(record artifact.Record) error {
	entry := Entry{
		Type: EntryArtifact, ArtifactID: record.ID, ArtifactPath: record.RelativePath,
		ArtifactStorageKind: string(record.StorageKind), ArtifactKey: record.RelativeKey,
		ArtifactTitle: record.Title, ArtifactKind: string(record.Kind), ArtifactMediaType: record.MediaType,
		ArtifactSize: record.Size, ArtifactWidth: record.Width, ArtifactHeight: record.Height,
		ArtifactSHA256: record.SHA256, ArtifactProviderID: record.ProviderID,
		ArtifactModelID: record.ModelID, ArtifactParentID: record.ParentArtifactID,
		OperationID: record.OperationID, ToolCallID: record.ToolCallID,
		ArtifactRevision: record.Revision, ArtifactFocus: record.Focus,
		ArtifactShareable: record.Shareable,
	}
	sessionID := r.UUID()
	lock, err := acquireSessionSecurityLock(sessionID)
	if err != nil {
		return err
	}
	defer func() { _ = lock.release() }()
	r.mu.Lock()
	if r.uuid != sessionID {
		r.mu.Unlock()
		return fmt.Errorf("session changed while recording artifact")
	}
	err = r.writePathBoundEntryLocked(entry)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	if err := reconcileArtifactSummary(r.UUID()); err != nil {
		config.Logger().Printf("[artifact] reconcile session summary %s: %v", r.UUID(), err)
	}
	return nil
}

// RecordGenerationOperation synchronously appends and fsyncs one provider
// dispatch transition. Callers must receive nil before initiating the billable
// POST. The operation contains fingerprints only, never raw credentials.
func (r *Recorder) RecordGenerationOperation(operation GenerationOperation) error {
	if err := operation.validate(); err != nil {
		return err
	}
	updatedAt := operation.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	sessionID := r.UUID()
	lock, err := acquireSessionSecurityLock(sessionID)
	if err != nil {
		return err
	}
	defer func() { _ = lock.release() }()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.uuid != sessionID {
		return fmt.Errorf("session changed while recording generation operation")
	}
	return r.writeSecurityEntryLocked(generationOperationEntry(operation, updatedAt))
}

func generationOperationEntry(operation GenerationOperation, updatedAt time.Time) Entry {
	return Entry{
		Type: EntryGenerationOperation, OperationID: operation.OperationID,
		ToolCallID: operation.ToolCallID, OperationState: string(operation.State),
		OperationCapabilityKey:         &operation.CapabilityKey,
		OperationCredentialFingerprint: operation.CredentialFingerprint,
		OperationConfigEpoch:           operation.ConfigEpoch,
		OperationIdempotencyKey:        operation.IdempotencyKey,
		OperationProviderRequestIDHash: operation.ProviderRequestIDHash,
		ArtifactIDs:                    append([]string(nil), operation.ArtifactIDs...),
		ErrorCode:                      operation.ErrorCode, OperationUpdatedAt: updatedAt.Format(time.RFC3339Nano),
	}
}

func reconcileArtifactSummary(sessionID string) error {
	records, err := LoadArtifactRecords(sessionID)
	if err != nil {
		return err
	}
	latest := time.Time{}
	for i := range records {
		if records[i].UpdatedAt.After(latest) {
			latest = records[i].UpdatedAt
		}
	}
	_, err = UpdateSessionMeta(sessionID, func(meta *SessionMeta) {
		meta.ArtifactCount = len(records)
		if !latest.IsZero() {
			meta.ArtifactUpdatedAt = latest.Format(time.RFC3339Nano)
		}
		legacyViewedAt, _ := time.Parse(time.RFC3339Nano, meta.ArtifactViewedAt)
		meta.ArtifactUnseen = false
		for i := range records {
			if meta.ArtifactViewedRevisions[records[i].ID] >= records[i].Revision {
				continue
			}
			if !legacyViewedAt.IsZero() && !records[i].UpdatedAt.After(legacyViewedAt) {
				continue
			}
			meta.ArtifactUnseen = true
			break
		}
	})
	return err
}

var (
	// ErrArtifactNotFound reports that an opaque artifact ID is not present in
	// the requested session.
	ErrArtifactNotFound = errors.New("artifact not found")
	// ErrArtifactRevisionMismatch prevents a stale viewer from marking a newer
	// artifact revision as read.
	ErrArtifactRevisionMismatch = errors.New("artifact revision changed")
)

// MarkArtifactViewed advances exactly one artifact's revision cursor. It is
// serialized with artifact journal appends so a concurrent new revision is
// always left unseen. The operation never advances the legacy session-wide
// timestamp.
func MarkArtifactViewed(sessionID, artifactID string, revision int) error {
	if err := ValidateSessionID(sessionID); err != nil {
		return err
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" || len(artifactID) > 512 || revision <= 0 {
		return ErrArtifactNotFound
	}
	lock, err := acquireSessionSecurityLock(sessionID)
	if err != nil {
		return err
	}
	defer func() { _ = lock.release() }()

	records, err := LoadArtifactRecords(sessionID)
	if err != nil {
		return err
	}
	found := false
	for i := range records {
		if records[i].ID != artifactID {
			continue
		}
		found = true
		if records[i].Revision != revision {
			return ErrArtifactRevisionMismatch
		}
		break
	}
	if !found {
		return ErrArtifactNotFound
	}
	meta, err := UpdateSessionMeta(sessionID, func(meta *SessionMeta) {
		if meta.ArtifactViewedRevisions == nil {
			meta.ArtifactViewedRevisions = make(map[string]int)
		}
		if meta.ArtifactViewedRevisions[artifactID] < revision {
			meta.ArtifactViewedRevisions[artifactID] = revision
		}
	})
	if err != nil {
		return err
	}
	if meta == nil {
		return ErrArtifactNotFound
	}
	return reconcileArtifactSummary(sessionID)
}

// RecordPlanUpdate appends a plan state change entry.
func (r *Recorder) RecordPlanUpdate(status, title, content, feedback string) {
	_ = r.writeEntry(Entry{
		Type:        EntryPlanUpdate,
		PlanStatus:  status,
		PlanTitle:   title,
		PlanContent: content,
		Feedback:    feedback,
	})
}

// RecordGoalUpdate appends a goal state change entry. An empty status records
// a "cleared" marker so resume knows the goal was removed.
func (r *Recorder) RecordGoalUpdate(objective, status string, tokensUsed, createdAt, updatedAt int64) {
	if status == "" {
		status = "cleared"
	}
	_ = r.writeEntry(Entry{
		Type:           EntryGoalUpdate,
		GoalObjective:  objective,
		GoalStatus:     status,
		GoalTokensUsed: tokensUsed,
		GoalCreatedAt:  createdAt,
		GoalUpdatedAt:  updatedAt,
	})
}

// RecordTodoSnapshot appends a full todo list snapshot entry.
func (r *Recorder) RecordTodoSnapshot(todos []TodoSnapshotItem) {
	_ = r.writeEntry(Entry{Type: EntryTodoSnapshot, Todos: todos})
}

// RecordSubagentStart appends a subagent launch entry.
func (r *Recorder) RecordSubagentStart(name, agentType string) {
	_ = r.writeEntry(Entry{Type: EntrySubagentStart, SubagentName: name, SubagentType: agentType})
}

// RecordSubagentResult appends a subagent completion entry.
func (r *Recorder) RecordSubagentResult(name, output string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	_ = r.writeEntry(Entry{Type: EntrySubagentResult, SubagentName: name, Output: output, Error: errStr})
}

// RecordSubagentAsync appends an async subagent launch entry with the task ID.
func (r *Recorder) RecordSubagentAsync(name, taskID, agentType string) {
	_ = r.writeEntry(Entry{Type: EntrySubagentAsync, SubagentName: name, Output: taskID, SubagentType: agentType})
}

// RecordModeChange appends a mode transition entry on a best-effort basis.
// Authorization-sensitive callers that must not publish an in-memory mode
// before it is durable use RecordModeChangeStrict instead.
func (r *Recorder) RecordModeChange(mode string) {
	if err := r.RecordModeChangeStrict(mode); err != nil {
		config.Logger().Printf("[session] record mode change: %v", err)
	}
}

// RecordModeChangeStrict durably appends a canonical unified mode transition.
// Mode is authorization state (Full access removes per-call approval), so it is
// serialized with transcript replacement and bound to the current on-disk
// inode just like the other append-only session metadata. A nil return is the
// caller's permission to publish the corresponding in-memory/UI transition.
func (r *Recorder) RecordModeChangeStrict(mode string) error {
	if !validRecordedSessionMode(mode) {
		return fmt.Errorf("invalid session mode %q", mode)
	}

	sessionID := r.UUID()
	lock, err := acquireSessionSecurityLock(sessionID)
	if err != nil {
		return err
	}
	defer func() { _ = lock.release() }()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.uuid != sessionID {
		return fmt.Errorf("session changed while recording mode")
	}
	return r.writePathBoundEntryLocked(Entry{Type: EntryModeChange, Mode: mode})
}

func validRecordedSessionMode(mode string) bool {
	switch mode {
	case "approval", "plan", "auto", "full_access":
		return true
	default:
		return false
	}
}

// RecordCompact appends a compact/summarization event entry. keptN is the
// number of trailing messages preserved verbatim alongside the summary, so a
// resume can rebuild the same tail from the entries already on disk.
func (r *Recorder) RecordCompact(summary string, compactedN, keptN int) {
	_ = r.writeEntry(Entry{Type: EntryCompact, Summary: summary, CompactedN: compactedN, KeptN: keptN})
}

// RecordSystemPrompt buffers the system prompt so it can be written together
// with the first real message. This avoids creating a session file when the
// user opens and immediately closes jcode without any conversation.
func (r *Recorder) RecordSystemPrompt(prompt, envInfo string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendingSystemPrompt = prompt
	r.pendingEnvInfo = envInfo
}

// TruncateAtUserMessage rewrites the conversational projection, keeping only
// entries that appear before the (beforeCount)th user message (0-indexed).
// Security-critical provider dispatch journals and session tool policy entries
// are append-only and survive truncation wherever they occur in the file. If
// beforeCount == 0, conversational history is truncated to session_start.
// The recorder is reset to append mode on the (now shorter) file.
// This preserves the session UUID and index entry — no new session is created.
func (r *Recorder) TruncateAtUserMessage(beforeCount int) error {
	sessionID := r.UUID()
	lock, err := acquireSessionSecurityLock(sessionID)
	if err != nil {
		return err
	}
	defer func() { _ = lock.release() }()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.uuid != sessionID {
		return fmt.Errorf("session changed while truncating history")
	}

	if r.agentID != "" {
		return fmt.Errorf("TruncateAtUserMessage not supported for teammate recorders")
	}

	dir, err := config.SessionsDir()
	if err != nil {
		return err
	}
	filePath := filepath.Join(dir, r.uuid+".json")

	// Load existing entries.
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing to truncate.
			return nil
		}
		return fmt.Errorf("read session file: %w", err)
	}

	// Collect entries to keep: session_start and append-only security journals
	// always, plus conversational entries before the target user message.
	var keep []string
	userCount := 0
	conversationCut := beforeCount == 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return fmt.Errorf("parse session line during truncation: %w", err)
		}
		// Always keep the session_start header.
		if e.Type == EntrySessionStart {
			keep = append(keep, line)
			continue
		}
		if appendOnlySessionEntry(e.Type) {
			keep = append(keep, line)
			continue
		}
		if conversationCut {
			continue
		}
		// Cut conversational entries as soon as we reach the Nth user message,
		// but continue scanning so later append-only journals are retained.
		if e.Type == EntryUser {
			if userCount >= beforeCount {
				conversationCut = true
				continue
			}
			userCount++
		}
		keep = append(keep, line)
	}

	// Atomically and durably rewrite the file. The replacement is fsynced
	// before rename and the parent directory is fsynced afterwards so a crash
	// cannot erase previously durable security journals.
	content := strings.Join(keep, "\n")
	if len(keep) > 0 {
		content += "\n"
	}
	// Keep the existing append handle alive until parsing has completely
	// succeeded. A malformed line must abort without rewriting the transcript
	// or leaving this Recorder unable to continue appending.
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
	if err := durableReplacePrivateSessionFile(filePath, []byte(content)); err != nil {
		return fmt.Errorf("write truncated session: %w", err)
	}

	// Reopen for append so subsequent writes go to the correct file.
	f, err := openPrivateSessionAppend(filePath)
	if err != nil {
		return fmt.Errorf("reopen session for append: %w", err)
	}
	r.file = f
	r.resuming = true
	r.toolOverrides = nil
	r.toolOverridesLoaded = false
	return nil
}

func securityJournalEntry(entryType EntryType) bool {
	switch entryType {
	case EntryGenerationOperation, EntryProviderToolOperation, EntrySessionToolOverride:
		return true
	default:
		return false
	}
}

func appendOnlySessionEntry(entryType EntryType) bool {
	return securityJournalEntry(entryType) || entryType == EntryArtifact || entryType == EntryModeChange
}

// Close flushes and closes the underlying file.  Safe to call multiple times.
// If no messages were ever recorded the file is never created.
func (r *Recorder) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
}

// NewTeammateRecorder creates a Recorder that stores its JSONL transcript
// under the leader session's subagents directory:
//
//	~/.jcode/sessions/{leaderUUID}/subagents/agent-{agentID}.jsonl
//
// This mirrors Claude Code's per-agent transcript pattern.
func NewTeammateRecorder(leaderUUID, agentID, model string) (*Recorder, error) {
	r := &Recorder{
		uuid:      leaderUUID + "/" + agentID,
		project:   agentID,
		provider:  "teammate",
		model:     model,
		startTime: time.Now(),
	}
	// Override ensureFile to write to subagent path.
	r.customDir = leaderUUID
	r.agentID = agentID
	return r, nil
}

// ensureFile creates the session file and writes the session_start header the
// first time it is called.  Must be called with r.mu held.
func (r *Recorder) ensureFile() error {
	if r.file != nil {
		return nil
	}
	dir, err := config.SessionsDir()
	if err != nil {
		return err
	}
	if err := ensurePrivateSessionDir(dir); err != nil {
		return fmt.Errorf("secure sessions dir: %w", err)
	}

	var filePath string
	if r.agentID != "" {
		// Teammate recorder: ~/.jcode/sessions/{leaderUUID}/subagents/agent-{agentID}.jsonl
		leaderDir := filepath.Join(dir, r.customDir)
		if err := ensurePrivateSessionDir(leaderDir); err != nil {
			return fmt.Errorf("create leader session dir: %w", err)
		}
		subDir := filepath.Join(leaderDir, "subagents")
		if err := ensurePrivateSessionDir(subDir); err != nil {
			return fmt.Errorf("create subagents dir: %w", err)
		}
		filePath = filepath.Join(subDir, "agent-"+r.agentID+".jsonl")
	} else {
		// Leader recorder: ~/.jcode/sessions/{uuid}.json
		filePath = filepath.Join(dir, r.uuid+".json")
	}

	// When resuming an existing session, open the file for append instead of creating new.
	if r.resuming {
		f, err := openPrivateSessionAppend(filePath)
		if err != nil {
			return fmt.Errorf("open session file for append: %w", err)
		}
		r.file = f
		return nil
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateSessionFileMode)
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	r.file = f

	// Write the header entry (timestamp already known).
	startEntry := Entry{
		Type:          EntrySessionStart,
		UUID:          r.uuid,
		Project:       r.project,
		WorkspaceKind: NormalizeWorkspaceKind(r.workspaceKind),
		Provider:      r.provider,
		Model:         r.model,
		Agent:         r.agent,
		Timestamp:     r.startTime.Format(time.RFC3339),
	}
	data, err := json.Marshal(startEntry)
	if err != nil {
		return err
	}
	if _, err = f.WriteString(string(data) + "\n"); err != nil {
		return err
	}

	// Update the shared index (non-fatal, skip for teammate recorders).
	if r.agentID == "" {
		_ = addToIndex(r.project, SessionMeta{
			UUID:          r.uuid,
			Project:       r.project,
			WorkspaceKind: NormalizeWorkspaceKind(r.workspaceKind),
			Provider:      r.provider,
			Model:         r.model,
			Agent:         r.agent,
			StartTime:     r.startTime.Format(time.RFC3339),
		})
	}
	return nil
}

func (r *Recorder) writeEntry(e Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writeEntryLocked(e)
}

// writeSecurityEntryLocked appends billing/policy evidence after first making
// sure this Recorder still points at the path currently protected by the
// session security lock. Another process may have atomically replaced the
// transcript during truncation while this Recorder retained an fd for the old,
// now-unlinked inode; writing through that stale fd would falsely report a
// durable append that replay can never observe.
func (r *Recorder) writeSecurityEntryLocked(e Entry) error {
	if !securityJournalEntry(e.Type) {
		return fmt.Errorf("entry type %q is not a security journal", e.Type)
	}
	return r.writePathBoundEntryLocked(e)
}

// writePathBoundEntryLocked appends metadata that must survive transcript
// replacement. It must be called with r.mu and the cross-process session lock
// held so truncate cannot drop the entry or redirect it to an unlinked inode.
func (r *Recorder) writePathBoundEntryLocked(e Entry) error {
	if !appendOnlySessionEntry(e.Type) {
		return fmt.Errorf("entry type %q is not append-only session metadata", e.Type)
	}
	if err := r.refreshSecurityAppendFileLocked(); err != nil {
		return err
	}
	return r.writeEntryLocked(e)
}

// refreshSecurityAppendFileLocked must be called with r.mu and the
// cross-process session security lock held.
func (r *Recorder) refreshSecurityAppendFileLocked() error {
	dir, err := config.SessionsDir()
	if err != nil {
		return err
	}
	var path string
	if r.agentID != "" {
		path = filepath.Join(dir, r.customDir, "subagents", "agent-"+r.agentID+".jsonl")
	} else {
		path = filepath.Join(dir, r.uuid+".json")
	}
	pathInfo, pathErr := os.Stat(path)
	if r.file == nil {
		if pathErr == nil {
			r.resuming = true
			return nil
		}
		if os.IsNotExist(pathErr) {
			return nil
		}
		return fmt.Errorf("stat current session journal: %w", pathErr)
	}
	if pathErr != nil {
		return fmt.Errorf("current session journal is unavailable: %w", pathErr)
	}
	openInfo, err := r.file.Stat()
	if err != nil {
		return fmt.Errorf("stat open session journal: %w", err)
	}
	if os.SameFile(openInfo, pathInfo) {
		return nil
	}
	old := r.file
	r.file = nil
	if err := old.Close(); err != nil {
		return fmt.Errorf("close stale session journal: %w", err)
	}
	fresh, err := openPrivateSessionAppend(path)
	if err != nil {
		return fmt.Errorf("reopen current session journal: %w", err)
	}
	r.file = fresh
	r.resuming = true
	return nil
}

// writeEntryLocked appends and fsyncs one entry while r.mu is held. Keeping
// the append primitive available under the recorder lock lets stateful CAS
// records validate their expected revision and durably append without a race
// window between those two operations.
func (r *Recorder) writeEntryLocked(e Entry) error {
	e.Timestamp = time.Now().Format(time.RFC3339)
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	// Lazily initialise the file on the first real write.
	if err := r.ensureFile(); err != nil {
		return err
	}
	// Flush buffered system prompt before the first real entry.
	if r.pendingSystemPrompt != "" {
		sp := Entry{
			Type:      EntrySystemPrompt,
			Content:   r.pendingSystemPrompt,
			EnvInfo:   r.pendingEnvInfo,
			Timestamp: e.Timestamp,
		}
		r.pendingSystemPrompt = ""
		r.pendingEnvInfo = ""
		if spData, spErr := json.Marshal(sp); spErr == nil {
			_, _ = r.file.WriteString(string(spData) + "\n")
		}
	}
	if _, err = r.file.WriteString(string(data) + "\n"); err != nil {
		return err
	}
	// Sync to disk so entries survive a crash.
	return r.file.Sync()
}

// indexMu serializes the read-modify-rename writers of the shared session index
// (session.json). Atomic rename prevents torn files but NOT lost updates: two
// concurrent writers each read the same old index, mutate, and rename — last one
// wins — and they also race on the shared "<index>.tmp" path. Once multiple web
// task Engines create/title/update/delete sessions in parallel this is a real
// lost-update + tmp-corruption hazard, so every writer takes this lock. Readers
// (ListSessions/ListAllSessions) rely on rename atomicity and stay lock-free.
var indexMu sync.Mutex

// addToIndex adds a SessionMeta to the shared index file.
func addToIndex(project string, meta SessionMeta) error {
	indexMu.Lock()
	defer indexMu.Unlock()
	indexPath, err := config.SessionsIndexPath()
	if err != nil {
		return err
	}
	// Ensure parent dir exists
	if err := ensurePrivateSessionDir(filepath.Dir(indexPath)); err != nil {
		return err
	}

	idx := &sessionIndex{Sessions: make(map[string][]SessionMeta)}
	if data, err := os.ReadFile(indexPath); err == nil {
		_ = json.Unmarshal(data, idx)
	}
	if idx.Sessions == nil {
		idx.Sessions = make(map[string][]SessionMeta)
	}

	idx.Sessions[project] = append(idx.Sessions[project], meta)
	newData, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: write to temp file then rename to prevent corruption
	// from concurrent writes or interrupted I/O.
	tmpPath := indexPath + ".tmp"
	if err := writePrivateSessionFile(tmpPath, newData); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, indexPath); err != nil {
		return err
	}
	// A new session is project activity: bump the project-level timestamp so
	// the sidebar's project ordering reflects it. Best-effort AFTER the index
	// write succeeded — a projects-file hiccup must not fail session creation
	// (the sidebar falls back to session-derived recency).
	_ = touchProjectLocked(project, meta.StartTime, meta.WorkspaceKind)
	return nil
}

// generateTitle creates a human-readable session title from the first user message.
// It truncates to a reasonable length and strips newlines.
func generateTitle(content string) string {
	// Take first line only
	title := content
	if idx := strings.IndexAny(title, "\n\r"); idx >= 0 {
		title = title[:idx]
	}
	title = strings.TrimSpace(title)
	if len(title) == 0 {
		return "New session"
	}
	// Truncate to 80 chars
	runes := []rune(title)
	if len(runes) > 80 {
		title = string(runes[:80]) + "…"
	}
	return title
}

// updateIndexTitle updates the title of a session in the shared index file.
// userSet distinguishes an explicit user rename from an automatic write (the
// async LLM refiner): once a session's meta carries TitleUserSet, automatic
// writes are dropped inside the same locked section — so an in-flight
// suggestion from any process cannot clobber the title the user saved. A user
// write always wins and sets the flag.
func updateIndexTitle(project, uuid, title string, userSet bool) error {
	indexMu.Lock()
	defer indexMu.Unlock()
	indexPath, err := config.SessionsIndexPath()
	if err != nil {
		return err
	}
	data, readErr := os.ReadFile(indexPath)
	if readErr != nil {
		return readErr
	}
	var idx sessionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}
	metas := idx.Sessions[project]
	found := false
	for i := range metas {
		if metas[i].UUID == uuid {
			found = true
			if metas[i].TitleUserSet && !userSet {
				config.Logger().Printf("[session] dropping automatic title for %s (user set the title)", uuid)
				return nil
			}
			metas[i].Title = title
			if userSet {
				metas[i].TitleUserSet = true
			}
			break
		}
	}
	if !found {
		return fmt.Errorf("session %s not found in index", uuid)
	}
	idx.Sessions[project] = metas
	newData, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := indexPath + ".tmp"
	if err := writePrivateSessionFile(tmpPath, newData); err != nil {
		return err
	}
	return os.Rename(tmpPath, indexPath)
}

// DeleteSessionByUUID removes a session (index entry + JSONL file) located by
// uuid across ALL projects. The web task tree can delete a task that does not
// belong to the active project, so we must not assume a single project key.
// Returns false if no session with that uuid exists. The JSONL file is only
// removed when the uuid was actually found in the index, which also prevents a
// crafted uuid from deleting an arbitrary file.
func DeleteSessionByUUID(uuid string) (bool, error) {
	indexMu.Lock()
	defer indexMu.Unlock()
	indexPath, err := config.SessionsIndexPath()
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var idx sessionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return false, err
	}
	found := false
	for project, metas := range idx.Sessions {
		filtered := make([]SessionMeta, 0, len(metas))
		for _, m := range metas {
			if m.UUID == uuid {
				found = true
				continue
			}
			filtered = append(filtered, m)
		}
		idx.Sessions[project] = filtered
		// Deliberately do NOT touch the projects file here: the project-level
		// timestamp records "last activity", and a deletion is not activity.
		// Leaving it alone keeps the sidebar's project ordering stable across
		// deletes (deleting the newest conversation must not sink its project).
	}
	if !found {
		return false, nil
	}
	newData, err := json.MarshalIndent(&idx, "", "  ")
	if err != nil {
		return true, err
	}
	tmpPath := indexPath + ".tmp"
	if err := writePrivateSessionFile(tmpPath, newData); err != nil {
		return true, err
	}
	if err := os.Rename(tmpPath, indexPath); err != nil {
		return true, err
	}
	dir, err := config.SessionsDir()
	if err != nil {
		return true, err
	}
	if rmErr := os.Remove(filepath.Join(dir, uuid+".json")); rmErr != nil && !os.IsNotExist(rmErr) {
		return true, fmt.Errorf("delete session file: %w", rmErr)
	}
	return true, nil
}

// ListSessions returns all sessions recorded for a given project path, newest last.
func ListSessions(project string) ([]SessionMeta, error) {
	indexPath, err := config.SessionsIndexPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx sessionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return idx.Sessions[project], nil
}

// UpdateSessionMeta finds a session by uuid across all projects, applies mutate
// to its metadata, and persists the index atomically. Returns the updated meta,
// or (nil, nil) if no session with that uuid exists. uuid is only compared in
// memory (never used as a path), so no path validation is required here.
func UpdateSessionMeta(uuid string, mutate func(*SessionMeta)) (*SessionMeta, error) {
	indexMu.Lock()
	defer indexMu.Unlock()
	indexPath, err := config.SessionsIndexPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx sessionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	for project, metas := range idx.Sessions {
		for i := range metas {
			if metas[i].UUID == uuid {
				beforeUpdatedAt := metas[i].UpdatedAt
				mutate(&metas[i])
				idx.Sessions[project] = metas
				newData, err := json.MarshalIndent(&idx, "", "  ")
				if err != nil {
					return nil, err
				}
				tmpPath := indexPath + ".tmp"
				if err := writePrivateSessionFile(tmpPath, newData); err != nil {
					return nil, err
				}
				if err := os.Rename(tmpPath, indexPath); err != nil {
					return nil, err
				}
				// A moved UpdatedAt means real activity (a turn started/finished —
				// the web layer's setTaskStatus). Mirror it onto the project-level
				// timestamp so the sidebar's project ordering tracks activity.
				// Metadata-only edits (pin/archive/rename) deliberately leave
				// UpdatedAt untouched, so they never reorder projects either.
				// Best-effort, like addToIndex: the index write already succeeded.
				if metas[i].UpdatedAt != beforeUpdatedAt {
					_ = touchProjectLocked(project, metas[i].UpdatedAt, metas[i].WorkspaceKind)
				}
				updated := metas[i]
				// The index keys sessions by project, so the stored meta may not
				// carry its own Project. Populate it on the returned copy so callers
				// can build a self-describing task view without re-deriving the path.
				updated.Project = project
				return &updated, nil
			}
		}
	}
	return nil, nil
}

// ListAllSessions returns all sessions across all projects, keyed by project path.
func ListAllSessions() (map[string][]SessionMeta, error) {
	indexPath, err := config.SessionsIndexPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx sessionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return idx.Sessions, nil
}

// FindSessionMeta returns the indexed metadata for uuid regardless of which
// project owns it. The project is populated from the index key because older
// index entries may omit the redundant Project field. A nil result means the
// UUID is not indexed.
func FindSessionMeta(uuid string) (*SessionMeta, error) {
	if err := ValidateSessionID(uuid); err != nil {
		return nil, err
	}
	all, err := ListAllSessions()
	if err != nil {
		return nil, err
	}
	var found *SessionMeta
	for project, metas := range all {
		for i := range metas {
			if metas[i].UUID != uuid {
				continue
			}
			if found != nil {
				return nil, fmt.Errorf("session %s is indexed under multiple projects", uuid)
			}
			meta := metas[i]
			meta.Project = project
			found = &meta
		}
	}
	return found, nil
}

// ListProjectMeta returns the per-project metadata (last-activity timestamps)
// keyed by project path. A nil map (legacy install: no projects.json yet) is
// returned as-is; callers fall back to deriving recency from sessions.
// Lock-free like ListSessions/ListAllSessions: writers persist via atomic
// rename, so readers always observe a complete file.
func ListProjectMeta() (map[string]ProjectMeta, error) {
	return loadProjectsLocked()
}

// LoadSession reads all entries from a session JSONL file identified by uuid.
func LoadSession(id string) ([]Entry, error) {
	if err := ValidateSessionID(id); err != nil {
		return nil, err
	}
	dir, err := config.SessionsDir()
	if err != nil {
		return nil, err
	}
	filePath := filepath.Join(dir, id+".json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("session %s not found: %w", id, err)
	}

	var entries []Entry
	var skipped int
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			skipped++
			preview := line
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			config.Logger().Printf("[session] corrupted line in %s (skipped %d): %v — %s", id, skipped, err, preview)
			continue
		}
		entries = append(entries, e)
	}
	if skipped > 0 {
		config.Logger().Printf("[session] loaded %s with %d corrupted lines skipped", id, skipped)
	}
	return entries, nil
}

// LoadSessionModeStrict reads the latest durable unified mode without the
// conversational replay loader's corrupt-line tolerance. A malformed line
// could itself be a newer mode transition, so authorization restore must fail
// closed rather than silently skipping it and reviving an older Full access
// value. An empty result is the legacy/default Approval mode.
func LoadSessionModeStrict(id string) (string, error) {
	if err := ValidateSessionID(id); err != nil {
		return "", err
	}
	dir, err := config.SessionsDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return "", fmt.Errorf("session %s not found: %w", id, err)
	}

	latest := ""
	for index, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return "", fmt.Errorf("session mode journal line %d is malformed: %w", index+1, err)
		}
		if entry.Type != EntryModeChange {
			continue
		}
		if !validRecordedSessionMode(entry.Mode) {
			return "", fmt.Errorf("session mode journal line %d has invalid mode", index+1)
		}
		latest = entry.Mode
	}
	return latest, nil
}

// LoadArtifactRecords rebuilds the latest metadata revision for every Artifact
// in a session. Entry order is not trusted: the greatest revision wins, with a
// later timestamp breaking ties for defensive recovery from duplicated lines.
func LoadArtifactRecords(id string) ([]artifact.Record, error) {
	entries, err := LoadSession(id)
	if err != nil {
		return nil, err
	}
	latest := make(map[string]artifact.Record)
	for _, entry := range entries {
		if entry.Type != EntryArtifact || entry.ArtifactID == "" || entry.ArtifactRevision <= 0 {
			continue
		}
		updatedAt, _ := time.Parse(time.RFC3339Nano, entry.Timestamp)
		record := artifact.Record{
			ID: entry.ArtifactID, SessionID: id,
			StorageKind:  artifact.StorageKind(entry.ArtifactStorageKind),
			RelativePath: entry.ArtifactPath, RelativeKey: entry.ArtifactKey,
			Title: entry.ArtifactTitle, Kind: artifact.Kind(entry.ArtifactKind), MediaType: entry.ArtifactMediaType,
			Size: entry.ArtifactSize, Width: entry.ArtifactWidth, Height: entry.ArtifactHeight,
			SHA256: entry.ArtifactSHA256, ProviderID: entry.ArtifactProviderID,
			ModelID: entry.ArtifactModelID, ParentArtifactID: entry.ArtifactParentID,
			OperationID: entry.OperationID, ToolCallID: entry.ToolCallID,
			Revision: entry.ArtifactRevision, UpdatedAt: updatedAt,
			Status: artifact.StatusAvailable, Focus: entry.ArtifactFocus,
			Shareable: entry.ArtifactShareable || entry.ArtifactStorageKind != string(artifact.StorageManaged),
		}
		current, exists := latest[record.ID]
		if !exists || record.Revision > current.Revision ||
			(record.Revision == current.Revision && record.UpdatedAt.After(current.UpdatedAt)) {
			latest[record.ID] = record
		}
	}
	records := make([]artifact.Record, 0, len(latest))
	for _, record := range latest {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	return records, nil
}

// ReplayGenerationOperations rebuilds the latest monotonic projection for each
// operation and separately remembers whether its durable consume point was
// observed. It tolerates corrupted/out-of-order lines conservatively: terminal
// evidence never regresses to a progress state, while dispatch evidence is
// sticky for ledger reconstruction.
func ReplayGenerationOperations(entries []Entry) map[string]GenerationOperationSnapshot {
	snapshots := make(map[string]GenerationOperationSnapshot)
	for _, entry := range entries {
		if entry.Type != EntryGenerationOperation || entry.OperationID == "" {
			continue
		}
		operation := generationOperationFromEntry(entry)
		if operation.validate() != nil {
			continue
		}
		snapshot := snapshots[operation.OperationID]
		if operation.State == GenerationDispatchAttempted {
			snapshot.Dispatched = true
		}
		if snapshot.Latest.OperationID == "" || generationOperationWins(operation, snapshot.Latest) {
			snapshot.Latest = operation
		}
		snapshots[operation.OperationID] = snapshot
	}
	return snapshots
}

func generationOperationFromEntry(entry Entry) GenerationOperation {
	updatedAt, _ := time.Parse(time.RFC3339Nano, entry.OperationUpdatedAt)
	if updatedAt.IsZero() {
		updatedAt, _ = time.Parse(time.RFC3339Nano, entry.Timestamp)
	}
	capabilityKey := OperationCapabilityKey{}
	if entry.OperationCapabilityKey != nil {
		capabilityKey = *entry.OperationCapabilityKey
	}
	return GenerationOperation{
		OperationID: entry.OperationID, ToolCallID: entry.ToolCallID,
		State:                 GenerationOperationState(entry.OperationState),
		CapabilityKey:         capabilityKey,
		CredentialFingerprint: entry.OperationCredentialFingerprint,
		ConfigEpoch:           entry.OperationConfigEpoch,
		IdempotencyKey:        entry.OperationIdempotencyKey,
		ProviderRequestIDHash: entry.OperationProviderRequestIDHash,
		ArtifactIDs:           append([]string(nil), entry.ArtifactIDs...),
		ErrorCode:             entry.ErrorCode, UpdatedAt: updatedAt,
	}
}

func generationOperationWins(candidate, current GenerationOperation) bool {
	candidateRank := generationStateRank(candidate.State)
	currentRank := generationStateRank(current.State)
	if candidateRank != currentRank {
		return candidateRank > currentRank
	}
	if !candidate.UpdatedAt.Equal(current.UpdatedAt) {
		return candidate.UpdatedAt.After(current.UpdatedAt)
	}
	// Equal/legacy timestamps retain append order: Replay assigns the candidate
	// when it appears later in the JSONL stream.
	return true
}

// LoadGenerationOperations reads one session and returns snapshots sorted by
// operation ID for deterministic callers and tests.
func LoadGenerationOperations(id string) ([]GenerationOperationSnapshot, error) {
	entries, err := loadSecuritySession(id)
	if err != nil {
		return nil, err
	}
	byID := ReplayGenerationOperations(entries)
	result := make([]GenerationOperationSnapshot, 0, len(byID))
	for _, snapshot := range byID {
		result = append(result, snapshot)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Latest.OperationID < result[j].Latest.OperationID
	})
	return result, nil
}

// GenerationRecoveryEvidencePriority returns the fixed reconciliation ordering
// without deciding transport presentation. A terminal operation always wins;
// otherwise a safely verified artifact outranks a typed terminal tool result,
// which outranks a nonterminal operation.
func GenerationRecoveryEvidencePriority(
	snapshot GenerationOperationSnapshot,
	hasVerifiedArtifact bool,
	hasTerminalToolResult bool,
) GenerationRecoveryPriority {
	if snapshot.Latest.State.IsTerminal() {
		return GenerationRecoveryTerminalOperation
	}
	if hasVerifiedArtifact {
		return GenerationRecoveryArtifact
	}
	if hasTerminalToolResult {
		return GenerationRecoveryTerminalToolResult
	}
	if snapshot.Latest.OperationID != "" {
		return GenerationRecoveryNonTerminalOperation
	}
	return GenerationRecoveryNone
}
