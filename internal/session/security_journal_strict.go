package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cnjack/jcode/internal/config"
)

// loadSecuritySession is the fail-closed reader for authorization, billing,
// and quota decisions. Conversational replay deliberately tolerates a damaged
// line, but a security reader cannot prove that a skipped line was not a
// consume point or a newer deny preference.
func loadSecuritySession(id string) ([]Entry, error) {
	if err := ValidateSessionID(id); err != nil {
		return nil, err
	}
	dir, err := config.SessionsDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return nil, fmt.Errorf("session %s not found: %w", id, err)
	}

	entries := make([]Entry, 0)
	for index, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("session %s security journal line %d is malformed: %w", id, index+1, err)
		}
		entries = append(entries, entry)
	}
	if err := validateSecurityJournal(entries); err != nil {
		return nil, fmt.Errorf("session %s security journal is invalid: %w", id, err)
	}
	return entries, nil
}

func validateSecurityJournal(entries []Entry) error {
	generationLatest := make(map[string]GenerationOperation)
	providerLatest := make(map[string]ProviderToolOperation)
	overrideRevisions := make(map[SessionTool]map[uint64]bool)

	for index, entry := range entries {
		switch entry.Type {
		case EntryGenerationOperation:
			operation := generationOperationFromEntry(entry)
			if err := operation.validate(); err != nil {
				return fmt.Errorf("line %d generation operation: %w", index+1, err)
			}
			if previous, ok := generationLatest[operation.OperationID]; ok {
				if err := ValidateGenerationTransition(previous, operation); err != nil {
					return fmt.Errorf("line %d generation operation: %w", index+1, err)
				}
			} else if operation.State != GenerationDispatchAttempted {
				return fmt.Errorf(
					"line %d generation operation starts at %q instead of %q",
					index+1, operation.State, GenerationDispatchAttempted,
				)
			}
			generationLatest[operation.OperationID] = operation

		case EntryProviderToolOperation:
			operation := providerToolOperationFromEntry(entry)
			if err := operation.validate(); err != nil {
				return fmt.Errorf("line %d provider tool operation: %w", index+1, err)
			}
			if previous, ok := providerLatest[operation.OperationID]; ok {
				if err := ValidateProviderToolTransition(previous, operation); err != nil {
					return fmt.Errorf("line %d provider tool operation: %w", index+1, err)
				}
			} else if operation.State != ProviderToolDispatchAttempted {
				return fmt.Errorf(
					"line %d provider tool operation starts at %q instead of %q",
					index+1, operation.State, ProviderToolDispatchAttempted,
				)
			}
			providerLatest[operation.OperationID] = operation

		case EntrySessionToolOverride:
			tool, err := ParseSessionTool(entry.SessionToolOverrideTool)
			if err != nil {
				return fmt.Errorf("line %d session tool override: %w", index+1, err)
			}
			if entry.SessionToolOverrideRevision == 0 {
				return fmt.Errorf("line %d session tool override revision is required", index+1)
			}
			byRevision := overrideRevisions[tool]
			if byRevision == nil {
				byRevision = make(map[uint64]bool)
				overrideRevisions[tool] = byRevision
			}
			if persisted, exists := byRevision[entry.SessionToolOverrideRevision]; exists &&
				persisted != entry.SessionToolOverridePersisted {
				return fmt.Errorf(
					"line %d session tool override revision %d has conflicting values",
					index+1, entry.SessionToolOverrideRevision,
				)
			}
			byRevision[entry.SessionToolOverrideRevision] = entry.SessionToolOverridePersisted
		}
	}
	return nil
}
