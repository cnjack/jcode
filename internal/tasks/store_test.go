package tasks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir(), "/proj/a")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func mustCreate(t *testing.T, s *Store, in CreateInput) *Record {
	t.Helper()
	rec, err := s.Create(in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return rec
}

func TestNewRefUnique(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		ref := NewRef()
		if !ValidateRef(ref) {
			t.Fatalf("invalid ref %q", ref)
		}
		if seen[ref] {
			t.Fatalf("collision on %q", ref)
		}
		seen[ref] = true
	}
}

func TestCreateGetRoundTrip(t *testing.T) {
	s := newTestStore(t)
	rec := mustCreate(t, s, CreateInput{Name: "explore-auth", Description: "look at auth", Kind: KindSubagent, SessionID: "sess-1"})
	if !ValidateRef(rec.Ref) {
		t.Fatalf("bad ref %q", rec.Ref)
	}
	if rec.Status != StatusCreated {
		t.Fatalf("status = %q, want created", rec.Status)
	}
	got, err := s.Get(rec.Ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Ref != rec.Ref || got.Name != "explore-auth" || got.SessionID != "sess-1" {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	// Private file modes + default owner stamping.
	info, err := os.Stat(s.taskPath(rec.Ref))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("task log mode = %v, want 0600", info.Mode().Perm())
	}
	if rec.OwnerPID != s.PID() || rec.Hostname != s.Hostname() {
		t.Fatalf("owner not stamped: pid=%d host=%q", rec.OwnerPID, rec.Hostname)
	}
}

func TestCreateIdempotentByKey(t *testing.T) {
	s := newTestStore(t)
	first := mustCreate(t, s, CreateInput{Name: "one", Key: "op-123"})
	second := mustCreate(t, s, CreateInput{Name: "one", Key: "op-123"})
	if first.Ref != second.Ref {
		t.Fatalf("idempotent create minted a second record: %s vs %s", first.Ref, second.Ref)
	}
	recs, _ := s.List("")
	if len(recs) != 1 {
		t.Fatalf("List = %d records, want 1", len(recs))
	}
}

func TestConcurrentCreateDistinctRefs(t *testing.T) {
	s := newTestStore(t)
	const n = 32
	var wg sync.WaitGroup
	refs := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec, err := s.Create(CreateInput{Name: fmt.Sprintf("task-%d", i)})
			if err != nil {
				t.Errorf("Create %d: %v", i, err)
				return
			}
			refs <- rec.Ref
		}(i)
	}
	wg.Wait()
	close(refs)
	seen := map[string]bool{}
	for ref := range refs {
		if seen[ref] {
			t.Fatalf("duplicate ref %s under concurrent create", ref)
		}
		seen[ref] = true
	}
	recs, err := s.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != n {
		t.Fatalf("List = %d, want %d", len(recs), n)
	}
}

func TestCrossProjectPermissionDenied(t *testing.T) {
	root := t.TempDir()
	sa, err := NewStore(root, "/proj/a")
	if err != nil {
		t.Fatal(err)
	}
	sb, err := NewStore(root, "/proj/b")
	if err != nil {
		t.Fatal(err)
	}
	rec := mustCreate(t, sa, CreateInput{Name: "secret"})

	if _, err := sb.Get(rec.Ref); !errors.Is(err, ErrCrossProject) {
		t.Fatalf("Get across projects: err = %v, want ErrCrossProject", err)
	}
	if _, err := sb.Message(rec.Ref, "sess-b", "user", "hi", ""); !errors.Is(err, ErrCrossProject) {
		t.Fatalf("Message across projects: err = %v, want ErrCrossProject", err)
	}
	if _, err := sb.Archive(rec.Ref); !errors.Is(err, ErrCrossProject) {
		t.Fatalf("Archive across projects: err = %v, want ErrCrossProject", err)
	}
	// Name resolution in b must not surface a's tasks.
	if _, err := sb.Resolve("secret"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve foreign name: err = %v, want ErrNotFound", err)
	}
	// And b's list stays empty.
	recs, _ := sb.List("")
	if len(recs) != 0 {
		t.Fatalf("List leaked %d records across projects", len(recs))
	}
}

func TestMessageExactlyOnce(t *testing.T) {
	s := newTestStore(t)
	rec := mustCreate(t, s, CreateInput{Name: "m"})
	// Retried delivery with the same key lands exactly once.
	for i := 0; i < 3; i++ {
		if _, err := s.Message(rec.Ref, "sess-1", "user", "please continue", "msg-key-1"); err != nil {
			t.Fatalf("Message %d: %v", i, err)
		}
	}
	got, err := s.Get(rec.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Timeline) != 1 {
		t.Fatalf("timeline = %d entries, want 1 (exactly-once)", len(got.Timeline))
	}
	// Different keys are distinct messages.
	if _, err := s.Message(rec.Ref, "sess-1", "user", "second", "msg-key-2"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(rec.Ref)
	if len(got.Timeline) != 2 {
		t.Fatalf("timeline = %d entries, want 2", len(got.Timeline))
	}
}

func TestMessageConcurrentDuplicateSingleAppend(t *testing.T) {
	s := newTestStore(t)
	rec := mustCreate(t, s, CreateInput{Name: "m"})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Message(rec.Ref, "sess", "user", "dup", "same-key")
		}()
	}
	wg.Wait()
	got, err := s.Get(rec.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Timeline) != 1 {
		t.Fatalf("timeline = %d entries, want 1 under concurrent duplicate send", len(got.Timeline))
	}
}

func TestMessageTerminalAndArchivedErrors(t *testing.T) {
	s := newTestStore(t)
	rec := mustCreate(t, s, CreateInput{Name: "done"})
	if err := s.SetStatus(rec.Ref, StatusCompleted, "all good", ""); err != nil {
		t.Fatal(err)
	}
	_, err := s.Message(rec.Ref, "s", "user", "hi", "")
	if !errors.Is(err, ErrTerminal) {
		t.Fatalf("message completed task: err = %v, want ErrTerminal", err)
	}
	// Archived task: reads still surface the archived state (audit), while
	// messages give an explicit archived error.
	rec2 := mustCreate(t, s, CreateInput{Name: "arch"})
	if _, err := s.Archive(rec2.Ref); err != nil {
		t.Fatalf("Archive created task: %v", err)
	}
	got, err := s.Get(rec2.Ref)
	if err != nil {
		t.Fatalf("Get archived should remain readable for audit: %v", err)
	}
	if got.Status != StatusArchived {
		t.Fatalf("archived status = %s", got.Status)
	}
	if _, err := s.Message(rec2.Ref, "s", "user", "hi", ""); !errors.Is(err, ErrArchived) {
		t.Fatalf("Message archived: err = %v, want ErrArchived", err)
	}
}

func TestArchiveRunningRefused(t *testing.T) {
	s := newTestStore(t)
	rec := mustCreate(t, s, CreateInput{Name: "busy"})
	if err := s.SetStatus(rec.Ref, StatusRunning, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive(rec.Ref); err == nil {
		t.Fatal("archiving a running task must fail")
	}
}

func TestNotFoundIncludesGuidance(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get("task_0000000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "remote") {
		t.Fatalf("error should hint at remote/other-session tasks: %v", err)
	}
	_, err = s.Get("not-a-ref")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid ref err = %v, want ErrNotFound", err)
	}
}

func TestResolveRefShortRefAndName(t *testing.T) {
	s := newTestStore(t)
	rec := mustCreate(t, s, CreateInput{Name: "build-widgets"})
	short := strings.TrimPrefix(rec.Ref, "task_")

	if got, err := s.Resolve(rec.Ref); err != nil || got.Ref != rec.Ref {
		t.Fatalf("Resolve full ref: %v", err)
	}
	if got, err := s.Resolve(short); err != nil || got.Ref != rec.Ref {
		t.Fatalf("Resolve short ref: %v", err)
	}
	if got, err := s.Resolve("build-widgets"); err != nil || got.Ref != rec.Ref {
		t.Fatalf("Resolve name: %v", err)
	}
	if got, err := s.Resolve("@build-widgets"); err != nil || got.Ref != rec.Ref {
		t.Fatalf("Resolve @name: %v", err)
	}
}

func TestResolveAmbiguousName(t *testing.T) {
	s := newTestStore(t)
	mustCreate(t, s, CreateInput{Name: "dup"})
	mustCreate(t, s, CreateInput{Name: "dup"})
	_, err := s.Resolve("dup")
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("err = %v, want ErrAmbiguous", err)
	}
	if !strings.Contains(err.Error(), "task_") {
		t.Fatalf("ambiguity error should list candidate refs: %v", err)
	}
}

func TestZombieRunningTaskCorrected(t *testing.T) {
	s := newTestStore(t)
	// PID 1 always exists; fabricate an impossible-high pid that cannot exist.
	deadPID := 1 << 30
	rec, err := s.Create(CreateInput{Name: "orphan", OwnerPID: deadPID, Hostname: s.host})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(rec.Ref, StatusRunning, "", ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(rec.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || !got.Zombie {
		t.Fatalf("zombie correction failed: status=%s zombie=%v", got.Status, got.Zombie)
	}
	if !strings.Contains(got.Error, "exited") {
		t.Fatalf("zombie error = %q", got.Error)
	}
	// Live owner stays running.
	live, err := s.Create(CreateInput{Name: "alive", OwnerPID: 1, Hostname: s.host})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetStatus(live.Ref, StatusRunning, "", "")
	got, _ = s.Get(live.Ref)
	if got.Status != StatusRunning {
		t.Fatalf("live owner corrected to %s", got.Status)
	}
	// Remote host is never corrected (cannot verify).
	remote, err := s.Create(CreateInput{Name: "remote", OwnerPID: deadPID, Hostname: "other-host"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetStatus(remote.Ref, StatusRunning, "", "")
	got, _ = s.Get(remote.Ref)
	if got.Status != StatusRunning {
		t.Fatalf("remote owner corrected to %s; must stay running", got.Status)
	}
}

func TestTwoStoreInstancesShareRegistry(t *testing.T) {
	root := t.TempDir()
	// Two instances model two jcode processes on the same project.
	s1, err := NewStore(root, "/proj/a")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := NewStore(root, "/proj/a")
	if err != nil {
		t.Fatal(err)
	}
	rec := mustCreate(t, s1, CreateInput{Name: "shared"})
	if _, err := s2.Get(rec.Ref); err != nil {
		t.Fatalf("second process cannot read: %v", err)
	}
	// Retried delivery across processes with one key → exactly once.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := s1
			if i%2 == 0 {
				store = s2
			}
			_, _ = store.Message(rec.Ref, "s", "user", "once-only", "cross-proc-key")
		}(i)
	}
	wg.Wait()
	got, err := s1.Get(rec.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Timeline) != 1 {
		t.Fatalf("timeline = %d entries, want 1 across processes", len(got.Timeline))
	}
}

func TestTornTrailingLineTolerated(t *testing.T) {
	s := newTestStore(t)
	rec := mustCreate(t, s, CreateInput{Name: "torn"})
	// Simulate a crash mid-append: half a JSON line at the end of the log.
	f, err := os.OpenFile(s.taskPath(rec.Ref), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"id":"torn","type":"mes`); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	got, err := s.Get(rec.Ref)
	if err != nil {
		t.Fatalf("torn line broke reads: %v", err)
	}
	if got.Name != "torn" {
		t.Fatalf("fold corrupted: %+v", got)
	}
}

func TestSetStatusLifecycle(t *testing.T) {
	s := newTestStore(t)
	rec := mustCreate(t, s, CreateInput{Name: "lifecycle", Kind: KindSubagent})
	_ = s.SetStatus(rec.Ref, StatusRunning, "", "")
	got, _ := s.Get(rec.Ref)
	if got.Status != StatusRunning {
		t.Fatalf("status = %s", got.Status)
	}
	_ = s.SetStatus(rec.Ref, StatusCompleted, "the result", "")
	got, _ = s.Get(rec.Ref)
	if got.Status != StatusCompleted || got.Output != "the result" || got.EndedAt.IsZero() {
		t.Fatalf("terminal fold wrong: %+v", got)
	}
}

func TestListFilterAndOrder(t *testing.T) {
	s := newTestStore(t)
	a := mustCreate(t, s, CreateInput{Name: "a"})
	mustCreate(t, s, CreateInput{Name: "b"})
	_ = s.SetStatus(a.Ref, StatusRunning, "", "")
	recs, err := s.List(StatusRunning)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Ref != a.Ref {
		t.Fatalf("filter = %+v", recs)
	}
	all, _ := s.List("")
	if len(all) != 2 {
		t.Fatalf("all = %d", len(all))
	}
	if all[0].Ref != a.Ref { // newest activity first: a got a status update after b was created
		t.Fatalf("order wrong: %s before %s", all[0].Ref, all[1].Ref)
	}
}

func TestRegistryLayoutPerProject(t *testing.T) {
	root := t.TempDir()
	s, err := NewStore(root, "/proj/a")
	if err != nil {
		t.Fatal(err)
	}
	rec := mustCreate(t, s, CreateInput{Name: "x"})
	expected := filepath.Join(root, projectDirName("/proj/a"), rec.Ref+".jsonl")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected log at %s: %v", expected, err)
	}
}

func TestValidateName(t *testing.T) {
	if _, err := ValidateName("   "); err == nil {
		t.Fatal("empty name accepted")
	}
	n, err := ValidateName("  a\nb  ")
	if err != nil || n != "a b" {
		t.Fatalf("ValidateName = %q, %v", n, err)
	}
	long := strings.Repeat("x", 200)
	n, _ = ValidateName(long)
	if len(n) != maxNameLen {
		t.Fatalf("long name not truncated: %d", len(n))
	}
}
