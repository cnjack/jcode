package session

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/config"
	"github.com/cnjack/jcode/internal/toolpolicy"
)

func TestGenerationSecurityLoaderRejectsCorruptDispatchEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("generate an image")
	appendRawSessionLine(t, recorder.UUID(), `{"type":"generation_operation","operation_id":"hidden-consume","operation_state":"dispatch_attempted"}`)

	if _, err := LoadGenerationOperations(recorder.UUID()); err == nil {
		t.Fatal("corrupt dispatch entry was silently omitted from security replay")
	}
	operation := validGenerationDispatch("new-operation")
	err = recorder.RecordGenerationDispatch(operation, DispatchPolicy{
		Tool: SessionToolImageGeneration, MaxPerSession: 1,
	})
	if err == nil {
		t.Fatal("dispatch proceeded after corrupt durable consume evidence")
	}
	entries, loadErr := LoadSession(recorder.UUID())
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	for _, entry := range entries {
		if entry.OperationID == operation.OperationID {
			t.Fatalf("new dispatch was appended after corrupt replay: %#v", entry)
		}
	}
}

func TestProviderToolSecurityLoaderRejectsCorruptDispatchEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	if _, err := recorder.CompareAndSwapSessionToolOverride(SessionToolWebSearch, true, 0); err != nil {
		t.Fatal(err)
	}
	appendRawSessionLine(t, recorder.UUID(), `{"type":"provider_tool_operation","operation_id":"hidden-search-consume","provider_tool_state":"dispatch_attempted"}`)

	if _, err := LoadProviderToolOperations(recorder.UUID()); err == nil {
		t.Fatal("corrupt provider dispatch entry was silently omitted from security replay")
	}
}

func TestSecurityLoaderRejectsMalformedConversationalLine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("start session")
	appendRawSessionLine(t, recorder.UUID(), `{"type":"assistant"`)
	if _, err := LoadGenerationOperations(recorder.UUID()); err == nil {
		t.Fatal("security loader skipped malformed line whose type could not be trusted")
	}
}

func TestSessionOverrideLoaderRejectsCorruptLatestDisable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	if _, err := recorder.CompareAndSwapSessionToolOverride(SessionToolWebSearch, true, 0); err != nil {
		t.Fatal(err)
	}
	// A tolerant replay would ignore revision zero and incorrectly expose the
	// older enabled value. Security hydration must instead fail closed.
	appendRawSessionLine(t, recorder.UUID(), `{"type":"session_tool_override","session_tool_override_tool":"web_search","session_tool_override_persisted":false,"session_tool_override_revision":0}`)

	if _, err := recorder.SessionToolOverrides(); err == nil {
		t.Fatal("corrupt latest disable was ignored in favor of an older enable")
	}
}

func TestTruncateAbortsWithoutRewriteOnMalformedLine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	recorder.RecordUser("keep")
	appendRawSessionLine(t, recorder.UUID(), `{"type":"generation_operation"`)
	path := sessionTestPath(t, recorder.UUID())
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := recorder.TruncateAtUserMessage(0); err == nil {
		t.Fatal("truncate silently rewrote a transcript containing malformed JSON")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("truncate changed transcript bytes after parse failure")
	}
}

func TestGenerationDispatchIgnoresLegacyImageOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	if _, err := recorder.CompareAndSwapSessionToolOverride(SessionToolImageGeneration, true, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.CompareAndSwapSessionToolOverride(SessionToolImageGeneration, false, 1); err != nil {
		t.Fatal(err)
	}

	err = recorder.RecordGenerationDispatch(validGenerationDispatch("independent-image-operation"), DispatchPolicy{
		Tool: SessionToolImageGeneration, MaxPerSession: 20,
	})
	if err != nil {
		t.Fatalf("legacy override disabled independent image dispatch: %v", err)
	}
}

func TestGenerationDispatchSessionCapIsAtomicAcrossRecordersAndLedgers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	first, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	first.RecordUser("generate an image")
	second, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.SetUUID(first.UUID())

	recorders := []*Recorder{first, second}
	ledgers := []*toolpolicy.UsageLedger{
		toolpolicy.NewUsageLedger(1, 1, 0),
		toolpolicy.NewUsageLedger(1, 1, 0),
	}
	policy := DispatchPolicy{
		Tool: SessionToolImageGeneration, MaxPerSession: 1,
	}
	start := make(chan struct{})
	var accepted atomic.Int32
	var wait sync.WaitGroup
	for index := range recorders {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			reservation, reserveErr := ledgers[index].ReserveRun("same-turn", "op-ledger-"+string(rune('a'+index)))
			if reserveErr != nil {
				t.Errorf("ledger %d reserve: %v", index, reserveErr)
				return
			}
			<-start
			dispatchErr := recorders[index].RecordGenerationDispatch(
				validGenerationDispatch("cross-recorder-"+string(rune('a'+index))), policy,
			)
			if dispatchErr == nil {
				reservation.Commit()
				accepted.Add(1)
				return
			}
			reservation.Release()
			if !errors.Is(dispatchErr, ErrDispatchSessionLimit) {
				t.Errorf("recorder %d dispatch: %v", index, dispatchErr)
			}
		}(index)
	}
	close(start)
	wait.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted dispatches = %d, want exactly 1", accepted.Load())
	}
	snapshots, err := LoadGenerationOperations(first.UUID())
	if err != nil {
		t.Fatal(err)
	}
	dispatched := 0
	for _, snapshot := range snapshots {
		if snapshot.Dispatched {
			dispatched++
		}
	}
	if dispatched != 1 {
		t.Fatalf("durable dispatched operations = %d, want 1", dispatched)
	}
}

func TestProviderToolDispatchSessionCapIsAtomicAcrossRecordersAndLedgers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	first, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	_, err = first.CompareAndSwapSessionToolOverride(SessionToolWebSearch, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.SetUUID(first.UUID())

	recorders := []*Recorder{first, second}
	ledgers := []*toolpolicy.UsageLedger{
		toolpolicy.NewUsageLedger(1, 1, 0),
		toolpolicy.NewUsageLedger(1, 1, 0),
	}
	policy := DispatchPolicy{
		Tool: SessionToolWebSearch, MaxPerSession: 1,
	}
	start := make(chan struct{})
	var accepted atomic.Int32
	var wait sync.WaitGroup
	for index := range recorders {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			operationID := "cross-provider-recorder-" + string(rune('a'+index))
			reservation, reserveErr := ledgers[index].ReserveRun("same-turn", operationID)
			if reserveErr != nil {
				t.Errorf("ledger %d reserve: %v", index, reserveErr)
				return
			}
			<-start
			dispatchErr := recorders[index].RecordProviderToolDispatch(
				validProviderToolDispatch(operationID), policy,
			)
			if dispatchErr == nil {
				reservation.Commit()
				accepted.Add(1)
				return
			}
			reservation.Release()
			if !errors.Is(dispatchErr, ErrDispatchSessionLimit) {
				t.Errorf("recorder %d dispatch: %v", index, dispatchErr)
			}
		}(index)
	}
	close(start)
	wait.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted provider dispatches = %d, want exactly 1", accepted.Load())
	}
	snapshots, err := LoadProviderToolOperations(first.UUID())
	if err != nil {
		t.Fatal(err)
	}
	if dispatched := CountDispatchedProviderToolOperations(
		snapshots, "web.search", "provider",
	); dispatched != 1 {
		t.Fatalf("durable provider dispatches = %d, want 1", dispatched)
	}
}

func validGenerationDispatch(operationID string) GenerationOperation {
	return GenerationOperation{
		OperationID: operationID, ToolCallID: "call-" + operationID,
		State: GenerationDispatchAttempted,
		CapabilityKey: OperationCapabilityKey{
			ProviderProfileID: "provider", EndpointProfile: "image:endpoint", ModelID: "image-model",
		},
		CredentialFingerprint: "fingerprint", ConfigEpoch: 1,
		IdempotencyKey: "idempotency-" + operationID, UpdatedAt: time.Now().UTC(),
	}
}

func validProviderToolDispatch(operationID string) ProviderToolOperation {
	return ProviderToolOperation{
		OperationID: operationID, ToolCallID: "call-" + operationID,
		RunID: "same-turn", State: ProviderToolDispatchAttempted,
		CapabilityKey: "web.search", ProviderProfileID: "provider", ToolName: "web_search_prime",
		IntentHash: "intent-" + operationID, ConfigEpoch: "epoch",
		IdempotencyKey: "idempotency-" + operationID, UpdatedAt: time.Now().UTC(),
	}
}

func appendRawSessionLine(t *testing.T, sessionID, line string) {
	t.Helper()
	file, err := os.OpenFile(sessionTestPath(t, sessionID), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(line + "\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func sessionTestPath(t *testing.T, sessionID string) string {
	t.Helper()
	dir, err := config.SessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, sessionID+".json")
}
