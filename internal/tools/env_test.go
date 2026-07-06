//go:build !windows

package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type localExecResult struct {
	stdout string
	stderr string
	err    error
}

// runLocalExec runs a command through a LocalExecutor in a goroutine and fails
// the test if Exec does not return within wait. This bounds the pre-fix hang
// mode (a surviving grandchild holding the stdout pipe blocks cmd.Wait).
func runLocalExec(t *testing.T, command, dir string, timeout, wait time.Duration) localExecResult {
	t.Helper()
	ex := NewLocalExecutor(runtime.GOOS + "/" + runtime.GOARCH)
	ch := make(chan localExecResult, 1)
	go func() {
		stdout, stderr, err := ex.Exec(context.Background(), command, dir, timeout)
		ch <- localExecResult{stdout, stderr, err}
	}()
	select {
	case res := <-ch:
		return res
	case <-time.After(wait):
		t.Fatalf("Exec(%q, timeout=%v) did not return within %v", command, timeout, wait)
		return localExecResult{}
	}
}

// TestLocalExec_TimeoutKillsProcessGroup verifies that a timeout tears down the
// whole process tree, not just the bash leader: a grandchild (`sleep 600`)
// spawned by the command must be dead shortly after Exec returns.
func TestLocalExec_TimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")

	cmd := fmt.Sprintf("sleep 600 & echo $! > %s; wait", pidFile)
	res := runLocalExec(t, cmd, dir, 300*time.Millisecond, 5*time.Second)
	if res.err == nil || !strings.Contains(res.err.Error(), "timed out") {
		t.Fatalf("expected a timeout error, got %v", res.err)
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("bad grandchild pid %q: %v", data, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return // grandchild is gone
		}
		if time.Now().After(deadline) {
			t.Fatalf("grandchild sleep (pid %d) still alive 2s after the timeout", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestLocalExec_DaemonGrandchildDoesNotHangWait verifies two things at once:
// a backgrounded grandchild holding the stdout pipe must not stall Exec until
// the grandchild exits (WaitDelay cuts the pipe wait), and the resulting
// exec.ErrWaitDelay must be folded into success — `cmd &` is normal usage,
// not a failure.
func TestLocalExec_DaemonGrandchildDoesNotHangWait(t *testing.T) {
	start := time.Now()
	res := runLocalExec(t, "sleep 5 & echo done", t.TempDir(), 10*time.Second, 8*time.Second)
	elapsed := time.Since(start)

	if res.err != nil {
		t.Fatalf("expected success (ErrWaitDelay must fold to nil), got %v", res.err)
	}
	if !strings.Contains(res.stdout, "done") {
		t.Fatalf("stdout = %q, want it to contain %q", res.stdout, "done")
	}
	if elapsed >= 4*time.Second {
		t.Fatalf("Exec took %v; want < 4s (WaitDelay should cut the pipe wait)", elapsed)
	}
}

// TestLocalExec_NormalCommandUnaffected is the regression anchor: a plain
// command is untouched by the process-group/WaitDelay changes.
func TestLocalExec_NormalCommandUnaffected(t *testing.T) {
	res := runLocalExec(t, "echo hi", t.TempDir(), 10*time.Second, 5*time.Second)
	if res.err != nil {
		t.Fatalf("unexpected error: %v", res.err)
	}
	if strings.TrimSpace(res.stdout) != "hi" || res.stderr != "" {
		t.Fatalf("stdout=%q stderr=%q, want %q / empty", res.stdout, res.stderr, "hi")
	}
}

// #16: isSSHConnDead classifies only permanently-dead connections (EOF /
// closed network connection) as fatal; per-command failures stay retryable.
func TestIsSSHConnDead(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain command failure", errors.New("exit status 1"), false},
		{"io.EOF", io.EOF, true},
		{"wrapped io.EOF", fmt.Errorf("ssh session: %w", io.EOF), true},
		{"closed network connection text", errors.New("write tcp 1.2.3.4:22: use of closed network connection"), true},
		{"timeout is not dead", errors.New("command timed out after 30s"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSSHConnDead(tt.err); got != tt.want {
				t.Fatalf("isSSHConnDead(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
