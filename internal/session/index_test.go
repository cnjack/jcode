package session

import (
	"fmt"
	"sync"
	"testing"
)

// TestRecorderIndexingRequiresContent locks the contract the web server's
// todo/goal OnUpdate guard relies on: a recorder that has written nothing is
// NOT listed (so ambient todo/goal updates, which the server now skips while
// HasRecording() is false, can never create a phantom empty session), and the
// session only appears once a real user message has been recorded.
func TestRecorderIndexingRequiresContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const project = "/proj/elves"

	rec, err := NewRecorder(project, "zhipu", "glm-5.2")
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	// A fresh recorder has no file and must not be indexed.
	if rec.HasRecording() {
		t.Fatal("fresh recorder should report HasRecording() == false")
	}
	if metas, _ := ListSessions(project); len(metas) != 0 {
		t.Fatalf("a recorder that wrote nothing must not be indexed; got %d sessions", len(metas))
	}

	// The first user message creates the file and indexes the session.
	rec.RecordUser("hello")
	if !rec.HasRecording() {
		t.Fatal("recorder should report HasRecording() == true after a user message")
	}
	metas, err := ListSessions(project)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(metas) != 1 || metas[0].UUID != rec.UUID() {
		t.Fatalf("expected the session indexed after a user message, got %+v", metas)
	}
	rec.Close()
}

// TestProjectTimestampLifecycle locks the sidebar-ordering contract for the
// per-project "last activity" timestamp (persisted in projects.json next to
// the session index):
//   - creating a session stamps the project with the session's start time;
//   - moving a session's UpdatedAt (a real turn) moves the project forward;
//   - a metadata-only edit (title/pin/…) leaves it untouched;
//   - the timestamp is a monotonic max over parsed INSTANTS — an older write
//     never moves it backwards, even when its string sorts greater (mixed
//     UTC offsets: "+08:00" vs "Z");
//   - deleting a session NEVER rolls the project timestamp back — deleting
//     the newest conversation must not reorder the project list;
//   - the projects file survives unrelated index rewrites (title updates).
func TestProjectTimestampLifecycle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const project = "/proj/timestamps"

	// Legacy install: no projects.json yet → nil map, no error.
	meta, err := ListProjectMeta()
	if err != nil {
		t.Fatalf("ListProjectMeta (legacy): %v", err)
	}
	if meta != nil {
		t.Fatalf("legacy install should yield a nil project map, got %+v", meta)
	}

	// Creating a session stamps the project.
	if err := addToIndex(project, SessionMeta{UUID: "uuid-a", Project: project, StartTime: "2026-07-01T10:00:00+08:00"}); err != nil {
		t.Fatalf("addToIndex: %v", err)
	}
	meta, err = ListProjectMeta()
	if err != nil {
		t.Fatalf("ListProjectMeta: %v", err)
	}
	if got := meta[project].UpdatedAt; got != "2026-07-01T10:00:00+08:00" {
		t.Fatalf("project timestamp after create = %q, want the session start time", got)
	}

	// A real turn (UpdatedAt moves) moves the project forward.
	if _, err := UpdateSessionMeta("uuid-a", func(m *SessionMeta) {
		m.UpdatedAt = "2026-07-02T09:30:00+08:00"
	}); err != nil {
		t.Fatalf("UpdateSessionMeta: %v", err)
	}
	meta, _ = ListProjectMeta()
	if got := meta[project].UpdatedAt; got != "2026-07-02T09:30:00+08:00" {
		t.Fatalf("project timestamp after turn = %q, want the bumped UpdatedAt", got)
	}

	// A metadata-only edit must not touch the project timestamp.
	if _, err := UpdateSessionMeta("uuid-a", func(m *SessionMeta) {
		m.Title = "renamed"
		m.Pinned = true
	}); err != nil {
		t.Fatalf("UpdateSessionMeta(title): %v", err)
	}
	meta, _ = ListProjectMeta()
	if got := meta[project].UpdatedAt; got != "2026-07-02T09:30:00+08:00" {
		t.Fatalf("project timestamp moved on a metadata edit: %q", got)
	}

	// Mixed-offset monotonicity — the case string comparison gets WRONG.
	// Stored: 2026-07-02T09:30:00+08:00 (= 01:30Z). Candidate:
	// 2026-07-02T02:00:00Z (= 02:00Z) is a genuinely NEWER instant, yet its
	// string sorts BELOW the stored one ("02:00Z" < "09:30+08:00"). A
	// lexicographic max would keep the stale value; instant compare must
	// accept it.
	if _, err := UpdateSessionMeta("uuid-a", func(m *SessionMeta) {
		m.UpdatedAt = "2026-07-02T02:00:00Z"
	}); err != nil {
		t.Fatalf("UpdateSessionMeta(utc): %v", err)
	}
	meta, _ = ListProjectMeta()
	if got := meta[project].UpdatedAt; got != "2026-07-02T02:00:00Z" {
		t.Fatalf("project timestamp ignored a newer UTC instant (string-compare bug): %q", got)
	}

	// And the reverse: an older instant whose string sorts greater must NOT
	// win. Stored 02:00Z; candidate "2026-07-02T09:00:00+08:00" = 01:00Z
	// (older) but "09:00+08:00" > "02:00Z" lexicographically.
	if _, err := UpdateSessionMeta("uuid-a", func(m *SessionMeta) {
		m.UpdatedAt = "2026-07-02T09:00:00+08:00"
	}); err != nil {
		t.Fatalf("UpdateSessionMeta(old): %v", err)
	}
	meta, _ = ListProjectMeta()
	if got := meta[project].UpdatedAt; got != "2026-07-02T02:00:00Z" {
		t.Fatalf("project timestamp moved backwards on an older instant: %q", got)
	}

	// An unrelated index rewrite (title update) must leave projects.json intact.
	if err := updateIndexTitle(project, "uuid-a", "new title", false); err != nil {
		t.Fatalf("updateIndexTitle: %v", err)
	}
	meta, _ = ListProjectMeta()
	if got := meta[project].UpdatedAt; got != "2026-07-02T02:00:00Z" {
		t.Fatalf("projects file clobbered by a title update: %q", got)
	}

	// Deleting the session must NOT roll the timestamp back — this is what
	// keeps the sidebar's project ordering stable across deletes.
	found, err := DeleteSessionByUUID("uuid-a")
	if err != nil || !found {
		t.Fatalf("DeleteSessionByUUID: found=%v err=%v", found, err)
	}
	meta, _ = ListProjectMeta()
	if got := meta[project].UpdatedAt; got != "2026-07-02T02:00:00Z" {
		t.Fatalf("project timestamp changed by delete: %q, want it preserved", got)
	}
}

// TestTouchProjectIgnoresEmptyTimestamp guards the addToIndex path for a
// zero-value SessionMeta: an empty StartTime must not create a projects file
// entry with an empty (unparseable) timestamp.
func TestTouchProjectIgnoresEmptyTimestamp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const project = "/proj/empty-ts"
	if err := addToIndex(project, SessionMeta{UUID: "uuid-x", Project: project}); err != nil {
		t.Fatalf("addToIndex: %v", err)
	}
	meta, err := ListProjectMeta()
	if err != nil {
		t.Fatalf("ListProjectMeta: %v", err)
	}
	if got := meta[project].UpdatedAt; got != "" {
		t.Fatalf("empty StartTime must not stamp the project, got %q", got)
	}
}

// TestConcurrentIndexWritesNoLostUpdate guards the indexMu serialization: many
// goroutines adding distinct sessions concurrently must ALL survive in the
// index. Without the lock, the read-modify-rename writers lose updates (and
// corrupt the shared .tmp), so the final index would hold far fewer than N.
// Run with -race to also catch the data race.
func TestConcurrentIndexWritesNoLostUpdate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const project = "/proj/concurrent"
	const n = 50

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = addToIndex(project, SessionMeta{UUID: fmt.Sprintf("uuid-%d", i), Project: project})
		}(i)
	}
	wg.Wait()

	metas, err := ListSessions(project)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(metas) != n {
		t.Fatalf("lost updates: got %d sessions, want %d (concurrent addToIndex without serialization)", len(metas), n)
	}
}
