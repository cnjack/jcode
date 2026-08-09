package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecorderPersistsAndReplaysGenerationOperationJournal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorder, err := NewRecorder(t.TempDir(), "provider-a", "chat-a")
	if err != nil {
		t.Fatal(err)
	}
	base := generationOperationFixture()
	states := []GenerationOperationState{
		GenerationDispatchAttempted, GenerationAccepted, GenerationSaving, GenerationSucceeded,
	}
	for index, state := range states {
		operation := base
		operation.State = state
		operation.UpdatedAt = time.Date(2026, 8, 8, 10, index, 0, 0, time.UTC)
		if state == GenerationSucceeded {
			operation.ArtifactIDs = []string{"artifact-a"}
		}
		if err := recorder.RecordGenerationOperation(operation); err != nil {
			t.Fatalf("record %s: %v", state, err)
		}
	}

	snapshots, err := LoadGenerationOperations(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || !snapshots[0].Dispatched ||
		snapshots[0].Latest.State != GenerationSucceeded ||
		len(snapshots[0].Latest.ArtifactIDs) != 1 || snapshots[0].Latest.ArtifactIDs[0] != "artifact-a" {
		t.Fatalf("snapshots=%+v", snapshots)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".jcode", "sessions", recorder.UUID()+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"type":"generation_operation"`, `"operation_state":"dispatch_attempted"`, `"operation_state":"succeeded"`} {
		if !strings.Contains(string(raw), required) {
			t.Fatalf("journal missing %s: %s", required, raw)
		}
	}
	for _, forbidden := range []string{"raw-provider-key", "private prompt", "signed.example"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("journal leaked %q: %s", forbidden, raw)
		}
	}
}

func TestReplayGenerationOperationsIsConservativeForOutOfOrderEvidence(t *testing.T) {
	base := generationOperationFixture()
	entry := func(state GenerationOperationState, timestamp string) Entry {
		operation := base
		operation.State = state
		return Entry{
			Type: EntryGenerationOperation, OperationID: operation.OperationID,
			ToolCallID: operation.ToolCallID, OperationState: string(state),
			OperationCapabilityKey:         &operation.CapabilityKey,
			OperationCredentialFingerprint: operation.CredentialFingerprint,
			OperationConfigEpoch:           operation.ConfigEpoch,
			OperationIdempotencyKey:        operation.IdempotencyKey,
			OperationUpdatedAt:             timestamp, Timestamp: timestamp,
		}
	}
	entries := []Entry{
		entry(GenerationSaving, "2026-08-08T10:03:00Z"),
		entry(GenerationFailed, "2026-08-08T10:02:00Z"),
		entry(GenerationDispatchAttempted, "2026-08-08T10:04:00Z"),
		{Type: EntryGenerationOperation, OperationID: "invalid"},
	}
	snapshots := ReplayGenerationOperations(entries)
	snapshot := snapshots[base.OperationID]
	if !snapshot.Dispatched || snapshot.Latest.State != GenerationFailed {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if _, exists := snapshots["invalid"]; exists {
		t.Fatalf("invalid operation was replayed: %+v", snapshots["invalid"])
	}
	if priority := GenerationRecoveryEvidencePriority(snapshot, true, true); priority != GenerationRecoveryTerminalOperation {
		t.Fatalf("terminal priority=%d", priority)
	}

	nonterminal := GenerationOperationSnapshot{Latest: base, Dispatched: true}
	nonterminal.Latest.State = GenerationSaving
	if priority := GenerationRecoveryEvidencePriority(nonterminal, true, true); priority != GenerationRecoveryArtifact {
		t.Fatalf("artifact priority=%d", priority)
	}
	if priority := GenerationRecoveryEvidencePriority(nonterminal, false, true); priority != GenerationRecoveryTerminalToolResult {
		t.Fatalf("tool result priority=%d", priority)
	}
	if priority := GenerationRecoveryEvidencePriority(nonterminal, false, false); priority != GenerationRecoveryNonTerminalOperation {
		t.Fatalf("nonterminal priority=%d", priority)
	}
}

func TestValidateGenerationTransitionRejectsIntentMutationRegressionAndTerminalExit(t *testing.T) {
	base := generationOperationFixture()
	base.State = GenerationDispatchAttempted
	if err := ValidateGenerationStart(base); err != nil {
		t.Fatalf("valid start rejected: %v", err)
	}
	accepted := base
	accepted.State = GenerationAccepted
	if err := ValidateGenerationStart(accepted); err == nil {
		t.Fatal("non-dispatch start was accepted")
	}
	if err := ValidateGenerationTransition(base, accepted); err != nil {
		t.Fatalf("valid transition rejected: %v", err)
	}

	mutated := accepted
	mutated.CapabilityKey.ModelID = "different-model"
	if err := ValidateGenerationTransition(base, mutated); err == nil {
		t.Fatal("immutable intent mutation was accepted")
	}
	regressed := accepted
	regressed.State = GenerationDispatchAttempted
	if err := ValidateGenerationTransition(accepted, regressed); err == nil {
		t.Fatal("state regression was accepted")
	}
	terminal := accepted
	terminal.State = GenerationUncertain
	afterTerminal := terminal
	afterTerminal.State = GenerationSucceeded
	if err := ValidateGenerationTransition(terminal, afterTerminal); err == nil {
		t.Fatal("terminal state exit was accepted")
	}
	if err := ValidateGenerationTransition(terminal, terminal); err != nil {
		t.Fatalf("idempotent terminal transition rejected: %v", err)
	}
}

func TestRecordGenerationOperationRejectsIncompleteIntentBeforeCreatingSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorder, err := NewRecorder(t.TempDir(), "provider-a", "chat-a")
	if err != nil {
		t.Fatal(err)
	}
	operation := generationOperationFixture()
	operation.State = GenerationDispatchAttempted
	operation.CredentialFingerprint = ""
	if err := recorder.RecordGenerationOperation(operation); err == nil {
		t.Fatal("incomplete intent was recorded")
	}
	if recorder.HasRecording() {
		t.Fatal("invalid operation created a session transcript")
	}
}

func TestRecordToolResultWithDetailsPersistsTypedTerminalEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("generate an image")
	_, err = recorder.RecordToolResultWithDetails(
		"generate_image", `{"message":"saved"}`, "call-1", nil, false, time.Second,
		ToolResultDetails{
			OperationID: "operation-1", Outcome: "succeeded",
			Provider: "provider-old", Model: "image-old",
			ArtifactIDs: []string{"artifact-1"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := LoadSession(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	last := entries[len(entries)-1]
	if last.Type != EntryToolResult || last.OperationID != "operation-1" ||
		last.Outcome != "succeeded" || last.Provider != "provider-old" || last.Model != "image-old" ||
		len(last.ArtifactIDs) != 1 ||
		last.ArtifactIDs[0] != "artifact-1" {
		t.Fatalf("typed tool result = %#v", last)
	}
}

func generationOperationFixture() GenerationOperation {
	return GenerationOperation{
		OperationID: "operation-a", ToolCallID: "tool-call-a",
		State: GenerationDispatchAttempted,
		CapabilityKey: OperationCapabilityKey{
			ProviderProfileID: "provider-a", CredentialKind: "api_key",
			EndpointProfile: "images-v1", ModelID: "image-a",
		},
		CredentialFingerprint: "sha256:fingerprint", ConfigEpoch: 7,
		IdempotencyKey: "idempotency-a",
	}
}
