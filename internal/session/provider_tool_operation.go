package session

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ProviderToolOperationState is the durable lifecycle for a billable
// provider-managed tool call. There is intentionally no pre-dispatch state in
// the journal: dispatch_attempted is the atomic consume point written and
// fsynced immediately before the wrapped endpoint is invoked.
type ProviderToolOperationState string

const (
	ProviderToolDispatchAttempted ProviderToolOperationState = "dispatch_attempted"
	ProviderToolSucceeded         ProviderToolOperationState = "succeeded"
	ProviderToolFailed            ProviderToolOperationState = "failed"
	ProviderToolUncertain         ProviderToolOperationState = "uncertain"
)

func (state ProviderToolOperationState) IsTerminal() bool {
	switch state {
	case ProviderToolSucceeded, ProviderToolFailed, ProviderToolUncertain:
		return true
	default:
		return false
	}
}

func (state ProviderToolOperationState) valid() bool {
	return state == ProviderToolDispatchAttempted || state.IsTerminal()
}

// ProviderToolOperation is metadata-only durable evidence for a provider tool
// call. IntentHash binds the approved post-hook arguments and credential
// fingerprint without persisting either value. Tool arguments, credentials,
// endpoint URLs, provider bodies, and tool results must never be added here.
type ProviderToolOperation struct {
	OperationID       string                     `json:"operation_id"`
	ToolCallID        string                     `json:"tool_call_id"`
	RunID             string                     `json:"run_id"`
	State             ProviderToolOperationState `json:"state"`
	CapabilityKey     string                     `json:"capability_key"`
	ProviderProfileID string                     `json:"provider_profile_id"`
	ToolName          string                     `json:"tool_name"`
	IntentHash        string                     `json:"intent_hash"`
	ConfigEpoch       string                     `json:"config_epoch"`
	IdempotencyKey    string                     `json:"idempotency_key"`
	ErrorCode         string                     `json:"error_code,omitempty"`
	UpdatedAt         time.Time                  `json:"updated_at"`
}

func (operation ProviderToolOperation) validate() error {
	if strings.TrimSpace(operation.OperationID) == "" {
		return fmt.Errorf("provider tool operation ID is required")
	}
	if strings.TrimSpace(operation.ToolCallID) == "" {
		return fmt.Errorf("provider tool call ID is required")
	}
	if strings.TrimSpace(operation.RunID) == "" {
		return fmt.Errorf("provider tool run ID is required")
	}
	if !operation.State.valid() {
		return fmt.Errorf("invalid provider tool operation state %q", operation.State)
	}
	if strings.TrimSpace(operation.CapabilityKey) == "" ||
		strings.TrimSpace(operation.ProviderProfileID) == "" ||
		strings.TrimSpace(operation.ToolName) == "" {
		return fmt.Errorf("provider tool capability identity is incomplete")
	}
	if strings.TrimSpace(operation.IntentHash) == "" ||
		strings.TrimSpace(operation.ConfigEpoch) == "" ||
		strings.TrimSpace(operation.IdempotencyKey) == "" {
		return fmt.Errorf("provider tool immutable intent is incomplete")
	}
	return nil
}

// ValidateProviderToolStart must be called before the first synchronous
// journal append and before invoking the external endpoint.
func ValidateProviderToolStart(operation ProviderToolOperation) error {
	if err := operation.validate(); err != nil {
		return err
	}
	if operation.State != ProviderToolDispatchAttempted {
		return fmt.Errorf("provider tool operation must start at %q", ProviderToolDispatchAttempted)
	}
	return nil
}

// ValidateProviderToolTransition permits exactly one terminal transition and
// enforces immutable approved intent. Duplicate state writes are accepted for
// idempotent repair, but a terminal state can never change.
func ValidateProviderToolTransition(previous, next ProviderToolOperation) error {
	if err := previous.validate(); err != nil {
		return fmt.Errorf("invalid previous provider tool operation: %w", err)
	}
	if err := next.validate(); err != nil {
		return err
	}
	if previous.OperationID != next.OperationID || previous.ToolCallID != next.ToolCallID ||
		previous.RunID != next.RunID ||
		previous.CapabilityKey != next.CapabilityKey ||
		previous.ProviderProfileID != next.ProviderProfileID ||
		previous.ToolName != next.ToolName || previous.IntentHash != next.IntentHash ||
		previous.ConfigEpoch != next.ConfigEpoch ||
		previous.IdempotencyKey != next.IdempotencyKey {
		return fmt.Errorf("provider tool operation immutable intent changed")
	}
	if previous.State.IsTerminal() && previous.State != next.State {
		return fmt.Errorf("provider tool operation cannot leave terminal state %q", previous.State)
	}
	if previous.State == next.State {
		return nil
	}
	if previous.State != ProviderToolDispatchAttempted || !next.State.IsTerminal() {
		return fmt.Errorf("invalid provider tool operation transition from %q to %q", previous.State, next.State)
	}
	return nil
}

// ProviderToolOperationSnapshot is the replay projection used to reconstruct
// usage limits. Dispatched is sticky once dispatch_attempted was durably
// observed, even if later terminal evidence is corrupt or missing.
type ProviderToolOperationSnapshot struct {
	Latest     ProviderToolOperation `json:"latest"`
	Dispatched bool                  `json:"dispatched"`
}

// RecordProviderToolOperation synchronously appends and fsyncs one metadata-
// only state transition. Callers must receive nil for dispatch_attempted
// before invoking the external endpoint.
func (r *Recorder) RecordProviderToolOperation(operation ProviderToolOperation) error {
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
		return fmt.Errorf("session changed while recording provider tool operation")
	}
	return r.writeSecurityEntryLocked(providerToolOperationEntry(operation, updatedAt))
}

func providerToolOperationEntry(operation ProviderToolOperation, updatedAt time.Time) Entry {
	return Entry{
		Type: EntryProviderToolOperation, OperationID: operation.OperationID,
		ToolCallID: operation.ToolCallID, ErrorCode: operation.ErrorCode,
		ProviderToolState:             string(operation.State),
		ProviderToolRunID:             operation.RunID,
		ProviderToolCapabilityKey:     operation.CapabilityKey,
		ProviderToolProviderProfileID: operation.ProviderProfileID,
		ProviderToolName:              operation.ToolName,
		ProviderToolIntentHash:        operation.IntentHash,
		ProviderToolConfigEpoch:       operation.ConfigEpoch,
		ProviderToolIdempotencyKey:    operation.IdempotencyKey,
		ProviderToolUpdatedAt:         updatedAt.Format(time.RFC3339Nano),
	}
}

// ReplayProviderToolOperations conservatively rebuilds one snapshot per
// operation. Terminal evidence always outranks a later nonterminal line;
// malformed entries are ignored and dispatch evidence remains sticky.
func ReplayProviderToolOperations(entries []Entry) map[string]ProviderToolOperationSnapshot {
	snapshots := make(map[string]ProviderToolOperationSnapshot)
	for _, entry := range entries {
		if entry.Type != EntryProviderToolOperation || entry.OperationID == "" {
			continue
		}
		operation := providerToolOperationFromEntry(entry)
		if operation.validate() != nil {
			continue
		}
		snapshot := snapshots[operation.OperationID]
		if operation.State == ProviderToolDispatchAttempted {
			if !snapshot.Dispatched {
				snapshot.Dispatched = true
				// The durable consume point is authoritative for immutable
				// identity. If a corrupt terminal line appeared first with a
				// different identity, retain the dispatch metadata so filtered
				// replay cannot undercount provider usage.
				if snapshot.Latest.OperationID != "" &&
					!sameProviderToolIntent(operation, snapshot.Latest) {
					snapshot.Latest = operation
				}
			} else if snapshot.Latest.OperationID != "" &&
				!sameProviderToolIntent(operation, snapshot.Latest) {
				continue
			}
		} else if snapshot.Latest.OperationID != "" &&
			!sameProviderToolIntent(operation, snapshot.Latest) {
			continue
		}
		if snapshot.Latest.OperationID == "" || providerToolOperationWins(operation, snapshot.Latest) {
			snapshot.Latest = operation
		}
		snapshots[operation.OperationID] = snapshot
	}
	return snapshots
}

func sameProviderToolIntent(left, right ProviderToolOperation) bool {
	return left.OperationID == right.OperationID && left.ToolCallID == right.ToolCallID &&
		left.RunID == right.RunID &&
		left.CapabilityKey == right.CapabilityKey &&
		left.ProviderProfileID == right.ProviderProfileID &&
		left.ToolName == right.ToolName && left.IntentHash == right.IntentHash &&
		left.ConfigEpoch == right.ConfigEpoch && left.IdempotencyKey == right.IdempotencyKey
}

func providerToolOperationFromEntry(entry Entry) ProviderToolOperation {
	updatedAt, _ := time.Parse(time.RFC3339Nano, entry.ProviderToolUpdatedAt)
	if updatedAt.IsZero() {
		updatedAt, _ = time.Parse(time.RFC3339Nano, entry.Timestamp)
	}
	return ProviderToolOperation{
		OperationID: entry.OperationID, ToolCallID: entry.ToolCallID,
		RunID:             entry.ProviderToolRunID,
		State:             ProviderToolOperationState(entry.ProviderToolState),
		CapabilityKey:     entry.ProviderToolCapabilityKey,
		ProviderProfileID: entry.ProviderToolProviderProfileID,
		ToolName:          entry.ProviderToolName, IntentHash: entry.ProviderToolIntentHash,
		ConfigEpoch:    entry.ProviderToolConfigEpoch,
		IdempotencyKey: entry.ProviderToolIdempotencyKey,
		ErrorCode:      entry.ErrorCode, UpdatedAt: updatedAt,
	}
}

func providerToolOperationWins(candidate, current ProviderToolOperation) bool {
	candidateTerminal := candidate.State.IsTerminal()
	currentTerminal := current.State.IsTerminal()
	if candidateTerminal != currentTerminal {
		return candidateTerminal
	}
	if !candidate.UpdatedAt.Equal(current.UpdatedAt) {
		return candidate.UpdatedAt.After(current.UpdatedAt)
	}
	return true
}

// LoadProviderToolOperations returns replay snapshots sorted by operation ID.
func LoadProviderToolOperations(id string) ([]ProviderToolOperationSnapshot, error) {
	entries, err := loadSecuritySession(id)
	if err != nil {
		return nil, err
	}
	byID := ReplayProviderToolOperations(entries)
	result := make([]ProviderToolOperationSnapshot, 0, len(byID))
	for _, snapshot := range byID {
		result = append(result, snapshot)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Latest.OperationID < result[j].Latest.OperationID
	})
	return result, nil
}

// CountDispatchedProviderToolOperations counts durable consume points for one
// capability/provider pair. Empty filters match all values.
func CountDispatchedProviderToolOperations(
	snapshots []ProviderToolOperationSnapshot,
	capabilityKey, providerProfileID string,
) int {
	count := 0
	for _, snapshot := range snapshots {
		if !snapshot.Dispatched {
			continue
		}
		if capabilityKey != "" && snapshot.Latest.CapabilityKey != capabilityKey {
			continue
		}
		if providerProfileID != "" && snapshot.Latest.ProviderProfileID != providerProfileID {
			continue
		}
		count++
	}
	return count
}
