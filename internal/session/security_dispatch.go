package session

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrDispatchSessionLimit means the durable consume count already reached
	// the policy cap. Callers must not invoke the provider.
	ErrDispatchSessionLimit = errors.New("provider call limit reached for this session")
	// ErrDispatchOperationExists prevents reuse of an operation identifier from
	// creating another provider side effect while counting as one operation.
	ErrDispatchOperationExists = errors.New("provider operation already exists")
)

// DispatchPolicy binds a prepared billable tool to its hard session cap.
type DispatchPolicy struct {
	Tool          SessionTool
	MaxPerSession int
}

func (policy DispatchPolicy) validate() error {
	if _, err := ParseSessionTool(string(policy.Tool)); err != nil {
		return err
	}
	if policy.MaxPerSession <= 0 {
		return fmt.Errorf("provider session call limit must be positive")
	}
	return nil
}

// RecordGenerationDispatch atomically performs strict replay, hard session-cap
// enforcement, and the fsynced consume append under the per-session OS lock.
// A nil result is the sole permission to invoke the external image provider.
func (r *Recorder) RecordGenerationDispatch(
	operation GenerationOperation,
	policy DispatchPolicy,
) error {
	if err := ValidateGenerationStart(operation); err != nil {
		return err
	}
	if policy.Tool != SessionToolImageGeneration {
		return fmt.Errorf("generation dispatch requires tool %q", SessionToolImageGeneration)
	}
	if err := policy.validate(); err != nil {
		return err
	}
	updatedAt := operation.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	return r.withSecurityDispatchTransaction(func(entries []Entry) error {
		operations := ReplayGenerationOperations(entries)
		if _, exists := operations[operation.OperationID]; exists {
			return fmt.Errorf("%w: %s", ErrDispatchOperationExists, operation.OperationID)
		}
		dispatched := 0
		for _, snapshot := range operations {
			if snapshot.Dispatched {
				dispatched++
			}
		}
		if dispatched >= policy.MaxPerSession {
			return ErrDispatchSessionLimit
		}
		return r.writeSecurityEntryLocked(generationOperationEntry(operation, updatedAt))
	})
}

// RecordProviderToolDispatch is the provider-managed equivalent of
// RecordGenerationDispatch. Its durable count is scoped to the capability and
// provider profile, matching ledger reconstruction.
func (r *Recorder) RecordProviderToolDispatch(
	operation ProviderToolOperation,
	policy DispatchPolicy,
) error {
	if err := ValidateProviderToolStart(operation); err != nil {
		return err
	}
	if policy.Tool != SessionToolWebSearch {
		return fmt.Errorf("provider tool dispatch requires %q session policy", SessionToolWebSearch)
	}
	if err := policy.validate(); err != nil {
		return err
	}
	updatedAt := operation.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	return r.withSecurityDispatchTransaction(func(entries []Entry) error {
		operations := ReplayProviderToolOperations(entries)
		if _, exists := operations[operation.OperationID]; exists {
			return fmt.Errorf("%w: %s", ErrDispatchOperationExists, operation.OperationID)
		}
		dispatched := CountDispatchedProviderToolOperations(
			providerToolSnapshots(operations), operation.CapabilityKey, operation.ProviderProfileID,
		)
		if dispatched >= policy.MaxPerSession {
			return ErrDispatchSessionLimit
		}
		return r.writeSecurityEntryLocked(providerToolOperationEntry(operation, updatedAt))
	})
}

func (r *Recorder) withSecurityDispatchTransaction(checkAndAppend func([]Entry) error) error {
	sessionID := r.UUID()
	lock, err := acquireSessionSecurityLock(sessionID)
	if err != nil {
		return err
	}
	defer func() { _ = lock.release() }()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.uuid != sessionID {
		return fmt.Errorf("session changed while recording provider dispatch")
	}
	entries, err := loadSecuritySession(sessionID)
	if err != nil {
		return err
	}
	return checkAndAppend(entries)
}

func providerToolSnapshots(
	byID map[string]ProviderToolOperationSnapshot,
) []ProviderToolOperationSnapshot {
	result := make([]ProviderToolOperationSnapshot, 0, len(byID))
	for _, snapshot := range byID {
		result = append(result, snapshot)
	}
	return result
}
