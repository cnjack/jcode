package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cnjack/jcode/internal/tasks"
)

func newTestHub(t *testing.T, project string) *TaskHub {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store, err := tasks.NewStore(t.TempDir(), project)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return NewTaskHub(store, NewSubagentTaskManager(4, 50), func() string { return "sess-test" })
}

// The eino tool interface is exercised via the concrete tool types below.

func TestTaskCreateReadMessageTools(t *testing.T) {
	hub := newTestHub(t, "/proj/tools")

	create := NewTaskCreateTool(hub)
	out, err := create.InvokableRun(context.Background(), `{"name":"audit-db","description":"check db schema"}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(out, "task_") {
		t.Fatalf("create output missing ref: %s", out)
	}
	// Extract the ref from the output.
	refStart := strings.Index(out, "task_")
	ref := out[refStart:]
	if i := strings.IndexAny(ref, " \n"); i > 0 {
		ref = ref[:i]
	}
	if !tasks.ValidateRef(ref) {
		t.Fatalf("bad ref extracted: %q", ref)
	}

	// read resolves by name and shows the record.
	read := NewTaskReadTool(hub)
	out, err = read.InvokableRun(context.Background(), `{"task_ref":"audit-db"}`)
	if err != nil || !strings.Contains(out, "check db schema") {
		t.Fatalf("read by name: %v %s", err, out)
	}

	// message lands in the timeline exactly once per key.
	msg := NewTaskMessageTool(hub)
	args, _ := json.Marshal(map[string]string{"task_ref": ref, "message": "please also check indexes", "idempotency_key": "k1"})
	for i := 0; i < 3; i++ {
		if _, err := msg.InvokableRun(context.Background(), string(args)); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
	}
	out, _ = read.InvokableRun(context.Background(), `{"task_ref":"`+ref+`"}`)
	if strings.Count(out, "please also check indexes") != 1 {
		t.Fatalf("message not exactly-once in timeline:\n%s", out)
	}
}

func TestTaskMessageCompletedTaskError(t *testing.T) {
	hub := newTestHub(t, "/proj/tools")
	rec, err := hub.Store.Create(tasks.CreateInput{Name: "done-task"})
	if err != nil {
		t.Fatal(err)
	}
	_ = hub.Store.SetStatus(rec.Ref, tasks.StatusCompleted, "finished", "")

	msg := NewTaskMessageTool(hub)
	out, err := msg.InvokableRun(context.Background(), `{"task_ref":"done-task","message":"hi"}`)
	if err != nil {
		t.Fatalf("tool must return error text, not error: %v", err)
	}
	if !strings.Contains(out, "already finished") && !strings.Contains(out, "completed") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestTaskListFromRegistry(t *testing.T) {
	hub := newTestHub(t, "/proj/tools")
	_, err := hub.Store.Create(tasks.CreateInput{Name: "alpha", SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	list := NewTaskListTool(hub)
	out, err := list.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "sess-1") {
		t.Fatalf("list output missing record: %s", out)
	}
	filtered, _ := list.InvokableRun(context.Background(), `{"status_filter":"running"}`)
	if strings.Contains(filtered, "alpha") {
		t.Fatalf("filter should exclude created task: %s", filtered)
	}
}

func TestTaskStopSemantics(t *testing.T) {
	hub := newTestHub(t, "/proj/tools")
	stop := NewTaskStopTool(hub)

	// 1. Live task in this process: stop by ref.
	block := make(chan struct{})
	rec, err := hub.Store.Create(tasks.CreateInput{Name: "live", Kind: tasks.KindSubagent})
	if err != nil {
		t.Fatal(err)
	}
	_ = hub.Store.SetStatus(rec.Ref, tasks.StatusRunning, "", "")
	_, _, err = hub.Manager.Submit(context.Background(), &SubagentTask{
		Name: "live", Ref: rec.Ref,
		OnFinish: func(status SubagentTaskStatus, output, errMsg string) {
			_ = hub.Store.SetStatus(rec.Ref, localToStatus(status), output, errMsg)
		},
	}, func(ctx context.Context) (string, error) {
		<-block
		return "", ctx.Err()
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	out, err := stop.InvokableRun(context.Background(), `{"task_id":"`+rec.Ref+`"}`)
	if err != nil || !strings.Contains(out, "stopped") {
		t.Fatalf("stop live by ref: %v %s", err, out)
	}
	close(block)

	// 2. Running in another process → explicit error. PID 1 is alive but is
	// not tracked by this manager.
	foreign, err := hub.Store.Create(tasks.CreateInput{Name: "foreign", Kind: tasks.KindSubagent, OwnerPID: 1, Hostname: hub.Store.Hostname()})
	if err != nil {
		t.Fatal(err)
	}
	_ = hub.Store.SetStatus(foreign.Ref, tasks.StatusRunning, "", "")
	out, _ = stop.InvokableRun(context.Background(), `{"task_id":"`+foreign.Ref+`"}`)
	if !strings.Contains(out, "another session") {
		t.Fatalf("foreign stop should explain ownership: %s", out)
	}

	// 3. Completed → not running error.
	done, _ := hub.Store.Create(tasks.CreateInput{Name: "done"})
	_ = hub.Store.SetStatus(done.Ref, tasks.StatusCompleted, "ok", "")
	out, _ = stop.InvokableRun(context.Background(), `{"task_id":"`+done.Ref+`"}`)
	if !strings.Contains(out, "not running") {
		t.Fatalf("completed stop output: %s", out)
	}

	// 4. Unknown → not found.
	out, _ = stop.InvokableRun(context.Background(), `{"task_id":"task_0123456789abcdef"}`)
	if !strings.Contains(out, "not found") {
		t.Fatalf("unknown stop output: %s", out)
	}
}

func TestTaskGetPrefersRegistry(t *testing.T) {
	hub := newTestHub(t, "/proj/tools")
	rec, err := hub.Store.Create(tasks.CreateInput{Name: "cross-session", SessionID: "old-session"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = hub.Store.Message(rec.Ref, "old-session", "user", "earlier note", "")
	get := NewTaskGetTool(hub)
	out, err := get.InvokableRun(context.Background(), `{"task_id":"`+rec.Ref+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "old-session") || !strings.Contains(out, "earlier note") {
		t.Fatalf("get output missing cross-session data: %s", out)
	}
}

func TestTaskToolsWithoutStore(t *testing.T) {
	hub := NewTaskHub(nil, NewSubagentTaskManager(4, 50), nil)

	// list falls back to the (empty) manager.
	list := NewTaskListTool(hub)
	out, err := list.InvokableRun(context.Background(), `{}`)
	if err != nil || out != "[]" {
		t.Fatalf("legacy list: %v %s", err, out)
	}

	// create/read/message error clearly.
	if out, _ := NewTaskCreateTool(hub).InvokableRun(context.Background(), `{"name":"x"}`); !strings.Contains(out, "not available") {
		t.Fatalf("create without store: %s", out)
	}
	if out, _ := NewTaskReadTool(hub).InvokableRun(context.Background(), `{"task_ref":"x"}`); !strings.Contains(out, "not available") {
		t.Fatalf("read without store: %s", out)
	}
	if out, _ := NewTaskMessageTool(hub).InvokableRun(context.Background(), `{"task_ref":"x","message":"y"}`); !strings.Contains(out, "not available") {
		t.Fatalf("message without store: %s", out)
	}
}

func TestTaskToolsCrossProjectDenied(t *testing.T) {
	root := t.TempDir()
	other, err := tasks.NewStore(root, "/proj/other")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := other.Create(tasks.CreateInput{Name: "theirs"})
	if err != nil {
		t.Fatal(err)
	}
	mine, err := tasks.NewStore(root, "/proj/mine")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewTaskHub(mine, nil, nil)

	read := NewTaskReadTool(hub)
	out, _ := read.InvokableRun(context.Background(), `{"task_ref":"`+rec.Ref+`"}`)
	if !strings.Contains(out, "different project") {
		t.Fatalf("cross-project read must be denied: %s", out)
	}
	msg := NewTaskMessageTool(hub)
	out, _ = msg.InvokableRun(context.Background(), `{"task_ref":"`+rec.Ref+`","message":"hi"}`)
	if !strings.Contains(out, "different project") {
		t.Fatalf("cross-project message must be denied: %s", out)
	}
}

// TestBackgroundSubagentPersistsRegistry covers the subagent background path:
// the durable record is created, transitions to running, and mirrors the
// final result — visible from a brand-new session (store instance).
func TestBackgroundSubagentPersistsRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	project := "/proj/subagent"
	store, err := tasks.NewStore(root, project)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewSubagentTaskManager(4, 50)

	task := &SubagentTask{Name: "bg-probe", AgentType: AgentTypeExplore}
	rec, err := store.Create(tasks.CreateInput{Name: "bg-probe", Kind: tasks.KindSubagent, SessionID: "sess-A"})
	if err != nil {
		t.Fatal(err)
	}
	task.Ref = rec.Ref
	task.OnFinish = func(status SubagentTaskStatus, output, errMsg string) {
		_ = store.SetStatus(rec.Ref, localToStatus(status), output, errMsg)
	}
	_ = store.SetStatus(rec.Ref, tasks.StatusRunning, "", "")

	_, _, err = manager.Submit(context.Background(), task, func(ctx context.Context) (string, error) {
		return "probe result", nil
	}, true)
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, gerr := store.Get(rec.Ref)
		if gerr != nil {
			t.Fatal(gerr)
		}
		if got.Status == tasks.StatusCompleted && got.Output == "probe result" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task never completed in registry: %+v", got)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A second "session" (fresh store over the same root/project) sees it.
	second, err := tasks.NewStore(root, project)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := second.Get(rec.Ref)
	if err != nil {
		t.Fatalf("second session cannot read: %v", err)
	}
	if got2.Status != tasks.StatusCompleted || got2.Output != "probe result" {
		t.Fatalf("second session sees %+v", got2)
	}
}
