package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/artifact"
)

func TestTruncatePreservesBillableJournalsAndSessionToolPolicy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()

	recorder.RecordUser("keep this turn")
	recorder.RecordAssistant("kept response")
	recorder.RecordUser("truncate from this turn")
	recorder.RecordAssistant("removed response")

	generation := GenerationOperation{
		OperationID: "host-generation-1", ToolCallID: "model-call-reused",
		State: GenerationDispatchAttempted,
		CapabilityKey: OperationCapabilityKey{
			ProviderProfileID: "provider", EndpointProfile: "image:endpoint", ModelID: "image-model",
		},
		CredentialFingerprint: "fingerprint", ConfigEpoch: 1,
		IdempotencyKey: "generation-idempotency", UpdatedAt: time.Now().UTC(),
	}
	if err := recorder.RecordGenerationOperation(generation); err != nil {
		t.Fatal(err)
	}
	providerTool := ProviderToolOperation{
		OperationID: "host-search-1", ToolCallID: "model-call-reused", RunID: "turn-2",
		State: ProviderToolDispatchAttempted, CapabilityKey: "web.search",
		ProviderProfileID: "provider", ToolName: "web_search",
		IntentHash: "intent-hash", ConfigEpoch: "epoch", IdempotencyKey: "search-idempotency",
		UpdatedAt: time.Now().UTC(),
	}
	if err := recorder.RecordProviderToolOperation(providerTool); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.CompareAndSwapSessionToolOverride(SessionToolWebSearch, true, 0); err != nil {
		t.Fatal(err)
	}

	if err := recorder.TruncateAtUserMessage(1); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadSession(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, entry := range entries {
		if entry.Type == EntryUser {
			users++
			if entry.Content != "keep this turn" {
				t.Fatalf("unexpected retained user entry: %#v", entry)
			}
		}
	}
	if users != 1 {
		t.Fatalf("retained user messages = %d, want 1", users)
	}
	generations, err := LoadGenerationOperations(recorder.UUID())
	if err != nil || len(generations) != 1 || !generations[0].Dispatched {
		t.Fatalf("generation journal after truncate = %#v, err=%v", generations, err)
	}
	providerTools, err := LoadProviderToolOperations(recorder.UUID())
	if err != nil || len(providerTools) != 1 || !providerTools[0].Dispatched {
		t.Fatalf("provider journal after truncate = %#v, err=%v", providerTools, err)
	}
	overrides, err := recorder.SessionToolOverrides()
	if err != nil || !overrides[SessionToolWebSearch].Persisted || overrides[SessionToolWebSearch].Revision != 1 {
		t.Fatalf("session tool policy after truncate = %#v, err=%v", overrides, err)
	}
}

func TestSecurityAppendReopensFileReplacedByAnotherRecorder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	first, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	first.RecordUser("keep")

	second, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.SetUUID(first.UUID())
	// Open a second append fd for the original inode before first replaces the
	// path during truncation.
	second.RecordAssistant("open old inode")
	if err := first.TruncateAtUserMessage(0); err != nil {
		t.Fatal(err)
	}

	operation := GenerationOperation{
		OperationID: "host-generation-after-rewrite", ToolCallID: "model-call-after-rewrite",
		State: GenerationDispatchAttempted,
		CapabilityKey: OperationCapabilityKey{
			ProviderProfileID: "provider", EndpointProfile: "image:endpoint", ModelID: "image-model",
		},
		CredentialFingerprint: "fingerprint", ConfigEpoch: 1,
		IdempotencyKey: "generation-after-rewrite", UpdatedAt: time.Now().UTC(),
	}
	if err := second.RecordGenerationOperation(operation); err != nil {
		t.Fatal(err)
	}
	snapshots, err := LoadGenerationOperations(first.UUID())
	if err != nil || len(snapshots) != 1 || !snapshots[0].Dispatched ||
		snapshots[0].Latest.OperationID != operation.OperationID {
		t.Fatalf("visible security journal = %#v, err=%v", snapshots, err)
	}
}

func TestArtifactAppendReopensReplacedFileAndSurvivesLaterTruncate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	first, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	first.RecordUser("keep")

	second, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.SetUUID(first.UUID())
	second.RecordAssistant("open old inode")
	if err := first.TruncateAtUserMessage(0); err != nil {
		t.Fatal(err)
	}

	record := artifact.Record{
		ID: "artifact-after-rewrite", SessionID: first.UUID(),
		StorageKind: artifact.StorageManaged, RelativeKey: "images/generated.png",
		Title: "Generated image", Kind: artifact.KindImage, MediaType: "image/png",
		Size: 42, Width: 1, Height: 1, SHA256: "digest", Revision: 1,
		OperationID: "generation-after-rewrite", ToolCallID: "tool-after-rewrite",
	}
	if err := second.RecordArtifact(record); err != nil {
		t.Fatal(err)
	}
	recorded, err := LoadArtifactRecords(first.UUID())
	if err != nil || len(recorded) != 1 || recorded[0].ID != record.ID {
		t.Fatalf("visible artifact journal = %#v, err=%v", recorded, err)
	}

	if err := first.TruncateAtUserMessage(0); err != nil {
		t.Fatal(err)
	}
	recorded, err = LoadArtifactRecords(first.UUID())
	if err != nil || len(recorded) != 1 || recorded[0].ID != record.ID {
		t.Fatalf("artifact journal after later truncate = %#v, err=%v", recorded, err)
	}
}

func TestSessionSecurityLockSerializesAcrossProcesses(t *testing.T) {
	if os.Getenv("JCODE_SESSION_LOCK_HELPER") == "1" {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	const sessionID = "cross-process-session"
	lock, err := acquireSessionSecurityLock(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	acquired := filepath.Join(t.TempDir(), "acquired")
	command := exec.Command(os.Args[0], "-test.run=TestSessionSecurityLockHelper", "--")
	command.Env = append(os.Environ(),
		"HOME="+home,
		"JCODE_SESSION_LOCK_HELPER=1",
		"JCODE_SESSION_LOCK_ID="+sessionID,
		"JCODE_SESSION_LOCK_READY="+ready,
		"JCODE_SESSION_LOCK_ACQUIRED="+acquired,
	)
	if err := command.Start(); err != nil {
		_ = lock.release()
		t.Fatal(err)
	}
	waitForTestFile(t, ready, time.Second)
	time.Sleep(75 * time.Millisecond)
	if _, err := os.Stat(acquired); err == nil {
		_ = lock.release()
		_ = command.Wait()
		t.Fatal("second process acquired the session security lock before release")
	}
	if err := lock.release(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("lock helper: %v", err)
	}
	if _, err := os.Stat(acquired); err != nil {
		t.Fatalf("second process never acquired released lock: %v", err)
	}
}

func TestSessionSecurityLockHelper(t *testing.T) {
	if os.Getenv("JCODE_SESSION_LOCK_HELPER") != "1" {
		return
	}
	if err := os.WriteFile(os.Getenv("JCODE_SESSION_LOCK_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireSessionSecurityLock(os.Getenv("JCODE_SESSION_LOCK_ID"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if releaseErr := lock.release(); releaseErr != nil {
			t.Errorf("release session security lock: %v", releaseErr)
		}
	}()
	if err := os.WriteFile(os.Getenv("JCODE_SESSION_LOCK_ACQUIRED"), []byte("acquired"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForTestFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
