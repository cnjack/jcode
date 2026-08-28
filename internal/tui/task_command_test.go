package tui

import (
	"strings"
	"testing"

	"github.com/cnjack/jcode/internal/tasks"
	"github.com/cnjack/jcode/internal/tools"
)

func newTaskTestModel(t *testing.T) (Model, *tasks.Store) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store, err := tasks.NewStore(t.TempDir(), "/proj/tui")
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(false, "/proj/tui", nil)
	m.taskHub = tools.NewTaskHub(store, nil, func() string { return "sess-tui" })
	return m, store
}

func TestUpdateTaskSuggestionsScopedToVisibleTasks(t *testing.T) {
	m, store := newTaskTestModel(t)
	if _, err := store.Create(tasks.CreateInput{Name: "build-widgets"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(tasks.CreateInput{Name: "build-gadgets"}); err != nil {
		t.Fatal(err)
	}
	archived, err := store.Create(tasks.CreateInput{Name: "build-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Archive(archived.Ref); err != nil {
		t.Fatal(err)
	}

	m.textarea.SetValue("check on @build")
	m.updateTaskSuggestions()
	if !m.taskSuggestionActive || len(m.taskSuggestions) != 2 {
		t.Fatalf("suggestions = %+v active=%v", m.taskSuggestions, m.taskSuggestionActive)
	}
	for _, s := range m.taskSuggestions {
		if s.name == "build-secret" {
			t.Fatal("archived task leaked into suggestions")
		}
	}

	// Ref-prefix matching and cleanup on non-mention input.
	m.textarea.SetValue("plain text")
	m.updateTaskSuggestions()
	if m.taskSuggestionActive {
		t.Fatal("suggestions active without @token")
	}
}

func TestAcceptTaskSuggestionReplacesToken(t *testing.T) {
	m, store := newTaskTestModel(t)
	rec, err := store.Create(tasks.CreateInput{Name: "build-widgets"})
	if err != nil {
		t.Fatal(err)
	}
	m.textarea.SetValue("status of @build-wid")
	m.updateTaskSuggestions()
	m.taskSuggestionIndex = 0
	m.acceptTaskSuggestion(m.taskSuggestions[0])
	got := m.textarea.Value()
	if got != "status of @"+rec.Ref+" " {
		t.Fatalf("value = %q", got)
	}
	if m.taskSuggestionActive {
		t.Fatal("suggestions not dismissed after accept")
	}
}

func TestExpandMentionsInjectsContext(t *testing.T) {
	m, store := newTaskTestModel(t)
	rec, err := store.Create(tasks.CreateInput{Name: "build-widgets", Description: "widgets plan"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Message(rec.Ref, "s1", "user", "first note", "")

	out, errs := m.expandMentions("what about @build-widgets?")
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if !strings.Contains(out, "widgets plan") || !strings.Contains(out, rec.Ref) || !strings.Contains(out, "first note") {
		t.Fatalf("context block missing data: %s", out)
	}
	if !strings.Contains(out, "untrusted") {
		t.Fatalf("context block missing untrusted label")
	}
}

func TestExpandMentionsUnknownBlocksSubmit(t *testing.T) {
	m, _ := newTaskTestModel(t)
	out, errs := m.expandMentions("ping @no-such-task")
	if len(errs) == 0 {
		t.Fatal("unknown mention must produce an error")
	}
	if strings.Contains(out, "task-context") {
		t.Fatal("no context block should be attached on error")
	}
	if !strings.Contains(errs[0], "no-such-task") {
		t.Fatalf("error should name the mention: %v", errs)
	}
}

func TestExpandMentionsAmbiguousNamesError(t *testing.T) {
	m, store := newTaskTestModel(t)
	if _, err := store.Create(tasks.CreateInput{Name: "dup"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(tasks.CreateInput{Name: "dup"}); err != nil {
		t.Fatal(err)
	}
	_, errs := m.expandMentions("hey @dup")
	if len(errs) == 0 || !strings.Contains(errs[0], "ambiguous") {
		t.Fatalf("ambiguous mention errors = %v", errs)
	}
}

func TestHandleTaskInputCommands(t *testing.T) {
	m, _ := newTaskTestModel(t)

	// create
	model, _ := m.handleTaskInput("/task create audit-auth check the auth flow", nil)
	mm := model.(*Model)
	joined := joinLines(mm)
	if !strings.Contains(joined, "Task created") || !strings.Contains(joined, "task_") {
		t.Fatalf("create output: %s", joined)
	}

	// message + read
	model, _ = mm.handleTaskInput("/task message audit-auth please continue", nil)
	mm = model.(*Model)
	if !strings.Contains(joinLines(mm), "Sent to") {
		t.Fatalf("message output: %s", joinLines(mm))
	}
	model, _ = mm.handleTaskInput("/task read audit-auth", nil)
	mm = model.(*Model)
	joined = joinLines(mm)
	if !strings.Contains(joined, "please continue") {
		t.Fatalf("read should show timeline: %s", joined)
	}

	// stop (not running) and archive
	model, _ = mm.handleTaskInput("/task stop audit-auth", nil)
	mm = model.(*Model)
	if !strings.Contains(joinLines(mm), "not running") {
		t.Fatalf("stop output: %s", joinLines(mm))
	}
	model, _ = mm.handleTaskInput("/task archive audit-auth", nil)
	mm = model.(*Model)
	if !strings.Contains(joinLines(mm), "Archived") {
		t.Fatalf("archive output: %s", joinLines(mm))
	}

	// archived mention now errors
	_, errs := mm.expandMentions("and @audit-auth?")
	if len(errs) == 0 || !strings.Contains(errs[0], "archived") {
		t.Fatalf("archived mention errors = %v", errs)
	}
}

func joinLines(m *Model) string {
	var b strings.Builder
	for _, l := range m.lines {
		b.WriteString(l.text)
		b.WriteString("\n")
	}
	return b.String()
}
