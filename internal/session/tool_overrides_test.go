package session

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestReplaySessionToolOverridesUsesGreatestValidRevision(t *testing.T) {
	entries := []Entry{
		{Type: EntrySessionToolOverride, SessionToolOverrideTool: "image_generation", SessionToolOverridePersisted: true, SessionToolOverrideRevision: 2},
		{Type: EntrySessionToolOverride, SessionToolOverrideTool: "image_generation", SessionToolOverridePersisted: false, SessionToolOverrideRevision: 1},
		{Type: EntrySessionToolOverride, SessionToolOverrideTool: "web_search", SessionToolOverridePersisted: true, SessionToolOverrideRevision: 4},
		{Type: EntrySessionToolOverride, SessionToolOverrideTool: "execute", SessionToolOverridePersisted: true, SessionToolOverrideRevision: 99},
		{Type: EntrySessionToolOverride, SessionToolOverrideTool: "web_search", SessionToolOverrideRevision: 0},
	}
	overrides := ReplaySessionToolOverrides(entries)
	if got := overrides[SessionToolImageGeneration]; !got.Persisted || got.Revision != 2 {
		t.Fatalf("image override = %+v", got)
	}
	if got := overrides[SessionToolWebSearch]; !got.Persisted || got.Revision != 4 {
		t.Fatalf("search override = %+v", got)
	}
	if len(overrides) != 2 {
		t.Fatalf("unexpected replayed tools: %+v", overrides)
	}
}

func TestRecorderSessionToolOverrideCASConcurrentAndReplay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recorder, err := NewRecorder(t.TempDir(), "provider", "model")
	if err != nil {
		t.Fatal(err)
	}
	const contenders = 16
	var successes atomic.Int32
	var conflicts atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, casErr := recorder.CompareAndSwapSessionToolOverride(
				SessionToolImageGeneration, true, 0,
			)
			switch {
			case casErr == nil:
				successes.Add(1)
			case errors.Is(casErr, ErrSessionToolOverrideRevision):
				conflicts.Add(1)
			default:
				t.Errorf("unexpected CAS error: %v", casErr)
			}
		}()
	}
	close(start)
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != contenders-1 {
		t.Fatalf("success=%d conflicts=%d", successes.Load(), conflicts.Load())
	}

	next, err := recorder.CompareAndSwapSessionToolOverride(
		SessionToolImageGeneration, false, 1,
	)
	if err != nil || next.Persisted || next.Revision != 2 {
		t.Fatalf("second CAS = %+v, %v", next, err)
	}

	replayed, err := LoadSessionToolOverrides(recorder.UUID())
	if err != nil {
		t.Fatal(err)
	}
	if got := replayed[SessionToolImageGeneration]; got.Persisted || got.Revision != 2 {
		t.Fatalf("replayed = %+v", got)
	}

	resumed, _ := NewRecorder(t.TempDir(), "provider", "model")
	resumed.SetUUID(recorder.UUID())
	snapshot, err := resumed.SessionToolOverrides()
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot[SessionToolImageGeneration]; got.Persisted || got.Revision != 2 {
		t.Fatalf("resumed snapshot = %+v", got)
	}
	if _, err := resumed.CompareAndSwapSessionToolOverride(SessionToolImageGeneration, true, 1); !errors.Is(err, ErrSessionToolOverrideRevision) {
		t.Fatalf("stale CAS error = %v", err)
	}
}

func TestRecorderSessionToolOverrideDoesNotPublishAfterDiskFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recorder, _ := NewRecorder(t.TempDir(), "provider", "model")
	if snapshot, err := recorder.SessionToolOverrides(); err != nil || len(snapshot) != 0 {
		t.Fatalf("initial snapshot=%+v err=%v", snapshot, err)
	}
	if err := os.WriteFile(filepath.Join(home, ".jcode"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.CompareAndSwapSessionToolOverride(SessionToolWebSearch, true, 0); err == nil {
		t.Fatal("expected durable append failure")
	}
	recorder.mu.Lock()
	_, published := recorder.toolOverrides[SessionToolWebSearch]
	recorder.mu.Unlock()
	if published {
		t.Fatal("failed append published in-memory state")
	}
}

func TestRecorderSessionToolOverrideCASSerializesDistinctRecorders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const taskID = "shared-task"
	first, _ := NewRecorder(t.TempDir(), "provider", "model")
	second, _ := NewRecorder(t.TempDir(), "provider", "model")
	first.SetUUID(taskID)
	second.SetUUID(taskID)

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, recorder := range []*Recorder{first, second} {
		go func(recorder *Recorder) {
			<-start
			_, err := recorder.CompareAndSwapSessionToolOverride(SessionToolWebSearch, true, 0)
			results <- err
		}(recorder)
	}
	close(start)
	var success, conflict int
	for i := 0; i < 2; i++ {
		err := <-results
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrSessionToolOverrideRevision):
			conflict++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	// The recorder that lost the first CAS can still append the next revision;
	// load detected the file created after its SetUUID call and switched it to
	// append/resume mode.
	if _, err := second.CompareAndSwapSessionToolOverride(SessionToolWebSearch, false, 1); err != nil {
		// If second happened to win, first is the recorder that must append.
		if _, firstErr := first.CompareAndSwapSessionToolOverride(SessionToolWebSearch, false, 1); firstErr != nil {
			t.Fatalf("neither recorder could append revision 2: second=%v first=%v", err, firstErr)
		}
	}
	replayed, err := LoadSessionToolOverrides(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got := replayed[SessionToolWebSearch]; got.Persisted || got.Revision != 2 {
		t.Fatalf("replayed=%+v", got)
	}
}

func TestParseSessionToolAllowlist(t *testing.T) {
	if got := SupportedSessionTools(); len(got) != 0 {
		t.Fatalf("configurable session tools = %#v", got)
	}
	for _, tool := range []string{"image_generation", "web_search"} {
		parsed, err := ParseSessionTool(tool)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if IsConfigurableSessionTool(parsed) {
			t.Fatalf("historical tool %s remains configurable", parsed)
		}
	}
	for _, tool := range []string{"", "execute", " image_generation ", "image_generation/../execute"} {
		if _, err := ParseSessionTool(tool); err == nil {
			t.Fatalf("expected %q to be rejected", tool)
		}
	}
}
