package tools

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// newBgManagerForTest returns a BackgroundManager over a local Env with HOME
// pointed at a temp dir, so task logs land under an isolated ~/.jcode.
func newBgManagerForTest(t *testing.T) *BackgroundManager {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return NewBackgroundManager(NewEnv(t.TempDir(), runtime.GOOS+"/"+runtime.GOARCH))
}

// waitBgTask polls until the task leaves the running state.
func waitBgTask(t *testing.T, bm *BackgroundManager, id string) *BgTask {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		task := bm.GetTask(id)
		if task == nil {
			t.Fatalf("task %s not found", id)
		}
		if task.Status != BgStatusRunning {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s still running after 10s", id)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestBackground_HeadTailTruncation(t *testing.T) {
	bm := newBgManagerForTest(t)
	id := bm.Run(context.Background(), "echo HEAD_MARK; seq 1 5000; echo TAIL_ERR")
	task := waitBgTask(t, bm, id)

	if task.Status != BgStatusDone {
		t.Fatalf("status = %s, want done (output: %.200q)", task.Status, task.Output)
	}
	if !strings.Contains(task.Output, "HEAD_MARK") {
		t.Fatalf("head lost: %.200q", task.Output)
	}
	if !strings.Contains(task.Output, "TAIL_ERR") {
		t.Fatalf("tail lost (head-only truncation?): ...%q", task.Output[len(task.Output)-100:])
	}
	if !strings.Contains(task.Output, "output truncated") {
		t.Fatalf("truncation marker with drop count missing: %.200q", task.Output)
	}
	if len(task.Output) >= 5000 {
		t.Fatalf("in-memory output length = %d, want < 5000", len(task.Output))
	}
}

// TestBackground_TaskLogAlwaysWritten: the full-output spill must work without
// SetStorage ever being called (it has no production call sites).
func TestBackground_TaskLogAlwaysWritten(t *testing.T) {
	bm := newBgManagerForTest(t)
	id := bm.Run(context.Background(), "echo HEAD_MARK; seq 1 5000; echo TAIL_ERR")
	task := waitBgTask(t, bm, id)

	if task.LogPath == "" {
		t.Fatal("LogPath is empty; want a spill file even without SetStorage")
	}
	data, err := os.ReadFile(task.LogPath)
	if err != nil {
		t.Fatalf("read task log: %v", err)
	}
	full := string(data)
	for _, want := range []string{"HEAD_MARK", "\n3000\n", "TAIL_ERR"} {
		if !strings.Contains(full, want) {
			t.Fatalf("task log missing %q — not the full output (len=%d)", want, len(full))
		}
	}
}

func TestBackground_UniqueLogPerTask(t *testing.T) {
	bm := newBgManagerForTest(t)
	id1 := bm.Run(context.Background(), "echo one")
	id2 := bm.Run(context.Background(), "echo two")
	t1 := waitBgTask(t, bm, id1)
	t2 := waitBgTask(t, bm, id2)

	if t1.LogPath == "" || t2.LogPath == "" {
		t.Fatalf("missing log paths: %q / %q", t1.LogPath, t2.LogPath)
	}
	if t1.LogPath == t2.LogPath {
		t.Fatalf("two tasks share one log file: %s", t1.LogPath)
	}
}

func TestFormatTask_IncludesLogPath(t *testing.T) {
	tests := []struct {
		name    string
		logPath string
		output  string
		want    bool
	}{
		{"no log path", "", "hi", false},
		{"log path with output", "/tmp/x/bg_1_1.log", "hi", true},
		{"log path without output", "/tmp/x/bg_1_1.log", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &BgTask{
				ID:      "bg_1",
				Command: "echo hi",
				Status:  BgStatusDone,
				Started: time.Now(),
				Output:  tc.output,
				LogPath: tc.logPath,
			}
			got := formatTask(task)
			if tc.want && !strings.Contains(got, "Full log: "+tc.logPath) {
				t.Fatalf("formatTask missing 'Full log: %s':\n%s", tc.logPath, got)
			}
			if !tc.want && strings.Contains(got, "Full log") {
				t.Fatalf("formatTask must not mention a log when LogPath is empty:\n%s", got)
			}
		})
	}
}
