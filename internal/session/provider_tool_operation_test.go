package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProviderToolOperationJournalReplayAndPrivacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorder, err := NewRecorder(t.TempDir(), "chat-provider", "chat-model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	operation := providerToolOperationFixture()
	operation.State = ProviderToolDispatchAttempted
	if err := recorder.RecordProviderToolOperation(operation); err != nil {
		t.Fatal(err)
	}
	terminal := operation
	terminal.State = ProviderToolSucceeded
	terminal.UpdatedAt = operation.UpdatedAt.Add(time.Second)
	if err := recorder.RecordProviderToolOperation(terminal); err != nil {
		t.Fatal(err)
	}

	snapshots, err := LoadProviderToolOperations(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || !snapshots[0].Dispatched ||
		snapshots[0].Latest.State != ProviderToolSucceeded {
		t.Fatalf("snapshots = %#v", snapshots)
	}
	if got := CountDispatchedProviderToolOperations(
		snapshots, "web.search", "zhipuai-coding-plan",
	); got != 1 {
		t.Fatalf("dispatched count = %d", got)
	}
	if got := CountDispatchedProviderToolOperations(snapshots, "image.generate", ""); got != 0 {
		t.Fatalf("filtered dispatched count = %d", got)
	}

	raw, err := os.ReadFile(filepath.Join(home, ".jcode", "sessions", recorder.UUID()+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.Contains(line, `"type":"provider_tool_operation"`) {
			continue
		}
		for _, forbidden := range []string{
			`"args"`, `credential`, `"url"`, `"body"`, "private search query",
			"raw-provider-key", "provider response body",
		} {
			if strings.Contains(strings.ToLower(line), strings.ToLower(forbidden)) {
				t.Fatalf("provider operation line leaked %q: %s", forbidden, line)
			}
		}
	}
}

func TestReplayProviderToolOperationsIsConservative(t *testing.T) {
	base := providerToolOperationFixture()
	entry := func(state ProviderToolOperationState, timestamp string) Entry {
		return Entry{
			Type: EntryProviderToolOperation, OperationID: base.OperationID,
			ToolCallID: base.ToolCallID, ErrorCode: base.ErrorCode,
			ProviderToolRunID: base.RunID,
			ProviderToolState: string(state), ProviderToolCapabilityKey: base.CapabilityKey,
			ProviderToolProviderProfileID: base.ProviderProfileID,
			ProviderToolName:              base.ToolName, ProviderToolIntentHash: base.IntentHash,
			ProviderToolConfigEpoch:    base.ConfigEpoch,
			ProviderToolIdempotencyKey: base.IdempotencyKey,
			ProviderToolUpdatedAt:      timestamp, Timestamp: timestamp,
		}
	}
	snapshots := ReplayProviderToolOperations([]Entry{
		entry(ProviderToolFailed, "2026-08-08T10:01:00Z"),
		entry(ProviderToolDispatchAttempted, "2026-08-08T10:02:00Z"),
		{Type: EntryProviderToolOperation, OperationID: "invalid"},
	})
	snapshot := snapshots[base.OperationID]
	if !snapshot.Dispatched || snapshot.Latest.State != ProviderToolFailed {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if _, exists := snapshots["invalid"]; exists {
		t.Fatalf("invalid operation was replayed: %#v", snapshots["invalid"])
	}
}

func TestReplayProviderToolOperationsKeepsDispatchIdentityForLimits(t *testing.T) {
	base := providerToolOperationFixture()
	dispatch := Entry{
		Type: EntryProviderToolOperation, OperationID: base.OperationID,
		ToolCallID:                    base.ToolCallID,
		ProviderToolRunID:             base.RunID,
		ProviderToolState:             string(ProviderToolDispatchAttempted),
		ProviderToolCapabilityKey:     base.CapabilityKey,
		ProviderToolProviderProfileID: base.ProviderProfileID,
		ProviderToolName:              base.ToolName, ProviderToolIntentHash: base.IntentHash,
		ProviderToolConfigEpoch:    base.ConfigEpoch,
		ProviderToolIdempotencyKey: base.IdempotencyKey,
		ProviderToolUpdatedAt:      "2026-08-08T10:00:00Z",
	}
	mutatedTerminal := dispatch
	mutatedTerminal.ProviderToolState = string(ProviderToolSucceeded)
	mutatedTerminal.ProviderToolProviderProfileID = "different-provider"
	mutatedTerminal.ProviderToolIntentHash = "different-intent"
	mutatedTerminal.ProviderToolUpdatedAt = "2026-08-08T10:01:00Z"

	snapshot := ReplayProviderToolOperations([]Entry{dispatch, mutatedTerminal})[base.OperationID]
	if !snapshot.Dispatched || snapshot.Latest.ProviderProfileID != base.ProviderProfileID ||
		snapshot.Latest.State != ProviderToolDispatchAttempted {
		t.Fatalf("mutated terminal changed dispatch identity: %#v", snapshot)
	}
	if got := CountDispatchedProviderToolOperations(
		[]ProviderToolOperationSnapshot{snapshot}, base.CapabilityKey, base.ProviderProfileID,
	); got != 1 {
		t.Fatalf("dispatched count after mutated terminal = %d", got)
	}
}

func TestValidateProviderToolOperationTransition(t *testing.T) {
	start := providerToolOperationFixture()
	start.State = ProviderToolDispatchAttempted
	if err := ValidateProviderToolStart(start); err != nil {
		t.Fatalf("valid start: %v", err)
	}
	terminal := start
	terminal.State = ProviderToolUncertain
	if err := ValidateProviderToolTransition(start, terminal); err != nil {
		t.Fatalf("valid terminal transition: %v", err)
	}
	mutated := terminal
	mutated.IntentHash = "different"
	if err := ValidateProviderToolTransition(start, mutated); err == nil {
		t.Fatal("intent mutation was accepted")
	}
	afterTerminal := terminal
	afterTerminal.State = ProviderToolSucceeded
	if err := ValidateProviderToolTransition(terminal, afterTerminal); err == nil {
		t.Fatal("terminal exit was accepted")
	}
}

func TestInvalidProviderToolOperationDoesNotCreateSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	operation := providerToolOperationFixture()
	operation.IntentHash = ""
	if err := recorder.RecordProviderToolOperation(operation); err == nil {
		t.Fatal("incomplete operation was recorded")
	}
	if recorder.HasRecording() {
		t.Fatal("invalid provider operation created a session transcript")
	}
}

func providerToolOperationFixture() ProviderToolOperation {
	return ProviderToolOperation{
		OperationID: "operation-search-1", ToolCallID: "call-search-1",
		RunID: "turn-1",
		State: ProviderToolDispatchAttempted, CapabilityKey: "web.search",
		ProviderProfileID: "zhipuai-coding-plan", ToolName: "web_search_prime",
		IntentHash: "intent-hash", ConfigEpoch: "config-epoch",
		IdempotencyKey: "idempotency-key",
		UpdatedAt:      time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC),
	}
}
