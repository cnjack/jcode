package tools

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTaskManager_SyncSubmit(t *testing.T) {
	mgr := NewSubagentTaskManager(5, 10)
	task := &SubagentTask{Name: "sync-test", AgentType: AgentTypeExplore}
	taskID, result, err := mgr.Submit(context.Background(), task, func(ctx context.Context) (string, error) {
		return "hello sync", nil
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if result != "hello sync" {
		t.Fatalf("expected 'hello sync', got %q", result)
	}

	got, err := mgr.Get(taskID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s", got.Status)
	}
}

func TestTaskManager_SyncSubmitError(t *testing.T) {
	mgr := NewSubagentTaskManager(5, 10)
	task := &SubagentTask{Name: "sync-err", AgentType: AgentTypeExplore}
	_, _, err := mgr.Submit(context.Background(), task, func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("oops")
	}, false)
	if err == nil {
		t.Fatal("expected error")
	}
	got, _ := mgr.Get(task.ID)
	if got.Status != TaskStatusFailed {
		t.Fatalf("expected failed, got %s", got.Status)
	}
	if got.Error != "oops" {
		t.Fatalf("expected 'oops', got %q", got.Error)
	}
}

func TestTaskManager_AsyncSubmit(t *testing.T) {
	mgr := NewSubagentTaskManager(5, 10)
	ch := make(chan struct{})
	task := &SubagentTask{Name: "async-test", AgentType: AgentTypeGeneral}
	taskID, result, err := mgr.Submit(context.Background(), task, func(ctx context.Context) (string, error) {
		<-ch
		return "async result", nil
	}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if result != "" {
		t.Fatalf("expected empty result for async, got %q", result)
	}

	// Task should be running.
	got, _ := mgr.Get(taskID)
	if got.Status != TaskStatusRunning {
		t.Fatalf("expected running, got %s", got.Status)
	}

	// Let it finish.
	close(ch)
	time.Sleep(50 * time.Millisecond)

	got, _ = mgr.Get(taskID)
	if got.Status != TaskStatusCompleted {
		t.Fatalf("expected completed, got %s", got.Status)
	}
	if got.Output != "async result" {
		t.Fatalf("expected 'async result', got %q", got.Output)
	}
}

func TestTaskManager_ListByStatus(t *testing.T) {
	mgr := NewSubagentTaskManager(5, 10)
	mgr.Submit(context.Background(), &SubagentTask{Name: "a"}, func(ctx context.Context) (string, error) {
		return "ok", nil
	}, false)
	mgr.Submit(context.Background(), &SubagentTask{Name: "b"}, func(ctx context.Context) (string, error) {
		return "", fmt.Errorf("fail")
	}, false)

	all := mgr.List("")
	if len(all) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(all))
	}
	completed := mgr.List(TaskStatusCompleted)
	if len(completed) != 1 {
		t.Fatalf("expected 1 completed, got %d", len(completed))
	}
	failed := mgr.List(TaskStatusFailed)
	if len(failed) != 1 {
		t.Fatalf("expected 1 failed, got %d", len(failed))
	}
}

func TestTaskManager_StopRunning(t *testing.T) {
	mgr := NewSubagentTaskManager(5, 10)
	ch := make(chan struct{})
	task := &SubagentTask{Name: "stop-me"}
	taskID, _, _ := mgr.Submit(context.Background(), task, func(ctx context.Context) (string, error) {
		<-ctx.Done()
		<-ch
		return "", ctx.Err()
	}, true)

	time.Sleep(20 * time.Millisecond)
	if err := mgr.Stop(taskID); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	close(ch)
	time.Sleep(50 * time.Millisecond)

	got, _ := mgr.Get(taskID)
	if got.Status != TaskStatusStopped {
		t.Fatalf("expected stopped, got %s", got.Status)
	}
}

func TestTaskManager_StopNonRunning(t *testing.T) {
	mgr := NewSubagentTaskManager(5, 10)
	mgr.Submit(context.Background(), &SubagentTask{Name: "done"}, func(ctx context.Context) (string, error) {
		return "ok", nil
	}, false)
	tasks := mgr.List(TaskStatusCompleted)
	if len(tasks) == 0 {
		t.Fatal("expected at least 1 completed task")
	}
	err := mgr.Stop(tasks[0].ID)
	if err == nil {
		t.Fatal("expected error stopping completed task")
	}
}

func TestTaskManager_DrainNotifications(t *testing.T) {
	mgr := NewSubagentTaskManager(5, 10)
	ch := make(chan struct{})
	mgr.Submit(context.Background(), &SubagentTask{Name: "notify"}, func(ctx context.Context) (string, error) {
		<-ch
		return "done", nil
	}, true)
	close(ch)
	time.Sleep(50 * time.Millisecond)

	notifs := mgr.DrainNotifications()
	if len(notifs) == 0 {
		t.Fatal("expected at least 1 notification")
	}
	if notifs[0].Status != TaskStatusCompleted {
		t.Fatalf("expected completed notification, got %s", notifs[0].Status)
	}

	// Second drain should be empty.
	notifs2 := mgr.DrainNotifications()
	if len(notifs2) != 0 {
		t.Fatalf("expected 0 notifications after drain, got %d", len(notifs2))
	}
}

func TestTaskManager_MaxParallelLimit(t *testing.T) {
	mgr := NewSubagentTaskManager(1, 10)
	ch := make(chan struct{})
	// First async task takes the slot.
	mgr.Submit(context.Background(), &SubagentTask{Name: "first"}, func(ctx context.Context) (string, error) {
		<-ch
		return "ok", nil
	}, true)

	time.Sleep(20 * time.Millisecond)

	// Second async should fail due to limit.
	_, _, err := mgr.Submit(context.Background(), &SubagentTask{Name: "second"}, func(ctx context.Context) (string, error) {
		return "ok", nil
	}, true)
	if err == nil {
		t.Fatal("expected max parallel error")
	}

	close(ch)
	time.Sleep(50 * time.Millisecond)
}

func TestTaskManager_ConcurrentSubmit(t *testing.T) {
	mgr := NewSubagentTaskManager(10, 50)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			mgr.Submit(context.Background(), &SubagentTask{Name: fmt.Sprintf("t%d", n)}, func(ctx context.Context) (string, error) {
				return fmt.Sprintf("result-%d", n), nil
			}, false)
		}(i)
	}
	wg.Wait()
	all := mgr.List("")
	if len(all) != 10 {
		t.Fatalf("expected 10 tasks, got %d", len(all))
	}
}
