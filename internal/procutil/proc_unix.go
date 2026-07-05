//go:build !windows

// Package procutil provides small cross-platform process helpers shared by
// every place that shells out (tool executors, hook runner).
package procutil

import (
	"os/exec"
	"syscall"
)

// SetupProcessGroup runs the command's shell in its own process group and makes
// timeout/cancel kill the whole group, so a grandchild the shell spawned (e.g.
// a `sleep`) is torn down instead of surviving as an orphan.
//
// cmd must have been created by exec.CommandContext (Cancel requires it) and
// must not have started yet.
func SetupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid signals the whole process group (leader pid == group id).
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
