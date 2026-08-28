package session

import (
	"fmt"
	"sync"
	"testing"
)

// TestTitleRefinerFiresOnceWithFirstMessage locks the refiner contract: it is
// invoked exactly once, with the first user message, after the truncated
// fallback title is already persisted — and it is released afterwards.
func TestTitleRefinerFiresOnceWithFirstMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const project = "/proj/title"

	rec, err := NewRecorder(project, "prov", "model-x")
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	var calls []string
	rec.SetTitleRefiner(func(first string) { calls = append(calls, first) })

	rec.RecordUser("第一条消息:帮我修复登录问题")
	rec.RecordUser("第二条消息")

	if len(calls) != 1 {
		t.Fatalf("refiner should fire exactly once, got %d", len(calls))
	}
	if calls[0] != "第一条消息:帮我修复登录问题" {
		t.Errorf("refiner got %q", calls[0])
	}

	// The truncated fallback title must already be in the index when the
	// refiner runs (async upgrades can never race an untitled session).
	metas, _ := ListSessions(project)
	if len(metas) != 1 || metas[0].Title == "" {
		t.Fatalf("fallback title missing: %+v", metas)
	}

	// One-shot: the refiner is released after firing so its closure can be GC'd.
	rec.mu.Lock()
	released := rec.titleRefiner == nil
	rec.mu.Unlock()
	if !released {
		t.Error("titleRefiner should be nil after firing")
	}
}

// TestTitleRefinerFiresOnceUnderConcurrency locks the atomic claim: N racing
// first messages must produce exactly one refiner invocation.
func TestTitleRefinerFiresOnceUnderConcurrency(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	rec, _ := NewRecorder("/proj/title-race", "prov", "model-x")
	var mu sync.Mutex
	fires := 0
	rec.SetTitleRefiner(func(string) {
		mu.Lock()
		fires++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rec.RecordUser(fmt.Sprintf("并发首消息 %d", n))
		}(i)
	}
	wg.Wait()

	if fires != 1 {
		t.Fatalf("refiner fired %d times, want exactly 1", fires)
	}
}

// TestSetTitleForOverridesAndPersists covers the async upgrade path.
func TestSetTitleForOverridesAndPersists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const project = "/proj/title2"

	rec, _ := NewRecorder(project, "prov", "model-x")
	rec.RecordUser("some long first message that becomes the fallback title")

	rec.SetTitleFor(rec.UUID(), "修复登录超时")
	metas, _ := ListSessions(project)
	if len(metas) != 1 || metas[0].Title != "修复登录超时" {
		t.Fatalf("SetTitleFor not persisted: %+v", metas)
	}

	// Blank titles are ignored — the previous title survives.
	rec.SetTitleFor(rec.UUID(), "   ")
	metas, _ = ListSessions(project)
	if metas[0].Title != "修复登录超时" {
		t.Errorf("blank SetTitleFor overwrote title: %q", metas[0].Title)
	}
}

// TestSetUserTitleStickyAgainstAutomaticRefines locks the /rename ordering
// contract: once the user renames a session, any later automatic SetTitleFor
// (e.g. an LLM refiner goroutine still in flight) must be dropped — in memory
// and in the index.
func TestSetUserTitleStickyAgainstAutomaticRefines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const project = "/proj/user-title"

	rec, _ := NewRecorder(project, "prov", "model-x")
	rec.RecordUser("first message")

	if err := rec.SetUserTitle(rec.UUID(), "我的手工标题"); err != nil {
		t.Fatalf("SetUserTitle: %v", err)
	}

	// Late automatic refine arrives after the user already renamed.
	rec.SetTitleFor(rec.UUID(), "异步模型标题")

	metas, _ := ListSessions(project)
	if len(metas) != 1 {
		t.Fatalf("want 1 session, got %d", len(metas))
	}
	if metas[0].Title != "我的手工标题" {
		t.Fatalf("automatic refine overwrote user title: %q", metas[0].Title)
	}
	if !metas[0].TitleUserSet {
		t.Fatal("TitleUserSet flag not persisted")
	}

	// A later explicit rename still wins (user overrides user).
	if err := rec.SetUserTitle(rec.UUID(), "第二次改名"); err != nil {
		t.Fatalf("second SetUserTitle: %v", err)
	}
	metas, _ = ListSessions(project)
	if metas[0].Title != "第二次改名" {
		t.Fatalf("second user rename not applied: %q", metas[0].Title)
	}
}

// TestAutomaticTitleDroppedAfterCrossTransportRename covers a web-side rename
// (UpdateSessionMeta with TitleUserSet) racing a TUI-side automatic refiner:
// the index write itself must refuse to clobber the user title, even though
// the writing recorder never saw the rename.
func TestAutomaticTitleDroppedAfterCrossTransportRename(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const project = "/proj/cross-title"

	rec, _ := NewRecorder(project, "prov", "model-x")
	rec.RecordUser("first message")
	id := rec.UUID()

	// Another transport (web PATCH /tasks) renames while the refiner is in flight.
	if _, err := UpdateSessionMeta(id, func(m *SessionMeta) {
		m.Title = "Web 改名"
		m.TitleUserSet = true
	}); err != nil {
		t.Fatalf("UpdateSessionMeta: %v", err)
	}

	// The in-flight automatic result must not land (index-level guard).
	if err := updateIndexTitle(project, id, "迟到的自动标题", false); err != nil {
		t.Fatalf("updateIndexTitle: %v", err)
	}
	metas, _ := ListSessions(project)
	if metas[0].Title != "Web 改名" {
		t.Fatalf("cross-transport user title clobbered: %q", metas[0].Title)
	}
}

// TestSetUserTitleRejectsEmptySession guards the no-conversation feedback:
// /rename before the first message has nothing to persist and must say so
// instead of silently succeeding.
func TestSetUserTitleRejectsEmptySession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rec, _ := NewRecorder("/proj/empty-title", "prov", "model-x")
	if err := rec.SetUserTitle(rec.UUID(), "too early"); err == nil {
		t.Fatal("SetUserTitle on an unstarted session should fail")
	}
}

// TestSetUserTitleRejectsStaleIDAfterResume guards the resume ordering: a save
// bound to the previous session id must not rename the resumed target.
func TestSetUserTitleRejectsStaleIDAfterResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const project = "/proj/stale-user-title"

	recB, _ := NewRecorder(project, "prov", "model-x")
	recB.RecordUser("B 的首条消息")
	uuidB := recB.UUID()
	recB.Close()

	recA, _ := NewRecorder(project, "prov", "model-x")
	recA.RecordUser("A 的首条消息")
	uuidA := recA.UUID()
	recA.SetUUID(uuidB) // /resume swaps the recorder onto session B

	if err := recA.SetUserTitle(uuidA, "A 的改名"); err == nil {
		t.Fatal("stale SetUserTitle should fail")
	}
	metas, _ := ListSessions(project)
	for _, m := range metas {
		if m.UUID == uuidB && m.Title == "A 的改名" {
			t.Fatalf("stale rename landed on session B: %+v", metas)
		}
	}
}

// TestRecorderTitleGetter covers the Title accessor used by the TUI /rename
// editor seeding.
func TestRecorderTitleGetter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rec, _ := NewRecorder("/proj/title-getter", "prov", "model-x")
	if got := rec.Title(); got != "" {
		t.Fatalf("new recorder title = %q, want empty", got)
	}
	rec.RecordUser("the fallback title comes from here")
	if got := rec.Title(); got == "" {
		t.Fatal("title should be set after first message")
	}
}

// TestSetTitleForDropsStaleTitleAfterSetUUID locks the /resume guard: a title
// computed for session A must not land after the recorder was re-pointed at
// session B (the TUI /resume path reuses the live recorder via SetUUID).
func TestSetTitleForDropsStaleTitleAfterSetUUID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const project = "/proj/title3"

	// Session B exists on disk with its own title.
	recB, _ := NewRecorder(project, "prov", "model-x")
	recB.RecordUser("B 的首条消息")
	uuidB := recB.UUID()
	recB.Close()

	// Session A starts on the same (reused) recorder pattern; its refiner is
	// in flight when the user resumes B.
	recA, _ := NewRecorder(project, "prov", "model-x")
	recA.RecordUser("A 的首条消息")
	uuidA := recA.UUID()
	recA.SetUUID(uuidB) // /resume swaps the recorder onto session B

	// The stale async result for A must be dropped, not written to B.
	recA.SetTitleFor(uuidA, "A 的 LLM 标题")

	metas, _ := ListSessions(project)
	for _, m := range metas {
		if m.Title == "A 的 LLM 标题" {
			t.Fatalf("stale title landed on session %s: %+v", m.UUID, metas)
		}
	}
}
